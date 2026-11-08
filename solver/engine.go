package solver

import (
	"fmt"
	"sync"

	"NHubs/model"
)

type Snapshot struct {
	Iteration uint64
	Solution  model.Solution
	Finished  bool
}

type Engine struct {
	mu                 sync.RWMutex
	solver             Iterative
	snapshot           Snapshot
	iteration          uint64
	visualizationEvery uint64
}

func NewEngine(solver Iterative, initial model.Solution, visualizationEvery uint64) (*Engine, error) {
	if solver == nil {
		return nil, fmt.Errorf("solver is nil")
	}
	if visualizationEvery == 0 {
		visualizationEvery = 1
	}
	return &Engine{
		solver:             solver,
		snapshot:           Snapshot{Solution: cloneSolution(initial)},
		visualizationEvery: visualizationEvery,
	}, nil
}

func (e *Engine) Step() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.snapshot.Finished {
		return nil
	}
	result, err := e.solver.Step()
	if err != nil {
		return err
	}
	e.iteration++
	e.publishLocked(result)
	return nil
}

func (e *Engine) Run(iterations uint64) error {
	for index := uint64(0); index < iterations; index++ {
		e.mu.Lock()
		if e.snapshot.Finished {
			e.mu.Unlock()
			return nil
		}
		result, err := e.solver.Step()
		if err != nil {
			e.mu.Unlock()
			return err
		}
		e.iteration++
		if result.Done || e.iteration%e.visualizationEvery == 0 {
			e.publishLocked(result)
		}
		e.mu.Unlock()
	}
	return nil
}

func (e *Engine) Replace(solver Iterative, initial model.Solution) error {
	if solver == nil {
		return fmt.Errorf("solver is nil")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.solver = solver
	e.iteration = 0
	e.snapshot = Snapshot{Solution: cloneSolution(initial)}
	return nil
}

func (e *Engine) publishLocked(result StepResult) {
	e.snapshot.Iteration = e.iteration
	e.snapshot.Solution = cloneSolution(result.Solution)
	e.snapshot.Finished = result.Done
}

func (e *Engine) Snapshot() Snapshot {
	e.mu.RLock()
	defer e.mu.RUnlock()
	snapshot := e.snapshot
	snapshot.Solution = cloneSolution(snapshot.Solution)
	return snapshot
}

func (e *Engine) Finished() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.snapshot.Finished
}

func (e *Engine) SolverName() string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if named, ok := e.solver.(Named); ok {
		return named.Name()
	}
	return "unnamed"
}

func (e *Engine) Restore(saved Snapshot) {
	e.RestoreWithSolver(NewStatic(saved.Solution), saved)
}

func (e *Engine) RestoreWithSolver(solver Iterative, saved Snapshot) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.solver = solver
	e.iteration = saved.Iteration
	e.snapshot = Snapshot{
		Iteration: saved.Iteration,
		Solution:  cloneSolution(saved.Solution),
		Finished:  saved.Finished,
	}
}

func cloneSolution(solution model.Solution) model.Solution {
	solution.Hubs = append([]int(nil), solution.Hubs...)
	solution.Assign = append([]int(nil), solution.Assign...)
	return solution
}
