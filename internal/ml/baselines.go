package ml

import (
	"github.com/pranav-k/forkrisk/internal/features"
)

// ConstantNoForkBaseline always predicts probability 0.0 (predicting no fork / negative class).
type ConstantNoForkBaseline struct {
	Threshold float64
}

// NewConstantNoForkBaseline creates an always-no-fork baseline predictor.
func NewConstantNoForkBaseline() *ConstantNoForkBaseline {
	return &ConstantNoForkBaseline{
		Threshold: 0.5,
	}
}

// PredictProbability returns 0.0 for all observations.
func (b *ConstantNoForkBaseline) PredictProbability(_ []float64) float64 {
	return 0.0
}

// PredictClass returns binary class 0 (no fork) always.
func (b *ConstantNoForkBaseline) PredictClass(_ []float64) int {
	return 0
}

// PrevalenceBaseline predicts a constant probability matching the training prevalence.
type PrevalenceBaseline struct {
	TrainPrevalence float64
	Threshold       float64
}

// NewPrevalenceBaseline creates a baseline initialized with training prevalence.
func NewPrevalenceBaseline(trainPrevalence float64) *PrevalenceBaseline {
	return &PrevalenceBaseline{
		TrainPrevalence: trainPrevalence,
		Threshold:       0.5,
	}
}

// PredictProbability returns the constant prior probability.
func (b *PrevalenceBaseline) PredictProbability(_ []float64) float64 {
	return b.TrainPrevalence
}

// PredictClass returns binary class based on threshold 0.5.
func (b *PrevalenceBaseline) PredictClass(_ []float64) int {
	if b.TrainPrevalence >= b.Threshold {
		return 1
	}
	return 0
}

// HeuristicBaseline is a causal rule-based predictor using recent fork history and tip divergence.
type HeuristicBaseline struct {
	Features   []string
	featIdxMap map[string]int
	Threshold  float64
}

// NewHeuristicBaseline creates a rule-based baseline predictor.
func NewHeuristicBaseline(featNames []string) *HeuristicBaseline {
	fMap := make(map[string]int)
	for i, f := range featNames {
		fMap[f] = i
	}
	return &HeuristicBaseline{
		Features:   featNames,
		featIdxMap: fMap,
		Threshold:  0.5,
	}
}

// PredictProbability implements the heuristic risk assessment:
// If competing tips > 1 or recent forks > 0 or recent reorgs > 0 or tip height variance > 0:
// High risk (0.80), otherwise low risk (0.20).
func (h *HeuristicBaseline) PredictProbability(x []float64) float64 {
	compTipsIdx := h.featIdxMap[features.FeatCompetingTips]
	forkCountIdx := h.featIdxMap[features.FeatForkCount10]
	reorgCountIdx := h.featIdxMap[features.FeatReorgCount10]
	tipVarIdx := h.featIdxMap[features.FeatTipHeightVar]

	compTips := x[compTipsIdx]
	forkCount := x[forkCountIdx]
	reorgCount := x[reorgCountIdx]
	tipVar := x[tipVarIdx]

	if compTips > 1.0 || forkCount > 0.0 || reorgCount > 0.0 || tipVar > 0.1 {
		return 0.80
	}
	return 0.20
}

// PredictClass returns binary prediction.
func (h *HeuristicBaseline) PredictClass(x []float64) int {
	if h.PredictProbability(x) >= h.Threshold {
		return 1
	}
	return 0
}
