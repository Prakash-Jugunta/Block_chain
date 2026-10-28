package drift

import (
	"math/rand/v2"
	"testing"
)

// TestInsufficientHistory verifies that before WindowSize samples are processed,
// HasSuffHistory is false and no false drift alarm is triggered.
func TestInsufficientHistory(t *testing.T) {
	cfg := DetectorConfig{
		WindowSize:     50,
		ThresholdSigma: 2.0,
		Epsilon:        1e-6,
	}
	det := NewSlidingWindowDetector(cfg)

	// Feed first 50 observations (indices 0..49)
	for i := 0; i < 50; i++ {
		res := det.Process(100.0, i)
		if res.HasSuffHistory {
			t.Fatalf("expected HasSuffHistory=false at step %d, got true", i)
		}
		if res.DriftDetected {
			t.Fatalf("unexpected drift alarm at step %d with insufficient history", i)
		}
	}

	// At 51st sample (index 50), history has 50 samples
	res50 := det.Process(100.0, 50)
	if !res50.HasSuffHistory {
		t.Fatalf("expected HasSuffHistory=true at step 50, got false")
	}
}

// TestZeroVarianceReferenceWindow verifies clean handling when historical values are identical.
func TestZeroVarianceReferenceWindow(t *testing.T) {
	cfg := DetectorConfig{
		WindowSize:     20,
		ThresholdSigma: 2.0,
		Epsilon:        1e-6,
	}
	det := NewSlidingWindowDetector(cfg)

	// Feed identical values
	for i := 0; i < 20; i++ {
		det.Process(50.0, i)
	}

	// Identical next value -> zero variance, no drift
	res := det.Process(50.0, 20)
	if !res.ZeroVariance {
		t.Errorf("expected ZeroVariance=true, got false")
	}
	if res.DriftDetected {
		t.Errorf("unexpected drift on identical value")
	}

	// Shifted next value -> zero variance, drift detected
	resShift := det.Process(60.0, 21)
	if !resShift.DriftDetected {
		t.Errorf("expected drift on shift from constant reference, got false")
	}
}

// TestStationaryFeatureSequence verifies that stationary data has a low false-positive rate (< 5%).
func TestStationaryFeatureSequence(t *testing.T) {
	// With threshold 2.2-2.5 sigma, theoretical FPR is < 3%
	cfg := DetectorConfig{
		WindowSize:     100,
		ThresholdSigma: 2.2,
		Epsilon:        1e-6,
	}
	det := NewSlidingWindowDetector(cfg)

	rng := rand.New(rand.NewPCG(42, 1))
	totalPoints := 1000
	alarms := 0
	validPoints := 0

	for i := 0; i < totalPoints; i++ {
		// Gaussian stationary distribution: mean=0.50, std=0.05
		val := 0.50 + rng.NormFloat64()*0.05
		res := det.Process(val, i)
		if res.HasSuffHistory {
			validPoints++
			if res.DriftDetected {
				alarms++
			}
		}
	}

	fpr := float64(alarms) / float64(validPoints)
	t.Logf("Stationary FPR: %.4f (%d alarms / %d points)", fpr, alarms, validPoints)
	if fpr > 0.05 {
		t.Errorf("expected stationary FPR < 0.05, got %.4f", fpr)
	}
}

// TestKnownSyntheticMeanShift verifies immediate detection upon a distribution shift.
func TestKnownSyntheticMeanShift(t *testing.T) {
	cfg := DetectorConfig{
		WindowSize:     50,
		ThresholdSigma: 2.0,
		Epsilon:        1e-6,
	}
	det := NewSlidingWindowDetector(cfg)

	rng := rand.New(rand.NewPCG(99, 1))

	// Phase 1: mean 50.0, std 5.0
	for i := 0; i < 50; i++ {
		val := 50.0 + rng.NormFloat64()*5.0
		det.Process(val, i)
	}

	// Phase 2: sudden shift to 100.0 (10 standard deviations shift)
	shiftDetected := false
	for i := 50; i < 60; i++ {
		val := 100.0 + rng.NormFloat64()*5.0
		res := det.Process(val, i)
		if res.DriftDetected {
			shiftDetected = true
			t.Logf("Shift detected at block %d (Z-Score: %.2f)", res.BlockHeight, res.ZScore)
			break
		}
	}

	if !shiftDetected {
		t.Fatalf("failed to detect known synthetic mean shift of 10 std deviations")
	}
}

// TestDynamicTransitionAtBlock5000 verifies detection timing and delay for block 5,000 transition.
func TestDynamicTransitionAtBlock5000(t *testing.T) {
	// Using tuned parameter (W=100, θ=2.5) to maintain FPR < 5% on pre-transition
	cfg := DetectorConfig{WindowSize: 100, ThresholdSigma: 2.5, Epsilon: 1e-6}
	det := NewSlidingWindowDetector(cfg)

	rng := rand.New(rand.NewPCG(1234, 5678))

	// Pre-transition: blocks 1 to 4,999 with baseline latency ~0.05s
	firstAlarm := -1
	preTransitionAlarms := 0

	for h := 1; h < 5000; h++ {
		val := 0.050 + rng.NormFloat64()*0.005
		res := det.Process(val, h)
		if res.HasSuffHistory && res.DriftDetected {
			preTransitionAlarms++
		}
	}

	// Post-transition: blocks 5,000+ with shifted latency ~0.300s (6x shift)
	for h := 5000; h <= 5100; h++ {
		val := 0.300 + rng.NormFloat64()*0.030
		res := det.Process(val, h)
		if res.DriftDetected && firstAlarm < 0 {
			firstAlarm = h
		}
	}

	preTransitionFPR := float64(preTransitionAlarms) / float64(5000-cfg.WindowSize)
	t.Logf("Pre-transition FPR (blocks 101–4999): %.4f (%d alarms)", preTransitionFPR, preTransitionAlarms)
	if preTransitionFPR > 0.05 {
		t.Errorf("pre-transition FPR exceeds 5%%: %.4f", preTransitionFPR)
	}

	if firstAlarm < 0 {
		t.Fatalf("failed to detect dynamic transition at or after block 5000")
	}

	delay := firstAlarm - 5000
	t.Logf("First alarm at block %d (Detection delay: %d blocks)", firstAlarm, delay)
	if delay < 0 || delay > 10 {
		t.Errorf("expected prompt detection within 10 blocks, got delay %d", delay)
	}
}

// TestDeterministicRepeatedExecution proves that repeating the detector on the same input
// produces identical results every time.
func TestDeterministicRepeatedExecution(t *testing.T) {
	cfg := DefaultConfig()
	det1 := NewSlidingWindowDetector(cfg)
	det2 := NewSlidingWindowDetector(cfg)

	vals := []float64{1.0, 1.2, 1.1, 0.9, 1.0, 5.0, 5.2, 5.1}
	heights := []int{10, 11, 12, 13, 14, 15, 16, 17}

	res1 := det1.EvaluateStream(vals, heights)
	res2 := det2.EvaluateStream(vals, heights)

	if len(res1) != len(res2) {
		t.Fatalf("different result lengths")
	}
	for i := range res1 {
		if res1[i].DriftDetected != res2[i].DriftDetected ||
			res1[i].ZScore != res2[i].ZScore ||
			res1[i].RefMean != res2[i].RefMean {
			t.Errorf("mismatch at index %d: %+v vs %+v", i, res1[i], res2[i])
		}
	}
}

// TestNoFutureDataLeakage proves that appending future values does not alter past decisions.
func TestNoFutureDataLeakage(t *testing.T) {
	cfg := DetectorConfig{WindowSize: 10, ThresholdSigma: 2.0, Epsilon: 1e-6}

	// Stream A: 15 samples
	valsA := make([]float64, 15)
	for i := 0; i < 15; i++ {
		valsA[i] = float64(i)
	}

	detA := NewSlidingWindowDetector(cfg)
	resA := detA.EvaluateStream(valsA, nil)

	// Stream B: 15 identical samples followed by a massive future spike
	valsB := append(append([]float64(nil), valsA...), 9999.0, 9999.0, 9999.0)

	detB := NewSlidingWindowDetector(cfg)
	resB := detB.EvaluateStream(valsB, nil)

	// Past 15 decisions must be bitwise identical
	for i := 0; i < 15; i++ {
		if resA[i].DriftDetected != resB[i].DriftDetected ||
			resA[i].ZScore != resB[i].ZScore ||
			resA[i].RefMean != resB[i].RefMean {
			t.Fatalf("leakage detected at index %d: future data influenced past decision", i)
		}
	}
}

// TestGridTuneValidation tests the parameter selection logic on synthetic streams.
func TestGridTuneValidation(t *testing.T) {
	// Stationary stream: 500 samples
	statVals := make([]float64, 500)
	statHeights := make([]int, 500)
	rng := rand.New(rand.NewPCG(111, 222))
	for i := 0; i < 500; i++ {
		statVals[i] = 10.0 + rng.NormFloat64()*0.5
		statHeights[i] = i
	}

	// Transition stream: 500 samples with shift at 250
	transVals := make([]float64, 500)
	transHeights := make([]int, 500)
	for i := 0; i < 500; i++ {
		transHeights[i] = i
		if i < 250 {
			transVals[i] = 10.0 + rng.NormFloat64()*0.5
		} else {
			transVals[i] = 25.0 + rng.NormFloat64()*0.5
		}
	}

	windowSizes := []int{50, 100, 200}
	thresholds := []float64{1.5, 2.0, 2.5}

	bestCfg, candidates, err := GridTune(windowSizes, thresholds, statVals, statHeights, transVals, transHeights, 250)
	if err != nil {
		t.Fatalf("GridTune error: %v", err)
	}

	if len(candidates) != 9 {
		t.Fatalf("expected 9 candidates, got %d", len(candidates))
	}

	t.Logf("Selected Best Config: Window=%d, Threshold=%.1f", bestCfg.WindowSize, bestCfg.ThresholdSigma)
	if bestCfg.WindowSize <= 0 || bestCfg.ThresholdSigma <= 0 {
		t.Errorf("invalid best config: %+v", bestCfg)
	}
}
