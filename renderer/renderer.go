package renderer

import (
	"fmt"
	"image/color"
	"math"
	"time"

	"NHubs/model"
	"NHubs/solver"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/hajimehoshi/ebiten/v2/text"
	"github.com/hajimehoshi/ebiten/v2/vector"
	"github.com/ncruces/zenity"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
)

const (
	defaultIterationsPerFrame = 10
	maxIterationsPerFrame     = 5000
	targetFPS                 = 30
	renderFPSLimit            = 60
	frameTimeSmoothing        = 0.2
	solverBudgetFraction      = 0.6
	backgroundColor           = uint8(13)
	panelWidth                = 290
)

type Renderer struct {
	instance           *model.Instance
	engine             *solver.Engine
	path               string
	solution           model.Solution
	graph              *ebiten.Image
	width              int
	height             int
	zoom               float64
	offsetX            float64
	offsetY            float64
	dragging           bool
	lastX              int
	lastY              int
	selected           int
	dirty              bool
	running            bool
	iterationsPerFrame uint64
	frameTime          time.Duration
	solverTime         time.Duration
	lastUpdate         time.Time
	renderFrameTime    time.Duration
	lastDraw           time.Time
	publishedIteration uint64
	runLogged          bool
	newSolver          func(*model.Instance, *model.Solution) (solver.Iterative, error)
	nodeTraffic        []float64
	maxTraffic         float64
	font               font.Face
}

func New(instance *model.Instance, engine *solver.Engine, path string, newSolver func(*model.Instance, *model.Solution) (solver.Iterative, error)) (*Renderer, error) {
	ebiten.SetTPS(int(targetFPS))
	snapshot := engine.Snapshot()
	parsedFont, err := opentype.Parse(goregular.TTF)
	if err != nil {
		return nil, fmt.Errorf("parse UI font: %w", err)
	}
	uiFont, err := opentype.NewFace(parsedFont, &opentype.FaceOptions{
		Size:    14,
		DPI:     72,
		Hinting: font.HintingFull,
	})
	if err != nil {
		return nil, fmt.Errorf("create UI font: %w", err)
	}
	renderer := &Renderer{
		instance:           instance,
		engine:             engine,
		path:               path,
		solution:           snapshot.Solution,
		zoom:               1,
		selected:           -1,
		dirty:              true,
		newSolver:          newSolver,
		font:               uiFont,
		iterationsPerFrame: defaultIterationsPerFrame,
	}
	renderer.nodeTraffic, renderer.maxTraffic = trafficByNode(instance)
	return renderer, nil
}

func (r *Renderer) Update() error {
	now := time.Now()
	if !r.lastUpdate.IsZero() {
		frameTime := now.Sub(r.lastUpdate)
		if r.frameTime == 0 {
			r.frameTime = frameTime
		} else {
			r.frameTime = time.Duration(
				float64(r.frameTime)*(1-frameTimeSmoothing) +
					float64(frameTime)*frameTimeSmoothing,
			)
		}
	}
	r.lastUpdate = now

	if inpututil.IsKeyJustPressed(ebiten.KeySpace) {
		r.running = false
		if err := r.engine.Step(); err != nil {
			return err
		}
		r.updateSolution()
		if err := r.logIfFinished(); err != nil {
			return err
		}
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyEnter) {
		r.running = !r.running && !r.engine.Finished()
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyP) {
		r.running = false
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyBracketLeft) && r.iterationsPerFrame > 1 {
		r.iterationsPerFrame--
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyBracketRight) && r.iterationsPerFrame < maxIterationsPerFrame {
		r.iterationsPerFrame++
	}
	if r.running {
		batchStart := time.Now()
		if err := r.engine.Run(r.iterationsPerFrame); err != nil {
			return err
		}
		batchTime := time.Since(batchStart)
		if r.solverTime == 0 {
			r.solverTime = batchTime
		} else {
			r.solverTime = time.Duration(
				float64(r.solverTime)*(1-frameTimeSmoothing) +
					float64(batchTime)*frameTimeSmoothing,
			)
		}
		r.adjustIterations()
		r.updateSolution()
		if err := r.logIfFinished(); err != nil {
			return err
		}
		if r.engine.Finished() {
			r.running = false
		}
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyO) {
		path, err := zenity.SelectFile(zenity.FileFilters{
			{Name: "JSON files", Patterns: []string{"*.json"}},
		})
		if err == nil {
			instance, loadErr := model.Load(path)
			if loadErr != nil {
				return loadErr
			}
			r.instance, r.path = instance, path
			r.runLogged = false
			r.solverTime = 0
			r.nodeTraffic, r.maxTraffic = trafficByNode(instance)
			r.solution = solver.DefaultSolution(instance)
			nextSolver, err := r.newSolver(instance, nil)
			if err != nil {
				return err
			}
			if err := r.engine.Replace(nextSolver, solver.DefaultSolution(instance)); err != nil {
				return err
			}
			r.graph = nil
			r.zoom, r.offsetX, r.offsetY = 1, 0, 0
			r.selected, r.dirty = -1, true
		} else if err != zenity.ErrCanceled {
			return err
		}
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyS) {
		path, err := zenity.SelectFileSave(
			zenity.ConfirmOverwrite(),
			zenity.FileFilters{{Name: "Solution files", Patterns: []string{"*.solution.json"}}},
		)
		if err == nil {
			snapshot := r.engine.Snapshot()
			if cost, costErr := r.instance.TotalCost(snapshot.Solution); costErr == nil {
				snapshot.Solution.Cost = cost
			}
			if err := model.SaveSolution(path+".json", snapshot.Solution, snapshot.Iteration, snapshot.Finished); err != nil {
				return err
			}
		} else if err != zenity.ErrCanceled {
			return err
		}
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyL) {
		path, err := zenity.SelectFile(zenity.FileFilters{
			{Name: "Solution files", Patterns: []string{"*.solution.json", "*.json"}},
		})
		if err == nil {
			saved, loadErr := model.LoadSolution(path)
			if loadErr != nil {
				return loadErr
			}
			if len(saved.Assign) != len(r.instance.Nodes) {
				return fmt.Errorf("solution has %d assignments, current instance has %d nodes",
					len(saved.Assign), len(r.instance.Nodes))
			}
			solution, err := saved.ToSolution(r.instance)
			if err != nil {
				return err
			}
			nextSolver, err := r.newSolver(r.instance, &solution)
			if err != nil {
				return err
			}
			r.engine.RestoreWithSolver(nextSolver, solver.Snapshot{
				Iteration: 0,
				Solution:  solution,
				Finished:  false,
			})
			r.solverTime = 0
			r.publishedIteration = ^uint64(0)
			r.updateSolution()
			r.running = false
		} else if err != zenity.ErrCanceled {
			return err
		}
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyR) {
		r.zoom, r.offsetX, r.offsetY = 1, 0, 0
		r.dirty = true
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyEqual) || inpututil.IsKeyJustPressed(ebiten.KeyNumpadAdd) {
		x, y := ebiten.CursorPosition()
		r.zoomAt(1.15, x, y)
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyMinus) || inpututil.IsKeyJustPressed(ebiten.KeyNumpadSubtract) {
		x, y := ebiten.CursorPosition()
		r.zoomAt(1/1.15, x, y)
	}

	x, y := ebiten.CursorPosition()
	if ebiten.IsMouseButtonPressed(ebiten.MouseButtonLeft) {
		if !r.dragging {
			r.dragging, r.lastX, r.lastY = true, x, y
		} else {
			r.offsetX += float64(x - r.lastX)
			r.offsetY += float64(y - r.lastY)
			r.lastX, r.lastY, r.dirty = x, y, true
		}
	} else {
		r.dragging = false
	}
	_, wheel := ebiten.Wheel()
	if wheel != 0 {
		r.zoomAt(math.Pow(1.15, wheel), x, y)
	}
	if inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft) {
		selected := r.hitNode(x, y)
		if selected != r.selected {
			r.selected = selected
			r.dirty = true
		}
	}
	return nil
}

func (r *Renderer) Draw(screen *ebiten.Image) {
	now := time.Now()
	if !r.lastDraw.IsZero() {
		renderInterval := time.Second / renderFPSLimit
		if elapsed := now.Sub(r.lastDraw); elapsed < renderInterval {
			time.Sleep(renderInterval - elapsed)
			now = time.Now()
		}
		drawTime := now.Sub(r.lastDraw)
		if r.renderFrameTime == 0 {
			r.renderFrameTime = drawTime
		} else {
			r.renderFrameTime = time.Duration(
				float64(r.renderFrameTime)*(1-frameTimeSmoothing) +
					float64(drawTime)*frameTimeSmoothing,
			)
		}
	}
	r.lastDraw = now
	if r.width == 0 {
		return
	}
	if r.dirty || r.graph == nil {
		r.rebuildGraph()
	}
	screen.Fill(color.RGBA{backgroundColor, 17, 38, 255})
	screen.DrawImage(r.graph, nil)
	r.drawPanel(screen)
}

func (r *Renderer) Layout(width, height int) (int, int) {
	if width != r.width || height != r.height {
		r.width, r.height, r.dirty = width, height, true
	}
	return width, height
}

func (r *Renderer) updateSolution() {
	snapshot := r.engine.Snapshot()
	if snapshot.Iteration == r.publishedIteration {
		return
	}
	r.solution = snapshot.Solution
	r.publishedIteration = snapshot.Iteration
	r.dirty = true
}

func (r *Renderer) logIfFinished() error {
	snapshot := r.engine.Snapshot()
	if !snapshot.Finished || r.runLogged {
		return nil
	}
	cost, err := r.instance.TotalCost(snapshot.Solution)
	if err != nil {
		return fmt.Errorf("calculate final run cost: %w", err)
	}
	if err := model.AppendRunLog("solver_runs.csv", r.path, r.engine.SolverName(),
		snapshot.Iteration, cost, snapshot.Finished); err != nil {
		return err
	}
	r.runLogged = true
	return nil
}

func (r *Renderer) rebuildGraph() {
	graphWidth := r.width - panelWidth
	if graphWidth < 1 {
		graphWidth = 1
	}
	if r.graph == nil || r.graph.Bounds().Dx() != graphWidth || r.graph.Bounds().Dy() != r.height {
		r.graph = ebiten.NewImage(graphWidth, r.height)
	}
	r.graph.Fill(color.RGBA{backgroundColor, 17, 38, 255})
	minX, minY, maxX, maxY := bounds(r.instance.Nodes)
	baseScale := math.Min(float64(graphWidth-80)/(maxX-minX), float64(r.height-80)/(maxY-minY))
	if baseScale <= 0 {
		baseScale = 1
	}
	positions := make([][2]float32, len(r.instance.Nodes))
	for index, node := range r.instance.Nodes {
		positions[index] = [2]float32{
			float32((float64(node.X)-minX)*baseScale*r.zoom + 40 + r.offsetX),
			float32((float64(node.Y)-minY)*baseScale*r.zoom + 40 + r.offsetY),
		}
	}
	for node, hub := range r.solution.Assign {
		if node == hub || !validNode(hub, len(positions)) {
			continue
		}
		p, q := positions[node], positions[hub]
		width := float32(1)
		edgeColor := color.RGBA{100, 150, 210, 150}
		if node == r.selected {
			width = 3
			edgeColor = color.RGBA{255, 255, 255, 230}
		}
		vector.StrokeLine(r.graph, p[0], p[1], q[0], q[1], width, edgeColor, true)
	}
	if validNode(r.selected, len(r.solution.Assign)) {
		selectedHub := r.solution.Assign[r.selected]
		if validNode(selectedHub, len(positions)) {
			for _, other := range r.solution.Hubs {
				if other == selectedHub || !validNode(other, len(positions)) {
					continue
				}
				p, q := positions[selectedHub], positions[other]
				vector.StrokeLine(r.graph, p[0], p[1], q[0], q[1], 3, color.RGBA{255, 190, 60, 220}, true)
			}
		}
	}
	for index, position := range positions {
		radius := trafficRadius(r.nodeTraffic[index], r.maxTraffic) - 2
		fill := trafficColor(r.nodeTraffic[index], r.maxTraffic)
		if contains(r.solution.Hubs, index) {
			radius += 4
			fill = color.RGBA{255, 190, 60, 255}
		}
		if index == r.selected {
			vector.StrokeCircle(r.graph, position[0], position[1], radius+5, 2, color.White, true)
		}
		vector.DrawFilledCircle(r.graph, position[0], position[1], radius, fill, true)
	}
	r.dirty = false
}

func (r *Renderer) drawPanel(screen *ebiten.Image) {
	snapshot := r.engine.Snapshot()
	panelX := r.width - panelWidth
	ebitenutil.DrawRect(screen, float64(panelX), 0, float64(panelWidth), float64(r.height), color.RGBA{24, 29, 52, 255})
	ebitenutil.DrawRect(screen, float64(panelX), 0, 3, float64(r.height), color.RGBA{70, 120, 190, 255})
	lines := []string{
		"Solver workbench",
		"",
		fmt.Sprintf("Iteration: %d", snapshot.Iteration),
		fmt.Sprintf("Hubs: %d / %d", len(r.solution.Hubs), r.instance.Parameters.HubCount),
		fmt.Sprintf("Status: %s", statusText(snapshot.Finished, r.running)),
		fmt.Sprintf("Iterations/frame: %d", r.iterationsPerFrame),
		fmt.Sprintf("Target updates: %.0f  Actual: %.1f", targetFPS, r.updateFPS()),
		fmt.Sprintf("Render FPS: %.1f", r.renderFPS()),
		fmt.Sprintf("Zoom: %.2fx", r.zoom),
		"",
		"Controls",
		"Space  step",
		"Enter  run / pause",
		"P      pause",
		"[ / ]  iterations/frame (manual)",
		"O      open JSON",
		"S      save solution",
		"L      load solution",
		"R      reset view",
		"Wheel  zoom",
		"Drag   pan",
		"",
		fmt.Sprintf("Total traffic: %.0f", totalTraffic(r.nodeTraffic)),
		fmt.Sprintf("Cost: %.2f", solutionCost(r.instance, r.solution)),
		"",
		"Traffic: node size/color",
		"Blue low  ->  red high",
	}

	if r.selected >= 0 && r.selected < len(r.solution.Assign) {
		hub := r.solution.Assign[r.selected]
		lines = append(lines, "", fmt.Sprintf("Selected node: %d", r.selected),
			fmt.Sprintf("Assigned hub: %d", hub),
			fmt.Sprintf("Node traffic: %.0f", r.nodeTraffic[r.selected]),
			fmt.Sprintf("Hub load: %d nodes", assignedCount(r.solution.Assign, hub)))
	}
	for index, line := range lines {
		text.Draw(screen, line, r.font, panelX+18, 28+index*20, color.RGBA{235, 240, 250, 255})
	}
}

func (r *Renderer) adjustIterations() {
	if r.solverTime <= 0 {
		return
	}
	targetSolverTime := time.Duration(float64(time.Second) / targetFPS * solverBudgetFraction)
	ratio := float64(targetSolverTime) / float64(r.solverTime)
	if ratio > 1 {
		ratio = math.Min(ratio, 1.25)
	} else {
		ratio = math.Max(ratio, 0.5)
	}
	next := uint64(math.Round(float64(r.iterationsPerFrame) * ratio))
	r.iterationsPerFrame = maxIterations(1, math.Min(float64(maxIterationsPerFrame), float64(next)))
}

func (r *Renderer) updateFPS() float64 {
	if r.frameTime <= 0 {
		return 0
	}
	return 1 / r.frameTime.Seconds()
}

func (r *Renderer) renderFPS() float64 {
	if r.renderFrameTime <= 0 {
		return 0
	}
	return 1 / r.renderFrameTime.Seconds()
}

func maxIterations(minimum uint64, value float64) uint64 {
	if value < float64(minimum) {
		return minimum
	}
	return uint64(value)
}

func solutionCost(instance *model.Instance, solution model.Solution) float64 {
	cost, err := instance.TotalCost(solution)
	if err != nil {
		return 0
	}
	return cost
}

func statusText(finished, running bool) string {
	if finished {
		return "finished"
	}
	if running {
		return "running"
	}
	return "paused"
}

func trafficByNode(instance *model.Instance) ([]float64, float64) {
	traffic := make([]float64, len(instance.Nodes))
	for from := 0; from < len(instance.Nodes); from++ {
		for to := from + 1; to < len(instance.Nodes); to++ {
			quantity := float64(instance.Quantity.At(from, to))
			traffic[from] += quantity
			traffic[to] += quantity
		}
	}
	maximum := 0.0
	for _, value := range traffic {
		if value > maximum {
			maximum = value
		}
	}
	return traffic, maximum
}

func trafficRadius(value, maximum float64) float32 {
	if maximum <= 0 {
		return 4
	}
	return float32(4 + 5*math.Sqrt(value/maximum))
}

func trafficColor(value, maximum float64) color.RGBA {
	if maximum <= 0 {
		return color.RGBA{90, 180, 230, 255}
	}
	ratio := math.Sqrt(value / maximum)
	return color.RGBA{
		R: uint8(70 + 185*ratio),
		G: uint8(190 - 120*ratio),
		B: uint8(240 - 170*ratio),
		A: 255,
	}
}

func totalTraffic(traffic []float64) float64 {
	total := 0.0
	for _, value := range traffic {
		total += value
	}
	return total / 2
}

func assignedCount(assign []int, hub int) int {
	count := 0
	for _, assignedHub := range assign {
		if assignedHub == hub {
			count++
		}
	}
	return count
}
func (r *Renderer) zoomAt(factor float64, x, y int) {
	oldZoom := r.zoom
	r.zoom = math.Max(0.1, math.Min(20, r.zoom*factor))
	scale := r.zoom / oldZoom
	r.offsetX = float64(x) - (float64(x)-r.offsetX)*scale
	r.offsetY = float64(y) - (float64(y)-r.offsetY)*scale
	r.dirty = true
}

func (r *Renderer) hitNode(x, y int) int {
	if r.graph == nil {
		return -1
	}
	minX, minY, maxX, maxY := bounds(r.instance.Nodes)
	graphWidth := r.width - panelWidth
	scale := math.Min(float64(graphWidth-80)/(maxX-minX), float64(r.height-80)/(maxY-minY)) * r.zoom
	closest, distance := -1, float64(12*12)
	for index, node := range r.instance.Nodes {
		sx := (float64(node.X)-minX)*scale + 40 + r.offsetX
		sy := (float64(node.Y)-minY)*scale + 40 + r.offsetY
		d := (sx-float64(x))*(sx-float64(x)) + (sy-float64(y))*(sy-float64(y))
		if d <= distance {
			closest, distance = index, d
		}
	}
	return closest
}

func validNode(index, count int) bool {
	return index >= 0 && index < count
}

func contains(values []int, value int) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func bounds(nodes []model.Node) (float64, float64, float64, float64) {
	minX, minY := float64(nodes[0].X), float64(nodes[0].Y)
	maxX, maxY := minX, minY
	for _, node := range nodes[1:] {
		x, y := float64(node.X), float64(node.Y)
		minX, minY = math.Min(minX, x), math.Min(minY, y)
		maxX, maxY = math.Max(maxX, x), math.Max(maxY, y)
	}
	return minX, minY, math.Max(maxX, minX+1), math.Max(maxY, minY+1)
}
