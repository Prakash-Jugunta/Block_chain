package viz

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestChartGeneration(t *testing.T) {
	tmpDir := t.TempDir()

	stats := []ScenarioStats{
		{ScenarioName: "baseline", ForkRateMean: 0.02, ForkRateStd: 0.005, StaleRateMean: 0.35, PropTimeMean: 0.45, PropTimeP95: 0.55, RawTPSMean: 9.8, GoodputMean: 6.2},
		{ScenarioName: "latency_stress", ForkRateMean: 0.18, ForkRateStd: 0.02, StaleRateMean: 0.50, PropTimeMean: 1.25, PropTimeP95: 1.45, RawTPSMean: 9.7, GoodputMean: 4.8},
		{ScenarioName: "dynamic_network", ForkRateMean: 0.06, ForkRateStd: 0.01, StaleRateMean: 0.40, PropTimeMean: 0.70, PropTimeP95: 0.90, RawTPSMean: 9.8, GoodputMean: 5.5},
	}

	timeline := []CurvePoint{
		{X: 100, Y: 0.99},
		{X: 4900, Y: 0.99},
		{X: 5100, Y: 0.82},
		{X: 10000, Y: 0.85},
	}

	rocModels := map[string][]CurvePoint{
		"Logistic Regression": {{X: 0, Y: 0}, {X: 0.2, Y: 0.8}, {X: 1, Y: 1}},
		"Random Forest":       {{X: 0, Y: 0}, {X: 0.15, Y: 0.82}, {X: 1, Y: 1}},
	}

	prModels := map[string][]CurvePoint{
		"Logistic Regression": {{X: 0, Y: 1}, {X: 0.8, Y: 0.75}, {X: 1, Y: 0.5}},
		"Random Forest":       {{X: 0, Y: 1}, {X: 0.85, Y: 0.78}, {X: 1, Y: 0.5}},
	}

	features := []FeatureImportance{
		{Feature: "prop_time_mean_10", Score: 245},
		{Feature: "latency_mean_10", Score: 210},
		{Feature: "competing_tips", Score: 180},
	}

	plots := map[string]func(string) error{
		"fork_rate_by_scenario.png":   func(p string) error { return PlotForkRateByScenario(stats, p) },
		"stale_rate_by_scenario.png":  func(p string) error { return PlotStaleRateByScenario(stats, p) },
		"propagation_performance.png": func(p string) error { return PlotPropagationPerformance(stats, p) },
		"node_view_consistency.png":   func(p string) error { return PlotNodeViewConsistency(stats, timeline, p) },
		"roc_curves.png":              func(p string) error { return PlotROCCurves(rocModels, p) },
		"precision_recall_curves.png": func(p string) error { return PlotPrecisionRecallCurves(prModels, p) },
		"feature_importance.png":      func(p string) error { return PlotFeatureImportance(features, p) },
		"tps_vs_fork_rate.png":        func(p string) error { return PlotTPSVsForkRate(stats, p) },
		"goodput_vs_fork_rate.png":    func(p string) error { return PlotGoodputVsForkRate(stats, p) },
	}

	for name, fn := range plots {
		outPath := filepath.Join(tmpDir, name)
		if err := fn(outPath); err != nil {
			t.Fatalf("failed to generate %s: %v", name, err)
		}

		fi, err := os.Stat(outPath)
		if err != nil || fi.Size() == 0 {
			t.Fatalf("plot %s was not written or empty: %v", name, err)
		}

		// Verify PNG integrity by decoding
		f, err := os.Open(outPath)
		if err != nil {
			t.Fatalf("open %s: %v", name, err)
		}
		_, err = png.Decode(f)
		f.Close()
		if err != nil {
			t.Fatalf("file %s is not a valid PNG: %v", name, err)
		}
	}
}
