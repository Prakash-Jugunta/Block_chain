package ml

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/pranav-k/forkrisk/internal/features"
)

// RunSplitInfo records run-level details within the split manifest.
type RunSplitInfo struct {
	RunID         string  `json:"run_id"`
	Scenario      string  `json:"scenario"`
	Seed          int64   `json:"seed"`
	Split         string  `json:"split"` // "train", "val", or "test"
	TotalRows     int     `json:"total_rows"`
	PositiveCount int     `json:"positive_count"`
	PositiveRate  float64 `json:"positive_rate"`
}

// SplitManifest stores run-level split assignments and summary statistics.
type SplitManifest struct {
	Strategy        string                  `json:"strategy"`
	TrainRuns       []string                `json:"train_runs"`
	ValRuns         []string                `json:"val_runs"`
	TestRuns        []string                `json:"test_runs"`
	TrainRunCount   int                     `json:"train_run_count"`
	ValRunCount     int                     `json:"val_run_count"`
	TestRunCount    int                     `json:"test_run_count"`
	TrainRows       int                     `json:"train_rows"`
	ValRows         int                     `json:"val_rows"`
	TestRows        int                     `json:"test_rows"`
	TrainPositives  int                     `json:"train_positives"`
	ValPositives    int                     `json:"val_positives"`
	TestPositives   int                     `json:"test_positives"`
	TrainPrevalence float64                 `json:"train_prevalence"`
	ValPrevalence   float64                 `json:"val_prevalence"`
	TestPrevalence  float64                 `json:"test_prevalence"`
	RunDetails      map[string]RunSplitInfo `json:"run_details"`
}

// DatasetSplit holds the separated in-memory data matrices and labels for each split.
type DatasetSplit struct {
	Features []string

	XTrain [][]float64
	YTrain []float64

	XVal [][]float64
	YVal []float64

	XTest [][]float64
	YTest []float64

	TrainRows []features.DatasetRow
	ValRows   []features.DatasetRow
	TestRows  []features.DatasetRow
}

// CreateSeedHeldOutSplit creates a run-level split where:
// Seed 1 runs -> Train
// Seed 2 runs -> Validation
// Seed 3 runs -> Held-Out Test
func CreateSeedHeldOutSplit(rows []features.DatasetRow) (*SplitManifest, *DatasetSplit, error) {
	manifest := &SplitManifest{
		Strategy:   "seed_held_out (Train: Seed 1, Val: Seed 2, Test: Seed 3)",
		TrainRuns:  make([]string, 0),
		ValRuns:    make([]string, 0),
		TestRuns:   make([]string, 0),
		RunDetails: make(map[string]RunSplitInfo),
	}

	split := &DatasetSplit{
		Features: features.CanonicalFeatures,
	}

	runMap := make(map[string][]features.DatasetRow)
	for _, r := range rows {
		runMap[r.RunID] = append(runMap[r.RunID], r)
	}

	// Sort run IDs for total determinism
	var runIDs []string
	for rid := range runMap {
		runIDs = append(runIDs, rid)
	}
	sort.Strings(runIDs)

	for _, rid := range runIDs {
		rRows := runMap[rid]
		first := rRows[0]
		seed := first.Seed
		scen := first.Scenario

		pos := 0
		for _, row := range rRows {
			if row.LabelForkH == 1 {
				pos++
			}
		}

		var targetSplit string
		switch seed {
		case 1:
			targetSplit = "train"
			manifest.TrainRuns = append(manifest.TrainRuns, rid)
			manifest.TrainRows += len(rRows)
			manifest.TrainPositives += pos
			for _, row := range rRows {
				split.XTrain = append(split.XTrain, extractFeatureVector(row))
				split.YTrain = append(split.YTrain, float64(row.LabelForkH))
				split.TrainRows = append(split.TrainRows, row)
			}
		case 2:
			targetSplit = "val"
			manifest.ValRuns = append(manifest.ValRuns, rid)
			manifest.ValRows += len(rRows)
			manifest.ValPositives += pos
			for _, row := range rRows {
				split.XVal = append(split.XVal, extractFeatureVector(row))
				split.YVal = append(split.YVal, float64(row.LabelForkH))
				split.ValRows = append(split.ValRows, row)
			}
		default: // Seed 3 and any others
			targetSplit = "test"
			manifest.TestRuns = append(manifest.TestRuns, rid)
			manifest.TestRows += len(rRows)
			manifest.TestPositives += pos
			for _, row := range rRows {
				split.XTest = append(split.XTest, extractFeatureVector(row))
				split.YTest = append(split.YTest, float64(row.LabelForkH))
				split.TestRows = append(split.TestRows, row)
			}
		}

		manifest.RunDetails[rid] = RunSplitInfo{
			RunID:         rid,
			Scenario:      scen,
			Seed:          seed,
			Split:         targetSplit,
			TotalRows:     len(rRows),
			PositiveCount: pos,
			PositiveRate:  float64(pos) / float64(len(rRows)),
		}
	}

	manifest.TrainRunCount = len(manifest.TrainRuns)
	manifest.ValRunCount = len(manifest.ValRuns)
	manifest.TestRunCount = len(manifest.TestRuns)

	if manifest.TrainRows > 0 {
		manifest.TrainPrevalence = float64(manifest.TrainPositives) / float64(manifest.TrainRows)
	}
	if manifest.ValRows > 0 {
		manifest.ValPrevalence = float64(manifest.ValPositives) / float64(manifest.ValRows)
	}
	if manifest.TestRows > 0 {
		manifest.TestPrevalence = float64(manifest.TestPositives) / float64(manifest.TestRows)
	}

	return manifest, split, nil
}

// SaveSplitManifest writes the split manifest to disk in JSON format.
func SaveSplitManifest(path string, manifest *SplitManifest) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("creating split dir: %w", err)
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling manifest: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}

// LoadSplitManifest loads a split manifest from disk.
func LoadSplitManifest(path string) (*SplitManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading split manifest: %w", err)
	}
	var manifest SplitManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("parsing split manifest: %w", err)
	}
	return &manifest, nil
}

func extractFeatureVector(r features.DatasetRow) []float64 {
	vec := make([]float64, len(features.CanonicalFeatures))
	for i, featName := range features.CanonicalFeatures {
		vec[i] = r.Features[featName]
	}
	return vec
}
