package solver

import (
	"testing"

	"NHubs/model"
)

type countingSolver struct {
	iteration uint64
}

func (s *countingSolver) Step() (StepResult, error) {
	s.iteration++
	return StepResult{
		Solution: model.Solution{Cost: float64(s.iteration)},
		Done:     s.iteration == 250,
	}, nil
}

func TestEnginePublishesAtConfiguredInterval(t *testing.T) {
	engine, err := NewEngine(&countingSolver{}, model.Solution{}, 100)
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.Run(150); err != nil {
		t.Fatal(err)
	}
	if got := engine.Snapshot().Iteration; got != 100 {
		t.Fatalf("published iteration = %d, want 100", got)
	}
	if err := engine.Step(); err != nil {
		t.Fatal(err)
	}
	if got := engine.Snapshot().Iteration; got != 151 {
		t.Fatalf("step iteration = %d, want 151", got)
	}
}
