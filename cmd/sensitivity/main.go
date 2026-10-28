// Command sensitivity performs empirical forecast horizon sensitivity analysis
// across horizons H=1 through H=5, evaluates trained models on seed-held-out splits,
// outputs a canonical sensitivity CSV and JSON, and renders a publication-grade chart.
//
// Usage:
//
//	go run ./cmd/sensitivity --runs-dir outputs/runs --pilot-csv outputs/pilot_final/pilot_summary.csv --out-dir outputs/final_evaluation
package main

import (
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/pranav-k/forkrisk/internal/config"
	"github.com/pranav-k/forkrisk/internal/features"
	"github.com/pranav-k/forkrisk/internal/fork"
	"github.com/pranav-k/forkrisk/internal/logstore"
	"github.com/pranav-k/forkrisk/internal/ml"
	"github.com/pranav-k/forkrisk/internal/viz"
)

type SensitivityRecord struct {
	Horizon        int      `json:"horizon"`
	TotalRows      int      `json:"total_rows"`
	PositiveCount  int      `json:"positive_count"`
	NegativeCount  int      `json:"negative_count"`
	Prevalence     float64  `json:"prevalence"`
	TrainRuns      int      `json:"train_runs"`
	ValRuns        int      `json:"val_runs"`
	TestRuns       int      `json:"test_runs"`
	TestSamples    int      `json:"test_samples"`
	TestPositives  int      `json:"test_positives"`
	TestPrevalence float64  `json:"test_prevalence"`
	LR_ROCAUC      *float64 `json:"lr_roc_auc"`
	LR_PRAUC       float64  `json:"lr_pr_auc"`
	LR_F1          float64  `json:"lr_f1"`
	LR_Precision   float64  `json:"lr_precision"`
	LR_Recall      float64  `json:"lr_recall"`
	LR_Brier       float64  `json:"lr_brier"`
	LR_Threshold   float64  `json:"lr_threshold"`
	RF_ROCAUC      *float64 `json:"rf_roc_auc"`
	RF_PRAUC       float64  `json:"rf_pr_auc"`
	RF_F1          float64  `json:"rf_f1"`
	RF_Precision   float64  `json:"rf_precision"`
	RF_Recall      float64  `json:"rf_recall"`
	RF_Brier       float64  `json:"rf_brier"`
	RF_Threshold   float64  `json:"rf_threshold"`
	SuccessfulRuns int      `json:"successful_runs"`
	ExcludedRuns   int      `json:"excluded_runs"`
}

type SensitivityReport struct {
	GeneratedAtUTC string              `json:"generated_at_utc"`
	WarmUpBlocks   int                 `json:"warmup_blocks"`
	EvaluatedSeeds string              `json:"evaluated_seeds"`
	Horizons       []SensitivityRecord `json:"horizons"`
}

func formatAUC(auc *float64) string {
	if auc == nil {
		return "N/A"
	}
	return fmt.Sprintf("%.4f", *auc)
}

func main() {
	runsDir := flag.String("runs-dir", "outputs/runs", "Base directory containing simulation run folders")
	pilotCSV := flag.String("pilot-csv", "outputs/pilot_final/pilot_summary.csv", "Path to summary CSV to select authoritative runs")
	outDir := flag.String("out-dir", "outputs/final_evaluation", "Output directory for sensitivity results")
	warmup := flag.Int("warmup", 10, "Warm-up blocks to skip")
	flag.Parse()

	if err := os.MkdirAll(*outDir, 0755); err != nil {
		log.Fatalf("Failed to create out-dir: %v", err)
	}
	chartsDir := filepath.Join(*outDir, "charts")
	if err := os.MkdirAll(chartsDir, 0755); err != nil {
		log.Fatalf("Failed to create charts dir: %v", err)
	}

	// 1. Identify target run directories
	var targetRunDirs []string
	if *pilotCSV != "" && fileExists(*pilotCSV) {
		f, err := os.Open(*pilotCSV)
		if err == nil {
			defer f.Close()
			reader := csv.NewReader(f)
			records, _ := reader.ReadAll()
			for _, rec := range records[1:] {
				if len(rec) > 0 && strings.TrimSpace(rec[0]) != "" {
					rDir := filepath.Join(*runsDir, strings.TrimSpace(rec[0]))
					targetRunDirs = append(targetRunDirs, rDir)
				}
			}
		}
	}
	if len(targetRunDirs) == 0 {
		entries, _ := os.ReadDir(*runsDir)
		for _, e := range entries {
			if e.IsDir() {
				targetRunDirs = append(targetRunDirs, filepath.Join(*runsDir, e.Name()))
			}
		}
	}
	sort.Strings(targetRunDirs)
	fmt.Printf("=== Running Forecast Horizon Sensitivity Sweep (H=1..5) ===\n")
	fmt.Printf("Target Runs: %d | Warm-Up Blocks: %d\n\n", len(targetRunDirs), *warmup)

	// 2. Preload simulation logs to avoid re-reading disk 5 times
	type loadedRun struct {
		runID       string
		scenario    string
		seed        int64
		scenarioCfg config.ScenarioConfig
		events      []logstore.LogEntry
		proposals   []features.BlockProposalInfo
		nodeIDs     []string
		detection   fork.DetectionResult
	}
	var loadedRuns []loadedRun

	for _, rDir := range targetRunDirs {
		metaPath := filepath.Join(rDir, "metadata.json")
		eventsPath := filepath.Join(rDir, "events.jsonl")
		if !fileExists(metaPath) || !fileExists(eventsPath) {
			continue
		}
		metaBytes, err := os.ReadFile(metaPath)
		if err != nil {
			continue
		}
		var runMeta logstore.RunMetadata
		if err := json.Unmarshal(metaBytes, &runMeta); err != nil {
			continue
		}
		events, err := features.ReadEventLog(eventsPath)
		if err != nil || len(events) == 0 {
			continue
		}
		proposals := features.ExtractProposals(events)
		if len(proposals) <= *warmup+5 { // need enough proposals for up to H=5
			continue
		}

		cfgMap, _ := runMeta.Config.(map[string]interface{})
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
		if v, ok := cfgMap["block_capacity"].(float64); ok {
			scenarioCfg.BlockCapacity = int(v)
		}
		scenarioCfg.WarmUpBlocks = *warmup

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

		var forkEvents []fork.Event
		for _, e := range events {
			forkEvents = append(forkEvents, fork.Event{
				Seq:           e.Seq,
				SimTime:       e.SimTime,
				EventType:     e.EventType,
				NodeID:        e.NodeID,
				BlockHash:     e.BlockHash,
				BlockHeight:   e.BlockHeight,
				ProposalIndex: e.ProposalIndex,
				ProposerID:    e.ProposerID,
				ParentHash:    e.ParentHash,
				TipHash:       e.TipHash,
				TipHeight:     e.TipHeight,
				OldTipHash:    e.OldTipHash,
				Reason:        e.Reason,
				TxCount:       e.TxCount,
			})
		}
		detector := fork.NewDetector(nodeIDs, *warmup)
		detection := detector.Analyze(forkEvents)

		loadedRuns = append(loadedRuns, loadedRun{
			runID:       runMeta.RunID,
			scenario:    runMeta.ScenarioName,
			seed:        runMeta.Seed,
			scenarioCfg: scenarioCfg,
			events:      events,
			proposals:   proposals,
			nodeIDs:     nodeIDs,
			detection:   detection,
		})
	}
	fmt.Printf("Successfully preloaded %d simulation runs for sensitivity analysis.\n", len(loadedRuns))

	var records []SensitivityRecord
	var vizPoints []viz.HorizonSensitivityPoint

	for h := 1; h <= 5; h++ {
		t0 := time.Now()
		var allRows []features.DatasetRow
		posCount := 0
		negCount := 0

		for _, lr := range loadedRuns {
			labeler := features.NewLabeler(h, *warmup)
			labels, eligibleHeights := labeler.ComputeLabels(lr.proposals, lr.detection)

			extractor := features.NewExtractor(lr.nodeIDs, *warmup, lr.scenarioCfg)
			proposalIndexMap := make(map[int]int)
			for idx, p := range lr.proposals {
				if _, exists := proposalIndexMap[p.Height]; !exists {
					proposalIndexMap[p.Height] = idx
				}
			}

			for _, height := range eligibleHeights {
				pIdx := proposalIndexMap[height]
				proposal := lr.proposals[pIdx]
				feats := extractor.ExtractFeaturesAtWithDetection(lr.events, lr.proposals, pIdx, &lr.detection)
				lbl := labels[height]

				allRows = append(allRows, features.DatasetRow{
					RunID:       lr.runID,
					Scenario:    lr.scenario,
					Seed:        lr.seed,
					BlockHeight: height,
					SimTime:     proposal.SimTime,
					Features:    feats,
					LabelForkH:  lbl,
				})

				if lbl == 1 {
					posCount++
				} else {
					negCount++
				}
			}
		}

		totalRows := len(allRows)
		prev := 0.0
		if totalRows > 0 {
			prev = float64(posCount) / float64(totalRows)
		}

		// Run-level split: Seed 1 train, Seed 2 val, Seed 3 test
		manifest, split, err := ml.CreateSeedHeldOutSplit(allRows)
		if err != nil {
			log.Fatalf("Split creation failed for H=%d: %v", h, err)
		}

		// 1. Train Logistic Regression
		lrCfg := ml.DefaultLogisticConfig()
		lrModel := ml.NewLogisticRegression(features.CanonicalFeatures, lrCfg)
		if err := lrModel.Train(split.XTrain, split.YTrain, split.XVal, split.YVal); err != nil {
			log.Fatalf("LR training failed for H=%d: %v", h, err)
		}

		// Evaluate LR on test set
		testLRProbs := make([]float64, len(split.XTest))
		for i, x := range split.XTest {
			p, _ := lrModel.PredictProbability(x)
			testLRProbs[i] = p
		}
		lrTestMetrics := ml.EvaluateModel("LogisticRegression", "test", split.YTest, testLRProbs, lrModel.Threshold)

		// 2. Train Random Forest
		rfCfg := ml.DefaultForestConfig()
		rfCfg.NumTrees = 50
		rfCfg.MaxDepth = 8
		rfModel := ml.NewRandomForest(features.CanonicalFeatures, rfCfg)
		if err := rfModel.Train(split.XTrain, split.YTrain, split.XVal, split.YVal); err != nil {
			log.Fatalf("RF training failed for H=%d: %v", h, err)
		}

		// Evaluate RF on test set
		testRFProbs := make([]float64, len(split.XTest))
		for i, x := range split.XTest {
			p, _ := rfModel.PredictProbability(x)
			testRFProbs[i] = p
		}
		rfTestMetrics := ml.EvaluateModel("RandomForest", "test", split.YTest, testRFProbs, rfModel.Threshold)

		rec := SensitivityRecord{
			Horizon:        h,
			TotalRows:      totalRows,
			PositiveCount:  posCount,
			NegativeCount:  negCount,
			Prevalence:     prev,
			TrainRuns:      manifest.TrainRunCount,
			ValRuns:        manifest.ValRunCount,
			TestRuns:       manifest.TestRunCount,
			TestSamples:    len(split.XTest),
			TestPositives:  lrTestMetrics.ConfusionMatrix.TP + lrTestMetrics.ConfusionMatrix.FN,
			TestPrevalence: lrTestMetrics.Prevalence,
			LR_ROCAUC:      lrTestMetrics.ROCAUC,
			LR_PRAUC:       lrTestMetrics.PRAUC,
			LR_F1:          lrTestMetrics.F1Score,
			LR_Precision:   lrTestMetrics.Precision,
			LR_Recall:      lrTestMetrics.Recall,
			LR_Brier:       lrTestMetrics.BrierScore,
			LR_Threshold:   lrModel.Threshold,
			RF_ROCAUC:      rfTestMetrics.ROCAUC,
			RF_PRAUC:       rfTestMetrics.PRAUC,
			RF_F1:          rfTestMetrics.F1Score,
			RF_Precision:   rfTestMetrics.Precision,
			RF_Recall:      rfTestMetrics.Recall,
			RF_Brier:       rfTestMetrics.BrierScore,
			RF_Threshold:   rfModel.Threshold,
			SuccessfulRuns: len(loadedRuns),
			ExcludedRuns:   0,
		}
		records = append(records, rec)

		vizPoints = append(vizPoints, viz.HorizonSensitivityPoint{
			Horizon:       h,
			TotalRows:     totalRows,
			PositiveCount: posCount,
			Prevalence:    prev,
			LR_ROCAUC:     lrTestMetrics.ROCAUC,
			LR_PRAUC:      lrTestMetrics.PRAUC,
			LR_F1:         lrTestMetrics.F1Score,
			RF_ROCAUC:     rfTestMetrics.ROCAUC,
			RF_PRAUC:      rfTestMetrics.PRAUC,
			RF_F1:         rfTestMetrics.F1Score,
		})

		fmt.Printf("H=%d completed in %v: Rows=%d, Prev=%.2f%% | LR ROC-AUC=%s, PR-AUC=%.4f, F1=%.4f | RF ROC-AUC=%s, PR-AUC=%.4f, F1=%.4f\n",
			h, time.Since(t0), totalRows, prev*100,
			formatAUC(lrTestMetrics.ROCAUC), lrTestMetrics.PRAUC, lrTestMetrics.F1Score,
			formatAUC(rfTestMetrics.ROCAUC), rfTestMetrics.PRAUC, rfTestMetrics.F1Score)
	}

	// 3. Write CSV
	csvPath := filepath.Join(*outDir, "horizon_sensitivity.csv")
	fCSV, err := os.Create(csvPath)
	if err != nil {
		log.Fatalf("Failed to create CSV: %v", err)
	}
	defer fCSV.Close()

	w := csv.NewWriter(fCSV)
	w.Write([]string{
		"horizon", "total_rows", "positive_count", "negative_count", "prevalence",
		"train_runs", "val_runs", "test_runs", "test_samples", "test_prevalence",
		"lr_roc_auc", "lr_pr_auc", "lr_f1", "lr_precision", "lr_recall", "lr_brier", "lr_threshold",
		"rf_roc_auc", "rf_pr_auc", "rf_f1", "rf_precision", "rf_recall", "rf_brier", "rf_threshold",
		"successful_runs", "excluded_runs",
	})
	for _, r := range records {
		w.Write([]string{
			strconv.Itoa(r.Horizon),
			strconv.Itoa(r.TotalRows),
			strconv.Itoa(r.PositiveCount),
			strconv.Itoa(r.NegativeCount),
			fmt.Sprintf("%.6f", r.Prevalence),
			strconv.Itoa(r.TrainRuns),
			strconv.Itoa(r.ValRuns),
			strconv.Itoa(r.TestRuns),
			strconv.Itoa(r.TestSamples),
			fmt.Sprintf("%.6f", r.TestPrevalence),
			formatAUC(r.LR_ROCAUC),
			fmt.Sprintf("%.4f", r.LR_PRAUC),
			fmt.Sprintf("%.4f", r.LR_F1),
			fmt.Sprintf("%.4f", r.LR_Precision),
			fmt.Sprintf("%.4f", r.LR_Recall),
			fmt.Sprintf("%.4f", r.LR_Brier),
			fmt.Sprintf("%.2f", r.LR_Threshold),
			formatAUC(r.RF_ROCAUC),
			fmt.Sprintf("%.4f", r.RF_PRAUC),
			fmt.Sprintf("%.4f", r.RF_F1),
			fmt.Sprintf("%.4f", r.RF_Precision),
			fmt.Sprintf("%.4f", r.RF_Recall),
			fmt.Sprintf("%.4f", r.RF_Brier),
			fmt.Sprintf("%.2f", r.RF_Threshold),
			strconv.Itoa(r.SuccessfulRuns),
			strconv.Itoa(r.ExcludedRuns),
		})
	}
	w.Flush()
	fmt.Printf("\nWrote sensitivity CSV to:  %s\n", csvPath)

	// 4. Write JSON
	report := SensitivityReport{
		GeneratedAtUTC: time.Now().UTC().Format(time.RFC3339),
		WarmUpBlocks:   *warmup,
		EvaluatedSeeds: "Seed 3 test split (Seeds 1-2 train/val)",
		Horizons:       records,
	}
	bJSON, _ := json.MarshalIndent(report, "", "  ")
	jsonPath := filepath.Join(*outDir, "horizon_sensitivity.json")
	os.WriteFile(jsonPath, bJSON, 0644)
	fmt.Printf("Wrote sensitivity JSON to: %s\n", jsonPath)

	// 5. Render publication chart
	chartPath := filepath.Join(chartsDir, "horizon_sensitivity.png")
	if err := viz.PlotHorizonSensitivity(vizPoints, chartPath); err != nil {
		log.Fatalf("Failed to plot chart: %v", err)
	}
	fmt.Printf("Rendered sensitivity chart: %s\n", chartPath)

	// Copy chart to docs/figures/
	docsFigDir := filepath.Join("docs", "figures")
	os.MkdirAll(docsFigDir, 0755)
	copyFile(chartPath, filepath.Join(docsFigDir, "horizon_sensitivity.png"))
	fmt.Printf("Copied sensitivity chart to: docs/figures/horizon_sensitivity.png\n")
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}
