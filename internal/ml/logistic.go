package ml

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
)

// LogisticConfig specifies training hyperparameters for Logistic Regression.
type LogisticConfig struct {
	LearningRate float64 `json:"learning_rate"`
	L2Lambda     float64 `json:"l2_lambda"`
	MaxEpochs    int     `json:"max_epochs"`
	BatchSize    int     `json:"batch_size"`
	ClassWeight  bool    `json:"class_weight"`
	Patience     int     `json:"patience"`
	Seed         int64   `json:"seed"`
}

// DefaultLogisticConfig provides sensible default hyperparameters.
func DefaultLogisticConfig() LogisticConfig {
	return LogisticConfig{
		LearningRate: 0.05,
		L2Lambda:     0.001,
		MaxEpochs:    300,
		BatchSize:    64,
		ClassWeight:  true,
		Patience:     25,
		Seed:         42,
	}
}

// LogisticRegression is a binary classification model with L2 regularization and standardization.
type LogisticRegression struct {
	FeatureSchemaVersion string            `json:"feature_schema_version"`
	Features             []string          `json:"features"`
	Weights              []float64         `json:"weights"`
	Bias                 float64           `json:"bias"`
	Threshold            float64           `json:"threshold"`
	Scaler               *StandardScaler   `json:"scaler"`
	Config               LogisticConfig    `json:"config"`
	TrainLosses          []float64         `json:"train_losses"`
	ValLosses            []float64         `json:"val_losses"`
	TrainStats           EvaluationMetrics `json:"train_stats"`
	ValStats             EvaluationMetrics `json:"val_stats"`
}

// SupportedFeatureSchemaVersion defines the exact supported feature schema version for ML models.
const SupportedFeatureSchemaVersion = "v1.3"

// NewLogisticRegression creates an uninitialized model for the given feature list.
func NewLogisticRegression(features []string, cfg LogisticConfig) *LogisticRegression {
	return &LogisticRegression{
		FeatureSchemaVersion: SupportedFeatureSchemaVersion,
		Features:             features,
		Weights:              make([]float64, len(features)),
		Scaler:               NewStandardScaler(features),
		Config:               cfg,
	}
}

// Train fits the logistic regression model using mini-batch gradient descent with Adam optimizer.
func (lr *LogisticRegression) Train(XTrain [][]float64, YTrain []float64, XVal [][]float64, YVal []float64) error {
	m := len(XTrain)
	if m == 0 {
		return fmt.Errorf("empty training dataset")
	}
	n := len(lr.Features)

	// 1. Fit scaler strictly on training data
	if err := lr.Scaler.Fit(XTrain); err != nil {
		return fmt.Errorf("fitting scaler: %w", err)
	}
	XTrainScaled := lr.Scaler.Transform(XTrain)

	var XValScaled [][]float64
	if len(XVal) > 0 {
		XValScaled = lr.Scaler.Transform(XVal)
	}

	// 2. Class weights for imbalance handling
	var posCount, negCount float64
	for _, y := range YTrain {
		if y == 1.0 {
			posCount++
		} else {
			negCount++
		}
	}

	wPos := 1.0
	if lr.Config.ClassWeight && posCount > 0 && negCount > 0 {
		wPos = negCount / posCount
	}

	// Initialize weights
	rng := rand.New(rand.NewPCG(uint64(lr.Config.Seed), 1))
	lr.Weights = make([]float64, n)
	for j := range lr.Weights {
		lr.Weights[j] = (rng.Float64() - 0.5) * 0.02
	}
	lr.Bias = 0.0

	// Adam optimizer parameters
	beta1 := 0.9
	beta2 := 0.999
	eps := 1e-8
	mW := make([]float64, n)
	vW := make([]float64, n)
	mB := 0.0
	vB := 0.0
	step := 0

	batchSize := lr.Config.BatchSize
	if batchSize <= 0 || batchSize > m {
		batchSize = m
	}

	indices := make([]int, m)
	for i := range indices {
		indices[i] = i
	}

	bestValLoss := math.MaxFloat64
	bestW := make([]float64, n)
	bestB := 0.0
	patienceCounter := 0

	for epoch := 0; epoch < lr.Config.MaxEpochs; epoch++ {
		// Shuffle training indices
		rng.Shuffle(len(indices), func(i, j int) {
			indices[i], indices[j] = indices[j], indices[i]
		})

		var epochLoss float64

		// Mini-batch loop
		for bStart := 0; bStart < m; bStart += batchSize {
			bEnd := bStart + batchSize
			if bEnd > m {
				bEnd = m
			}
			bSize := float64(bEnd - bStart)

			gradW := make([]float64, n)
			var gradB float64

			for i := bStart; i < bEnd; i++ {
				idx := indices[i]
				x := XTrainScaled[idx]
				y := YTrain[idx]

				p := lr.sigmoid(lr.dot(x) + lr.Bias)
				weight := 1.0
				if y == 1.0 {
					weight = wPos
				}

				err := weight * (p - y)
				gradB += err
				for j := 0; j < n; j++ {
					gradW[j] += err * x[j]
				}

				// Loss computation
				pSafe := math.Max(1e-15, math.Min(1.0-1e-15, p))
				if y == 1.0 {
					epochLoss -= wPos * math.Log(pSafe)
				} else {
					epochLoss -= math.Log(1.0 - pSafe)
				}
			}

			// Add L2 regularization gradient
			for j := 0; j < n; j++ {
				gradW[j] = (gradW[j] / bSize) + lr.Config.L2Lambda*lr.Weights[j]
			}
			gradB = gradB / bSize

			// Adam update
			step++
			tFloat := float64(step)

			for j := 0; j < n; j++ {
				mW[j] = beta1*mW[j] + (1-beta1)*gradW[j]
				vW[j] = beta2*vW[j] + (1-beta2)*gradW[j]*gradW[j]
				mHat := mW[j] / (1 - math.Pow(beta1, tFloat))
				vHat := vW[j] / (1 - math.Pow(beta2, tFloat))
				lr.Weights[j] -= lr.Config.LearningRate * mHat / (math.Sqrt(vHat) + eps)
			}

			mB = beta1*mB + (1-beta1)*gradB
			vB = beta2*vB + (1-beta2)*gradB*gradB
			mBHat := mB / (1 - math.Pow(beta1, tFloat))
			vBHat := vB / (1 - math.Pow(beta2, tFloat))
			lr.Bias -= lr.Config.LearningRate * mBHat / (math.Sqrt(vBHat) + eps)
		}

		epochLoss /= float64(m)
		lr.TrainLosses = append(lr.TrainLosses, epochLoss)

		// Validation check
		if len(XValScaled) > 0 {
			var valLoss float64
			for i, x := range XValScaled {
				y := YVal[i]
				p := lr.sigmoid(lr.dot(x) + lr.Bias)
				pSafe := math.Max(1e-15, math.Min(1.0-1e-15, p))
				if y == 1.0 {
					valLoss -= wPos * math.Log(pSafe)
				} else {
					valLoss -= math.Log(1.0 - pSafe)
				}
			}
			valLoss /= float64(len(XValScaled))
			lr.ValLosses = append(lr.ValLosses, valLoss)

			if valLoss < bestValLoss {
				bestValLoss = valLoss
				copy(bestW, lr.Weights)
				bestB = lr.Bias
				patienceCounter = 0
			} else {
				patienceCounter++
				if patienceCounter >= lr.Config.Patience {
					// Early stopping triggered
					copy(lr.Weights, bestW)
					lr.Bias = bestB
					break
				}
			}
		}
	}

	// Tune classification threshold on validation set if available
	if len(XValScaled) > 0 {
		valProbs := make([]float64, len(XVal))
		for i, x := range XValScaled {
			valProbs[i] = lr.sigmoid(lr.dot(x) + lr.Bias)
		}
		lr.Threshold = FindOptimalThreshold(YVal, valProbs)
		lr.ValStats = EvaluateModel("LogisticRegression", "val", YVal, valProbs, lr.Threshold)
	} else {
		lr.Threshold = 0.5
	}

	// Record train evaluation
	trainProbs := make([]float64, len(XTrain))
	for i, x := range XTrainScaled {
		trainProbs[i] = lr.sigmoid(lr.dot(x) + lr.Bias)
	}
	lr.TrainStats = EvaluateModel("LogisticRegression", "train", YTrain, trainProbs, lr.Threshold)

	return nil
}

// PredictProbability returns P(fork = 1) for an unscaled raw feature vector.
func (lr *LogisticRegression) PredictProbability(rawFeatures []float64) (float64, error) {
	if len(rawFeatures) != len(lr.Features) {
		return 0, fmt.Errorf("expected %d features, got %d", len(lr.Features), len(rawFeatures))
	}
	scaled := lr.Scaler.TransformRow(rawFeatures)
	return lr.sigmoid(lr.dot(scaled) + lr.Bias), nil
}

// PredictClass returns binary 1 or 0 using the tuned threshold.
func (lr *LogisticRegression) PredictClass(rawFeatures []float64) (int, float64, error) {
	prob, err := lr.PredictProbability(rawFeatures)
	if err != nil {
		return 0, 0, err
	}
	if prob >= lr.Threshold {
		return 1, prob, nil
	}
	return 0, prob, nil
}

// Save writes the model configuration, scaler parameters, and weights to a JSON file.
func (lr *LogisticRegression) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("creating directory: %w", err)
	}
	data, err := json.MarshalIndent(lr, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling model: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}

// LoadLogisticRegression loads a trained logistic regression model from a JSON file.
func LoadLogisticRegression(path string) (*LogisticRegression, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading model file: %w", err)
	}
	var lr LogisticRegression
	if err := json.Unmarshal(data, &lr); err != nil {
		return nil, fmt.Errorf("parsing model file: %w", err)
	}
	if lr.FeatureSchemaVersion != SupportedFeatureSchemaVersion {
		return nil, fmt.Errorf("incompatible model feature schema version %q (expected %q)", lr.FeatureSchemaVersion, SupportedFeatureSchemaVersion)
	}
	return &lr, nil
}

func (lr *LogisticRegression) dot(x []float64) float64 {
	var sum float64
	for j, val := range x {
		sum += val * lr.Weights[j]
	}
	return sum
}

func (lr *LogisticRegression) sigmoid(z float64) float64 {
	if z > 35.0 {
		return 1.0
	}
	if z < -35.0 {
		return 0.0
	}
	return 1.0 / (1.0 + math.Exp(-z))
}
