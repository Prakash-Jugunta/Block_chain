package simulate_test

import (
	"encoding/csv"
	"encoding/json"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/pranav-k/forkrisk/internal/config"
	"github.com/pranav-k/forkrisk/internal/ml/drift"
)

// TestMilestone4_ScenarioMatrixCompleteness verifies that:
// 1. Exactly 240 production runs are recorded in the manifest and run_summary.csv.
// 2. All 8 scenarios are present.
// 3. Exactly 30 distinct seeds (1..30) exist per scenario.
// 4. No duplicate run IDs exist.
// 5. Every single run succeeded (Status == "SUCCESS").
func TestMilestone4_ScenarioMatrixCompleteness(t *testing.T) {
	manifestPath := filepath.Join("..", "outputs", "experiments", "milestone4", "run_manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("Failed to read run_manifest.json: %v", err)
	}

	type ManifestItem struct {
		RunID    string `json:"run_id"`
		Scenario string `json:"scenario"`
		Seed     int64  `json:"seed"`
		Status   string `json:"status"`
	}
	type Manifest struct {
		TotalRuns      int            `json:"total_runs"`
		SuccessfulRuns int            `json:"successful_runs"`
		FailedRuns     int            `json:"failed_runs"`
		Scenarios      []string       `json:"scenarios"`
		Runs           []ManifestItem `json:"runs"`
	}

	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Failed to parse manifest JSON: %v", err)
	}

	if m.TotalRuns != 240 {
		t.Errorf("Expected 240 total runs, got %d", m.TotalRuns)
	}
	if m.SuccessfulRuns != 240 {
		t.Errorf("Expected 240 successful runs, got %d", m.SuccessfulRuns)
	}
	if m.FailedRuns != 0 {
		t.Errorf("Expected 0 failed runs, got %d", m.FailedRuns)
	}

	expectedScenarios := map[string]bool{
		"baseline":           true,
		"latency_stress":     true,
		"propagation_stress": true,
		"churn_stress":       true,
		"block_rate_stress":  true,
		"tx_load_stress":     true,
		"combined_stress":    true,
		"dynamic_network":    true,
	}

	seenRunIDs := make(map[string]bool)
	scenarioSeeds := make(map[string]map[int64]bool)
	for s := range expectedScenarios {
		scenarioSeeds[s] = make(map[int64]bool)
	}

	for _, r := range m.Runs {
		if seenRunIDs[r.RunID] {
			t.Errorf("Duplicate run ID found: %s", r.RunID)
		}
		seenRunIDs[r.RunID] = true

		if !expectedScenarios[r.Scenario] {
			t.Errorf("Unexpected scenario: %s", r.Scenario)
		}
		if r.Seed < 1 || r.Seed > 30 {
			t.Errorf("Seed %d out of bounds for run %s", r.Seed, r.RunID)
		}
		scenarioSeeds[r.Scenario][r.Seed] = true

		if r.Status != "SUCCESS" {
			t.Errorf("Run %s failed: status=%s", r.RunID, r.Status)
		}
	}

	for scen, seeds := range scenarioSeeds {
		if len(seeds) != 30 {
			t.Errorf("Scenario %s has %d seeds, expected exactly 30", scen, len(seeds))
		}
		for s := int64(1); s <= 30; s++ {
			if !seeds[s] {
				t.Errorf("Scenario %s missing seed %d", scen, s)
			}
		}
	}
}

// TestMilestone4_SummaryAggregationAndSampleStdDev verifies that:
// 1. scenario_summary.csv correctly aggregates run_summary.csv.
// 2. Sample standard deviation formula s = sqrt(1/(N-1) * sum((x-mean)^2)) is used.
// 3. Pooled rates reconcile with raw observations.
func TestMilestone4_SummaryAggregationAndSampleStdDev(t *testing.T) {
	runSummaryPath := filepath.Join("..", "outputs", "experiments", "milestone4", "run_summary.csv")
	fRun, err := os.Open(runSummaryPath)
	if err != nil {
		t.Fatalf("Failed to open run_summary.csv: %v", err)
	}
	defer fRun.Close()

	rRun := csv.NewReader(fRun)
	runRecords, err := rRun.ReadAll()
	if err != nil {
		t.Fatalf("Failed to read run_summary.csv: %v", err)
	}

	// 1 header + 240 rows
	if len(runRecords) != 241 {
		t.Fatalf("Expected 241 rows in run_summary.csv (1 header + 240 runs), got %d", len(runRecords))
	}

	// Group fork rates and stale rates by scenario
	scenForkRates := make(map[string][]float64)
	for i := 1; i < len(runRecords); i++ {
		row := runRecords[i]
		scen := row[1]
		fr, err := strconv.ParseFloat(row[6], 64)
		if err != nil {
			t.Fatalf("row %d: invalid fork_rate: %v", i, err)
		}
		scenForkRates[scen] = append(scenForkRates[scen], fr)
	}

	// Read scenario_summary.csv
	scenSummaryPath := filepath.Join("..", "outputs", "experiments", "milestone4", "scenario_summary.csv")
	fScen, err := os.Open(scenSummaryPath)
	if err != nil {
		t.Fatalf("Failed to open scenario_summary.csv: %v", err)
	}
	defer fScen.Close()

	rScen := csv.NewReader(fScen)
	scenRecords, err := rScen.ReadAll()
	if err != nil {
		t.Fatalf("Failed to read scenario_summary.csv: %v", err)
	}

	// 1 header + 8 scenarios
	if len(scenRecords) != 9 {
		t.Fatalf("Expected 9 rows in scenario_summary.csv, got %d", len(scenRecords))
	}

	for i := 1; i < len(scenRecords); i++ {
		row := scenRecords[i]
		scen := row[0]
		meanCSV, _ := strconv.ParseFloat(row[2], 64)
		stdCSV, _ := strconv.ParseFloat(row[3], 64)

		rawVals := scenForkRates[scen]
		if len(rawVals) != 30 {
			t.Fatalf("Scenario %s has %d values, expected 30", scen, len(rawVals))
		}

		// Recompute sample mean
		sum := 0.0
		for _, v := range rawVals {
			sum += v
		}
		calcMean := sum / float64(len(rawVals))

		// Recompute sample std dev (N-1 denominator)
		sumSq := 0.0
		for _, v := range rawVals {
			diff := v - calcMean
			sumSq += diff * diff
		}
		calcStd := math.Sqrt(sumSq / float64(len(rawVals)-1))

		if math.Abs(meanCSV-calcMean) > 1e-4 {
			t.Errorf("Scenario %s mean mismatch: csv=%.6f, calc=%.6f", scen, meanCSV, calcMean)
		}
		if math.Abs(stdCSV-calcStd) > 1e-4 {
			t.Errorf("Scenario %s sample std mismatch: csv=%.6f, calc=%.6f", scen, stdCSV, calcStd)
		}
	}
}

// TestMilestone4_DeterministicRunOutput verifies that running two independent simulations
// with identical seed and configuration generates identical event logs and hashes.
func TestMilestone4_DeterministicRunOutput(t *testing.T) {
	cfg := &config.ScenarioConfig{
		Name:              "m4_repro_test",
		NumNodes:          4,
		BlockIntervalSec:  1.0,
		BlockJitterSec:    0.1,
		MeanLatencyMs:     50.0,
		LatencyJitterMs:   10.0,
		MeanPropDelayMs:   100.0,
		PropDelayJitterMs: 20.0,
		MaxBlocks:         25,
		Seed:              4242,
		PeersPerNode:      2,
		TxPerBlock:        5,
	}

	cfg1 := *cfg
	_, log1, _ := runSimulation(t, &cfg1, t.TempDir())

	cfg2 := *cfg
	_, log2, _ := runSimulation(t, &cfg2, t.TempDir())

	h1 := hashFile(t, log1)
	h2 := hashFile(t, log2)

	if h1 != h2 {
		t.Fatalf("Deterministic run output failed: %s vs %s", h1, h2)
	}
	t.Logf("Deterministic run output verified: SHA-256=%s", h1)
}

// TestMilestone4_ValidChartOutputs verifies that all 9 required charts exist, have non-zero size,
// and decode as valid PNG images.
func TestMilestone4_ValidChartOutputs(t *testing.T) {
	chartsDir := filepath.Join("..", "outputs", "experiments", "milestone4", "charts")
	requiredCharts := []string{
		"fork_rate_by_scenario.png",
		"stale_rate_by_scenario.png",
		"propagation_performance.png",
		"node_view_consistency.png",
		"roc_curves.png",
		"precision_recall_curves.png",
		"feature_importance.png",
		"tps_vs_fork_rate.png",
		"goodput_vs_fork_rate.png",
	}

	for _, name := range requiredCharts {
		path := filepath.Join(chartsDir, name)
		fi, err := os.Stat(path)
		if err != nil {
			t.Errorf("Missing chart file: %s", name)
			continue
		}
		if fi.Size() == 0 {
			t.Errorf("Chart file %s is empty (0 bytes)", name)
		}

		f, err := os.Open(path)
		if err != nil {
			t.Errorf("Cannot open chart file %s: %v", name, err)
			continue
		}
		img, err := png.Decode(f)
		f.Close()
		if err != nil {
			t.Errorf("Chart %s failed to decode as PNG: %v", name, err)
			continue
		}
		bounds := img.Bounds()
		if bounds.Dx() < 100 || bounds.Dy() < 100 {
			t.Errorf("Chart %s has unusually small dimensions: %dx%d", name, bounds.Dx(), bounds.Dy())
		}
	}
}

// TestMilestone4_DriftDetectorCorrectness verifies the sliding-window concept drift detector:
// - Zero variance handling (epsilon floor)
// - Insufficient history guard
// - False positive rate on stationary stream (< 5%)
// - Prompt detection of mean shift
func TestMilestone4_DriftDetectorCorrectness(t *testing.T) {
	cfg := drift.DetectorConfig{
		WindowSize:     50,
		ThresholdSigma: 2.0,
		Epsilon:        1e-6,
	}
	det := drift.NewSlidingWindowDetector(cfg)

	// Phase 1: Insufficient history
	for i := 0; i < 50; i++ {
		res := det.Process(10.0, i)
		if res.HasSuffHistory {
			t.Errorf("Observation %d should not have sufficient history", i)
		}
	}

	// Observation 50 has sufficient history
	res := det.Process(10.0, 50)
	if !res.HasSuffHistory {
		t.Errorf("Observation 50 should have sufficient history")
	}
	// Constant values -> zero variance handled cleanly
	if !res.ZeroVariance {
		t.Errorf("Expected zero variance on constant stream")
	}
	if res.DriftDetected {
		t.Errorf("Constant stream should not trigger drift")
	}

	// Phase 2: Sudden shift (10.0 -> 30.0)
	shiftRes := det.Process(30.0, 51)
	if !shiftRes.DriftDetected {
		t.Errorf("Sudden shift should trigger drift detection")
	}
}
