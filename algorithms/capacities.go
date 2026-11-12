package algorithms

import (
	"fmt"

	"NHubs/model"
)

func hubCapacity(instance *model.Instance, hubs []int, hub int) int {
	for slot, candidate := range hubs {
		if candidate == hub {
			return instance.Capacities()[slot]
		}
	}
	return 0
}

func validateCapacity(instance *model.Instance, hubs []int) error {
	total := 0
	for slot := range hubs {
		total += instance.Capacities()[slot]
	}
	if total < len(instance.Nodes) {
		return fmt.Errorf("hub capacities provide %d places for %d nodes", total, len(instance.Nodes))
	}
	return nil
}
