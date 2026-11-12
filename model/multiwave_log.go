package model

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const multiWaveLogHeader = "solver,dataset,run_id,timestamp,wave,wave_iteration,iteration,cost,solution"

func NewMultiWaveLogPath(directory, solver, dataset, runID string) (string, error) {
	if err := os.MkdirAll(directory, 0755); err != nil {
		return "", fmt.Errorf("create multi-wave log directory %q: %w", directory, err)
	}
	datasetName := filepath.Base(dataset)
	datasetName = strings.TrimSuffix(datasetName, filepath.Ext(datasetName))
	return filepath.Join(directory, sanitizeLogName(solver)+"_"+sanitizeLogName(datasetName)+"_"+runID+".csv"), nil
}

func AppendMultiWaveLog(path, solver, dataset, runID string, wave, waveIteration, iteration uint64, cost float64, solution Solution) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("open multi-wave log %q: %w", path, err)
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return fmt.Errorf("inspect multi-wave log %q: %w", path, err)
	}
	writer := csv.NewWriter(file)
	if stat.Size() == 0 {
		if err := writer.Write([]string{multiWaveLogHeader}); err != nil {
			return fmt.Errorf("write multi-wave log header: %w", err)
		}
	}
	encodedSolution, err := json.Marshal(struct {
		Hubs   []int `json:"hubs"`
		Assign []int `json:"assign"`
	}{
		Hubs: solution.Hubs, Assign: solution.Assign,
	})
	if err != nil {
		return fmt.Errorf("encode multi-wave solution: %w", err)
	}
	if err := writer.Write([]string{
		solver,
		dataset,
		runID,
		time.Now().UTC().Format(time.RFC3339Nano),
		strconv.FormatUint(wave, 10),
		strconv.FormatUint(waveIteration, 10),
		strconv.FormatUint(iteration, 10),
		strconv.FormatFloat(cost, 'f', 6, 64),
		string(encodedSolution),
	}); err != nil {
		return fmt.Errorf("write multi-wave log: %w", err)
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return fmt.Errorf("flush multi-wave log: %w", err)
	}
	return nil
}

func sanitizeLogName(value string) string {
	var builder strings.Builder
	for _, character := range value {
		if (character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') ||
			character == '-' || character == '_' {
			builder.WriteRune(character)
		} else {
			builder.WriteRune('_')
		}
	}
	return builder.String()
}
