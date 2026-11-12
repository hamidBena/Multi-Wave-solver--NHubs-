package model

import (
	"fmt"
	"math"
)

type Parameters struct {
	NodeCount     int     `json:"numberOfNodes"`
	HubCount      int     `json:"numberOfHubs"`
	Alpha         float64 `json:"alpha"`
	HubCapacities []int   `json:"hubCapacities,omitempty"`
}

type Node struct {
	X float32 `json:"x,omitempty"`
	Y float32 `json:"y,omitempty"`
}

type QuantityMatrix struct {
	Size int
	Data []uint32
}

type Instance struct {
	Parameters Parameters `json:"parameters"`
	Nodes      []Node     `json:"nodes"`
	Quantity   QuantityMatrix
	distances  []float64
}

// Capacities returns one capacity per hub slot. Legacy datasets without an
// explicit list are assigned balanced capacities that sum to the node count.
func (i *Instance) Capacities() []int {
	capacities := make([]int, i.Parameters.HubCount)
	if len(i.Parameters.HubCapacities) == len(capacities) {
		copy(capacities, i.Parameters.HubCapacities)
		return capacities
	}
	if len(capacities) == 0 {
		return capacities
	}
	for hub := range capacities {
		capacities[hub] = len(i.Nodes) / len(capacities)
		if hub < len(i.Nodes)%len(capacities) {
			capacities[hub]++
		}
	}
	return capacities
}

type Solution struct {
	Hubs   []int
	Assign []int
	Cost   float64
}

type Statistics struct {
	NodeCount      int
	TotalTraffic   float64
	AverageTraffic float64
	MaxNodeTraffic float64
}

func (i *Instance) BuildDistanceCache() {
	nodeCount := len(i.Nodes)
	i.distances = make([]float64, nodeCount*nodeCount)

	for from := 0; from < nodeCount; from++ {
		for to := from; to < nodeCount; to++ {
			dx := float64(i.Nodes[from].X) - float64(i.Nodes[to].X)
			dy := float64(i.Nodes[from].Y) - float64(i.Nodes[to].Y)
			distance := math.Hypot(dx, dy)

			i.distances[from*nodeCount+to] = distance
			i.distances[to*nodeCount+from] = distance
		}
	}
}

const maxQuantityMatrixBytes = 512 << 20

func NewQuantityMatrix(size int) (QuantityMatrix, error) {
	if size < 0 {
		return QuantityMatrix{}, fmt.Errorf("invalid quantity matrix size %d", size)
	}
	pairs := uint64(size) * uint64(size-1) / 2
	if pairs*4 > maxQuantityMatrixBytes {
		return QuantityMatrix{}, fmt.Errorf("quantity matrix requires %d MB; maximum is %d MB",
			(pairs*4)/(1<<20), maxQuantityMatrixBytes/(1<<20))
	}
	return QuantityMatrix{Size: size, Data: make([]uint32, int(pairs))}, nil
}

func (m QuantityMatrix) At(from, to int) uint32 {
	if from == to {
		return 0
	}
	if from > to {
		from, to = to, from
	}
	return m.Data[from*m.Size-from*(from+1)/2+to-from-1]
}

func (m QuantityMatrix) Add(from, to int, quantity uint32) {
	if from == to {
		return
	}
	if from > to {
		from, to = to, from
	}
	m.Data[from*m.Size-from*(from+1)/2+to-from-1] += quantity
}
