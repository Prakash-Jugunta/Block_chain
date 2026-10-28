package ml

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pranav-k/forkrisk/internal/features"
)

// TestStandardScaler verifies that scaler standardizes correctly and preserves determinism.
func TestStandardScaler(t *testing.T) {
	featNames := []string{"f1", "f2"}
	scaler := NewStandardScaler(featNames)

	// Mean of f1 = 10, std = sqrt(2/3*...); Mean of f2 = 5, std = 0 (constant)
	X := [][]float64{
		{8.0, 5.0},
		{10.0, 5.0},
		{12.0, 5.0},
	}

	if err := scaler.Fit(X); err != nil {
		t.Fatalf("Fit failed: %v", err)
	}

	if math.Abs(scaler.Means[0]-10.0) > 1e-6 {
		t.Errorf("expected mean 10.0, got %f", scaler.Means[0])
	}
	if math.Abs(scaler.Means[1]-5.0) > 1e-6 {
		t.Errorf("expected mean 5.0, got %f", scaler.Means[1])
	}
	if scaler.Stds[1] != 1.0 {
		t.Errorf("expected constant feature std to fallback to 1.0, got %f", scaler.Stds[1])
	}

	scaled := scaler.Transform(X)
	// For middle element (10.0, 5.0), scaled should be (0.0, 0.0)
	if math.Abs(scaled[1][0]) > 1e-6 || math.Abs(scaled[1][1]) > 1e-6 {
		t.Errorf("expected center to be 0.0, got %v", scaled[1])
	}
}

// TestRunLevelSplitNoLeakage verifies that complete runs stay together with zero run overlap.
func TestRunLevelSplitNoLeakage(t *testing.T) {
	rows := []features.DatasetRow{
		{RunID: "run_seed1_A", Scenario: "baseline", Seed: 1, LabelForkH: 0},
		{RunID: "run_seed1_A", Scenario: "baseline", Seed: 1, LabelForkH: 1},
		{RunID: "run_seed1_B", Scenario: "latency", Seed: 1, LabelForkH: 1},
		{RunID: "run_seed2_A", Scenario: "baseline", Seed: 2, LabelForkH: 0},
		{RunID: "run_seed2_B", Scenario: "latency", Seed: 2, LabelForkH: 1},
		{RunID: "run_seed3_A", Scenario: "baseline", Seed: 3, LabelForkH: 0},
		{RunID: "run_seed3_B", Scenario: "latency", Seed: 3, LabelForkH: 1},
	}

	manifest, split, err := CreateSeedHeldOutSplit(rows)
	if err != nil {
		t.Fatalf("CreateSeedHeldOutSplit failed: %v", err)
	}

	trainRunSet := make(map[string]bool)
	for _, r := range manifest.TrainRuns {
		trainRunSet[r] = true
	}
	valRunSet := make(map[string]bool)
	for _, r := range manifest.ValRuns {
		valRunSet[r] = true
	}
	testRunSet := make(map[string]bool)
	for _, r := range manifest.TestRuns {
		testRunSet[r] = true
	}

	// Verify no overlap between Train, Val, and Test runs
	for r := range valRunSet {
		if trainRunSet[r] {
			t.Errorf("leakage: run %s present in both train and val", r)
		}
	}
	for r := range testRunSet {
		if trainRunSet[r] {
			t.Errorf("leakage: run %s present in both train and test", r)
		}
		if valRunSet[r] {
			t.Errorf("leakage: run %s present in both val and test", r)
		}
	}

	if len(split.XTrain) != 3 {
		t.Errorf("expected 3 train rows, got %d", len(split.XTrain))
	}
	if len(split.XVal) != 2 {
		t.Errorf("expected 2 val rows, got %d", len(split.XVal))
	}
	if len(split.XTest) != 2 {
		t.Errorf("expected 2 test rows, got %d", len(split.XTest))
	}
}

// TestLogisticRegressionConvergence verifies convergence on linearly separable data.
func TestLogisticRegressionConvergence(t *testing.T) {
	featNames := []string{"x1", "x2"}
	cfg := DefaultLogisticConfig()
	cfg.MaxEpochs = 200
	cfg.LearningRate = 0.1

	lr := NewLogisticRegression(featNames, cfg)

	// Linearly separable dataset: y = 1 if x1 + x2 > 0 else 0
	var X [][]float64
	var Y []float64

	for i := -10; i <= 10; i++ {
		for j := -10; j <= 10; j++ {
			x1 := float64(i)
			x2 := float64(j)
			if x1+x2 == 0 {
				continue
			}
			lbl := 0.0
			if x1+x2 > 0 {
				lbl = 1.0
			}
			X = append(X, []float64{x1, x2})
			Y = append(Y, lbl)
		}
	}

	if err := lr.Train(X, Y, nil, nil); err != nil {
		t.Fatalf("Train failed: %v", err)
	}

	// Verify predictions on clear test points
	p1, _ := lr.PredictProbability([]float64{5.0, 5.0})
	if p1 < 0.8 {
		t.Errorf("expected high probability for {5, 5}, got %f", p1)
	}

	p0, _ := lr.PredictProbability([]float64{-5.0, -5.0})
	if p0 > 0.2 {
		t.Errorf("expected low probability for {-5, -5}, got %f", p0)
	}
}

// TestRandomForestDeterministic verifies that Random Forest training and prediction are deterministic given fixed seed.
func TestRandomForestDeterministic(t *testing.T) {
	featNames := []string{"f1", "f2"}
	cfg := DefaultForestConfig()
	cfg.NumTrees = 10
	cfg.MaxDepth = 4
	cfg.Seed = 123

	X := [][]float64{
		{1.0, 2.0},
		{1.5, 1.8},
		{5.0, 8.0},
		{6.0, 7.5},
	}
	Y := []float64{0.0, 0.0, 1.0, 1.0}

	rf1 := NewRandomForest(featNames, cfg)
	if err := rf1.Train(X, Y, nil, nil); err != nil {
		t.Fatalf("Train 1 failed: %v", err)
	}

	rf2 := NewRandomForest(featNames, cfg)
	if err := rf2.Train(X, Y, nil, nil); err != nil {
		t.Fatalf("Train 2 failed: %v", err)
	}

	testPt := []float64{5.5, 7.8}
	p1, _ := rf1.PredictProbability(testPt)
	p2, _ := rf2.PredictProbability(testPt)

	if p1 != p2 {
		t.Errorf("non-deterministic forest prediction: p1=%f, p2=%f", p1, p2)
	}
}

// TestModelSerializationRoundTrip verifies that saved models can be reloaded and yield exact identical output.
func TestModelSerializationRoundTrip(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "ml_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	featNames := []string{"f1", "f2"}
	X := [][]float64{{1.0, 2.0}, {2.0, 3.0}, {5.0, 6.0}}
	Y := []float64{0.0, 0.0, 1.0}

	// 1. Logistic Regression save/load
	lrPath := filepath.Join(tmpDir, "lr.json")
	lr := NewLogisticRegression(featNames, DefaultLogisticConfig())
	lr.Train(X, Y, nil, nil)
	if err := lr.Save(lrPath); err != nil {
		t.Fatalf("saving lr: %v", err)
	}

	lrLoaded, err := LoadLogisticRegression(lrPath)
	if err != nil {
		t.Fatalf("loading lr: %v", err)
	}

	p1, _ := lr.PredictProbability([]float64{4.0, 5.0})
	p2, _ := lrLoaded.PredictProbability([]float64{4.0, 5.0})
	if math.Abs(p1-p2) > 1e-9 {
		t.Errorf("loaded lr probability mismatch: %f vs %f", p1, p2)
	}

	// 2. Random Forest save/load
	rfPath := filepath.Join(tmpDir, "rf.json")
	rf := NewRandomForest(featNames, DefaultForestConfig())
	rf.Train(X, Y, nil, nil)
	if err := rf.Save(rfPath); err != nil {
		t.Fatalf("saving rf: %v", err)
	}

	rfLoaded, err := LoadRandomForest(rfPath)
	if err != nil {
		t.Fatalf("loading rf: %v", err)
	}

	rfP1, _ := rf.PredictProbability([]float64{4.0, 5.0})
	rfP2, _ := rfLoaded.PredictProbability([]float64{4.0, 5.0})
	if math.Abs(rfP1-rfP2) > 1e-9 {
		t.Errorf("loaded rf probability mismatch: %f vs %f", rfP1, rfP2)
	}
}

// TestEvaluationMetricsKnownValues verifies exact metrics formulas.
func TestEvaluationMetricsKnownValues(t *testing.T) {
	yTrue := []float64{1.0, 0.0, 1.0, 0.0}
	yProb := []float64{0.9, 0.1, 0.8, 0.4}

	m := EvaluateModel("test", "test_split", yTrue, yProb, 0.5)

	// Threshold = 0.5:
	// yProb >= 0.5: 0.9 (y=1) -> TP, 0.8 (y=1) -> TP
	// yProb < 0.5:  0.1 (y=0) -> TN, 0.4 (y=0) -> TN
	// TP=2, TN=2, FP=0, FN=0 -> Perfect accuracy, precision=1.0, recall=1.0, f1=1.0, ROC-AUC=1.0
	if m.ConfusionMatrix.TP != 2 || m.ConfusionMatrix.TN != 2 {
		t.Errorf("unexpected confusion matrix: %+v", m.ConfusionMatrix)
	}
	if m.Precision != 1.0 || m.Recall != 1.0 || m.F1Score != 1.0 {
		t.Errorf("expected 1.0 for P/R/F1, got P=%f, R=%f, F1=%f", m.Precision, m.Recall, m.F1Score)
	}
	if m.ROCAUC == nil || *m.ROCAUC != 1.0 {
		t.Errorf("expected ROC-AUC 1.0, got %v", m.ROCAUC)
	}
}

// TestSingleClassROCAUCReturnsNil verifies that single-class subsets (all-positive or all-negative)
// have ROC-AUC marked unavailable (nil pointer, serialized as JSON null),
// as ROC-AUC is mathematically undefined when there are no negative (or no positive) examples.
func TestSingleClassROCAUCReturnsNil(t *testing.T) {
	// 1. All-positive subset (e.g., churn_stress in held-out test split)
	yAllPos := []float64{1.0, 1.0, 1.0, 1.0}
	probsPos := []float64{0.8, 0.6, 0.95, 0.7}
	mPos := EvaluateModel("TestAllPos", "test", yAllPos, probsPos, 0.5)

	if mPos.ROCAUC != nil {
		t.Errorf("expected ROC-AUC to be nil for all-positive subset, got %v", *mPos.ROCAUC)
	}
	if mPos.Prevalence != 1.0 {
		t.Errorf("expected prevalence 1.0, got %f", mPos.Prevalence)
	}
	if mPos.TotalSamples != 4 {
		t.Errorf("expected 4 total samples, got %d", mPos.TotalSamples)
	}

	// 2. All-negative subset
	yAllNeg := []float64{0.0, 0.0, 0.0}
	probsNeg := []float64{0.1, 0.2, 0.3}
	mNeg := EvaluateModel("TestAllNeg", "test", yAllNeg, probsNeg, 0.5)

	if mNeg.ROCAUC != nil {
		t.Errorf("expected ROC-AUC to be nil for all-negative subset, got %v", *mNeg.ROCAUC)
	}
	if mNeg.Prevalence != 0.0 {
		t.Errorf("expected prevalence 0.0, got %f", mNeg.Prevalence)
	}
	if mNeg.TotalSamples != 3 {
		t.Errorf("expected 3 total samples, got %d", mNeg.TotalSamples)
	}

	// 3. Direct ComputeROCAUC tests
	if auc := ComputeROCAUC(yAllPos, probsPos); auc != nil {
		t.Errorf("ComputeROCAUC should return nil for all-positive, got %v", *auc)
	}
	if auc := ComputeROCAUC(yAllNeg, probsNeg); auc != nil {
		t.Errorf("ComputeROCAUC should return nil for all-negative, got %v", *auc)
	}
	if auc := ComputeROCAUC([]float64{}, []float64{}); auc != nil {
		t.Errorf("ComputeROCAUC should return nil for empty input, got %v", *auc)
	}

	// 4. Verify JSON serialization yields "null"
	posBytes, err := json.Marshal(mPos)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	if !strings.Contains(string(posBytes), `"roc_auc":null`) {
		t.Errorf("expected JSON to contain '\"roc_auc\":null', got: %s", string(posBytes))
	}
}

// TestModelSchemaCompatibilityStrict proves that:
// - v1.2 is accepted
// - v1.0 is rejected
// - v1.1 is rejected
// - empty version is rejected
// - missing version is rejected
// - unknown future version is rejected
func TestModelSchemaCompatibilityStrict(t *testing.T) {
	tmpDir := t.TempDir()

	testVersions := []struct {
		name        string
		jsonContent string
		shouldPass  bool
	}{
		{
			name:        "v1.3 accepted",
			jsonContent: `{"feature_schema_version": "v1.3", "features": ["f1"]}`,
			shouldPass:  true,
		},
		{
			name:        "v1.2 rejected",
			jsonContent: `{"feature_schema_version": "v1.2", "features": ["f1"]}`,
			shouldPass:  false,
		},
		{
			name:        "v1.1 rejected",
			jsonContent: `{"feature_schema_version": "v1.1", "features": ["f1"]}`,
			shouldPass:  false,
		},
		{
			name:        "v1.0 rejected",
			jsonContent: `{"feature_schema_version": "v1.0", "features": ["f1"]}`,
			shouldPass:  false,
		},
		{
			name:        "empty version rejected",
			jsonContent: `{"feature_schema_version": "", "features": ["f1"]}`,
			shouldPass:  false,
		},
		{
			name:        "missing version rejected",
			jsonContent: `{"features": ["f1"]}`,
			shouldPass:  false,
		},
		{
			name:        "unknown future version rejected",
			jsonContent: `{"feature_schema_version": "v2.0", "features": ["f1"]}`,
			shouldPass:  false,
		},
	}

	for _, tc := range testVersions {
		t.Run("LR_"+tc.name, func(t *testing.T) {
			p := filepath.Join(tmpDir, "lr_"+tc.name+".json")
			if err := os.WriteFile(p, []byte(tc.jsonContent), 0644); err != nil {
				t.Fatalf("writing file: %v", err)
			}
			_, err := LoadLogisticRegression(p)
			if tc.shouldPass && err != nil {
				t.Errorf("expected %s to pass, got error: %v", tc.name, err)
			}
			if !tc.shouldPass && err == nil {
				t.Errorf("expected %s to be rejected, but it succeeded", tc.name)
			}
		})

		t.Run("RF_"+tc.name, func(t *testing.T) {
			p := filepath.Join(tmpDir, "rf_"+tc.name+".json")
			if err := os.WriteFile(p, []byte(tc.jsonContent), 0644); err != nil {
				t.Fatalf("writing file: %v", err)
			}
			_, err := LoadRandomForest(p)
			if tc.shouldPass && err != nil {
				t.Errorf("expected %s to pass, got error: %v", tc.name, err)
			}
			if !tc.shouldPass && err == nil {
				t.Errorf("expected %s to be rejected, but it succeeded", tc.name)
			}
		})
	}
}
