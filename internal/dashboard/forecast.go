package dashboard

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	"github.com/pranav-k/forkrisk/internal/config"
	"github.com/pranav-k/forkrisk/internal/features"
	"github.com/pranav-k/forkrisk/internal/logstore"
	"github.com/pranav-k/forkrisk/internal/ml"
)

type blockForecast struct {
	Height      int     `json:"height"`
	Position    int     `json:"position"`
	Probability float64 `json:"probability"`
	HighRisk    bool    `json:"high_risk"`
}

type replayForecast struct {
	Model     string          `json:"model"`
	Horizon   int             `json:"horizon"`
	Warmup    int             `json:"warmup"`
	Threshold float64         `json:"threshold"`
	Scores    []blockForecast `json:"scores"`
	Message   string          `json:"message,omitempty"`
	Forest    *modelForecast  `json:"forest,omitempty"`
}

type modelForecast struct {
	Model     string          `json:"model"`
	Threshold float64         `json:"threshold"`
	Scores    []blockForecast `json:"scores"`
}

type forecastPredictor interface {
	PredictClass([]float64) (int, float64, error)
}

func validForecastTree(node *ml.TreeNode, featureCount int) bool {
	return node != nil && (node.IsLeaf || (node.FeatureIndex >= 0 && node.FeatureIndex < featureCount &&
		validForecastTree(node.Left, featureCount) && validForecastTree(node.Right, featureCount)))
}

func scoreReplayBlock(model forecastPredictor, names []string, vector map[string]float64, height, position int) (blockForecast, bool) {
	values := make([]float64, len(names))
	for i, name := range names {
		values[i] = vector[name]
	}
	class, probability, err := model.PredictClass(values)
	return blockForecast{Height: height, Position: position, Probability: probability, HighRisk: class == 1},
		err == nil && !math.IsNaN(probability) && !math.IsInf(probability, 0)
}

// getReplayForecast scores the first proposal at each relevant height using
// only the recorded prefix visible at the selected position.
func (dl *DataLoader) getReplayForecast(ctx context.Context, dir string, position int) (replayForecast, error) {
	result := replayForecast{
		Model: "Logistic regression", Horizon: features.DefaultForecastHorizon, Warmup: 10,
		Scores: []blockForecast{},
	}
	modelsDir := filepath.Join(dl.BaseDir, "ml", "models")
	model, err := ml.LoadLogisticRegression(filepath.Join(modelsDir, "logistic_regression", "model.json"))
	hasLogistic := err == nil && model.Scaler != nil && len(model.Features) > 0 &&
		len(model.Weights) == len(model.Features) && len(model.Scaler.Features) == len(model.Features) &&
		len(model.Scaler.Means) == len(model.Features) && len(model.Scaler.Stds) == len(model.Features)
	forest, forestErr := ml.LoadRandomForest(filepath.Join(modelsDir, "random_forest", "model.json"))
	hasForest := forestErr == nil && len(forest.Features) > 0 && len(forest.Trees) > 0
	if hasForest {
		for _, tree := range forest.Trees {
			if tree == nil || !validForecastTree(tree.Root, len(forest.Features)) {
				hasForest = false
				break
			}
		}
	}
	if !hasLogistic && !hasForest {
		result.Message = "The trained forecast models are unavailable."
		return result, nil
	}
	if hasLogistic {
		result.Threshold = model.Threshold
	}
	if hasForest {
		result.Forest = &modelForecast{Model: "Random forest", Threshold: forest.Threshold, Scores: []blockForecast{}}
	}
	var dataset struct {
		Schema  string `json:"feature_schema_version"`
		Horizon int    `json:"forecast_horizon"`
		Warmup  int    `json:"warmup_blocks"`
	}
	if data, err := os.ReadFile(filepath.Join(dl.BaseDir, "ml", "dataset", "dataset_metadata.json")); err == nil &&
		json.Unmarshal(data, &dataset) == nil && dataset.Schema == features.FeatureSchemaVersion && dataset.Horizon > 0 {
		result.Horizon, result.Warmup = dataset.Horizon, max(0, dataset.Warmup)
	}

	var metadata struct {
		Config config.ScenarioConfig `json:"config"`
	}
	data, err := os.ReadFile(filepath.Join(dir, "metadata.json"))
	if err != nil || json.Unmarshal(data, &metadata) != nil || metadata.Config.NumNodes < 1 {
		result.Message = "Run configuration is unavailable for inference."
		return result, nil
	}
	file, err := os.Open(filepath.Join(dir, "events.jsonl"))
	if os.IsNotExist(err) {
		result.Message = "This run has no recording."
		return result, nil
	}
	if err != nil {
		return result, err
	}
	defer file.Close()

	nodeIDs := make(map[string]bool)
	for i := 0; i < metadata.Config.NumNodes; i++ {
		nodeIDs["N"+strconv.Itoa(i)] = true
	}
	var events []logstore.LogEntry
	positions := make(map[int]int)
	latestHeight := 0
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for len(events) < position && scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		var event logstore.LogEntry
		if json.Unmarshal(scanner.Bytes(), &event) != nil || event.EventType == "" {
			continue
		}
		events = append(events, event)
		if event.EventType == "BlockPropose" && event.BlockHash != "" {
			latestHeight = event.BlockHeight
			if _, exists := positions[latestHeight]; !exists {
				positions[latestHeight] = len(events)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return result, err
	}
	if latestHeight <= result.Warmup {
		result.Message = fmt.Sprintf("Model warm-up covers the first %d block heights.", result.Warmup)
		return result, nil
	}

	proposals := features.ExtractProposals(events)
	for idx, proposal := range proposals {
		height := proposal.Height
		at := positions[height]
		if at == 0 || events[at-1].BlockHash != proposal.Hash || events[at-1].SimTime != proposal.SimTime ||
			height <= result.Warmup || height < latestHeight-result.Horizon || height > latestHeight {
			continue
		}
		delete(positions, height)
		if err := ctx.Err(); err != nil {
			return result, err
		}
		// Configured node IDs and node IDs observed up to this proposal keep the
		// inference independent of node activity later in the recording.
		nodes := make([]string, 0, len(nodeIDs))
		observed := make(map[string]bool)
		for _, event := range events[:at] {
			if event.NodeID != "" && !nodeIDs[event.NodeID] {
				observed[event.NodeID] = true
			}
		}
		for id := range nodeIDs {
			nodes = append(nodes, id)
		}
		for id := range observed {
			nodes = append(nodes, id)
		}
		sort.Strings(nodes)
		vector := features.NewExtractor(nodes, result.Warmup, metadata.Config).ExtractFeaturesAt(events[:at], proposals, idx)
		if hasLogistic {
			if score, ok := scoreReplayBlock(model, model.Features, vector, height, at); ok {
				result.Scores = append(result.Scores, score)
			}
		}
		if hasForest {
			if score, ok := scoreReplayBlock(forest, forest.Features, vector, height, at); ok {
				result.Forest.Scores = append(result.Forest.Scores, score)
			}
		}
	}
	if len(result.Scores) == 0 && (result.Forest == nil || len(result.Forest.Scores) == 0) {
		result.Message = "No forecast is available at this position."
	}
	return result, nil
}
