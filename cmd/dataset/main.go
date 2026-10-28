// Command dataset generates a structured ML prediction dataset from simulation event logs.
//
// Usage:
//
//	go run ./cmd/dataset --runs-dir outputs/runs --pilot-csv outputs/pilot_final/pilot_summary.csv --output-dir outputs/ml/dataset
//
// Flags:
//
//	--runs-dir    Base directory containing simulation run folders (default: outputs/runs)
//	--pilot-csv   Optional path to summary CSV to select authoritative runs (e.g. outputs/pilot_final/pilot_summary.csv)
//	--output-dir  Directory to write fork_risk_dataset.csv and dataset_metadata.json (default: outputs/ml/dataset)
//	--horizon     Forecast horizon H in blocks (default: 3)
//	--warmup      Warm-up blocks to skip (default: 10)
package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pranav-k/forkrisk/internal/features"
)

func main() {
	runsDir := flag.String("runs-dir", "outputs/runs", "Base directory containing simulation run folders")
	pilotCSV := flag.String("pilot-csv", "outputs/pilot_final/pilot_summary.csv", "Path to summary CSV to select authoritative runs (empty = all runs in runs-dir)")
	outputDir := flag.String("output-dir", "outputs/ml/dataset", "Directory to write dataset outputs")
	horizon := flag.Int("horizon", 3, "Forecast horizon H in blocks")
	warmup := flag.Int("warmup", 10, "Warm-up blocks to skip")
	flag.Parse()

	var targetRunDirs []string

	if *pilotCSV != "" && fileExists(*pilotCSV) {
		f, err := os.Open(*pilotCSV)
		if err != nil {
			log.Fatalf("Failed to open pilot CSV %s: %v", *pilotCSV, err)
		}
		defer f.Close()

		reader := csv.NewReader(f)
		records, err := reader.ReadAll()
		if err != nil {
			log.Fatalf("Failed to read pilot CSV %s: %v", *pilotCSV, err)
		}

		if len(records) > 1 {
			for _, rec := range records[1:] {
				if len(rec) > 0 && strings.TrimSpace(rec[0]) != "" {
					runID := strings.TrimSpace(rec[0])
					rDir := filepath.Join(*runsDir, runID)
					targetRunDirs = append(targetRunDirs, rDir)
				}
			}
		}
		fmt.Printf("Loaded %d authoritative run IDs from %s\n", len(targetRunDirs), *pilotCSV)
	} else {
		// Scan runsDir
		entries, err := os.ReadDir(*runsDir)
		if err != nil {
			log.Fatalf("Failed to read runs dir %s: %v", *runsDir, err)
		}
		for _, e := range entries {
			if e.IsDir() && e.Name() != "historical_incomplete" {
				targetRunDirs = append(targetRunDirs, filepath.Join(*runsDir, e.Name()))
			}
		}
		fmt.Printf("Found %d run directories in %s\n", len(targetRunDirs), *runsDir)
	}

	sort.Strings(targetRunDirs)

	cfg := features.PipelineConfig{
		RunDirs:      targetRunDirs,
		OutputDir:    *outputDir,
		Horizon:      *horizon,
		WarmUpBlocks: *warmup,
	}

	fmt.Printf("Generating prediction dataset with horizon H=%d, warmup=%d...\n", cfg.Horizon, cfg.WarmUpBlocks)
	meta, err := features.GenerateDataset(cfg)
	if err != nil {
		log.Fatalf("Dataset generation failed: %v", err)
	}

	fmt.Println("=== Dataset Generation Complete ===")
	fmt.Printf("Total Rows:        %d\n", meta.TotalRows)
	fmt.Printf("Included Runs:     %d\n", meta.IncludedRunCount)
	fmt.Printf("Positive Labels:   %d (%.2f%%)\n", meta.PositiveCount, meta.PositiveRate*100)
	fmt.Printf("Negative Labels:   %d (%.2f%%)\n", meta.NegativeCount, (1.0-meta.PositiveRate)*100)
	fmt.Printf("Excluded Runs:     %d\n", len(meta.ExcludedRuns))
	fmt.Printf("Dataset SHA-256:   %s\n", meta.DatasetSHA256)
	fmt.Printf("Output File:       %s\n", filepath.Join(*outputDir, "fork_risk_dataset.csv"))
	fmt.Printf("Metadata File:     %s\n", filepath.Join(*outputDir, "dataset_metadata.json"))
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	if os.IsNotExist(err) {
		return false
	}
	return !info.IsDir()
}
