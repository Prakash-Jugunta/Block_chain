// Command split creates reproducible run-level train/validation/test data splits without leakage.
//
// Usage:
//
//	go run ./cmd/split --dataset outputs/ml/dataset/fork_risk_dataset.csv --output outputs/ml/splits/split_manifest.json
//
// Flags:
//
//	--dataset  Path to fork_risk_dataset.csv (default: outputs/ml/dataset/fork_risk_dataset.csv)
//	--output   Path to write split_manifest.json (default: outputs/ml/splits/split_manifest.json)
package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"

	"github.com/pranav-k/forkrisk/internal/features"
	"github.com/pranav-k/forkrisk/internal/ml"
)

func main() {
	datasetPath := flag.String("dataset", "outputs/ml/dataset/fork_risk_dataset.csv", "Path to dataset.csv")
	outputPath := flag.String("output", "outputs/ml/splits/split_manifest.json", "Path to write split_manifest.json")
	flag.Parse()

	rows, err := loadDatasetRows(*datasetPath)
	if err != nil {
		log.Fatalf("Failed to load dataset: %v", err)
	}

	manifest, _, err := ml.CreateSeedHeldOutSplit(rows)
	if err != nil {
		log.Fatalf("Failed to create split: %v", err)
	}

	if err := ml.SaveSplitManifest(*outputPath, manifest); err != nil {
		log.Fatalf("Failed to save split manifest: %v", err)
	}

	fmt.Println("=== Run-Level Split Created ===")
	fmt.Printf("Strategy:         %s\n", manifest.Strategy)
	fmt.Printf("Train Runs:       %d (%d rows, prevalence: %.2f%%)\n", manifest.TrainRunCount, manifest.TrainRows, manifest.TrainPrevalence*100)
	fmt.Printf("Validation Runs:  %d (%d rows, prevalence: %.2f%%)\n", manifest.ValRunCount, manifest.ValRows, manifest.ValPrevalence*100)
	fmt.Printf("Test Runs:        %d (%d rows, prevalence: %.2f%%)\n", manifest.TestRunCount, manifest.TestRows, manifest.TestPrevalence*100)
	fmt.Printf("Output File:      %s\n", *outputPath)
}

func loadDatasetRows(path string) ([]features.DatasetRow, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	reader := csv.NewReader(f)
	records, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}

	if len(records) < 2 {
		return nil, fmt.Errorf("dataset has no data rows")
	}

	header := records[0]
	colIdx := make(map[string]int)
	for i, h := range header {
		colIdx[h] = i
	}

	var rows []features.DatasetRow
	for _, rec := range records[1:] {
		seed, _ := strconv.ParseInt(rec[colIdx["seed"]], 10, 64)
		h, _ := strconv.Atoi(rec[colIdx["block_height"]])
		tSim, _ := strconv.ParseFloat(rec[colIdx["sim_time"]], 64)
		lbl, _ := strconv.Atoi(rec[colIdx[features.TargetColumn]])

		feats := make(map[string]float64)
		for _, fName := range features.CanonicalFeatures {
			idx, ok := colIdx[fName]
			if ok {
				val, _ := strconv.ParseFloat(rec[idx], 64)
				feats[fName] = val
			}
		}

		rows = append(rows, features.DatasetRow{
			RunID:       rec[colIdx["run_id"]],
			Scenario:    rec[colIdx["scenario"]],
			Seed:        seed,
			BlockHeight: h,
			SimTime:     tSim,
			Features:    feats,
			LabelForkH:  lbl,
		})
	}

	return rows, nil
}
