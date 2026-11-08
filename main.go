package main

import (
	"log"
	"net/http"
	"os"

	"NHubs/algorithms"
	"NHubs/model"
	"NHubs/renderer"
	"NHubs/solver"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/ncruces/zenity"
	_ "net/http/pprof"
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

	multiWave, err := algorithms.NewMultiWave(instance)
	if err != nil {
		log.Fatal(err)
	}
	/*localSearch, err := algorithms.NewLocalSearch(instance)
	if err != nil {
		log.Fatal(err)
	}*/

	engine, err := solver.NewEngine(multiWave, solution, 1)
	if err != nil {
		log.Fatal(err)
	}

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
