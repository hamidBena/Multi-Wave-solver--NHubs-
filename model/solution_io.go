package model

import (
	"encoding/json"
	"fmt"
	"os"
)

type SavedSolution struct {
	Iteration uint64  `json:"iteration"`
	Finished  bool    `json:"finished"`
	Hubs      []int   `json:"hubs"`
	Assign    []int   `json:"assign"`
	Cost      float64 `json:"cost,omitempty"`
}

func (s SavedSolution) ToSolution(instance *Instance) (Solution, error) {
	if instance == nil {
		return Solution{}, fmt.Errorf("solution instance is nil")
	}
	solution := Solution{
		Hubs:   append([]int(nil), s.Hubs...),
		Assign: append([]int(nil), s.Assign...),
		Cost:   s.Cost,
	}
	if err := instance.ValidateSolution(solution); err != nil {
		return Solution{}, fmt.Errorf("validate saved solution: %w", err)
	}
	return solution, nil
}

func SaveSolution(path string, solution Solution, iteration uint64, finished bool) error {
	saved := SavedSolution{
		Iteration: iteration,
		Finished:  finished,
		Hubs:      append([]int(nil), solution.Hubs...),
		Assign:    append([]int(nil), solution.Assign...),
		Cost:      solution.Cost,
	}
	data, err := json.MarshalIndent(saved, "", "  ")
	if err != nil {
		return fmt.Errorf("encode solution: %w", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write solution %q: %w", path, err)
	}
	return nil
}

func LoadSolution(path string) (SavedSolution, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return SavedSolution{}, fmt.Errorf("read solution %q: %w", path, err)
	}
	var saved SavedSolution
	if err := json.Unmarshal(data, &saved); err != nil {
		return SavedSolution{}, fmt.Errorf("decode solution %q: %w", path, err)
	}
	if len(saved.Hubs) == 0 || len(saved.Assign) == 0 {
		return SavedSolution{}, fmt.Errorf("validate solution %q: hubs and assignments must not be empty", path)
	}
	for node, hub := range saved.Assign {
		if hub < 0 || hub >= len(saved.Assign) {
			return SavedSolution{}, fmt.Errorf("validate solution %q: node %d has invalid hub %d", path, node, hub)
		}
	}
	for _, hub := range saved.Hubs {
		if hub < 0 || hub >= len(saved.Assign) || saved.Assign[hub] != hub {
			return SavedSolution{}, fmt.Errorf("validate solution %q: invalid hub %d", path, hub)
		}
	}
	return saved, nil
}
