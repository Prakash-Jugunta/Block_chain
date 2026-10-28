// Package drift implements a deterministic sliding-window concept drift detector
// for online monitoring of blockchain feature streams, following §6 of CASE_STUDY_DEMO_PLAN.md.
package drift

import (
	"fmt"
	"math"
)

// DetectorConfig defines the operational parameters for the sliding-window drift detector.
type DetectorConfig struct {
	// WindowSize (W) is the number of historical observations used to compute reference statistics.
	WindowSize int `json:"window_size"`

	// ThresholdSigma (θ) is the threshold multiplier in units of standard deviations from reference mean.
	ThresholdSigma float64 `json:"threshold_sigma"`

	// Epsilon is the minimum variance floor to prevent division by zero in zero-variance regimes.
	Epsilon float64 `json:"epsilon"`
}

// DefaultConfig returns the conservative defaults specified in §6:
// WindowSize = 100 observations, ThresholdSigma = 2.0 std deviations.
func DefaultConfig() DetectorConfig {
	return DetectorConfig{
		WindowSize:     100,
		ThresholdSigma: 2.0,
		Epsilon:        1e-6,
	}
}

// DriftResult represents the detection output at a single observation point.
type DriftResult struct {
	Index          int     `json:"index"`
	BlockHeight    int     `json:"block_height"`
	Value          float64 `json:"value"`
	RefMean        float64 `json:"ref_mean"`
	RefStd         float64 `json:"ref_std"`
	ZScore         float64 `json:"z_score"`
	DriftDetected  bool    `json:"drift_detected"`
	HasSuffHistory bool    `json:"has_sufficient_history"`
	ZeroVariance   bool    `json:"zero_variance"`
}

// SlidingWindowDetector monitors a numerical stream and detects distribution shifts
// without observing future data.
type SlidingWindowDetector struct {
	cfg       DetectorConfig
	history   []float64
	currIndex int
}

// NewSlidingWindowDetector creates a detector with the given configuration.
func NewSlidingWindowDetector(cfg DetectorConfig) *SlidingWindowDetector {
	if cfg.WindowSize <= 1 {
		cfg.WindowSize = 100
	}
	if cfg.ThresholdSigma <= 0 {
		cfg.ThresholdSigma = 2.0
	}
	if cfg.Epsilon <= 0 {
		cfg.Epsilon = 1e-6
	}
	return &SlidingWindowDetector{
		cfg:     cfg,
		history: make([]float64, 0, cfg.WindowSize*2),
	}
}

// Reset clears the detector history.
func (d *SlidingWindowDetector) Reset() {
	d.history = d.history[:0]
	d.currIndex = 0
}

// Process processes one observation in chronological sequence and evaluates drift.
// blockHeight is the associated block height.
func (d *SlidingWindowDetector) Process(val float64, blockHeight int) DriftResult {
	idx := d.currIndex
	d.currIndex++

	// Check for insufficient history
	if len(d.history) < d.cfg.WindowSize {
		d.history = append(d.history, val)
		return DriftResult{
			Index:          idx,
			BlockHeight:    blockHeight,
			Value:          val,
			HasSuffHistory: false,
			DriftDetected:  false,
		}
	}

	// Compute reference window statistics across the trailing W observations
	w := d.cfg.WindowSize
	refSlice := d.history[len(d.history)-w:]

	var sum float64
	for _, v := range refSlice {
		sum += v
	}
	mean := sum / float64(w)

	var sumSq float64
	for _, v := range refSlice {
		diff := v - mean
		sumSq += diff * diff
	}
	variance := sumSq / float64(w)
	std := math.Sqrt(variance)

	zeroVar := variance < (d.cfg.Epsilon * d.cfg.Epsilon)

	var zScore float64
	var drift bool

	if zeroVar {
		// Zero variance handling: if reference was constant, any change larger than epsilon is drift
		absDiff := math.Abs(val - mean)
		if absDiff > d.cfg.Epsilon {
			zScore = absDiff / d.cfg.Epsilon
			drift = true
		} else {
			zScore = 0.0
			drift = false
		}
	} else {
		// Standard deviation thresholding
		effectiveStd := math.Max(std, d.cfg.Epsilon)
		zScore = math.Abs(val-mean) / effectiveStd
		drift = zScore >= d.cfg.ThresholdSigma
	}

	// Append current value to rolling history
	d.history = append(d.history, val)
	// Prune history to avoid unbounded growth while preserving the reference window
	if len(d.history) > d.cfg.WindowSize*2 {
		copy(d.history, d.history[len(d.history)-d.cfg.WindowSize:])
		d.history = d.history[:d.cfg.WindowSize]
	}

	return DriftResult{
		Index:          idx,
		BlockHeight:    blockHeight,
		Value:          val,
		RefMean:        mean,
		RefStd:         std,
		ZScore:         zScore,
		DriftDetected:  drift,
		HasSuffHistory: true,
		ZeroVariance:   zeroVar,
	}
}

// EvaluateStream processes an entire sequence and returns all results.
func (d *SlidingWindowDetector) EvaluateStream(vals []float64, heights []int) []DriftResult {
	d.Reset()
	results := make([]DriftResult, len(vals))
	for i, v := range vals {
		h := i
		if i < len(heights) {
			h = heights[i]
		}
		results[i] = d.Process(v, h)
	}
	return results
}

// TuningCandidate represents one hyperparameter configuration in the validation tuning grid.
type TuningCandidate struct {
	WindowSize     int     `json:"window_size"`
	ThresholdSigma float64 `json:"threshold_sigma"`
	FPRStationary  float64 `json:"fpr_stationary"`
	Precision      float64 `json:"precision"`
	Recall         float64 `json:"recall"`
	F1Score        float64 `json:"f1_score"`
	DetectionDelay int     `json:"detection_delay"`
	Eligible       bool    `json:"eligible"`
}

// GridTune evaluates candidates on validation data and returns the optimal configuration.
// stationaryVals: values from stationary validation runs (ground truth = no drift).
// transitionVals: values from dynamic transition validation runs (transition at transitionBlock).
func GridTune(
	windowSizes []int,
	thresholds []float64,
	stationaryVals []float64,
	stationaryHeights []int,
	transitionVals []float64,
	transitionHeights []int,
	transitionBlock int,
) (DetectorConfig, []TuningCandidate, error) {
	var candidates []TuningCandidate
	bestF1 := -1.0
	var bestConfig DetectorConfig

	for _, w := range windowSizes {
		for _, th := range thresholds {
			cfg := DetectorConfig{
				WindowSize:     w,
				ThresholdSigma: th,
				Epsilon:        1e-6,
			}

			// 1. Evaluate on stationary validation stream (measure FPR)
			detStat := NewSlidingWindowDetector(cfg)
			statRes := detStat.EvaluateStream(stationaryVals, stationaryHeights)

			evalCount := 0
			fpCount := 0
			for _, r := range statRes {
				if r.HasSuffHistory {
					evalCount++
					if r.DriftDetected {
						fpCount++
					}
				}
			}

			fpr := 0.0
			if evalCount > 0 {
				fpr = float64(fpCount) / float64(evalCount)
			}

			// 2. Evaluate on transition validation stream (measure detection F1 & delay)
			detTrans := NewSlidingWindowDetector(cfg)
			transRes := detTrans.EvaluateStream(transitionVals, transitionHeights)

			tp := 0
			fp := 0
			fn := 0
			firstDetection := -1

			for _, r := range transRes {
				if !r.HasSuffHistory {
					continue
				}
				isPostTransition := r.BlockHeight >= transitionBlock

				if isPostTransition {
					if r.DriftDetected {
						tp++
						if firstDetection < 0 {
							firstDetection = r.BlockHeight
						}
					} else {
						fn++
					}
				} else {
					if r.DriftDetected {
						fp++
					}
				}
			}

			prec := 0.0
			if tp+fp > 0 {
				prec = float64(tp) / float64(tp+fp)
			}
			rec := 0.0
			if tp+fn > 0 {
				rec = float64(tp) / float64(tp+fn)
			}
			f1 := 0.0
			if prec+rec > 0 {
				f1 = 2 * (prec * rec) / (prec + rec)
			}

			delay := -1
			if firstDetection >= transitionBlock {
				delay = firstDetection - transitionBlock
			}

			eligible := fpr < 0.05
			candidate := TuningCandidate{
				WindowSize:     w,
				ThresholdSigma: th,
				FPRStationary:  fpr,
				Precision:      prec,
				Recall:         rec,
				F1Score:        f1,
				DetectionDelay: delay,
				Eligible:       eligible,
			}
			candidates = append(candidates, candidate)

			// Select best candidate based on F1 among eligible (FPR < 5%) candidates
			if eligible && f1 > bestF1 {
				bestF1 = f1
				bestConfig = cfg
			}
		}
	}

	if bestF1 < 0 {
		// Fallback to default if no candidate achieved FPR < 5%
		bestConfig = DefaultConfig()
		return bestConfig, candidates, fmt.Errorf("no candidate met FPR < 5%% constraint; defaulting to %v", bestConfig)
	}

	return bestConfig, candidates, nil
}
