// Command predict loads a trained model, extracts causal features from an event log,
// and produces fork-risk predictions for each block proposal.
//
// Usage:
//
//	go run ./cmd/predict --model outputs/ml/models/logistic_regression/model.json --run outputs/runs/baseline_seed1_1792863044
//	go run ./cmd/predict --model outputs/ml/models/random_forest/model.json --run outputs/runs/latency_stress_seed1_1792863045 --output predictions.csv
//
// Flags:
//
//	--model   Path to saved model JSON (required)
//	--run     Path to simulation run directory containing events.jsonl and metadata.json (required)
//	--output  Optional path to write predictions CSV (default: print summary to stdout)
//	--warmup  Warm-up blocks to skip (default: 10)
package main

import (
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pranav-k/forkrisk/internal/config"
	"github.com/pranav-k/forkrisk/internal/features"
	"github.com/pranav-k/forkrisk/internal/logstore"
	"github.com/pranav-k/forkrisk/internal/ml"
)

type Predictor interface {
	PredictProbability(features []float64) (float64, error)
	PredictClass(features []float64) (int, float64, error)
}

func main() {
	modelPath := flag.String("model", "", "Path to trained model JSON file (required)")
	runDir := flag.String("run", "", "Path to run directory containing events.jsonl and metadata.json (required)")
	outputPath := flag.String("output", "", "Optional path to export predictions CSV")
	warmup := flag.Int("warmup", 10, "Warm-up blocks to skip")
	flag.Parse()

	if *modelPath == "" || *runDir == "" {
		fmt.Fprintln(os.Stderr, "Error: --model and --run flags are required")
		fmt.Fprintln(os.Stderr, "Example: go run ./cmd/predict --model outputs/ml/models/logistic_regression/model.json --run outputs/runs/baseline_seed1_1792863044")
		os.Exit(1)
	}

	// 1. Detect and load model
	predictor, modelName, threshold, featList, err := loadModel(*modelPath)
	if err != nil {
		log.Fatalf("Failed to load model: %v", err)
	}
	fmt.Printf("Loaded model: %s (Threshold: %.2f, Features: %d)\n", modelName, threshold, len(featList))

	// 2. Load run metadata and events
	metaPath := filepath.Join(*runDir, "metadata.json")
	eventsPath := filepath.Join(*runDir, "events.jsonl")

	metaBytes, err := os.ReadFile(metaPath)
	if err != nil {
		log.Fatalf("Failed to read metadata: %v", err)
	}
	var runMeta logstore.RunMetadata
	if err := json.Unmarshal(metaBytes, &runMeta); err != nil {
		log.Fatalf("Failed to parse metadata: %v", err)
	}

	cfgMap, ok := runMeta.Config.(map[string]interface{})
	if !ok {
		log.Fatalf("Invalid config format in metadata.json")
	}

	scenarioCfg := config.ScenarioConfig{
		Name: runMeta.ScenarioName,
		Seed: runMeta.Seed,
	}
	if v, ok := cfgMap["num_nodes"].(float64); ok {
		scenarioCfg.NumNodes = int(v)
	}
	if v, ok := cfgMap["block_interval_sec"].(float64); ok {
		scenarioCfg.BlockIntervalSec = v
	}
	if v, ok := cfgMap["mean_latency_ms"].(float64); ok {
		scenarioCfg.MeanLatencyMs = v
	}
	if v, ok := cfgMap["mean_prop_delay_ms"].(float64); ok {
		scenarioCfg.MeanPropDelayMs = v
	}
	if v, ok := cfgMap["tx_arrival_rate"].(float64); ok {
		scenarioCfg.TxArrivalRate = v
	}
	if v, ok := cfgMap["tx_per_block"].(float64); ok {
		scenarioCfg.TxPerBlock = int(v)
	}
	if v, ok := cfgMap["block_capacity"].(float64); ok {
		scenarioCfg.BlockCapacity = int(v)
	}

	events, err := features.ReadEventLog(eventsPath)
	if err != nil {
		log.Fatalf("Failed to read event log: %v", err)
	}

	proposals := features.ExtractProposals(events)
	if len(proposals) <= *warmup {
		log.Fatalf("Insufficient proposals in run (%d <= warmup %d)", len(proposals), *warmup)
	}

	nodeIDsMap := make(map[string]bool)
	for _, e := range events {
		if e.NodeID != "" {
			nodeIDsMap[e.NodeID] = true
		}
	}
	var nodeIDs []string
	for nid := range nodeIDsMap {
		nodeIDs = append(nodeIDs, nid)
	}
	sort.Strings(nodeIDs)

	extractor := features.NewExtractor(nodeIDs, *warmup, scenarioCfg)

	proposalIndexMap := make(map[int]int)
	for idx, p := range proposals {
		if _, exists := proposalIndexMap[p.Height]; !exists {
			proposalIndexMap[p.Height] = idx
		}
	}

	maxHeight := proposals[len(proposals)-1].Height

	type PredRow struct {
		Height     int
		SimTime    float64
		RiskScore  float64
		PredClass  int
		Confidence float64
	}

	var predRows []PredRow
	highRiskCount := 0

	for h := *warmup + 1; h <= maxHeight; h++ {
		pIdx, exists := proposalIndexMap[h]
		if !exists {
			continue
		}
		p := proposals[pIdx]

		feats := extractor.ExtractFeaturesAt(events, proposals, pIdx)
		vec := make([]float64, len(featList))
		for i, fn := range featList {
			vec[i] = feats[fn]
		}

		class, prob, err := predictor.PredictClass(vec)
		if err != nil {
			log.Fatalf("Prediction error at block %d: %v", h, err)
		}

		conf := math.Abs(prob-0.5) * 2.0
		if class == 1 {
			highRiskCount++
		}

		predRows = append(predRows, PredRow{
			Height:     h,
			SimTime:    p.SimTime,
			RiskScore:  prob,
			PredClass:  class,
			Confidence: conf,
		})
	}

	fmt.Printf("\n=== Predictions for Run %s ===\n", runMeta.RunID)
	fmt.Printf("Total Blocks Evaluated: %d\n", len(predRows))
	fmt.Printf("High Risk Blocks (>= %.2f): %d (%.2f%%)\n", threshold, highRiskCount, float64(highRiskCount)*100.0/float64(len(predRows)))

	// Print sample predictions
	fmt.Printf("\n%-6s | %-10s | %-10s | %-10s | %-10s\n", "Block", "SimTime (s)", "Risk Score", "Prediction", "Confidence")
	fmt.Println(strings.Repeat("-", 55))

	sampleStep := len(predRows) / 10
	if sampleStep < 1 {
		sampleStep = 1
	}
	for i := 0; i < len(predRows); i += sampleStep {
		r := predRows[i]
		verdict := "LOW RISK"
		if r.PredClass == 1 {
			verdict = "HIGH RISK"
		}
		fmt.Printf("%-6d | %10.4f | %10.4f | %-10s | %9.2f%%\n",
			r.Height, r.SimTime, r.RiskScore, verdict, r.Confidence*100)
	}

	// Export CSV if requested
	if *outputPath != "" {
		f, err := os.Create(*outputPath)
		if err != nil {
			log.Fatalf("Creating output CSV failed: %v", err)
		}
		defer f.Close()

		w := csv.NewWriter(f)
		w.Write([]string{"block_height", "sim_time", "fork_risk_score", "predicted_class", "confidence"})
		for _, r := range predRows {
			w.Write([]string{
				fmt.Sprintf("%d", r.Height),
				fmt.Sprintf("%.4f", r.SimTime),
				fmt.Sprintf("%.6f", r.RiskScore),
				fmt.Sprintf("%d", r.PredClass),
				fmt.Sprintf("%.4f", r.Confidence),
			})
		}
		w.Flush()
		fmt.Printf("\nExported predictions to: %s\n", *outputPath)
	}
}

func loadModel(path string) (Predictor, string, float64, []string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", 0, nil, err
	}

	// Check if Logistic Regression
	var lr ml.LogisticRegression
	if err := json.Unmarshal(data, &lr); err == nil && len(lr.Weights) > 0 {
		return &lr, "LogisticRegression", lr.Threshold, lr.Features, nil
	}

	// Check if Random Forest
	var rf ml.RandomForest
	if err := json.Unmarshal(data, &rf); err == nil && len(rf.Trees) > 0 {
		return &rf, "RandomForest", rf.Threshold, rf.Features, nil
	}

	return nil, "", 0, nil, fmt.Errorf("unrecognized model format in %s", path)
}
