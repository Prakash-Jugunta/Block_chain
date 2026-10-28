// Command train trains Logistic Regression and Random Forest models on the run-level train split,
// tunes decision thresholds on the validation split, and saves model artifacts.
//
// Usage:
//
//	go run ./cmd/train --dataset outputs/ml/dataset/fork_risk_dataset.csv --manifest outputs/ml/splits/split_manifest.json --out-dir outputs/ml/models
//
// Flags:
//
//	--dataset   Path to dataset CSV (default: outputs/ml/dataset/fork_risk_dataset.csv)
//	--manifest  Path to split_manifest.json (default: outputs/ml/splits/split_manifest.json)
//	--out-dir   Base directory to save trained model artifacts (default: outputs/ml/models)
//	--model     Model to train ("all", "logistic", "forest") (default: "all")
package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"

	"github.com/pranav-k/forkrisk/internal/features"
	"github.com/pranav-k/forkrisk/internal/ml"
)

func formatAUC(auc *float64) string {
	if auc == nil {
		return "N/A"
	}
	return fmt.Sprintf("%.4f", *auc)
}

func main() {
	datasetPath := flag.String("dataset", "outputs/ml/dataset/fork_risk_dataset.csv", "Path to dataset.csv")
	manifestPath := flag.String("manifest", "outputs/ml/splits/split_manifest.json", "Path to split_manifest.json")
	outDir := flag.String("out-dir", "outputs/ml/models", "Directory to write model artifacts")
	modelType := flag.String("model", "all", "Model to train: 'all', 'logistic', or 'forest'")
	flag.Parse()

	rows, err := loadDatasetRows(*datasetPath)
	if err != nil {
		log.Fatalf("Failed to load dataset: %v", err)
	}

	manifest, err := ml.LoadSplitManifest(*manifestPath)
	if err != nil {
		log.Fatalf("Failed to load split manifest: %v", err)
	}

	split := buildSplitFromManifest(rows, manifest)
	fmt.Printf("Loaded split: %d train rows (%d runs), %d val rows (%d runs)\n",
		len(split.XTrain), manifest.TrainRunCount, len(split.XVal), manifest.ValRunCount)

	// 1. Train Logistic Regression
	if *modelType == "all" || *modelType == "logistic" {
		fmt.Println("\n--- Training Logistic Regression Baseline ---")
		lrCfg := ml.DefaultLogisticConfig()
		lr := ml.NewLogisticRegression(features.CanonicalFeatures, lrCfg)

		if err := lr.Train(split.XTrain, split.YTrain, split.XVal, split.YVal); err != nil {
			log.Fatalf("Training Logistic Regression failed: %v", err)
		}

		lrOutDir := filepath.Join(*outDir, "logistic_regression")
		lrPath := filepath.Join(lrOutDir, "model.json")
		if err := lr.Save(lrPath); err != nil {
			log.Fatalf("Saving Logistic Regression failed: %v", err)
		}

		fmt.Printf("Logistic Regression trained successfully!\n")
		fmt.Printf("Optimal Threshold (tuned on val): %.2f\n", lr.Threshold)
		fmt.Printf("Train ROC-AUC: %s | Train F1: %.4f\n", formatAUC(lr.TrainStats.ROCAUC), lr.TrainStats.F1Score)
		fmt.Printf("Val   ROC-AUC: %s | Val   F1: %.4f\n", formatAUC(lr.ValStats.ROCAUC), lr.ValStats.F1Score)
		fmt.Printf("Model saved to: %s\n", lrPath)
	}

	// 2. Train Random Forest
	if *modelType == "all" || *modelType == "forest" {
		fmt.Println("\n--- Training Random Forest Candidate ---")
		rfCfg := ml.DefaultForestConfig()
		rf := ml.NewRandomForest(features.CanonicalFeatures, rfCfg)

		if err := rf.Train(split.XTrain, split.YTrain, split.XVal, split.YVal); err != nil {
			log.Fatalf("Training Random Forest failed: %v", err)
		}

		rfOutDir := filepath.Join(*outDir, "random_forest")
		rfPath := filepath.Join(rfOutDir, "model.json")
		if err := rf.Save(rfPath); err != nil {
			log.Fatalf("Saving Random Forest failed: %v", err)
		}

		fmt.Printf("Random Forest trained successfully!\n")
		fmt.Printf("Optimal Threshold (tuned on val): %.2f\n", rf.Threshold)
		fmt.Printf("Train ROC-AUC: %s | Train F1: %.4f\n", formatAUC(rf.TrainStats.ROCAUC), rf.TrainStats.F1Score)
		fmt.Printf("Val   ROC-AUC: %s | Val   F1: %.4f\n", formatAUC(rf.ValStats.ROCAUC), rf.ValStats.F1Score)
		fmt.Printf("Model saved to: %s\n", rfPath)
	}

	fmt.Println("\n=== Training Completed Successfully ===")
}

func buildSplitFromManifest(rows []features.DatasetRow, manifest *ml.SplitManifest) *ml.DatasetSplit {
	trainSet := make(map[string]bool)
	for _, r := range manifest.TrainRuns {
		trainSet[r] = true
	}
	valSet := make(map[string]bool)
	for _, r := range manifest.ValRuns {
		valSet[r] = true
	}
	testSet := make(map[string]bool)
	for _, r := range manifest.TestRuns {
		testSet[r] = true
	}

	split := &ml.DatasetSplit{
		Features: features.CanonicalFeatures,
	}

	for _, r := range rows {
		vec := make([]float64, len(features.CanonicalFeatures))
		for i, featName := range features.CanonicalFeatures {
			vec[i] = r.Features[featName]
		}

		if trainSet[r.RunID] {
			split.XTrain = append(split.XTrain, vec)
			split.YTrain = append(split.YTrain, float64(r.LabelForkH))
			split.TrainRows = append(split.TrainRows, r)
		} else if valSet[r.RunID] {
			split.XVal = append(split.XVal, vec)
			split.YVal = append(split.YVal, float64(r.LabelForkH))
			split.ValRows = append(split.ValRows, r)
		} else if testSet[r.RunID] {
			split.XTest = append(split.XTest, vec)
			split.YTest = append(split.YTest, float64(r.LabelForkH))
			split.TestRows = append(split.TestRows, r)
		}
	}

	return split
}

func loadDatasetRows(path string) ([]features.DatasetRow, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	reader := csv.NewReader(f)
	records, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}

	if len(records) < 2 {
		return nil, fmt.Errorf("dataset has no data rows")
	}

	header := records[0]
	colIdx := make(map[string]int)
	for i, h := range header {
		colIdx[h] = i
	}

	var rows []features.DatasetRow
	for _, rec := range records[1:] {
		seed, _ := strconv.ParseInt(rec[colIdx["seed"]], 10, 64)
		h, _ := strconv.Atoi(rec[colIdx["block_height"]])
		tSim, _ := strconv.ParseFloat(rec[colIdx["sim_time"]], 64)
		lbl, _ := strconv.Atoi(rec[colIdx[features.TargetColumn]])

		feats := make(map[string]float64)
		for _, fName := range features.CanonicalFeatures {
			idx, ok := colIdx[fName]
			if ok {
				val, _ := strconv.ParseFloat(rec[idx], 64)
				feats[fName] = val
			}
		}

		rows = append(rows, features.DatasetRow{
			RunID:       rec[colIdx["run_id"]],
			Scenario:    rec[colIdx["scenario"]],
			Seed:        seed,
			BlockHeight: h,
			SimTime:     tSim,
			Features:    feats,
			LabelForkH:  lbl,
		})
	}

	return rows, nil
}
