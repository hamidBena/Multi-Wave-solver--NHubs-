package main

import (
	"log"
	"net/http"
	"os"

	"NHubs/algorithms"
	"NHubs/model"
	"NHubs/renderer"
	"NHubs/solver"

	_ "net/http/pprof"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/ncruces/zenity"
)

func main() {

	go func() {
		if err := http.ListenAndServe("127.0.0.1:6060", nil); err != nil {
			log.Printf("pprof server stopped: %v", err)
		}
	}()

	path, err := zenity.SelectFile(zenity.FileFilters{
		{Name: "JSON files", Patterns: []string{"*.json"}},
	})
	if err != nil {
		log.Fatal(err)
	}
	instance, err := model.Load(path)
	if err != nil {
		log.Fatal(err)
	}
	if os.Getenv("NHUBS_PRINT_INSTANCE") == "1" {
		instance.PrintInstance()
	}
	solution := solver.DefaultSolution(instance)

	/*multiWave, err := algorithms.NewMultiWave(instance)
	if err != nil {
		log.Fatal(err)
	}*/

	/*localSearch, err := algorithms.NewLocalSearch(instance)
	if err != nil {
		log.Fatal(err)
	}*/

	multiWave2, err := algorithms.NewMultiWave2(instance)
	if err != nil {
		log.Fatal(err)
	}

	engine, err := solver.NewEngine(multiWave2, solution, 1)
	if err != nil {
		log.Fatal(err)
	}
	engine.SetDataset(path)
	var loggedRunID string
	var lastLoggedCost float64
	var hasLoggedCost bool
	var runLogPath string
	engine.SetStepLogger(func(step solver.StepLog) error {
		if !step.Progress.Available {
			return nil
		}
		if step.RunID != loggedRunID {
			loggedRunID = step.RunID
			hasLoggedCost = false
			runLogPath = ""
		}
		if hasLoggedCost && step.Progress.Cost == lastLoggedCost {
			return nil
		}
		lastLoggedCost = step.Progress.Cost
		hasLoggedCost = true
		if runLogPath == "" {
			var err error
			runLogPath, err = model.NewMultiWaveLogPath("runs", step.Solver, step.Dataset, step.RunID)
			if err != nil {
				return err
			}
		}
		return model.AppendMultiWaveLog(
			runLogPath,
			step.Solver,
			step.Dataset,
			step.RunID,
			step.Progress.Wave,
			step.Progress.WaveIteration,
			step.Iteration,
			step.Progress.Cost,
			step.Solution,
		)
	})

	ebiten.SetWindowSize(1280, 800)
	ebiten.SetWindowTitle("NHubs viewer")
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)
	gameRenderer, err := renderer.New(instance, engine, path, func(instance *model.Instance, seed *model.Solution) (solver.Iterative, error) {
		if seed != nil {
			return algorithms.NewMultiWaveWithSeed(instance, *seed)
		}
		return algorithms.NewMultiWave(instance)
	})
	if err != nil {
		log.Fatal(err)
	}
	if err := ebiten.RunGame(gameRenderer); err != nil {
		log.Fatal(err)
	}
}
