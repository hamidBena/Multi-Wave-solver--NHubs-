package algorithms

import (
	"fmt"
	"math/rand"
	"slices"
	"time"

	"NHubs/model"
	"NHubs/solver"
)

type LocalSearch struct {
	instance  *model.Instance
	solution  model.Solution
	load      []int // nodes per group, hub included
	iteration uint64
	maxSteps  uint64
	finished  bool
	random    *rand.Rand
}

func (s *LocalSearch) Name() string { return "local-search" }

type moveKind int

const (
	moveReassign moveKind = iota // move one member to a hub with spare room
	moveSwap                     // exchange the hubs of two members
	movePromote                  // make a member the hub of its group
)

type localMove struct {
	kind    moveKind
	node    int // reassign/swap: the node; promote: the new hub
	other   int // swap: second node; promote: the old hub
	fromHub int // reassign only
	toHub   int // reassign only
}

func NewLocalSearch(instance *model.Instance) (*LocalSearch, error) {
	if instance == nil || len(instance.Nodes) == 0 {
		return nil, fmt.Errorf("local search requires a non-empty instance")
	}
	target := instance.Parameters.HubCount
	if target < 1 || target > len(instance.Nodes) {
		return nil, fmt.Errorf("local search requires between 1 and %d hubs, got %d",
			len(instance.Nodes), target)
	}
	if target*maxNodesPerHub < len(instance.Nodes) {
		return nil, fmt.Errorf(
			"local search cannot assign %d nodes with %d hubs at a capacity of %d nodes per hub",
			len(instance.Nodes), target, maxNodesPerHub)
	}

	solution := model.Solution{
		Hubs:   make([]int, target),
		Assign: make([]int, len(instance.Nodes)),
	}
	for hub := range solution.Hubs {
		solution.Hubs[hub] = hub
		solution.Assign[hub] = hub
	}
	return newLocalSearch(instance, solution, true)
}

func NewLocalSearchWithSeed(instance *model.Instance, seed model.Solution) (*LocalSearch, error) {
	if instance == nil || len(instance.Nodes) == 0 {
		return nil, fmt.Errorf("local search requires a non-empty instance")
	}
	if err := instance.ValidateSolution(seed); err != nil {
		return nil, fmt.Errorf("invalid local-search seed: %w", err)
	}
	return newLocalSearch(instance, seed, false)
}

func newLocalSearch(instance *model.Instance, solution model.Solution, initialize bool) (*LocalSearch, error) {
	if len(solution.Hubs) == 0 {
		return nil, fmt.Errorf("local search requires at least one hub")
	}
	nodeCount := len(instance.Nodes)

	solution.Hubs = slices.Clone(solution.Hubs)
	if initialize || len(solution.Assign) != nodeCount {
		solution.Assign = make([]int, nodeCount)
	} else {
		solution.Assign = slices.Clone(solution.Assign)
	}

	s := &LocalSearch{
		instance: instance,
		solution: solution,
		load:     make([]int, nodeCount),
		maxSteps: 1000,
		random:   rand.New(rand.NewSource(time.Now().UnixNano())),
	}

	for _, hub := range s.solution.Hubs {
		if hub < 0 || hub >= nodeCount {
			return nil, fmt.Errorf("local search has invalid hub %d", hub)
		}
		s.solution.Assign[hub] = hub
		s.load[hub]++
	}

	if initialize {
		if err := s.assignInitialNodes(); err != nil {
			return nil, err
		}
	} else {
		for node, hub := range s.solution.Assign {
			if node != hub {
				if hub < 0 || hub >= nodeCount {
					return nil, fmt.Errorf("local search seed has invalid assignment %d -> %d", node, hub)
				}
				s.load[hub]++
			}
		}
	}

	for _, load := range s.load {
		if load > maxNodesPerHub {
			return nil, fmt.Errorf("local-search seed exceeds the %d-node hub capacity", maxNodesPerHub)
		}
	}
	return s, nil
}

func (s *LocalSearch) Step() (solver.StepResult, error) {
	if s.finished {
		return solver.StepResult{Solution: s.snapshot(), Done: true}, nil
	}

	improved, err := s.improveSolution()
	if err != nil {
		return solver.StepResult{}, err
	}
	s.iteration++
	s.finished = !improved || s.iteration >= s.maxSteps
	return solver.StepResult{Solution: s.snapshot(), Done: s.finished}, nil
}

func (s *LocalSearch) snapshot() model.Solution {
	out := s.solution
	out.Hubs = slices.Clone(s.solution.Hubs)
	out.Assign = slices.Clone(s.solution.Assign)
	return out
}

func (s *LocalSearch) assignInitialNodes() error {
	isHub := make([]bool, len(s.instance.Nodes))
	for _, hub := range s.solution.Hubs {
		isHub[hub] = true
	}

	members := make([]int, 0, len(s.instance.Nodes)-len(s.solution.Hubs))
	for node := range s.instance.Nodes {
		if !isHub[node] {
			members = append(members, node)
		}
	}
	s.random.Shuffle(len(members), func(i, j int) {
		members[i], members[j] = members[j], members[i]
	})

	for _, node := range members {
		open := make([]int, 0, len(s.solution.Hubs))
		for _, hub := range s.solution.Hubs {
			if s.load[hub] < maxNodesPerHub {
				open = append(open, hub)
			}
		}
		if len(open) == 0 {
			return fmt.Errorf("not enough hub capacity: %d hubs x %d cannot hold %d nodes",
				len(s.solution.Hubs), maxNodesPerHub, len(s.instance.Nodes))
		}
		hub := open[s.random.Intn(len(open))]
		s.solution.Assign[node] = hub
		s.load[hub]++
	}
	return nil
}

func (s *LocalSearch) improveSolution() (bool, error) {
	currentCost, err := s.instance.TotalCost(s.solution)
	if err != nil {
		return false, fmt.Errorf("calculate local-search cost: %w", err)
	}

	moves := s.generateMoves()
	s.random.Shuffle(len(moves), func(i, j int) {
		moves[i], moves[j] = moves[j], moves[i]
	})

	for _, move := range moves {
		s.apply(move)
		cost, costErr := s.instance.TotalCost(s.solution)
		if costErr != nil {
			s.apply(inverse(move))
			return false, fmt.Errorf("evaluate local-search move: %w", costErr)
		}
		if cost < currentCost {
			return true, nil
		}
		s.apply(inverse(move))
	}
	return false, nil
}

func (s *LocalSearch) generateMoves() []localMove {
	moves := make([]localMove, 0)
	nodeCount := len(s.instance.Nodes)

	for node := 0; node < nodeCount; node++ {
		hub := s.solution.Assign[node]
		if node == hub {
			continue
		}

		for _, other := range s.solution.Hubs {
			if other != hub && s.load[other] < maxNodesPerHub {
				moves = append(moves, localMove{
					kind: moveReassign, node: node, fromHub: hub, toHub: other,
				})
			}
		}

		moves = append(moves, localMove{kind: movePromote, node: node, other: hub})

		for other := node + 1; other < nodeCount; other++ {
			otherHub := s.solution.Assign[other]
			if other != otherHub && otherHub != hub {
				moves = append(moves, localMove{
					kind: moveSwap, node: node, other: other,
				})
			}
		}
	}
	return moves
}

func (s *LocalSearch) apply(move localMove) {
	switch move.kind {
	case moveReassign:
		s.solution.Assign[move.node] = move.toHub
		s.load[move.fromHub]--
		s.load[move.toHub]++
	case moveSwap:
		a, b := move.node, move.other
		s.solution.Assign[a], s.solution.Assign[b] =
			s.solution.Assign[b], s.solution.Assign[a]
	case movePromote:
		newHub, oldHub := move.node, move.other
		for node, hub := range s.solution.Assign {
			if hub == oldHub {
				s.solution.Assign[node] = newHub
			}
		}
		for index, hub := range s.solution.Hubs {
			if hub == oldHub {
				s.solution.Hubs[index] = newHub
				break
			}
		}
		s.load[newHub] = s.load[oldHub]
		s.load[oldHub] = 0
	}
}

func inverse(move localMove) localMove {
	switch move.kind {
	case moveReassign:
		move.fromHub, move.toHub = move.toHub, move.fromHub
	case movePromote:
		move.node, move.other = move.other, move.node
	}
	return move
}
