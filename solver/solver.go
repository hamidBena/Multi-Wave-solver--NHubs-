package solver

import "NHubs/model"

type StepResult struct {
	Solution model.Solution
	Done     bool
}

type Progress struct {
	Wave          uint64
	WaveIteration uint64
	Cost          float64
	Available     bool
}

type ProgressProvider interface {
	Progress() Progress
}

type Iterative interface {
	Step() (StepResult, error)
}

type Named interface {
	Name() string
}

type Static struct {
	solution model.Solution
	done     bool
}

func (s *Static) Name() string {
	return "static"
}

func DefaultSolution(instance *model.Instance) model.Solution {
	assignment := make([]int, len(instance.Nodes))
	hubs := []int{}
	if len(instance.Nodes) > 0 {
		hubs = append(hubs, 0)
		for node := range assignment {
			assignment[node] = hubs[0]
		}
	}
	return model.Solution{Hubs: hubs, Assign: assignment}
}

func NewStatic(solution model.Solution) *Static {
	return &Static{solution: solution}
}

func (s *Static) Step() (StepResult, error) {
	if s.done {
		return StepResult{Solution: s.solution, Done: true}, nil
	}
	s.done = true
	return StepResult{Solution: s.solution, Done: true}, nil
}
