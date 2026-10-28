package ml

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sort"
)

// ForestConfig specifies training hyperparameters for the Random Forest classifier.
type ForestConfig struct {
	NumTrees         int     `json:"num_trees"`
	MaxDepth         int     `json:"max_depth"`
	MinSamplesSplit  int     `json:"min_samples_split"`
	ClassWeight      bool    `json:"class_weight"`
	FeatureSubsample float64 `json:"feature_subsample"` // 0 = sqrt(N)
	Seed             int64   `json:"seed"`
}

// DefaultForestConfig provides balanced default hyperparameters.
func DefaultForestConfig() ForestConfig {
	return ForestConfig{
		NumTrees:         50,
		MaxDepth:         8,
		MinSamplesSplit:  5,
		ClassWeight:      true,
		FeatureSubsample: 0.0, // defaults to sqrt(D)
		Seed:             42,
	}
}

// TreeNode represents a single decision or leaf in a CART tree.
type TreeNode struct {
	FeatureIndex int       `json:"feat_idx"`
	Threshold    float64   `json:"threshold"`
	Left         *TreeNode `json:"left,omitempty"`
	Right        *TreeNode `json:"right,omitempty"`
	IsLeaf       bool      `json:"is_leaf"`
	Probability  float64   `json:"probability"`
	SampleCount  int       `json:"sample_count"`
}

// DecisionTree represents a single CART binary classification tree.
type DecisionTree struct {
	Root     *TreeNode `json:"root"`
	MaxDepth int       `json:"max_depth"`
	MinSplit int       `json:"min_split"`
}

// RandomForest is a bagged ensemble of deterministic decision trees.
type RandomForest struct {
	FeatureSchemaVersion string            `json:"feature_schema_version"`
	Features             []string          `json:"features"`
	Trees                []*DecisionTree   `json:"trees"`
	Threshold            float64           `json:"threshold"`
	Config               ForestConfig      `json:"config"`
	TrainStats           EvaluationMetrics `json:"train_stats"`
	ValStats             EvaluationMetrics `json:"val_stats"`
}

// NewRandomForest creates an uninitialized Random Forest model.
func NewRandomForest(features []string, cfg ForestConfig) *RandomForest {
	return &RandomForest{
		FeatureSchemaVersion: SupportedFeatureSchemaVersion,
		Features:             features,
		Trees:                make([]*DecisionTree, 0, cfg.NumTrees),
		Config:               cfg,
	}
}

// Train builds the forest using bootstrap aggregation and random feature selection.
func (rf *RandomForest) Train(XTrain [][]float64, YTrain []float64, XVal [][]float64, YVal []float64) error {
	m := len(XTrain)
	if m == 0 {
		return fmt.Errorf("empty training dataset")
	}
	n := len(rf.Features)

	// Compute class weights
	var posCount, negCount float64
	for _, y := range YTrain {
		if y == 1.0 {
			posCount++
		} else {
			negCount++
		}
	}

	wPos := 1.0
	if rf.Config.ClassWeight && posCount > 0 && negCount > 0 {
		wPos = negCount / posCount
	}

	weights := make([]float64, m)
	for i, y := range YTrain {
		if y == 1.0 {
			weights[i] = wPos
		} else {
			weights[i] = 1.0
		}
	}

	// Number of features to consider per split: sqrt(n)
	mTry := int(math.Sqrt(float64(n)))
	if mTry < 1 {
		mTry = 1
	}

	rf.Trees = make([]*DecisionTree, rf.Config.NumTrees)

	// Train trees sequentially with independent deterministic random streams
	for t := 0; t < rf.Config.NumTrees; t++ {
		treeRNG := rand.New(rand.NewPCG(uint64(rf.Config.Seed), uint64(t+1)))

		// Bootstrap sample of m rows
		bootX := make([][]float64, m)
		bootY := make([]float64, m)
		bootW := make([]float64, m)

		for i := 0; i < m; i++ {
			idx := treeRNG.IntN(m)
			bootX[i] = XTrain[idx]
			bootY[i] = YTrain[idx]
			bootW[i] = weights[idx]
		}

		builder := &treeBuilder{
			rng:      treeRNG,
			maxDepth: rf.Config.MaxDepth,
			minSplit: rf.Config.MinSamplesSplit,
			mTry:     mTry,
			nFeat:    n,
		}

		root := builder.build(bootX, bootY, bootW, 0)
		rf.Trees[t] = &DecisionTree{
			Root:     root,
			MaxDepth: rf.Config.MaxDepth,
			MinSplit: rf.Config.MinSamplesSplit,
		}
	}

	// Tune classification threshold on validation set if available
	if len(XVal) > 0 {
		valProbs := make([]float64, len(XVal))
		for i, x := range XVal {
			p, err := rf.PredictProbability(x)
			if err != nil {
				return err
			}
			valProbs[i] = p
		}
		rf.Threshold = FindOptimalThreshold(YVal, valProbs)
		rf.ValStats = EvaluateModel("RandomForest", "val", YVal, valProbs, rf.Threshold)
	} else {
		rf.Threshold = 0.5
	}

	// Record train evaluation
	trainProbs := make([]float64, len(XTrain))
	for i, x := range XTrain {
		p, _ := rf.PredictProbability(x)
		trainProbs[i] = p
	}
	rf.TrainStats = EvaluateModel("RandomForest", "train", YTrain, trainProbs, rf.Threshold)

	return nil
}

// PredictProbability returns the average risk score across all trees in the forest.
func (rf *RandomForest) PredictProbability(rawFeatures []float64) (float64, error) {
	if len(rawFeatures) != len(rf.Features) {
		return 0, fmt.Errorf("expected %d features, got %d", len(rf.Features), len(rawFeatures))
	}
	if len(rf.Trees) == 0 {
		return 0, fmt.Errorf("untrained random forest")
	}

	var sum float64
	for _, tree := range rf.Trees {
		sum += tree.predict(rawFeatures)
	}
	return sum / float64(len(rf.Trees)), nil
}

// PredictClass returns binary prediction (0 or 1) using tuned threshold.
func (rf *RandomForest) PredictClass(rawFeatures []float64) (int, float64, error) {
	p, err := rf.PredictProbability(rawFeatures)
	if err != nil {
		return 0, 0, err
	}
	if p >= rf.Threshold {
		return 1, p, nil
	}
	return 0, p, nil
}

// Save serializes the random forest to a JSON file.
func (rf *RandomForest) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("creating dir: %w", err)
	}
	data, err := json.MarshalIndent(rf, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling model: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}

// LoadRandomForest loads a trained random forest from JSON.
func LoadRandomForest(path string) (*RandomForest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading model file: %w", err)
	}
	var rf RandomForest
	if err := json.Unmarshal(data, &rf); err != nil {
		return nil, fmt.Errorf("parsing model file: %w", err)
	}
	if rf.FeatureSchemaVersion != SupportedFeatureSchemaVersion {
		return nil, fmt.Errorf("incompatible model feature schema version %q (expected %q)", rf.FeatureSchemaVersion, SupportedFeatureSchemaVersion)
	}
	return &rf, nil
}

func (t *DecisionTree) predict(x []float64) float64 {
	node := t.Root
	for !node.IsLeaf {
		if x[node.FeatureIndex] <= node.Threshold {
			node = node.Left
		} else {
			node = node.Right
		}
	}
	return node.Probability
}

type treeBuilder struct {
	rng      *rand.Rand
	maxDepth int
	minSplit int
	mTry     int
	nFeat    int
}

func (b *treeBuilder) build(X [][]float64, Y []float64, W []float64, depth int) *TreeNode {
	n := len(Y)
	prob, wTotal := weightedProb(Y, W)

	// Leaf conditions
	if depth >= b.maxDepth || n < b.minSplit || prob == 0.0 || prob == 1.0 {
		return &TreeNode{
			IsLeaf:      true,
			Probability: prob,
			SampleCount: n,
		}
	}

	bestFeat := -1
	bestThresh := 0.0
	bestGain := 0.0
	currentGini := 2.0 * prob * (1.0 - prob)

	// Randomly select mTry candidate features
	featIndices := make([]int, b.nFeat)
	for i := range featIndices {
		featIndices[i] = i
	}
	b.rng.Shuffle(len(featIndices), func(i, j int) {
		featIndices[i], featIndices[j] = featIndices[j], featIndices[i]
	})
	selectedFeats := featIndices[:b.mTry]

	for _, feat := range selectedFeats {
		// Extract sorted unique values
		vals := make([]float64, n)
		for i := 0; i < n; i++ {
			vals[i] = X[i][feat]
		}
		sort.Float64s(vals)

		// Evaluate candidate split thresholds
		for i := 0; i < n-1; i++ {
			if vals[i] == vals[i+1] {
				continue
			}
			thresh := (vals[i] + vals[i+1]) / 2.0

			var wL, wR float64
			var wPosL, wPosR float64
			var countL, countR int

			for k := 0; k < n; k++ {
				if X[k][feat] <= thresh {
					wL += W[k]
					if Y[k] == 1.0 {
						wPosL += W[k]
					}
					countL++
				} else {
					wR += W[k]
					if Y[k] == 1.0 {
						wPosR += W[k]
					}
					countR++
				}
			}

			if countL == 0 || countR == 0 {
				continue
			}

			pL := wPosL / wL
			pR := wPosR / wR
			giniL := 2.0 * pL * (1.0 - pL)
			giniR := 2.0 * pR * (1.0 - pR)

			splitGini := (wL/wTotal)*giniL + (wR/wTotal)*giniR
			gain := currentGini - splitGini

			if gain > bestGain {
				bestGain = gain
				bestFeat = feat
				bestThresh = thresh
			}
		}
	}

	if bestGain < 1e-7 || bestFeat == -1 {
		return &TreeNode{
			IsLeaf:      true,
			Probability: prob,
			SampleCount: n,
		}
	}

	// Split data
	var leftX, rightX [][]float64
	var leftY, rightY []float64
	var leftW, rightW []float64

	for i := 0; i < n; i++ {
		if X[i][bestFeat] <= bestThresh {
			leftX = append(leftX, X[i])
			leftY = append(leftY, Y[i])
			leftW = append(leftW, W[i])
		} else {
			rightX = append(rightX, X[i])
			rightY = append(rightY, Y[i])
			rightW = append(rightW, W[i])
		}
	}

	leftNode := b.build(leftX, leftY, leftW, depth+1)
	rightNode := b.build(rightX, rightY, rightW, depth+1)

	return &TreeNode{
		FeatureIndex: bestFeat,
		Threshold:    bestThresh,
		Left:         leftNode,
		Right:        rightNode,
		IsLeaf:       false,
		Probability:  prob,
		SampleCount:  n,
	}
}

func weightedProb(Y []float64, W []float64) (float64, float64) {
	var sumW, sumPosW float64
	for i, y := range Y {
		w := W[i]
		sumW += w
		if y == 1.0 {
			sumPosW += w
		}
	}
	if sumW == 0 {
		return 0.0, 0.0
	}
	return sumPosW / sumW, sumW
}
