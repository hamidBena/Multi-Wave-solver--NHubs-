package model

import (
	"encoding/csv"
	"fmt"
	"os"
	"strconv"
	"time"
)

const runLogHeader = "timestamp,dataset,solver,iterations,cost,finished"

func AppendRunLog(path, dataset, solver string, iterations uint64, cost float64, finished bool) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("open run log %q: %w", path, err)
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return fmt.Errorf("inspect run log %q: %w", path, err)
	}
	writer := csv.NewWriter(file)
	if stat.Size() == 0 {
		if err := writer.Write([]string{runLogHeader}); err != nil {
			return fmt.Errorf("write run log header: %w", err)
		}
	}
	if err := writer.Write([]string{
		time.Now().UTC().Format(time.RFC3339),
		dataset,
		solver,
		strconv.FormatUint(iterations, 10),
		strconv.FormatFloat(cost, 'f', 6, 64),
		strconv.FormatBool(finished),
	}); err != nil {
		return fmt.Errorf("write run log: %w", err)
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return fmt.Errorf("flush run log: %w", err)
	}
	return nil
}
