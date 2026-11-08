package algorithms

import (
	"fmt"
	"math"

	"NHubs/model"
	"NHubs/solver"
)

const maxNodesPerHub = 4 + 1

type Greedy struct {
	instance   *model.Instance
	targetHubs int
	hubs       []int
	used       []bool
	traffic    []float64
	current    model.Solution
	finished   bool
}

func (g *Greedy) Name() string { return "greedy" }

func NewGreedy(instance *model.Instance) (*Greedy, error) {
	if instance == nil || len(instance.Nodes) == 0 {
		return nil, fmt.Errorf("greedy solver requires a non-empty instance")
	}
	target := instance.Parameters.HubCount
	if target < 1 {
		target = 1
	}
	if target > len(instance.Nodes) {
		target = len(instance.Nodes)
	}
	if target*maxNodesPerHub < len(instance.Nodes)-instance.Parameters.HubCount {
		return nil, fmt.Errorf(
			"greedy solver cannot assign %d nodes with %d hubs at a capacity of %d nodes per hub",
			len(instance.Nodes), target, maxNodesPerHub)
	}
	g := &Greedy{
		instance:   instance,
		targetHubs: target,
		used:       make([]bool, len(instance.Nodes)),
		traffic:    make([]float64, len(instance.Nodes)),
		current:    model.Solution{Assign: make([]int, len(instance.Nodes))},
	}
	g.calculateTraffic()
	return g, nil
}

func (g *Greedy) Step() (solver.StepResult, error) {
	if g.finished {
		return solver.StepResult{Solution: g.current, Done: true}, nil
	}
	hub := g.nextHub()
	g.hubs = append(g.hubs, hub)
	g.used[hub] = true
	g.assignNodes()
	g.current.Hubs = append(g.current.Hubs[:0], g.hubs...)
	if len(g.hubs) == g.targetHubs {
		g.finished = true
	}
	return solver.StepResult{Solution: g.current, Done: g.finished}, nil
}

func (g *Greedy) calculateTraffic() {
	for from := range g.instance.Nodes {
		for to := from + 1; to < len(g.instance.Nodes); to++ {
			quantity := float64(g.instance.Quantity.At(from, to))
			g.traffic[from] += quantity
			g.traffic[to] += quantity
		}
	}
}

func (g *Greedy) nextHub() int {
	best, bestScore := -1, math.Inf(1)
	for candidate := range g.instance.Nodes {
		if g.used[candidate] {
			continue
		}
		score := 0.0
		for node := range g.instance.Nodes {
			score += g.instance.Cost(g.current, node, candidate)
		}
		if score < bestScore {
			best, bestScore = candidate, score
		}
	}
	return best
}

func (g *Greedy) assignNodes() {
	for node := range g.current.Assign {
		g.current.Assign[node] = -1
	}

	load := make(map[int]int, len(g.hubs))
	for _, hub := range g.hubs {
		g.current.Assign[hub] = hub
		load[hub] = 1
	}

	for node := range g.instance.Nodes {
		if g.current.Assign[node] != -1 {
			continue
		}

		bestHub := -1
		bestDistance := math.Inf(1)
		for _, hub := range g.hubs {
			if load[hub] >= maxNodesPerHub {
				continue
			}
			distance := g.instance.Distance(node, hub)
			if distance < bestDistance {
				bestHub, bestDistance = hub, distance
			}
		}
		if bestHub != -1 {
			g.current.Assign[node] = bestHub
			load[bestHub]++
		}
	}
}
