package model

import (
	"encoding/json"
	"fmt"
	"os"
)

func Load(path string) (*Instance, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %q: %w", path, err)
	}

	var raw struct {
		Parameters Parameters `json:"parameters"`
		Nodes      []struct {
			ID     int     `json:"id"`
			Legacy int     `json:"node"`
			X      float32 `json:"x"`
			Y      float32 `json:"y"`
		} `json:"nodes"`
		Edges []struct {
			From     int    `json:"fromNode"`
			To       int    `json:"toNode"`
			Quantity uint32 `json:"q"`
		} `json:"edges"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("decode %q: %w", path, err)
	}
	if len(raw.Nodes) == 0 {
		return nil, fmt.Errorf("validate %q: instance contains no nodes", path)
	}
	nodes := make([]Node, len(raw.Nodes))
	for index, node := range raw.Nodes {
		id := node.ID
		if id == 0 && node.Legacy != 0 {
			id = node.Legacy
		}
		if id != index {
			return nil, fmt.Errorf("validate %q: node at index %d has id %d; ids must equal indices",
				path, index, id)
		}
		nodes[index] = Node{X: node.X, Y: node.Y}
	}
	quantity, err := NewQuantityMatrix(len(nodes))
	if err != nil {
		return nil, fmt.Errorf("validate %q: %w", path, err)
	}
	instance := Instance{
		Parameters: raw.Parameters,
		Nodes:      nodes,
		Quantity:   quantity,
	}
	for _, edge := range raw.Edges {
		if edge.From < 0 || edge.From >= len(nodes) || edge.To < 0 || edge.To >= len(nodes) {
			return nil, fmt.Errorf("validate %q: edge %d -> %d references an unknown node",
				path, edge.From, edge.To)
		}
		instance.Quantity.Add(edge.From, edge.To, edge.Quantity)
	}
	if err := instance.Validate(); err != nil {
		return nil, fmt.Errorf("validate %q: %w", path, err)
	}
	instance.BuildDistanceCache()
	return &instance, nil
}

func (i *Instance) Validate() error {
	if len(i.Nodes) == 0 {
		return fmt.Errorf("instance contains no nodes")
	}
	if i.Parameters.NodeCount != 0 && i.Parameters.NodeCount != len(i.Nodes) {
		return fmt.Errorf("parameters.numberOfNodes=%d, but loaded %d nodes",
			i.Parameters.NodeCount, len(i.Nodes))
	}
	if i.Parameters.HubCount < 0 || i.Parameters.HubCount > len(i.Nodes) {
		return fmt.Errorf("parameters.numberOfHubs=%d, but must be between 0 and %d",
			i.Parameters.HubCount, len(i.Nodes))
	}
	if len(i.Parameters.HubCapacities) != 0 &&
		len(i.Parameters.HubCapacities) != i.Parameters.HubCount {
		return fmt.Errorf("parameters.hubCapacities has %d entries, expected %d",
			len(i.Parameters.HubCapacities), i.Parameters.HubCount)
	}
	if len(i.Parameters.HubCapacities) != 0 {
		total := 0
		for hub, capacity := range i.Parameters.HubCapacities {
			if capacity < 1 {
				return fmt.Errorf("parameters.hubCapacities[%d]=%d; capacities must be positive",
					hub, capacity)
			}
			total += capacity
		}
		if total != len(i.Nodes) {
			return fmt.Errorf("parameters.hubCapacities sum to %d, expected %d",
				total, len(i.Nodes))
		}
	}
	if i.Quantity.Size != len(i.Nodes) ||
		len(i.Quantity.Data) != len(i.Nodes)*(len(i.Nodes)-1)/2 {
		return fmt.Errorf("quantity matrix does not match node count")
	}
	return nil
}
