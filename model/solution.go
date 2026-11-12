package model

import (
	"fmt"
	"math"
)

func (inst *Instance) TotalCost(solution Solution) (float64, error) {
	var totalCost float64
	for i := 0; i < len(inst.Nodes); i++ {
		for j := i + 1; j < len(inst.Nodes); j++ {
			totalCost += inst.Cost(solution, i, j)
		}
	}
	return totalCost, nil
}

func (i *Instance) Distance(from, to int) float64 {
	if from < 0 || from >= len(i.Nodes) || to < 0 || to >= len(i.Nodes) {
		return math.Inf(1)
	}
	return i.distances[from*len(i.Nodes)+to]
}

func (i *Instance) Cost(solution Solution, from, to int) float64 {
	// Bounds check
	if from < 0 || from >= len(i.Nodes) || to < 0 || to >= len(i.Nodes) {
		return math.Inf(1)
	}

	// Get the assigned hub for the 'from' node and the 'to' node
	h1 := solution.Assign[from]
	h2 := solution.Assign[to]

	// Calculate the 3-leg unit transportation cost path:
	// 1. Collection: from -> h1
	costFromToHub1 := i.Distance(from, h1)

	// 2. Transfer: h1 -> h2 (discounted by alpha)
	costHub1ToHub2 := i.Parameters.Alpha * i.Distance(h1, h2)

	// 3. Distribution: h2 -> to
	costHub2ToDst := i.Distance(h2, to)

	unitCost := costFromToHub1 + costHub1ToHub2 + costHub2ToDst
	quantity := float64(i.Quantity.At(from, to))

	return unitCost * quantity
}

func (i *Instance) ValidateSolution(solution Solution) error {
	if len(solution.Assign) != len(i.Nodes) {
		return fmt.Errorf("solution assigns %d nodes, expected %d", len(solution.Assign), len(i.Nodes))
	}
	if len(solution.Hubs) != i.Parameters.HubCount {
		return fmt.Errorf("solution has %d hubs, expected %d", len(solution.Hubs), i.Parameters.HubCount)
	}
	hubSet := make(map[int]struct{}, len(solution.Hubs))
	for _, hub := range solution.Hubs {
		if hub < 0 || hub >= len(i.Nodes) {
			return fmt.Errorf("invalid hub %d", hub)
		}
		if _, exists := hubSet[hub]; exists {
			return fmt.Errorf("hub %d is listed more than once", hub)
		}
		hubSet[hub] = struct{}{}
	}
	counts := make(map[int]int, len(hubSet))
	for node, hub := range solution.Assign {
		if hub < 0 || hub >= len(i.Nodes) {
			return fmt.Errorf("node %d is assigned to invalid hub %d", node, hub)
		}
		if _, exists := hubSet[hub]; !exists {
			return fmt.Errorf("node %d is assigned to undeclared hub %d", node, hub)
		}
		if solution.Assign[hub] != hub {
			return fmt.Errorf("assigned hub %d is not assigned to itself", hub)
		}
		counts[hub]++
	}
	capacities := i.Capacities()
	for slot, hub := range solution.Hubs {
		if solution.Assign[hub] != hub {
			return fmt.Errorf("invalid hub %d", hub)
		}
		if counts[hub] != capacities[slot] {
			return fmt.Errorf("hub %d has %d assigned nodes, expected %d",
				hub, counts[hub], capacities[slot])
		}
	}
	return nil
}
