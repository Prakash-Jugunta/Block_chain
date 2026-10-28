package simulate_test

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/pranav-k/forkrisk/internal/features"
	"github.com/pranav-k/forkrisk/internal/ml/drift"
)

func TestDriftAudit_AllSeeds(t *testing.T) {
	runsDir := filepath.Join("..", "outputs", "experiments", "milestone4", "runs")
	if _, err := os.Stat(runsDir); err != nil {
		t.Skip("Production runs directory not present")
	}

	evalSeeds := make([]int, 0, 27)
	for s := 4; s <= 30; s++ {
		evalSeeds = append(evalSeeds, s)
	}

	bestCfg := drift.DetectorConfig{
		WindowSize:     200,
		ThresholdSigma: 2.0,
		Epsilon:        1e-6,
	}

	type SeedDriftStat struct {
		Seed           int
		Detected       bool
		FirstAlarmProp int
		DelayProposals int
		PreAlarms      int
		PreTotal       int
		PostAlarms     int
		PostTotal      int
	}

	var stats []SeedDriftStat
	var detectedDelays []int
	totalPreAlarms := 0
	totalPrePoints := 0
	totalPostAlarms := 0
	totalPostPoints := 0

	for _, s := range evalSeeds {
		runID := fmt.Sprintf("dynamic_network_seed%d", s)
		dVals, dHeights := extractFeatStreamForAudit(runsDir, runID, features.FeatLatencyMean10)
		if len(dVals) == 0 {
			t.Logf("Seed %d: no feature stream found", s)
			continue
		}

		det := drift.NewSlidingWindowDetector(bestCfg)
		results := det.EvaluateStream(dVals, dHeights)

		st := SeedDriftStat{Seed: s}
		firstAlarm := -1

		for _, r := range results {
			if !r.HasSuffHistory {
				continue
			}
			if r.BlockHeight < 5000 {
				st.PreTotal++
				if r.DriftDetected {
					st.PreAlarms++
				}
			} else {
				st.PostTotal++
				if r.DriftDetected {
					st.PostAlarms++
					if firstAlarm < 0 {
						firstAlarm = r.BlockHeight
					}
				}
			}
		}

		if firstAlarm >= 5000 {
			st.Detected = true
			st.FirstAlarmProp = firstAlarm
			st.DelayProposals = firstAlarm - 5000
			detectedDelays = append(detectedDelays, st.DelayProposals)
		} else {
			st.Detected = false
			st.FirstAlarmProp = -1
			st.DelayProposals = -1
		}

		totalPreAlarms += st.PreAlarms
		totalPrePoints += st.PreTotal
		totalPostAlarms += st.PostAlarms
		totalPostPoints += st.PostTotal
		stats = append(stats, st)
	}

	t.Logf("=== Per-Seed Drift Detection Analysis (W=200, theta=2.0sigma) ===")
	t.Logf("Total dynamic_network test seeds: %d", len(stats))
	detectedCount := 0
	undetectedCount := 0
	for _, st := range stats {
		if st.Detected {
			detectedCount++
			t.Logf("Seed %2d: DETECTED at proposal %d (delay: %d proposals) | PreAlarms: %d/%d (%.2f%%) | PostAlarms: %d/%d (%.2f%%)",
				st.Seed, st.FirstAlarmProp, st.DelayProposals, st.PreAlarms, st.PreTotal, float64(st.PreAlarms)/float64(st.PreTotal)*100, st.PostAlarms, st.PostTotal, float64(st.PostAlarms)/float64(st.PostTotal)*100)
		} else {
			undetectedCount++
			t.Logf("Seed %2d: NEVER DETECTED | PreAlarms: %d/%d | PostAlarms: %d/%d",
				st.Seed, st.PreAlarms, st.PreTotal, st.PostAlarms, st.PostTotal)
		}
	}

	t.Logf("Detection Summary: Detected %d / %d (%.2f%%), Undetected %d / %d (%.2f%%)",
		detectedCount, len(stats), float64(detectedCount)/float64(len(stats))*100,
		undetectedCount, len(stats), float64(undetectedCount)/float64(len(stats))*100)

	sort.Ints(detectedDelays)
	if len(detectedDelays) > 0 {
		sumD := 0
		for _, d := range detectedDelays {
			sumD += d
		}
		meanD := float64(sumD) / float64(len(detectedDelays))
		medianD := float64(detectedDelays[len(detectedDelays)/2])
		p25 := float64(detectedDelays[int(math.Floor(0.25*float64(len(detectedDelays)-1)))])
		p75 := float64(detectedDelays[int(math.Floor(0.75*float64(len(detectedDelays)-1)))])
		p95 := float64(detectedDelays[int(math.Floor(0.95*float64(len(detectedDelays)-1)))])
		minD := detectedDelays[0]
		maxD := detectedDelays[len(detectedDelays)-1]

		t.Logf("Delay Stats (among %d detected seeds): Min=%d, P25=%.1f, Median=%.1f, Mean=%.2f, P75=%.1f, P95=%.1f, Max=%d",
			len(detectedDelays), minD, p25, medianD, meanD, p75, p95, maxD)
	}

	preFPR := float64(totalPreAlarms) / float64(totalPrePoints) * 100
	postAlarmRate := float64(totalPostAlarms) / float64(totalPostPoints) * 100
	t.Logf("Pre-transition (1000..4999): %d alarms / %d points (FPR: %.2f%%)", totalPreAlarms, totalPrePoints, preFPR)
	t.Logf("Post-transition (5000..9999): %d alarms / %d points (Alarm Rate: %.2f%%)", totalPostAlarms, totalPostPoints, postAlarmRate)
}

func extractFeatStreamForAudit(runsDir, runID, featName string) ([]float64, []int) {
	logPath := filepath.Join(runsDir, runID, "events.jsonl")
	evs, err := features.ReadEventLog(logPath)
	if err != nil {
		return nil, nil
	}
	proposals := features.ExtractProposals(evs)
	if len(proposals) == 0 {
		return nil, nil
	}

	arrivalsByBlock := make(map[string][]float64)
	for _, e := range evs {
		if (e.EventType == "BlockArrive" || e.EventType == "BlockOrphan") && e.BlockHash != "" {
			arrivalsByBlock[e.BlockHash] = append(arrivalsByBlock[e.BlockHash], e.SimTime)
		}
	}

	var vals []float64
	var heights []int

	for idx, p := range proposals {
		if idx%5 != 0 {
			continue
		}
		startIdx := idx - 9
		if startIdx < 0 {
			startIdx = 0
		}
		var delays []float64
		predTime := p.SimTime
		for _, wp := range proposals[startIdx : idx+1] {
			for _, arrTime := range arrivalsByBlock[wp.Hash] {
				if arrTime <= predTime {
					d := arrTime - wp.SimTime
					if d >= 0 {
						delays = append(delays, d)
					}
				}
			}
		}
		val := 0.0
		if len(delays) > 0 {
			sum := 0.0
			for _, d := range delays {
				sum += d
			}
			val = sum / float64(len(delays))
		}
		vals = append(vals, val)
		heights = append(heights, p.ProposalIndex)
	}
	return vals, heights
}
