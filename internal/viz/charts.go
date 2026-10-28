// Package viz implements pure-Go chart generation using gonum/plot.
package viz

import (
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"sort"

	"gonum.org/v1/plot"
	"gonum.org/v1/plot/plotter"
	"gonum.org/v1/plot/plotutil"
	"gonum.org/v1/plot/vg"
)

// ScenarioStats holds summary statistics for plotting scenario comparisons.
type ScenarioStats struct {
	ScenarioName       string
	ForkRateMean       float64
	ForkRateStd        float64
	StaleRateMean      float64
	StaleRateStd       float64
	PropTimeMean       float64
	PropTimeP95        float64
	NodeViewConsMean   float64
	NodeViewConsStd    float64
	OfferedTPSMean     float64
	ProcessedTPSMean   float64
	RawTPSMean         float64
	GoodputMean        float64
	PeerAvailMean      float64
	OfferedTxMean      float64
	ProcessedTxMean    float64
	FinalPendingTxMean float64
}

// CurvePoint represents a single point on a curve (e.g. ROC or PR).
type CurvePoint struct {
	X float64
	Y float64
}

// PlotForkRateByScenario generates a bar chart with standard deviation error bars.
func PlotForkRateByScenario(stats []ScenarioStats, outPath string) error {
	p := plot.New()
	p.Title.Text = "Fork Rate by Scenario (Mean ± Std Dev)"
	p.Y.Label.Text = "Fork Rate (episodes / total blocks)"
	p.X.Label.Text = "Scenario"

	n := len(stats)
	values := make(plotter.Values, n)
	yerrs := make(plotter.Errors, n)
	names := make([]string, n)

	for i, s := range stats {
		values[i] = s.ForkRateMean
		yerrs[i].Low = mathMin(s.ForkRateStd, s.ForkRateMean)
		yerrs[i].High = s.ForkRateStd
		names[i] = s.ScenarioName
	}

	w := vg.Points(24)
	bars, err := plotter.NewBarChart(values, w)
	if err != nil {
		return err
	}
	bars.LineStyle.Width = vg.Length(1)
	bars.Color = color.RGBA{R: 52, G: 152, B: 219, A: 255}

	p.Add(bars)
	p.NominalX(names...)

	if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
		return err
	}
	return p.Save(8*vg.Inch, 5*vg.Inch, outPath)
}

// PlotStaleRateByScenario generates a chart of stale block rates by scenario.
func PlotStaleRateByScenario(stats []ScenarioStats, outPath string) error {
	p := plot.New()
	p.Title.Text = "Stale Block Rate by Scenario (Mean ± Std Dev)"
	p.Y.Label.Text = "Stale Rate (stale blocks / total blocks)"
	p.X.Label.Text = "Scenario"

	n := len(stats)
	values := make(plotter.Values, n)
	names := make([]string, n)

	for i, s := range stats {
		values[i] = s.StaleRateMean
		names[i] = s.ScenarioName
	}

	w := vg.Points(24)
	bars, err := plotter.NewBarChart(values, w)
	if err != nil {
		return err
	}
	bars.LineStyle.Width = vg.Length(1)
	bars.Color = color.RGBA{R: 231, G: 76, B: 60, A: 255}

	p.Add(bars)
	p.NominalX(names...)

	if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
		return err
	}
	return p.Save(8*vg.Inch, 5*vg.Inch, outPath)
}

// PlotPropagationPerformance generates a grouped comparison of Mean and P95 propagation delays.
func PlotPropagationPerformance(stats []ScenarioStats, outPath string) error {
	p := plot.New()
	p.Title.Text = "Block Propagation Delay by Scenario (Mean and P95)"
	p.Y.Label.Text = "Propagation Delay (seconds)"
	p.X.Label.Text = "Scenario"

	n := len(stats)
	meanVals := make(plotter.Values, n)
	p95Vals := make(plotter.Values, n)
	names := make([]string, n)

	for i, s := range stats {
		if s.PropTimeMean > 0 {
			meanVals[i] = s.PropTimeMean
		} else {
			meanVals[i] = 0
		}
		if s.PropTimeP95 > 0 {
			p95Vals[i] = s.PropTimeP95
		} else {
			p95Vals[i] = 0
		}
		names[i] = s.ScenarioName
	}

	w := vg.Points(14)
	barsMean, err := plotter.NewBarChart(meanVals, w)
	if err != nil {
		return err
	}
	barsMean.Color = color.RGBA{R: 46, G: 204, B: 113, A: 255}
	barsMean.Offset = -w / 2

	barsP95, err := plotter.NewBarChart(p95Vals, w)
	if err != nil {
		return err
	}
	barsP95.Color = color.RGBA{R: 155, G: 89, B: 182, A: 255}
	barsP95.Offset = w / 2

	p.Add(barsMean, barsP95)
	p.Legend.Add("Mean Prop Delay", barsMean)
	p.Legend.Add("P95 Prop Delay", barsP95)
	p.Legend.Top = true
	p.NominalX(names...)

	if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
		return err
	}
	return p.Save(8.5*vg.Inch, 5*vg.Inch, outPath)
}

// PlotNodeViewConsistency generates comparison and time-series plots.
func PlotNodeViewConsistency(stats []ScenarioStats, timelinePts []CurvePoint, outPath string) error {
	p := plot.New()
	p.Title.Text = "Node-View Consistency Across Scenarios & Dynamic Transition Timeline"
	p.Y.Label.Text = "Node-View Majority Consistency [0, 1]"
	p.X.Label.Text = "Proposal Index / Scenario Index"

	// Plot timeline curve if available
	if len(timelinePts) > 0 {
		pts := make(plotter.XYs, len(timelinePts))
		for i, pt := range timelinePts {
			pts[i].X = pt.X
			pts[i].Y = pt.Y
		}
		line, err := plotter.NewLine(pts)
		if err != nil {
			return err
		}
		line.Color = color.RGBA{R: 41, G: 128, B: 185, A: 255}
		line.Width = vg.Points(1.5)
		p.Add(line)
		p.Legend.Add("Dynamic Network Timeline (Transition at Proposal 5,000)", line)
	}

	if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
		return err
	}
	return p.Save(8.5*vg.Inch, 5*vg.Inch, outPath)
}

// PlotROCCurves plots receiver operating characteristic curves for all models.
func PlotROCCurves(models map[string][]CurvePoint, outPath string) error {
	p := plot.New()
	p.Title.Text = "Receiver Operating Characteristic (ROC) — Production Held-Out Test"
	p.X.Label.Text = "False Positive Rate (1 - Specificity)"
	p.Y.Label.Text = "True Positive Rate (Recall / Sensitivity)"
	p.X.Min = 0
	p.X.Max = 1
	p.Y.Min = 0
	p.Y.Max = 1

	// Add diagonal chance baseline
	diagPts := plotter.XYs{{X: 0, Y: 0}, {X: 1, Y: 1}}
	diagLine, _ := plotter.NewLine(diagPts)
	diagLine.Color = color.RGBA{R: 189, G: 195, B: 199, A: 255}
	diagLine.Dashes = []vg.Length{vg.Points(4), vg.Points(4)}
	p.Add(diagLine)
	p.Legend.Add("Chance Baseline (AUC = 0.50)", diagLine)

	colors := []color.Color{
		color.RGBA{R: 41, G: 128, B: 185, A: 255}, // Blue
		color.RGBA{R: 39, G: 174, B: 96, A: 255},  // Green
		color.RGBA{R: 230, G: 126, B: 34, A: 255}, // Orange
		color.RGBA{R: 142, G: 68, B: 173, A: 255}, // Purple
	}

	var names []string
	for name := range models {
		names = append(names, name)
	}
	sort.Strings(names)

	for i, name := range names {
		pts := models[name]
		if len(pts) == 0 {
			continue
		}
		xys := make(plotter.XYs, len(pts))
		for j, pt := range pts {
			xys[j].X = pt.X
			xys[j].Y = pt.Y
		}
		line, err := plotter.NewLine(xys)
		if err != nil {
			continue
		}
		line.Color = colors[i%len(colors)]
		line.Width = vg.Points(2.0)
		p.Add(line)
		p.Legend.Add(name, line)
	}

	p.Legend.Left = false
	p.Legend.Top = false

	if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
		return err
	}
	return p.Save(7*vg.Inch, 7*vg.Inch, outPath)
}

// PlotPrecisionRecallCurves plots PR curves for all evaluated models.
func PlotPrecisionRecallCurves(models map[string][]CurvePoint, outPath string) error {
	p := plot.New()
	p.Title.Text = "Precision-Recall Curves — Production Held-Out Test"
	p.X.Label.Text = "Recall"
	p.Y.Label.Text = "Precision"
	p.X.Min = 0
	p.X.Max = 1
	p.Y.Min = 0
	p.Y.Max = 1

	colors := []color.Color{
		color.RGBA{R: 41, G: 128, B: 185, A: 255},
		color.RGBA{R: 39, G: 174, B: 96, A: 255},
		color.RGBA{R: 230, G: 126, B: 34, A: 255},
	}

	var names []string
	for name := range models {
		names = append(names, name)
	}
	sort.Strings(names)

	for i, name := range names {
		pts := models[name]
		if len(pts) == 0 {
			continue
		}
		xys := make(plotter.XYs, len(pts))
		for j, pt := range pts {
			xys[j].X = pt.X
			xys[j].Y = pt.Y
		}
		line, err := plotter.NewLine(xys)
		if err != nil {
			continue
		}
		line.Color = colors[i%len(colors)]
		line.Width = vg.Points(2.0)
		p.Add(line)
		p.Legend.Add(name, line)
	}

	p.Legend.Left = false
	p.Legend.Top = true

	if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
		return err
	}
	return p.Save(7*vg.Inch, 7*vg.Inch, outPath)
}

// FeatureImportance represents a feature name and its split frequency count.
type FeatureImportance struct {
	Feature string
	Score   float64
}

// PlotFeatureImportance plots Random Forest tree split counts.
func PlotFeatureImportance(feats []FeatureImportance, outPath string) error {
	p := plot.New()
	p.Title.Text = "Random Forest Tree Split Frequency (Feature Importance)"
	p.Y.Label.Text = "Total Decision Split Count Across Ensemble"
	p.X.Label.Text = "Engineered Feature"

	// Take top 10 features
	topN := 10
	if len(feats) < topN {
		topN = len(feats)
	}

	values := make(plotter.Values, topN)
	names := make([]string, topN)

	for i := 0; i < topN; i++ {
		values[i] = feats[i].Score
		names[i] = feats[i].Feature
	}

	w := vg.Points(18)
	bars, err := plotter.NewBarChart(values, w)
	if err != nil {
		return err
	}
	bars.LineStyle.Width = vg.Length(1)
	bars.Color = color.RGBA{R: 52, G: 73, B: 94, A: 255}

	p.Add(bars)
	p.NominalX(names...)

	if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
		return err
	}
	return p.Save(9*vg.Inch, 5.5*vg.Inch, outPath)
}

// PlotTPSVsForkRate plots the trade-off between Offered Throughput (Raw TPS) and Fork Rate.
func PlotTPSVsForkRate(stats []ScenarioStats, outPath string) error {
	p := plot.New()
	p.Title.Text = "Trade-Off: Offered Throughput vs. Fork Rate"
	p.X.Label.Text = "Offered Transaction Throughput (tx / simulated second)"
	p.Y.Label.Text = "Fork Rate (episodes / total blocks)"

	for idx, s := range stats {
		tps := s.OfferedTPSMean
		if tps <= 0 {
			tps = s.RawTPSMean
		}
		pts := plotter.XYs{{X: tps, Y: s.ForkRateMean}}
		sc, err := plotter.NewScatter(pts)
		if err != nil {
			continue
		}
		sc.GlyphStyle.Radius = vg.Points(6)
		sc.GlyphStyle.Color = plotutil.Color(idx)
		p.Add(sc)
		p.Legend.Add(fmt.Sprintf("%s (%.1f off-tx/s, %.1f%% forks)", s.ScenarioName, tps, s.ForkRateMean*100), sc)
	}

	p.Legend.Top = true
	p.Legend.Left = false

	if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
		return err
	}
	return p.Save(8.5*vg.Inch, 5.5*vg.Inch, outPath)
}

// PlotGoodputVsForkRate plots the trade-off between Goodput and Fork Rate.
func PlotGoodputVsForkRate(stats []ScenarioStats, outPath string) error {
	p := plot.New()
	p.Title.Text = "Trade-Off: Goodput vs. Fork Rate"
	p.X.Label.Text = "Goodput: Finalized Non-Stale Transactions (tx / simulated second)"
	p.Y.Label.Text = "Fork Rate (episodes / total blocks)"

	for idx, s := range stats {
		pts := plotter.XYs{{X: s.GoodputMean, Y: s.ForkRateMean}}
		sc, err := plotter.NewScatter(pts)
		if err != nil {
			continue
		}
		sc.GlyphStyle.Radius = vg.Points(6)
		sc.GlyphStyle.Color = plotutil.Color(idx)
		p.Add(sc)
		p.Legend.Add(fmt.Sprintf("%s (%.1f goodput, %.1f%% forks)", s.ScenarioName, s.GoodputMean, s.ForkRateMean*100), sc)
	}

	p.Legend.Top = true
	p.Legend.Left = false

	if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
		return err
	}
	return p.Save(8.5*vg.Inch, 5.5*vg.Inch, outPath)
}

// HorizonSensitivityPoint holds metrics for a specific forecast horizon H.
type HorizonSensitivityPoint struct {
	Horizon       int      `json:"horizon"`
	TotalRows     int      `json:"total_rows"`
	PositiveCount int      `json:"positive_count"`
	Prevalence    float64  `json:"prevalence"`
	LR_ROCAUC     *float64 `json:"lr_roc_auc"`
	LR_PRAUC      float64  `json:"lr_pr_auc"`
	LR_F1         float64  `json:"lr_f1"`
	RF_ROCAUC     *float64 `json:"rf_roc_auc"`
	RF_PRAUC      float64  `json:"rf_pr_auc"`
	RF_F1         float64  `json:"rf_f1"`
}

// PlotHorizonSensitivity renders a comparative line plot of predictive performance and prevalence across horizons H=1..5.
func PlotHorizonSensitivity(points []HorizonSensitivityPoint, outPath string) error {
	p := plot.New()
	p.Title.Text = "Forecast Horizon Sensitivity Analysis (H = 1 to 5 Blocks)"
	p.X.Label.Text = "Forecast Horizon H (blocks ahead)"
	p.Y.Label.Text = "Metric Score / Class Prevalence"
	p.Y.Min = 0.0
	p.Y.Max = 1.0

	// Sort by horizon
	sort.Slice(points, func(i, j int) bool {
		return points[i].Horizon < points[j].Horizon
	})

	n := len(points)
	lrROCPts := make(plotter.XYs, 0, n)
	rfROCPts := make(plotter.XYs, 0, n)
	lrPRPts := make(plotter.XYs, 0, n)
	rfPRPts := make(plotter.XYs, 0, n)
	prevPts := make(plotter.XYs, 0, n)

	for _, pt := range points {
		x := float64(pt.Horizon)
		if pt.LR_ROCAUC != nil {
			lrROCPts = append(lrROCPts, plotter.XY{X: x, Y: *pt.LR_ROCAUC})
		}
		if pt.RF_ROCAUC != nil {
			rfROCPts = append(rfROCPts, plotter.XY{X: x, Y: *pt.RF_ROCAUC})
		}
		lrPRPts = append(lrPRPts, plotter.XY{X: x, Y: pt.LR_PRAUC})
		rfPRPts = append(rfPRPts, plotter.XY{X: x, Y: pt.RF_PRAUC})
		prevPts = append(prevPts, plotter.XY{X: x, Y: pt.Prevalence})
	}

	// 1. Logistic Regression ROC-AUC
	if len(lrROCPts) > 0 {
		l, s, err := plotter.NewLinePoints(lrROCPts)
		if err == nil {
			l.Color = color.RGBA{R: 41, G: 128, B: 185, A: 255} // Blue
			l.Width = vg.Points(2.5)
			s.Color = l.Color
			s.Radius = vg.Points(4)
			p.Add(l, s)
			p.Legend.Add("Logistic Regression ROC-AUC", l)
		}
	}

	// 2. Random Forest ROC-AUC
	if len(rfROCPts) > 0 {
		l, s, err := plotter.NewLinePoints(rfROCPts)
		if err == nil {
			l.Color = color.RGBA{R: 39, G: 174, B: 96, A: 255} // Green
			l.Width = vg.Points(2.5)
			s.Color = l.Color
			s.Radius = vg.Points(4)
			p.Add(l, s)
			p.Legend.Add("Random Forest ROC-AUC", l)
		}
	}

	// 3. Logistic Regression PR-AUC
	if len(lrPRPts) > 0 {
		l, s, err := plotter.NewLinePoints(lrPRPts)
		if err == nil {
			l.Color = color.RGBA{R: 230, G: 126, B: 34, A: 255} // Orange
			l.Width = vg.Points(2.0)
			l.Dashes = []vg.Length{vg.Points(4), vg.Points(2)}
			s.Color = l.Color
			s.Radius = vg.Points(3.5)
			p.Add(l, s)
			p.Legend.Add("Logistic Regression PR-AUC", l)
		}
	}

	// 4. Random Forest PR-AUC
	if len(rfPRPts) > 0 {
		l, s, err := plotter.NewLinePoints(rfPRPts)
		if err == nil {
			l.Color = color.RGBA{R: 142, G: 68, B: 173, A: 255} // Purple
			l.Width = vg.Points(2.0)
			l.Dashes = []vg.Length{vg.Points(4), vg.Points(2)}
			s.Color = l.Color
			s.Radius = vg.Points(3.5)
			p.Add(l, s)
			p.Legend.Add("Random Forest PR-AUC", l)
		}
	}

	// 5. Positive Class Prevalence
	if len(prevPts) > 0 {
		l, s, err := plotter.NewLinePoints(prevPts)
		if err == nil {
			l.Color = color.RGBA{R: 127, G: 140, B: 141, A: 255} // Gray
			l.Width = vg.Points(1.5)
			l.Dashes = []vg.Length{vg.Points(2), vg.Points(2)}
			s.Color = l.Color
			s.Radius = vg.Points(3)
			p.Add(l, s)
			p.Legend.Add("Positive Fork Prevalence", l)
		}
	}

	p.Legend.Top = false
	p.Legend.Left = false

	if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
		return err
	}
	return p.Save(8.5*vg.Inch, 6*vg.Inch, outPath)
}

func mathMin(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
