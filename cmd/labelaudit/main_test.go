package main

import (
	"testing"

	"github.com/pranav-k/forkrisk/internal/features"
	"github.com/pranav-k/forkrisk/internal/fork"
)

// TestIndependentReferenceLabeler_ForkBirthWithinHorizon tests that a new fork born strictly within
// the forward horizon (t, t+H] with BirthTime > T_t produces label 1.
func TestIndependentReferenceLabeler_ForkBirthWithinHorizon(t *testing.T) {
	proposals := []features.BlockProposalInfo{
		{Height: 11, SimTime: 15.0},
		{Height: 12, SimTime: 16.0},
		{Height: 13, SimTime: 17.0},
		{Height: 14, SimTime: 18.0},
		{Height: 15, SimTime: 19.0},
	}

	episodes := []fork.ForkEpisode{
		{
			ID:             1,
			BirthTime:      17.2,
			BirthHeight:    13,
			ResolutionTime: 17.8,
			Duration:       0.6,
			Height:         13,
		},
	}

	// For t=11, H=3, W=10, max=15 (censored boundary is 12).
	// t=11: window is (11, 14], BirthHeight=13 in (11, 14], BirthTime=17.2 > 15.0 => 1.
	// t=12: window is (12, 15], BirthHeight=13 in (12, 15], BirthTime=17.2 > 16.0 => 1.
	refLabels, eligible := IndependentReferenceLabeler(proposals, episodes, 3, 10)

	if len(eligible) != 2 || eligible[0] != 11 || eligible[1] != 12 {
		t.Fatalf("unexpected eligible heights: %v", eligible)
	}

	if refLabels[11] != 1 {
		t.Errorf("expected label 1 for height 11, got %d", refLabels[11])
	}
	if refLabels[12] != 1 {
		t.Errorf("expected label 1 for height 12, got %d", refLabels[12])
	}
}

// TestIndependentReferenceLabeler_EpisodeContinuationRejected verifies that an earlier fork episode
// that merely continues across heights without being born in (t, t+H] is rejected (label 0).
func TestIndependentReferenceLabeler_EpisodeContinuationRejected(t *testing.T) {
	proposals := []features.BlockProposalInfo{
		{Height: 11, SimTime: 15.0},
		{Height: 12, SimTime: 17.0},
		{Height: 13, SimTime: 19.0},
		{Height: 14, SimTime: 21.0},
		{Height: 15, SimTime: 23.0},
	}

	// Fork was born at height 11 at time 15.5.
	// It continues across blocks and reaches max height 14, resolving at 21.5.
	episodes := []fork.ForkEpisode{
		{
			ID:             1,
			BirthTime:      15.5,
			BirthHeight:    11,
			ResolutionTime: 21.5,
			Duration:       6.0,
			Height:         14, // Max height reached during continuation
		},
	}

	// For observation at height 12 (T_12 = 17.0):
	// The episode was born at 15.5 (< 17.0) and BirthHeight=11 (<= 12).
	// Even though ep.Height=14 is in (12, 15], this is a continuation, NOT a new birth!
	// Therefore, Y_12 must be 0!
	refLabels, eligible := IndependentReferenceLabeler(proposals, episodes, 3, 10)

	if refLabels[12] != 0 {
		t.Errorf("expected label 0 for height 12 (continuation rejection), got %d", refLabels[12])
	}

	// Contrast with legacy naive continuation labeler:
	oldLabeler := &OldContinuationLabeler{horizon: 3, warmup: 10}
	det := fork.DetectionResult{ForkEpisodes: episodes}
	oldLabels, _ := oldLabeler.ComputeOldLabels(proposals, det)

	if oldLabels[12] != 1 {
		t.Errorf("expected legacy continuation labeler to produce 1 (false positive), got %d", oldLabels[12])
	}

	// Ensure reference labeler also labeled height 11 correctly:
	// For t=11, window is (11, 14]. BirthHeight=11 is NOT in (11, 14], so Y_11 = 0!
	if refLabels[11] != 0 {
		t.Errorf("expected label 0 for height 11 (birth at current height, not future), got %d", refLabels[11])
	}
	_ = eligible
}

// TestIndependentReferenceLabeler_UnresolvedEpisodeBornInWindow tests that a fork episode born within
// the forward horizon that remains unresolved at simulation end (ResolutionTime == -1) is correctly labeled 1.
func TestIndependentReferenceLabeler_UnresolvedEpisodeBornInWindow(t *testing.T) {
	proposals := []features.BlockProposalInfo{
		{Height: 11, SimTime: 15.0},
		{Height: 12, SimTime: 17.0},
		{Height: 13, SimTime: 19.0},
		{Height: 14, SimTime: 21.0},
		{Height: 15, SimTime: 23.0},
	}

	// Unresolved episode born at height 13 at time 19.5
	episodes := []fork.ForkEpisode{
		{
			ID:             1,
			BirthTime:      19.5,
			BirthHeight:    13,
			ResolutionTime: -1,
			Duration:       -1,
			Height:         14,
		},
	}

	refLabels, _ := IndependentReferenceLabeler(proposals, episodes, 3, 10)

	// Observation at t=11 (T_11=15.0): window (11, 14]. BirthHeight=13, BirthTime=19.5 > 15.0 => 1.
	if refLabels[11] != 1 {
		t.Errorf("expected label 1 for height 11 from unresolved episode, got %d", refLabels[11])
	}
	// Observation at t=12 (T_12=17.0): window (12, 15]. BirthHeight=13, BirthTime=19.5 > 17.0 => 1.
	if refLabels[12] != 1 {
		t.Errorf("expected label 1 for height 12 from unresolved episode, got %d", refLabels[12])
	}
}

// TestIndependentReferenceLabeler_ForecastBoundaries verifies warm-up filtering and right-censoring boundaries.
func TestIndependentReferenceLabeler_ForecastBoundaries(t *testing.T) {
	var proposals []features.BlockProposalInfo
	for h := 1; h <= 25; h++ {
		proposals = append(proposals, features.BlockProposalInfo{
			Height:  h,
			SimTime: float64(h) * 10.0,
		})
	}

	// Warm-up = 10, Horizon = 3, MaxHeight = 25.
	// Eligible must be [11, 12, ..., 22].
	// Heights 1..10 excluded by warm-up.
	// Heights 23..25 censored by horizon.
	_, eligible := IndependentReferenceLabeler(proposals, nil, 3, 10)

	if len(eligible) != 12 {
		t.Fatalf("expected 12 eligible heights (11..22), got %d", len(eligible))
	}
	if eligible[0] != 11 {
		t.Errorf("expected first eligible height 11, got %d", eligible[0])
	}
	if eligible[len(eligible)-1] != 22 {
		t.Errorf("expected last eligible height 22 (25 - 3), got %d", eligible[len(eligible)-1])
	}

	// Now add a fork episode born at height 25 (the final block)
	episodes := []fork.ForkEpisode{
		{
			ID:             1,
			BirthTime:      255.0,
			BirthHeight:    25,
			ResolutionTime: 258.0,
			Duration:       3.0,
			Height:         25,
		},
	}

	refLabels, _ := IndependentReferenceLabeler(proposals, episodes, 3, 10)

	// Observation at height 22: window is (22, 25]. BirthHeight=25, BirthTime=255.0 > 220.0 => 1!
	if refLabels[22] != 1 {
		t.Errorf("expected height 22 to detect fork at height 25, got %d", refLabels[22])
	}
	// Observation at height 21: window is (21, 24]. BirthHeight=25 is outside => 0!
	if refLabels[21] != 0 {
		t.Errorf("expected height 21 to have label 0 (fork at height 25 outside horizon), got %d", refLabels[21])
	}
}

// TestIndependentReferenceLabeler_EquivalenceWithProduction verifies that the independent reference
// labeler and production features.Labeler produce identical results on a complex multi-episode scenario.
func TestIndependentReferenceLabeler_EquivalenceWithProduction(t *testing.T) {
	var proposals []features.BlockProposalInfo
	for h := 1; h <= 30; h++ {
		proposals = append(proposals, features.BlockProposalInfo{
			Height:  h,
			SimTime: float64(h) * 5.0,
		})
	}

	episodes := []fork.ForkEpisode{
		// 1. Transient resolved fork at height 12
		{ID: 1, BirthTime: 62.0, BirthHeight: 12, ResolutionTime: 64.0, Duration: 2.0, Height: 12},
		// 2. Multi-block fork born at height 15 continuing to height 18
		{ID: 2, BirthTime: 78.0, BirthHeight: 15, ResolutionTime: 92.0, Duration: 14.0, Height: 18},
		// 3. Unresolved fork born at height 24
		{ID: 3, BirthTime: 122.0, BirthHeight: 24, ResolutionTime: -1, Duration: -1, Height: 25},
	}

	refLabels, refEligible := IndependentReferenceLabeler(proposals, episodes, 3, 10)

	prodLabeler := features.NewLabeler(3, 10)
	prodLabels, prodEligible := prodLabeler.ComputeLabels(proposals, fork.DetectionResult{ForkEpisodes: episodes})

	if len(refEligible) != len(prodEligible) {
		t.Fatalf("eligible count mismatch: ref=%d, prod=%d", len(refEligible), len(prodEligible))
	}

	for _, h := range refEligible {
		if refLabels[h] != prodLabels[h] {
			t.Errorf("label mismatch at height %d: ref=%d, prod=%d", h, refLabels[h], prodLabels[h])
		}
	}
}
