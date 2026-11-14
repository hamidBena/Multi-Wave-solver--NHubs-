package algorithms

import (
	"fmt"
	"math"
	"math/rand"
	"slices"
	"sort"
	"time"

	"NHubs/model"
	"NHubs/solver"
)

// MultiWave2Config encapsulates strictly the parameters defined in Glover's paper.
type MultiWave2Config struct {
	MaxWaves uint    // MaxWave limit
	MaxPass  uint    // MaxPass limit
	Seed     int64   // Random seed for starting solution / probabilistic choices
	Lambda   float64 // Threshold parameter λ for candidate list filtering
	P        float64 // Power parameter p for probabilistic weights (w_i = Evaluation(m_i)^p)
	KStar    int     // Total number of horizontal intervention points scheduled per wave (k*)[cite: 16]
	D        int     // Number of moves dropped per horizontal step (d_k)[cite: 13, 17]
	Ne       int     // Estimated final size of n(AMR) when reaching a boundary solution[cite: 16]
}

var DefaultMultiWave2Config = MultiWave2Config{
	MaxWaves: 10000,
	MaxPass:  10000,
	Seed:     time.Now().UnixNano(),
	Lambda:   0.8,
	P:        1.0,
	KStar:    3,
	D:        1,
	Ne:       20,
}

type MultiWave2Status struct {
	WaveNumber      uint64
	PassNumber      uint64
	TotalIterations uint64
	BestCost        float64
	CurrentCost     float64
	Finished        bool
}

type MultiWave2 struct {
	instance *model.Instance
	config   MultiWave2Config
	random   *rand.Rand

	// Core State Variables from Glover's Paper
	current     model.Solution // x^c: Current solution[cite: 3]
	currentCost float64
	best        model.Solution // x*: Best solution found[cite: 3]
	bestCost    float64
	amr         []mw2Move // AMR: Active Move Record vector (m_1, m_2, ..., m_r)[cite: 9]
	vkList      []int     // v_k: Target n(AMR) intervention thresholds[cite: 13, 16]
	kIndex      int       // k: Current intervention index[cite: 12, 13]

	pass            uint64
	wave            uint64
	totalIterations uint64
	finished        bool
}

func (m *MultiWave2) Name() string { return "multiwave2" }

type mw2Move struct {
	a, b       int
	evaluation float64
}

func NewMultiWave2(instance *model.Instance) (*MultiWave2, error) {
	return NewMultiWave2WithConfig(instance, DefaultMultiWave2Config)
}

func NewMultiWave2WithConfig(instance *model.Instance, config MultiWave2Config) (*MultiWave2, error) {
	if instance == nil || len(instance.Nodes) == 0 {
		return nil, fmt.Errorf("multiwave2 solver requires a non-empty instance")
	}
	target := instance.Parameters.HubCount
	initial, err := randomInitialSolution2(instance, target, rand.New(rand.NewSource(config.Seed)))
	if err != nil {
		return nil, err
	}
	return newMultiWave2(instance, initial, config)
}

func newMultiWave2(instance *model.Instance, seed model.Solution, config MultiWave2Config) (*MultiWave2, error) {
	random := rand.New(rand.NewSource(config.Seed))
	current := cloneSolution2(seed)
	cost, err := instance.TotalCost(current)
	if err != nil {
		return nil, fmt.Errorf("calculate initial cost: %w", err)
	}

	// Initialize intervention thresholds v_k based on equal spacing (Algorithm 2 / Section 6.1)
	vkList := make([]int, config.KStar)
	if config.Ne > 0 && config.KStar > 0 {
		for k := 1; k <= config.KStar; k++ {
			vkList[k-1] = (k * config.Ne) / (config.KStar + 1)
		}
	}

	m := &MultiWave2{
		instance:    instance,
		config:      config,
		random:      random,
		current:     current,
		currentCost: cost,
		best:        cloneSolution2(current),
		bestCost:    cost,
		amr:         make([]mw2Move, 0),
		vkList:      vkList,
		kIndex:      1,
		pass:        1,
		wave:        1,
	}
	return m, nil
}

// Step performs a single wave/phase progression according to Algorithm 2
func (m *MultiWave2) Step() (solver.StepResult, error) {
	if m.finished {
		return solver.StepResult{Solution: cloneSolution2(m.best), Done: true}, nil
	}

	// 1. Check if an intervention point has been reached: n(AMR) == v_k (Section 5.1.1)
	if m.kIndex <= len(m.vkList) && len(m.amr) == m.vkList[m.kIndex-1] {
		if err := m.horizontalPhase(); err != nil {
			return solver.StepResult{}, err
		}
		m.kIndex++
	} else {
		// 2. Otherwise, execute a Vertical Phase (Algorithm 4)
		moved, found, err := m.verticalPhase()
		if err != nil {
			return solver.StepResult{}, err
		}
		if !found {
			// Boundary solution reached (local optimum) -> End-Wave Analysis
			if err := m.finishWave(); err != nil {
				return solver.StepResult{}, err
			}
			return solver.StepResult{Solution: cloneSolution2(m.best), Done: m.finished}, nil
		}
		m.amr = append(m.amr, moved)
	}

	m.totalIterations++
	return solver.StepResult{Solution: cloneSolution2(m.best), Done: m.finished}, nil
}

// Vertical Phase (Algorithm 4): Selects a forward move from CL and executes it
func (m *MultiWave2) verticalPhase() (mw2Move, bool, error) {
	cl, err := m.buildCandidateList()
	if err != nil || len(cl) == 0 {
		return mw2Move{}, false, err
	}

	// Select move from CL using Probabilistic Tabu Search (P-TS) (Section 2.1)
	selectedMove := m.selectProbabilisticMove(cl)
	m.applyMove(&m.current, selectedMove)
	cost, err := m.instance.TotalCost(m.current)
	if err != nil {
		return mw2Move{}, false, err
	}
	m.currentCost = cost

	if m.currentCost < m.bestCost {
		m.best = cloneSolution2(m.current)
		m.bestCost = m.currentCost
	}

	return selectedMove, true, nil
}

// Horizontal Phase (Algorithm 3): Drops d moves from AMR and adds d new moves from CL
func (m *MultiWave2) horizontalPhase() error {
	d := m.config.D
	h := m.config.KStar // Using h steps per intervention
	if d <= 0 {
		d = 1
	}

	for step := 0; step < h; step++ {
		// Drop d oldest moves from the front of AMR (Section 5.1.2)
		for j := 0; j < d && len(m.amr) > 0; j++ {
			reverseMove := m.amr[0]
			m.amr = m.amr[1:] // Remove from AMR, making it available to CL
			m.applyReverseMove(&m.current, reverseMove)
		}

		// Add d new moves from CL
		for j := 0; j < d; j++ {
			cl, err := m.buildCandidateList()
			if err != nil || len(cl) == 0 {
				break
			}
			selectedMove := m.selectProbabilisticMove(cl)
			m.applyMove(&m.current, selectedMove)
			m.amr = append(m.amr, selectedMove) // Add to new last position of AMR
		}
	}

	cost, err := m.instance.TotalCost(m.current)
	if err != nil {
		return err
	}
	m.currentCost = cost
	if m.currentCost < m.bestCost {
		m.best = cloneSolution2(m.current)
		m.bestCost = m.currentCost
	}
	return nil
}

// Candidate List Generation using formula (2.2) from Glover's paper
func (m *MultiWave2) buildCandidateList() ([]mw2Move, error) {
	n := len(m.current.Assign)
	allMoves := make([]mw2Move, 0)

	// Generate neighbor moves M(x)
	for a := 0; a < n; a++ {
		if m.current.Assign[a] == a {
			continue
		}
		for b := a + 1; b < n; b++ {
			if m.current.Assign[b] == b || m.current.Assign[a] == m.current.Assign[b] {
				continue
			}
			candidate := cloneSolution2(m.current)
			candidate.Assign[a], candidate.Assign[b] = candidate.Assign[b], candidate.Assign[a]
			cost, err := m.instance.TotalCost(candidate)
			if err != nil {
				continue
			}
			eval := m.currentCost - cost // Maximization objective change (improving > 0)
			if eval > 0 {                // Restrict to improving moves for iterated improvement
				allMoves = append(allMoves, mw2Move{a: a, b: b, evaluation: eval})
			}
		}
	}

	if len(allMoves) == 0 {
		return nil, nil
	}

	// Sort by evaluation descending: Evaluation(m_1) >= Evaluation(m_2) >= ... >= Evaluation(m_s)[cite: 5]
	sort.Slice(allMoves, func(i, j int) bool {
		return allMoves[i].evaluation > allMoves[j].evaluation
	})

	//minE := allMoves[len(allMoves)-1].evaluation
	maxE := allMoves[0].evaluation
	meanE := 0.0
	for _, mv := range allMoves {
		meanE += mv.evaluation
	}
	meanE /= float64(len(allMoves))

	// Threshold T based on formula (2.2): T = MeanE + λ * (MaxE - MeanE)[cite: 5]
	threshold := meanE + m.config.Lambda*(maxE-meanE)

	cl := make([]mw2Move, 0)
	for _, mv := range allMoves {
		if mv.evaluation >= threshold {
			cl = append(cl, mv)
		}
	}
	if len(cl) == 0 {
		cl = append(cl, allMoves[0]) // Ensure CL is never empty if moves exist
	}
	return cl, nil
}

// Probabilistic Tabu Search (P-TS) selection using weights w_i = Evaluation(m_i)^p (Section 2.1)
func (m *MultiWave2) selectProbabilisticMove(cl []mw2Move) mw2Move {
	if len(cl) == 1 {
		return cl[0]
	}

	weights := make([]float64, len(cl))
	sumWeights := 0.0
	for i, mv := range cl {
		w := math.Pow(math.Max(mv.evaluation, 1e-5), m.config.P)
		weights[i] = w
		sumWeights += w
	}

	r := m.random.Float64() * sumWeights
	cumulative := 0.0
	for i, w := range weights {
		cumulative += w
		if r <= cumulative {
			return cl[i]
		}
	}
	return cl[0]
}

func (m *MultiWave2) finishWave() error {
	if m.wave >= uint64(m.config.MaxWaves) {
		m.finished = true
		return nil
	}
	m.wave++
	m.amr = m.amr[:0]
	m.kIndex = 1
	return nil
}

func (m *MultiWave2) applyMove(s *model.Solution, move mw2Move) {
	s.Assign[move.a], s.Assign[move.b] = s.Assign[move.b], s.Assign[move.a]
}

func (m *MultiWave2) applyReverseMove(s *model.Solution, move mw2Move) {
	// Reversing a swap is executing the swap again
	s.Assign[move.a], s.Assign[move.b] = s.Assign[move.b], s.Assign[move.a]
}

func (m *MultiWave2) Status() MultiWave2Status {
	return MultiWave2Status{
		WaveNumber:      m.wave,
		PassNumber:      m.pass,
		TotalIterations: m.totalIterations,
		BestCost:        m.bestCost,
		CurrentCost:     m.currentCost,
		Finished:        m.finished,
	}
}

func (m *MultiWave2) Progress() solver.Progress {
	status := m.Status()
	return solver.Progress{
		Wave:          status.WaveNumber,
		WaveIteration: status.TotalIterations,
		Cost:          status.BestCost,
		Available:     true,
	}
}

func randomInitialSolution2(instance *model.Instance, hubCount int, random *rand.Rand) (model.Solution, error) {
	solution := model.Solution{
		Hubs:   make([]int, hubCount),
		Assign: make([]int, len(instance.Nodes)),
	}
	nodes := make([]int, len(instance.Nodes))
	for node := range nodes {
		nodes[node] = node
	}
	random.Shuffle(len(nodes), func(i, j int) {
		nodes[i], nodes[j] = nodes[j], nodes[i]
	})
	for index := range solution.Hubs {
		hub := nodes[index]
		solution.Hubs[index] = hub
		solution.Assign[hub] = hub
	}
	members := make([]int, 0, len(instance.Nodes)-hubCount)
	members = append(members, nodes[hubCount:]...)
	load := make([]int, len(instance.Nodes))
	for _, hub := range solution.Hubs {
		load[hub] = 1
	}
	for _, node := range members {
		open := make([]int, 0, hubCount)
		for _, hub := range solution.Hubs {
			if load[hub] < hubCapacity(instance, solution.Hubs, hub) {
				open = append(open, hub)
			}
		}
		if len(open) == 0 {
			return model.Solution{}, fmt.Errorf("not enough hub capacity")
		}
		hub := open[random.Intn(len(open))]
		solution.Assign[node] = hub
		load[hub]++
	}
	return solution, nil
}

func cloneSolution2(solution model.Solution) model.Solution {
	solution.Hubs = slices.Clone(solution.Hubs)
	solution.Assign = slices.Clone(solution.Assign)
	return solution
}
