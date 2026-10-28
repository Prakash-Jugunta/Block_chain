package ml

import (
	"math"
	"sort"
)

// ConfusionMatrix stores binary classification outcomes.
type ConfusionMatrix struct {
	TP int `json:"tp"`
	FP int `json:"fp"`
	TN int `json:"tn"`
	FN int `json:"fn"`
}

// EvaluationMetrics holds the comprehensive model evaluation results.
type EvaluationMetrics struct {
	ModelName       string          `json:"model_name"`
	DatasetSplit    string          `json:"dataset_split"`
	TotalSamples    int             `json:"total_samples"`
	Prevalence      float64         `json:"prevalence"`
	Threshold       float64         `json:"threshold"`
	Accuracy        float64         `json:"accuracy"`
	Precision       float64         `json:"precision"`
	Recall          float64         `json:"recall"`
	F1Score         float64         `json:"f1_score"`
	ROCAUC          *float64        `json:"roc_auc"`
	PRAUC           float64         `json:"pr_auc"`
	BrierScore      float64         `json:"brier_score"`
	ConfusionMatrix ConfusionMatrix `json:"confusion_matrix"`
}

// EvaluateModel computes all evaluation metrics for predicted probabilities against binary ground-truth labels.
func EvaluateModel(modelName, splitName string, yTrue []float64, yProb []float64, threshold float64) EvaluationMetrics {
	n := len(yTrue)
	if n == 0 {
		return EvaluationMetrics{ModelName: modelName, DatasetSplit: splitName}
	}

	cm := ConfusionMatrix{}
	var posCount float64
	var brierSum float64

	for i := 0; i < n; i++ {
		yt := yTrue[i]
		yp := yProb[i]

		if yt == 1.0 {
			posCount++
		}

		diff := yp - yt
		brierSum += diff * diff

		predClass := 0.0
		if yp >= threshold {
			predClass = 1.0
		}

		if predClass == 1.0 && yt == 1.0 {
			cm.TP++
		} else if predClass == 1.0 && yt == 0.0 {
			cm.FP++
		} else if predClass == 0.0 && yt == 0.0 {
			cm.TN++
		} else {
			cm.FN++
		}
	}

	prevalence := posCount / float64(n)
	brier := brierSum / float64(n)

	acc := float64(cm.TP+cm.TN) / float64(n)

	var prec float64
	if cm.TP+cm.FP > 0 {
		prec = float64(cm.TP) / float64(cm.TP+cm.FP)
	}

	var rec float64
	if cm.TP+cm.FN > 0 {
		rec = float64(cm.TP) / float64(cm.TP+cm.FN)
	}

	var f1 float64
	if prec+rec > 0 {
		f1 = 2 * (prec * rec) / (prec + rec)
	}

	rocAUC := ComputeROCAUC(yTrue, yProb)
	prAUC := ComputePRAUC(yTrue, yProb)

	return EvaluationMetrics{
		ModelName:       modelName,
		DatasetSplit:    splitName,
		TotalSamples:    n,
		Prevalence:      prevalence,
		Threshold:       threshold,
		Accuracy:        acc,
		Precision:       prec,
		Recall:          rec,
		F1Score:         f1,
		ROCAUC:          rocAUC,
		PRAUC:           prAUC,
		BrierScore:      brier,
		ConfusionMatrix: cm,
	}
}

// FindOptimalThreshold performs a grid search over validation probabilities to find the threshold maximizing F1 score.
func FindOptimalThreshold(yTrue []float64, yProb []float64) float64 {
	bestThresh := 0.5
	bestF1 := -1.0

	for t := 0.05; t <= 0.95; t += 0.05 {
		metrics := EvaluateModel("tune", "val", yTrue, yProb, t)
		if metrics.F1Score > bestF1 {
			bestF1 = metrics.F1Score
			bestThresh = t
		}
	}

	if bestF1 <= 0 {
		return 0.5
	}
	return bestThresh
}

type scoreLabelPair struct {
	score float64
	label float64
}

// ComputeROCAUC computes the Area Under the Receiver Operating Characteristic Curve using trapezoidal rule.
// Returns nil if either positive or negative samples are absent (single-class evaluation subset),
// as ROC-AUC is mathematically undefined without both classes.
func ComputeROCAUC(yTrue, yScore []float64) *float64 {
	n := len(yTrue)
	if n == 0 {
		return nil
	}

	pairs := make([]scoreLabelPair, n)
	var posCount, negCount float64
	for i := 0; i < n; i++ {
		pairs[i] = scoreLabelPair{score: yScore[i], label: yTrue[i]}
		if yTrue[i] == 1.0 {
			posCount++
		} else {
			negCount++
		}
	}

	if posCount == 0 || negCount == 0 {
		return nil // mathematically undefined for single-class subsets
	}

	// Sort descending by predicted score
	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].score > pairs[j].score
	})

	var auc float64
	var tp, fp float64
	var prevTP, prevFP float64

	for i := 0; i < n; i++ {
		if pairs[i].label == 1.0 {
			tp++
		} else {
			fp++
		}

		// When next score differs or at the end, compute trapezoid
		if i == n-1 || pairs[i+1].score != pairs[i].score {
			// Trapezoid area: (fp - prevFP) * (tp + prevTP) / 2
			auc += (fp - prevFP) * (tp + prevTP) / 2.0
			prevTP = tp
			prevFP = fp
		}
	}

	val := auc / (posCount * negCount)
	return &val
}

// ComputePRAUC computes the Precision-Recall Area Under Curve (Average Precision).
func ComputePRAUC(yTrue, yScore []float64) float64 {
	n := len(yTrue)
	if n == 0 {
		return 0
	}

	pairs := make([]scoreLabelPair, n)
	var totalPos float64
	for i := 0; i < n; i++ {
		pairs[i] = scoreLabelPair{score: yScore[i], label: yTrue[i]}
		if yTrue[i] == 1.0 {
			totalPos++
		}
	}

	if totalPos == 0 {
		return 0.0
	}

	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].score > pairs[j].score
	})

	var ap float64
	var tp, fp float64
	var prevRecall float64

	for i := 0; i < n; i++ {
		if pairs[i].label == 1.0 {
			tp++
		} else {
			fp++
		}

		if i == n-1 || pairs[i+1].score != pairs[i].score {
			prec := tp / (tp + fp)
			recall := tp / totalPos
			deltaRecall := recall - prevRecall
			if deltaRecall > 0 {
				ap += prec * deltaRecall
			}
			prevRecall = recall
		}
	}

	return math.Min(1.0, ap)
}
