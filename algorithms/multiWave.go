package algorithms

import (
	"fmt"
	"math"
	"math/rand"
	"runtime"
	"slices"
	"sort"
	"sync"
	"time"

	"NHubs/model"
	"NHubs/solver"
)

type MultiWaveConfig struct {
	WaveIterations    uint64
	MaxWaves          uint64
	EliteSize         int
	PerturbationSwaps int
	TabuTenure        uint64
	Seed              int64
}

var DefaultMultiWaveConfig = MultiWaveConfig{
	WaveIterations:    1000,
	MaxWaves:          50,
	EliteSize:         5,
	PerturbationSwaps: 20,
	TabuTenure:        30,
	Seed:              time.Now().UnixNano(),
}

type EliteCandidate struct {
	Solution model.Solution
	Cost     float64
}

// Status for logging
type MultiWaveStatus struct {
	Wave            uint64
	WaveIteration   uint64
	TotalIterations uint64
	BestCost        float64
	CurrentCost     float64
	EliteSize       int
	Finished        bool
}

type MultiWave struct {
	instance *model.Instance
	config   MultiWaveConfig
	random   *rand.Rand

	current      model.Solution
	currentCost  float64
	waveBest     model.Solution
	waveBestCost float64
	best         model.Solution
	bestCost     float64
	elite        []EliteCandidate
	tabu         map[moveKey]uint64

	wave            uint64
	waveIteration   uint64
	totalIterations uint64
	finished        bool
}

func (m *MultiWave) Name() string { return "multi-wave" }

func NewMultiWave(instance *model.Instance) (*MultiWave, error) {
	return NewMultiWaveWithConfig(instance, DefaultMultiWaveConfig)
}

func NewMultiWaveWithConfig(instance *model.Instance, config MultiWaveConfig) (*MultiWave, error) {
	if instance == nil || len(instance.Nodes) == 0 {
		return nil, fmt.Errorf("multi-wave solver requires a non-empty instance")
	}
	if err := validateMultiWaveConfig(config); err != nil {
		return nil, err
	}
	target := instance.Parameters.HubCount
	if target < 1 || target > len(instance.Nodes) {
		return nil, fmt.Errorf("multi-wave solver requires between 1 and %d hubs, got %d",
			len(instance.Nodes), target)
	}
	hubSlots := make([]int, target)
	for slot := range hubSlots {
		hubSlots[slot] = slot
	}
	if err := validateCapacity(instance, hubSlots); err != nil {
		return nil, fmt.Errorf("multi-wave solver: %w", err)
	}

	initial, err := randomInitialSolution(instance, target, rand.New(rand.NewSource(config.Seed)))
	if err != nil {
		return nil, err
	}
	return newMultiWave(instance, initial, config)
}

func NewMultiWaveWithSeed(instance *model.Instance, seed model.Solution) (*MultiWave, error) {
	return NewMultiWaveWithSeedAndConfig(instance, seed, DefaultMultiWaveConfig)
}

func NewMultiWaveWithSeedAndConfig(instance *model.Instance, seed model.Solution, config MultiWaveConfig) (*MultiWave, error) {
	if instance == nil || len(instance.Nodes) == 0 {
		return nil, fmt.Errorf("multi-wave solver requires a non-empty instance")
	}
	if err := validateMultiWaveConfig(config); err != nil {
		return nil, err
	}
	if err := instance.ValidateSolution(seed); err != nil {
		return nil, fmt.Errorf("invalid multi-wave seed: %w", err)
	}
	return newMultiWave(instance, seed, config)
}

func validateMultiWaveConfig(config MultiWaveConfig) error {
	if config.WaveIterations == 0 {
		return fmt.Errorf("multi-wave requires at least one iteration per wave")
	}
	if config.MaxWaves == 0 {
		return fmt.Errorf("multi-wave requires at least one wave")
	}
	if config.EliteSize < 1 {
		return fmt.Errorf("multi-wave requires an elite size of at least one")
	}
	if config.PerturbationSwaps < 1 {
		return fmt.Errorf("multi-wave requires at least one perturbation swap")
	}
	return nil
}

func newMultiWave(instance *model.Instance, seed model.Solution, config MultiWaveConfig) (*MultiWave, error) {
	random := rand.New(rand.NewSource(config.Seed))
	current := cloneSolution(seed)
	cost, err := instance.TotalCost(current)
	if err != nil {
		return nil, fmt.Errorf("calculate initial multi-wave cost: %w", err)
	}
	m := &MultiWave{
		instance:     instance,
		config:       config,
		random:       random,
		current:      current,
		currentCost:  cost,
		waveBest:     cloneSolution(current),
		waveBestCost: cost,
		best:         cloneSolution(current),
		bestCost:     cost,
		wave:         1,
		tabu:         make(map[moveKey]uint64),
	}
	m.addElite(current, cost)
	return m, nil
}

func (m *MultiWave) Step() (solver.StepResult, error) {
	if m.finished {
		return solver.StepResult{Solution: cloneSolution(m.best), Done: true}, nil
	}

	// 
	move, candidateCost, found, err := m.bestMove()
	if err != nil {
		return solver.StepResult{}, err
	}
	if found {
		m.recordTabu(move)
		m.applyTo(&m.current, move)
		m.currentCost = candidateCost
	}

	// f(x^c) > f(x*)
	if m.currentCost < m.waveBestCost {
		m.waveBest = cloneSolution(m.current)
		m.waveBestCost = m.currentCost
	}

	if m.currentCost < m.bestCost {
		m.best = cloneSolution(m.current)
		m.bestCost = m.currentCost
	}

	m.waveIteration++
	m.totalIterations++
	if m.waveIteration >= m.config.WaveIterations {
		if err := m.finishWave(); err != nil {
			return solver.StepResult{}, err
		}
	}
	return solver.StepResult{Solution: cloneSolution(m.best), Done: m.finished}, nil
}

func (m *MultiWave) Status() MultiWaveStatus {
	return MultiWaveStatus{
		Wave:            m.wave,
		WaveIteration:   m.waveIteration,
		TotalIterations: m.totalIterations,
		BestCost:        m.bestCost,
		CurrentCost:     m.currentCost,
		EliteSize:       len(m.elite),
		Finished:        m.finished,
	}
}

func (m *MultiWave) Progress() solver.Progress {
	status := m.Status()
	return solver.Progress{
		Wave:          status.Wave,
		WaveIteration: status.WaveIteration,
		Cost:          status.BestCost,
		Available:     true,
	}
}

func (m *MultiWave) finishWave() error {
	m.addElite(m.waveBest, m.waveBestCost)
	if m.wave >= m.config.MaxWaves {
		m.finished = true
		return nil
	}

	m.wave++
	m.waveIteration = 0
	base := m.elite[m.random.Intn(len(m.elite))].Solution //random elite selection
	m.current = m.perturb(base)
	cost, err := m.instance.TotalCost(m.current)
	if err != nil {
		return fmt.Errorf("calculate next multi-wave starting cost: %w", err)
	}
	m.currentCost = cost
	m.waveBest = cloneSolution(m.current)
	m.waveBestCost = m.currentCost
	m.tabu = make(map[moveKey]uint64)
	return nil
}

type moveKey struct {
	a, b int
}

type mwMove struct {
	relocate bool
	a, b     int
}

func (m *MultiWave) applyTo(s *model.Solution, move mwMove) {
	if !move.relocate {
		s.Assign[move.a], s.Assign[move.b] = s.Assign[move.b], s.Assign[move.a] // swap
		return
	}
	for node, hub := range s.Assign {
		if hub == move.a {
			s.Assign[node] = move.b
		}
	}
	for index, hub := range s.Hubs {
		if hub == move.a {
			s.Hubs[index] = move.b
		}
	}
}

func (m *MultiWave) isTabu(move mwMove) bool {
	if move.relocate {
		return m.tabu[moveKey{move.b, move.b}] > m.totalIterations
	}
	hubA, hubB := m.current.Assign[move.a], m.current.Assign[move.b]
	return m.tabu[moveKey{move.a, hubB}] > m.totalIterations ||
		m.tabu[moveKey{move.b, hubA}] > m.totalIterations
}

func (m *MultiWave) recordTabu(move mwMove) {
	until := m.totalIterations + m.config.TabuTenure
	if move.relocate {
		m.tabu[moveKey{move.a, move.a}] = until
		return
	}
	m.tabu[moveKey{move.a, m.current.Assign[move.a]}] = until
	m.tabu[moveKey{move.b, m.current.Assign[move.b]}] = until
}

func (m *MultiWave) bestMove() (mwMove, float64, bool, error) {
	n := len(m.current.Assign)
	moves := make([]mwMove, 0)
	for a := 0; a < n; a++ {
		if m.current.Assign[a] == a {
			continue
		}
		moves = append(moves, mwMove{relocate: true, a: m.current.Assign[a], b: a})
		for b := a + 1; b < n; b++ {
			if m.current.Assign[b] == b || m.current.Assign[a] == m.current.Assign[b] {
				continue
			}
			moves = append(moves, mwMove{a: a, b: b})
		}
	}
	if len(moves) == 0 {
		return mwMove{}, 0, false, nil
	}

	workers := runtime.GOMAXPROCS(0)
	if workers > len(moves) {
		workers = len(moves)
	}
	type result struct {
		move  mwMove
		cost  float64
		found bool
		err   error
	}
	results := make(chan result, workers)
	var wait sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		start := len(moves) * worker / workers
		end := len(moves) * (worker + 1) / workers
		wait.Add(1)
		go func(batch []mwMove) {
			defer wait.Done()
			local := result{cost: math.Inf(1)}
			for _, move := range batch {
				candidate := cloneSolution(m.current)
				affected := m.affectedNodes(move)
				oldCost := m.affectedCost(&m.current, affected)
				m.applyTo(&candidate, move)
				newCost := m.affectedCost(&candidate, affected)
				cost := m.currentCost - oldCost + newCost
				if m.isTabu(move) && cost >= m.bestCost {
					continue
				}
				if !local.found || cost < local.cost {
					local = result{move: move, cost: cost, found: true}
				}
			}
			results <- local
		}(moves[start:end])
	}
	wait.Wait()
	close(results)

	best := result{cost: math.Inf(1)}
	for candidate := range results {
		if candidate.err != nil {
			return mwMove{}, 0, false, candidate.err
		}
		if candidate.found && (!best.found || candidate.cost < best.cost) {
			best = candidate
		}
	}
	return best.move, best.cost, best.found, nil
}

func (m *MultiWave) affectedNodes(move mwMove) []int {
	if !move.relocate {
		return []int{move.a, move.b}
	}
	nodes := make([]int, 0)
	for node, hub := range m.current.Assign {
		if hub == move.a {
			nodes = append(nodes, node)
		}
	}
	return nodes
}

func (m *MultiWave) affectedCost(solution *model.Solution, nodes []int) float64 {
	n := len(solution.Assign)
	affected := make([]bool, n)

	for _, node := range nodes {
		affected[node] = true
	}

	total := 0.0
	for from := 0; from < n; from++ {
		if !affected[from] {
			continue
		}

		for to := 0; to < n; to++ {
			if from == to {
				continue
			}

			// Count pairs where both nodes are affected only once.
			if affected[to] && to < from {
				continue
			}

			total += m.instance.Cost(*solution, from, to)
		}
	}

	return total
}

func (m *MultiWave) addElite(solution model.Solution, cost float64) {
	for _, candidate := range m.elite {
		if slices.Equal(candidate.Solution.Assign, solution.Assign) {
			return //don't store clones
		}
	}
	m.elite = append(m.elite, EliteCandidate{Solution: cloneSolution(solution), Cost: cost})
	sort.Slice(m.elite, func(a, b int) bool {
		return m.elite[a].Cost < m.elite[b].Cost
	})
	if len(m.elite) > m.config.EliteSize {
		m.elite = m.elite[:m.config.EliteSize]
	}
}

func (m *MultiWave) perturb(source model.Solution) model.Solution {
	out := cloneSolution(source)
	members := make([]int, 0, len(out.Assign))
	for node, hub := range out.Assign {
		if node != hub {
			members = append(members, node)
		}
	}
	for relocation := 0; relocation < 1+m.config.PerturbationSwaps/2 && len(members) > 0; relocation++ {
		node := members[m.random.Intn(len(members))]
		if out.Assign[node] != node {
			m.applyTo(&out, mwMove{relocate: true, a: out.Assign[node], b: node})
			members = members[:0]
			for node, hub := range out.Assign {
				if node != hub {
					members = append(members, node)
				}
			}
		}
	}
	for swap := 0; swap < m.config.PerturbationSwaps; swap++ {
		for attempt := 0; attempt < 20 && len(members) >= 2; attempt++ {
			a := members[m.random.Intn(len(members))]
			b := members[m.random.Intn(len(members))]
			if a != b && out.Assign[a] != out.Assign[b] { //they're not the same and don't share the same hub
				out.Assign[a], out.Assign[b] = out.Assign[b], out.Assign[a]
				break
			}
		}
	}
	return out
}

func randomInitialSolution(instance *model.Instance, hubCount int, random *rand.Rand) (model.Solution, error) {
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
			return model.Solution{}, fmt.Errorf("not enough hub capacity for initial multi-wave solution")
		}
		hub := open[random.Intn(len(open))]
		solution.Assign[node] = hub
		load[hub]++
	}
	return solution, nil
}

func cloneSolution(solution model.Solution) model.Solution {
	solution.Hubs = slices.Clone(solution.Hubs)
	solution.Assign = slices.Clone(solution.Assign)
	return solution
}
