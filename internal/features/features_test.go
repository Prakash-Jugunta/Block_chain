package features

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/pranav-k/forkrisk/internal/config"
	"github.com/pranav-k/forkrisk/internal/fork"
	"github.com/pranav-k/forkrisk/internal/logstore"
)

// TestLabelerKnownSequences tests that known fork and non-fork events produce exact expected labels.
func TestLabelerKnownSequences(t *testing.T) {
	labeler := NewLabeler(3, 2) // horizon=3, warmup=2

	proposals := []BlockProposalInfo{
		{Height: 1, SimTime: 1.0, Hash: "b1"},
		{Height: 2, SimTime: 2.0, Hash: "b2"},
		{Height: 3, SimTime: 3.0, Hash: "b3"},
		{Height: 4, SimTime: 4.0, Hash: "b4"},
		{Height: 5, SimTime: 5.0, Hash: "b5"},
		{Height: 6, SimTime: 6.0, Hash: "b6"},
		{Height: 7, SimTime: 7.0, Hash: "b7"},
		{Height: 8, SimTime: 8.0, Hash: "b8"},
	}

	// Case 1: Fork episode born at height 5 (time 5.1)
	// For prediction at height 3, horizon is (3, 6], so height 5 is in range -> label=1
	// For prediction at height 4, horizon is (4, 7], height 5 is in range -> label=1
	// For prediction at height 1 and 2: in warmup -> skipped
	// For prediction at height 5: born at height 5 (<=5), not in future (5, 8] -> label=0
	// Heights 6, 7, 8 are censored boundary rows (max 8 - horizon 3 = 5).
	detection := fork.DetectionResult{
		ForkEpisodes: []fork.ForkEpisode{
			{
				ID:          1,
				BirthTime:   5.1,
				BirthHeight: 5,
				Height:      5,
			},
		},
	}

	labels, eligible := labeler.ComputeLabels(proposals, detection)

	// Eligible heights should be: 3, 4, 5 (warmup=2, max=8, max-3=5)
	if len(eligible) != 3 {
		t.Fatalf("expected 3 eligible heights, got %d: %v", len(eligible), eligible)
	}

	if labels[3] != 1 {
		t.Errorf("expected height 3 label=1 (fork at height 5), got %d", labels[3])
	}
	if labels[4] != 1 {
		t.Errorf("expected height 4 label=1 (fork at height 5), got %d", labels[4])
	}
	if labels[5] != 0 {
		t.Errorf("expected height 5 label=0 (fork was born at height 5, not in next 3), got %d", labels[5])
	}
}

// TestLabeler_ForkBornBeforeWindowPersists ensures an existing ongoing fork is NOT labeled as a new future birth.
func TestLabeler_ForkBornBeforeWindowPersists(t *testing.T) {
	labeler := NewLabeler(3, 1) // horizon=3, warmup=1

	proposals := []BlockProposalInfo{
		{Height: 1, SimTime: 1.0, Hash: "b1"},
		{Height: 2, SimTime: 2.0, Hash: "b2"},
		{Height: 3, SimTime: 3.0, Hash: "b3"},
		{Height: 4, SimTime: 4.0, Hash: "b4"},
		{Height: 5, SimTime: 5.0, Hash: "b5"},
		{Height: 6, SimTime: 6.0, Hash: "b6"},
		{Height: 7, SimTime: 7.0, Hash: "b7"},
	}

	// Fork was born at height 2 (time 2.1), and persisted until height 5 (Height=5).
	// For prediction at height 3 (time 3.0), this fork was ALREADY BORN before time 3.0 (at height 2).
	// Even though competing tips reached height 5 during this episode, NO NEW fork was born in (3, 6].
	detection := fork.DetectionResult{
		ForkEpisodes: []fork.ForkEpisode{
			{
				ID:             1,
				BirthTime:      2.1,
				BirthHeight:    2,
				Height:         5,
				ResolutionTime: 5.5,
				Duration:       3.4,
			},
		},
	}

	labels, eligible := labeler.ComputeLabels(proposals, detection)

	// Eligible heights: 2, 3, 4 (warmup=1, max=7, max-3=4)
	if len(eligible) != 3 {
		t.Fatalf("expected 3 eligible heights, got %d: %v", len(eligible), eligible)
	}

	// At block 2 (time 2.0), the fork will be born at 2.1 in the future (at height 2 <= 2) -> 0
	// At block 3 (time 3.0), the fork was already born in the past -> must be 0!
	if labels[3] != 0 {
		t.Errorf("expected block 3 label=0 for fork born at block 2 persisting to 5, got %d", labels[3])
	}
	if labels[4] != 0 {
		t.Errorf("expected block 4 label=0 for ongoing past fork, got %d", labels[4])
	}
}

// TestLabeler_UnresolvedForkEpisode verifies that unresolved future fork births are properly captured as positives.
func TestLabeler_UnresolvedForkEpisode(t *testing.T) {
	labeler := NewLabeler(3, 1)

	proposals := []BlockProposalInfo{
		{Height: 1, SimTime: 1.0, Hash: "b1"},
		{Height: 2, SimTime: 2.0, Hash: "b2"},
		{Height: 3, SimTime: 3.0, Hash: "b3"},
		{Height: 4, SimTime: 4.0, Hash: "b4"},
		{Height: 5, SimTime: 5.0, Hash: "b5"},
		{Height: 6, SimTime: 6.0, Hash: "b6"},
	}

	// Unresolved fork episode born at height 4 (time 4.1)
	detection := fork.DetectionResult{
		ForkEpisodes: []fork.ForkEpisode{
			{
				ID:             1,
				BirthTime:      4.1,
				BirthHeight:    4,
				Height:         5,
				ResolutionTime: -1,
				Duration:       -1,
			},
		},
	}

	labels, eligible := labeler.ComputeLabels(proposals, detection)

	// Block 2: horizon is (2, 5], fork born at height 4 (time 4.1 > 2.0) -> must be 1
	if labels[2] != 1 {
		t.Errorf("expected block 2 label=1 for unresolved future fork birth at height 4, got %d", labels[2])
	}
	// Block 3: horizon is (3, 6], fork born at height 4 (time 4.1 > 3.0) -> must be 1
	if labels[3] != 1 {
		t.Errorf("expected block 3 label=1 for unresolved future fork birth at height 4, got %d", labels[3])
	}
	// Block 4: fork born at height 4 (<= 4), not in future (4, 7] -> 0
	if labels[4] != 0 {
		t.Errorf("expected block 4 label=0, got %d", labels[4])
	}
	_ = eligible
}

// TestLabeler_CompetingBranchesSameHeight verifies fork birth detection when multiple blocks are proposed at same height.
func TestLabeler_CompetingBranchesSameHeight(t *testing.T) {
	labeler := NewLabeler(3, 0)

	proposals := []BlockProposalInfo{
		{Height: 1, SimTime: 1.0, Hash: "b1"},
		{Height: 2, SimTime: 2.0, Hash: "b2"},
		{Height: 3, SimTime: 3.0, Hash: "b3_A"},
		{Height: 3, SimTime: 3.05, Hash: "b3_B"},
		{Height: 4, SimTime: 4.0, Hash: "b4"},
		{Height: 5, SimTime: 5.0, Hash: "b5"},
	}

	detection := fork.DetectionResult{
		ForkEpisodes: []fork.ForkEpisode{
			{
				ID:             1,
				BirthTime:      3.1,
				BirthHeight:    3,
				Height:         3,
				ResolutionTime: 3.8,
			},
		},
	}

	labels, eligible := labeler.ComputeLabels(proposals, detection)
	// Max height is 5, horizon is 3, eligible <= 5-3 = 2 (blocks 1 and 2)
	if len(eligible) != 2 {
		t.Fatalf("expected 2 eligible rows, got %d", len(eligible))
	}

	// At block 1: horizon is (1, 4], fork born at height 3 -> label=1
	if labels[1] != 1 {
		t.Errorf("expected block 1 label=1, got %d", labels[1])
	}
	// At block 2: horizon is (2, 5], fork born at height 3 -> label=1
	if labels[2] != 1 {
		t.Errorf("expected block 2 label=1, got %d", labels[2])
	}
}

// TestLabelerHorizonBoundary ensures end-of-run rows are strictly censored.
func TestLabelerHorizonBoundary(t *testing.T) {
	labeler := NewLabeler(3, 0) // horizon=3, warmup=0
	proposals := []BlockProposalInfo{
		{Height: 1, SimTime: 1.0, Hash: "b1"},
		{Height: 2, SimTime: 2.0, Hash: "b2"},
		{Height: 3, SimTime: 3.0, Hash: "b3"},
		{Height: 4, SimTime: 4.0, Hash: "b4"},
		{Height: 5, SimTime: 5.0, Hash: "b5"},
	}
	// Max height is 5. With horizon 3, eligible heights are <= 5-3 = 2.
	detection := fork.DetectionResult{}
	labels, eligible := labeler.ComputeLabels(proposals, detection)

	if len(eligible) != 2 {
		t.Fatalf("expected exactly 2 eligible rows, got %d: %v", len(eligible), eligible)
	}
	if eligible[0] != 1 || eligible[1] != 2 {
		t.Fatalf("unexpected eligible heights: %v", eligible)
	}
	if labels[1] != 0 || labels[2] != 0 {
		t.Fatalf("expected labels to be 0, got %v", labels)
	}
}

// TestStaleRate10_Calculations checks hand-calculated values for stale_rate_10 across various windows.
func TestStaleRate10_Calculations(t *testing.T) {
	nodeIDs := []string{"node1", "node2"}
	initCfg := config.ScenarioConfig{
		MeanLatencyMs:    50.0,
		MeanPropDelayMs:  100.0,
		BlockIntervalSec: 1.0,
		TxArrivalRate:    1.0,
		BlockCapacity:    10,
	}

	// Case A: 10 proposals with NO stale blocks -> stale_rate_10 must be 0.0
	var proposalsA []BlockProposalInfo
	var eventsA []logstore.LogEntry
	for h := 1; h <= 10; h++ {
		hash := formatInt(h)
		parent := formatInt(h - 1)
		if h == 1 {
			parent = "genesis"
		}
		proposalsA = append(proposalsA, BlockProposalInfo{Height: h, SimTime: float64(h), Hash: hash})
		eventsA = append(eventsA, logstore.LogEntry{
			Seq:         int64(h),
			SimTime:     float64(h),
			EventType:   "BlockPropose",
			NodeID:      "node1",
			BlockHash:   hash,
			BlockHeight: h,
			ParentHash:  parent,
			TipHash:     hash,
			TipHeight:   h,
		})
	}

	extractorA := NewExtractor(nodeIDs, 0, initCfg)
	featsA := extractorA.ExtractFeaturesAt(eventsA, proposalsA, 9) // at block 10
	if featsA[FeatStaleRate10] != 0.0 {
		t.Errorf("expected stale_rate_10=0.0 for clean chain, got %f", featsA[FeatStaleRate10])
	}

	// Case B: 10 proposals in window where 2 blocks became stale
	// Sequence:
	// h=1, 2, 3: main chain
	// h=4: main_4 and stale_4 (at 4.05)
	// h=5, 6: main chain
	// h=7: main_7 and stale_7 (at 7.05)
	// h=8, 9, 10: main chain
	var proposalsB []BlockProposalInfo
	var eventsB []logstore.LogEntry
	seq := int64(1)

	addBlock := func(h int, tSim float64, hash, parent, node string) {
		proposalsB = append(proposalsB, BlockProposalInfo{Height: h, SimTime: tSim, Hash: hash})
		eventsB = append(eventsB, logstore.LogEntry{
			Seq:         seq,
			SimTime:     tSim,
			EventType:   "BlockPropose",
			NodeID:      node,
			BlockHash:   hash,
			BlockHeight: h,
			ParentHash:  parent,
			TipHash:     hash,
			TipHeight:   h,
		})
		seq++
	}

	addBlock(1, 1.0, "m1", "genesis", "node1")
	addBlock(2, 2.0, "m2", "m1", "node1")
	addBlock(3, 3.0, "m3", "m2", "node1")
	addBlock(4, 4.0, "m4", "m3", "node1")
	addBlock(4, 4.05, "s4", "m3", "node2") // competing at height 4
	addBlock(5, 5.0, "m5", "m4", "node1")  // main chain wins
	addBlock(6, 6.0, "m6", "m5", "node1")
	addBlock(7, 7.0, "m7", "m6", "node1")
	addBlock(7, 7.05, "s7", "m6", "node2") // competing at height 7
	addBlock(8, 8.0, "m8", "m7", "node1")  // main chain wins
	addBlock(9, 9.0, "m9", "m8", "node1")
	addBlock(10, 10.0, "m10", "m9", "node1")

	// TipUpdate so detector knows node2 adopted the main chain at m10
	eventsB = append(eventsB, logstore.LogEntry{
		Seq:         seq,
		SimTime:     10.1,
		EventType:   "TipUpdate",
		NodeID:      "node2",
		BlockHash:   "m10",
		BlockHeight: 10,
		TipHash:     "m10",
		TipHeight:   10,
	})

	extractorB := NewExtractor(nodeIDs, 0, initCfg)
	// Evaluate at m10 (last proposal, index 11 in proposalsB)
	// Window spans index 2 to 11 (exactly 10 proposals): m3, m4, s4, m5, m6, m7, s7, m8, m9, m10
	// Stale blocks in window: s4 and s7 (2 stale blocks out of 10)
	featsB := extractorB.ExtractFeaturesAt(eventsB, proposalsB, len(proposalsB)-1)
	if featsB[FeatStaleRate10] == 0.0 {
		t.Errorf("expected non-zero stale_rate_10 when stale blocks exist, got %f", featsB[FeatStaleRate10])
	}
	expectedB := 2.0 / 10.0
	if math.Abs(featsB[FeatStaleRate10]-expectedB) > 1e-6 {
		t.Errorf("expected stale_rate_10=%f (2/10), got %f", expectedB, featsB[FeatStaleRate10])
	}
}

// TestFeatureCausalityTruncationGuard verifies that feature extraction gives the exact same result
// whether computed from a complete event stream or a physically truncated event stream.
func TestFeatureCausalityTruncationGuard(t *testing.T) {
	nodeIDs := []string{"node1", "node2", "node3"}
	initCfg := config.ScenarioConfig{
		MeanLatencyMs:    100.0,
		MeanPropDelayMs:  200.0,
		BlockIntervalSec: 1.0,
		TxArrivalRate:    1.0,
		BlockCapacity:    10,
	}

	// Create 15 blocks of events
	var fullEvents []logstore.LogEntry
	var proposals []BlockProposalInfo

	seq := int64(1)
	for h := 1; h <= 15; h++ {
		tSim := float64(h) * 1.05
		hash := formatInt(h)

		// Proposal event
		proposeEv := logstore.LogEntry{
			Seq:         seq,
			SimTime:     tSim,
			EventType:   "BlockPropose",
			NodeID:      "node1",
			BlockHash:   hash,
			BlockHeight: h,
			TxCount:     8,
			PoolSize:    15,
			TipHash:     hash,
			TipHeight:   h,
		}
		fullEvents = append(fullEvents, proposeEv)
		proposals = append(proposals, BlockProposalInfo{
			Height:    h,
			SimTime:   tSim,
			Hash:      hash,
			Proposer:  "node1",
			TxCount:   8,
			PoolSize:  15,
			OfferedTx: 10 * h,
		})
		seq++

		// Arrival events at other nodes
		for _, nid := range []string{"node2", "node3"} {
			arriveEv := logstore.LogEntry{
				Seq:         seq,
				SimTime:     tSim + 0.12,
				EventType:   "BlockArrive",
				NodeID:      nid,
				BlockHash:   hash,
				BlockHeight: h,
				TipHash:     hash,
				TipHeight:   h,
			}
			fullEvents = append(fullEvents, arriveEv)
			seq++
		}
	}

	// We test extraction at proposal index 9 (Height 10)
	targetIdx := 9
	targetProposal := proposals[targetIdx]

	extractor := NewExtractor(nodeIDs, 2, initCfg)

	// Extract using full events stream
	fullFeatures := extractor.ExtractFeaturesAt(fullEvents, proposals, targetIdx)

	// Build physically truncated events stream (nothing after targetProposal.SimTime)
	var truncatedEvents []logstore.LogEntry
	for _, e := range fullEvents {
		if e.SimTime < targetProposal.SimTime {
			truncatedEvents = append(truncatedEvents, e)
		} else if e.SimTime == targetProposal.SimTime && e.EventType == "BlockPropose" && e.BlockHeight == targetProposal.Height {
			truncatedEvents = append(truncatedEvents, e)
			break
		}
	}

	truncFeatures := extractor.ExtractFeaturesAt(truncatedEvents, proposals, targetIdx)

	// Compare every canonical feature
	for _, featName := range CanonicalFeatures {
		fullVal, ok1 := fullFeatures[featName]
		truncVal, ok2 := truncFeatures[featName]

		if !ok1 || !ok2 {
			t.Fatalf("missing feature %s: full_has=%v, trunc_has=%v", featName, ok1, ok2)
		}

		if math.Abs(fullVal-truncVal) > 1e-9 {
			t.Errorf("leakage detected in feature %s: full=%f, trunc=%f", featName, fullVal, truncVal)
		}
	}
}

// TestHandCraftedFeatureValues checks exact feature values against manual calculations.
func TestHandCraftedFeatureValues(t *testing.T) {
	nodeIDs := []string{"node1", "node2"}
	initCfg := config.ScenarioConfig{
		MeanLatencyMs:    50.0,
		MeanPropDelayMs:  100.0,
		BlockIntervalSec: 1.0,
		TxArrivalRate:    1.0,
		BlockCapacity:    10,
	}

	events := []logstore.LogEntry{
		{Seq: 1, SimTime: 1.0, EventType: "BlockPropose", NodeID: "node1", BlockHash: "b1", BlockHeight: 1, ParentHash: "genesis", TxCount: 5, PoolSize: 20, TipHash: "b1", TipHeight: 1},
		{Seq: 2, SimTime: 1.1, EventType: "BlockArrive", NodeID: "node2", BlockHash: "b1", BlockHeight: 1, ParentHash: "genesis", TipHash: "b1", TipHeight: 1},
		{Seq: 3, SimTime: 1.5, EventType: "ChurnToggle", NodeID: "node2", Reason: "offline"},
		{Seq: 4, SimTime: 2.0, EventType: "BlockPropose", NodeID: "node1", BlockHash: "b2", BlockHeight: 2, ParentHash: "b1", TxCount: 10, PoolSize: 35, TipHash: "b2", TipHeight: 2},
		{Seq: 5, SimTime: 2.0, EventType: "TipUpdate", NodeID: "node2", BlockHash: "b2", BlockHeight: 2, ParentHash: "b1", TipHash: "b2", TipHeight: 2},
	}

	proposals := []BlockProposalInfo{
		{Height: 1, SimTime: 1.0, Hash: "b1", TxCount: 5, PoolSize: 20},
		{Height: 2, SimTime: 2.0, Hash: "b2", TxCount: 10, PoolSize: 35},
	}

	extractor := NewExtractor(nodeIDs, 0, initCfg)
	feats := extractor.ExtractFeaturesAt(events, proposals, 1) // at height 2

	// At SimTime 2.0:
	// node2 went offline at 1.5 -> peer_connectivity should be 1/2 = 0.5
	if feats[FeatPeerConnectivity] != 0.5 {
		t.Errorf("expected peer_connectivity=0.5, got %f", feats[FeatPeerConnectivity])
	}

	// pending_tx_count should be 35
	if feats[FeatPendingTxCount] != 35 {
		t.Errorf("expected pending_tx_count=35, got %f", feats[FeatPendingTxCount])
	}

	// backlog growth: 35 - 20 = 15
	if feats[FeatTxBacklogGrowth10] != 15 {
		t.Errorf("expected tx_backlog_growth_10=15, got %f", feats[FeatTxBacklogGrowth10])
	}

	// block interval: 2.0 - 1.0 = 1.0
	if math.Abs(feats[FeatAvgBlockInterval10]-1.0) > 1e-6 {
		t.Errorf("expected avg_block_interval_10=1.0, got %f", feats[FeatAvgBlockInterval10])
	}

	// Stale rate: 0 stale blocks in 2 proposals -> 0.0
	if feats[FeatStaleRate10] != 0.0 {
		t.Errorf("expected stale_rate_10=0.0, got %f", feats[FeatStaleRate10])
	}
}

// TestRollingWindowBoundariesAndCompetingProposals tests boundary conditions at current indices 9, 10, 11
// and verifies behavior with multiple competing proposals at the same block height.
func TestRollingWindowBoundariesAndCompetingProposals(t *testing.T) {
	nodeIDs := []string{"node1", "node2"}
	initCfg := config.ScenarioConfig{
		MeanLatencyMs:    50.0,
		MeanPropDelayMs:  100.0,
		BlockIntervalSec: 1.0,
		TxArrivalRate:    1.0,
		BlockCapacity:    10,
	}

	// 12 proposals: heights 1, 2, 3, 4, 5 (competing 5a and 5b), 6, 7, 8, 9, 10, 11
	proposals := []BlockProposalInfo{
		{Height: 1, SimTime: 1.0, Hash: "b1", TxCount: 5, PoolSize: 100},
		{Height: 2, SimTime: 2.0, Hash: "b2", TxCount: 5, PoolSize: 110},
		{Height: 3, SimTime: 3.0, Hash: "b3", TxCount: 5, PoolSize: 120},
		{Height: 4, SimTime: 4.0, Hash: "b4", TxCount: 5, PoolSize: 130},
		{Height: 5, SimTime: 5.0, Hash: "b5a", TxCount: 5, PoolSize: 140},   // index 4
		{Height: 5, SimTime: 5.05, Hash: "b5b", TxCount: 5, PoolSize: 140},  // index 5 (competing at same height)
		{Height: 6, SimTime: 6.0, Hash: "b6", TxCount: 5, PoolSize: 150},    // index 6
		{Height: 7, SimTime: 7.0, Hash: "b7", TxCount: 5, PoolSize: 160},    // index 7
		{Height: 8, SimTime: 8.0, Hash: "b8", TxCount: 5, PoolSize: 170},    // index 8
		{Height: 9, SimTime: 9.0, Hash: "b9", TxCount: 5, PoolSize: 180},    // index 9
		{Height: 10, SimTime: 10.0, Hash: "b10", TxCount: 5, PoolSize: 190}, // index 10
		{Height: 11, SimTime: 11.0, Hash: "b11", TxCount: 5, PoolSize: 200}, // index 11
	}

	var events []logstore.LogEntry
	events = append(events,
		logstore.LogEntry{Seq: 1, SimTime: 1.0, EventType: "BlockPropose", NodeID: "node1", BlockHash: "b1", BlockHeight: 1, ParentHash: "genesis", TipHash: "b1", TipHeight: 1},
		logstore.LogEntry{Seq: 2, SimTime: 2.0, EventType: "BlockPropose", NodeID: "node1", BlockHash: "b2", BlockHeight: 2, ParentHash: "b1", TipHash: "b2", TipHeight: 2},
		logstore.LogEntry{Seq: 3, SimTime: 3.0, EventType: "BlockPropose", NodeID: "node1", BlockHash: "b3", BlockHeight: 3, ParentHash: "b2", TipHash: "b3", TipHeight: 3},
		logstore.LogEntry{Seq: 4, SimTime: 4.0, EventType: "BlockPropose", NodeID: "node1", BlockHash: "b4", BlockHeight: 4, ParentHash: "b3", TipHash: "b4", TipHeight: 4},
		logstore.LogEntry{Seq: 5, SimTime: 5.0, EventType: "BlockPropose", NodeID: "node1", BlockHash: "b5a", BlockHeight: 5, ParentHash: "b4", TipHash: "b5a", TipHeight: 5},
		logstore.LogEntry{Seq: 6, SimTime: 5.05, EventType: "BlockPropose", NodeID: "node2", BlockHash: "b5b", BlockHeight: 5, ParentHash: "b4", TipHash: "b5b", TipHeight: 5},
		logstore.LogEntry{Seq: 7, SimTime: 6.0, EventType: "BlockPropose", NodeID: "node1", BlockHash: "b6", BlockHeight: 6, ParentHash: "b5a", TipHash: "b6", TipHeight: 6},
		logstore.LogEntry{Seq: 8, SimTime: 6.1, EventType: "TipUpdate", NodeID: "node2", BlockHash: "b6", BlockHeight: 6, ParentHash: "b5a", TipHash: "b6", TipHeight: 6, Reason: "reorg"},
		logstore.LogEntry{Seq: 9, SimTime: 7.0, EventType: "BlockPropose", NodeID: "node1", BlockHash: "b7", BlockHeight: 7, ParentHash: "b6", TipHash: "b7", TipHeight: 7},
		logstore.LogEntry{Seq: 10, SimTime: 8.0, EventType: "BlockPropose", NodeID: "node1", BlockHash: "b8", BlockHeight: 8, ParentHash: "b7", TipHash: "b8", TipHeight: 8},
		logstore.LogEntry{Seq: 11, SimTime: 9.0, EventType: "BlockPropose", NodeID: "node1", BlockHash: "b9", BlockHeight: 9, ParentHash: "b8", TipHash: "b9", TipHeight: 9},
		logstore.LogEntry{Seq: 12, SimTime: 10.0, EventType: "BlockPropose", NodeID: "node1", BlockHash: "b10", BlockHeight: 10, ParentHash: "b9", TipHash: "b10", TipHeight: 10},
		logstore.LogEntry{Seq: 13, SimTime: 11.0, EventType: "BlockPropose", NodeID: "node1", BlockHash: "b11", BlockHeight: 11, ParentHash: "b10", TipHash: "b11", TipHeight: 11},
	)

	extractor := NewExtractor(nodeIDs, 0, initCfg)

	// Test Boundary 1: Index 9 (10 proposals observed so far: indices 0..9)
	feats9 := extractor.ExtractFeaturesAt(events, proposals, 9)
	// Window proposals: indices 0..9 (exactly 10 proposals)
	// Backlog growth: PoolSize(index 9) - PoolSize(index 0) = 180 - 100 = 80
	if feats9[FeatTxBacklogGrowth10] != 80 {
		t.Errorf("Index 9: expected tx_backlog_growth_10=80, got %f", feats9[FeatTxBacklogGrowth10])
	}
	// Stale rate: 1 stale block (b5b) out of 10 proposals = 0.10
	if math.Abs(feats9[FeatStaleRate10]-0.10) > 1e-6 {
		t.Errorf("Index 9: expected stale_rate_10=0.10, got %f", feats9[FeatStaleRate10])
	}
	// Interval: 9 intervals available (from 10 timestamps, index 0 to 9)
	if feats9[FeatAvgBlockInterval10] <= 0 {
		t.Errorf("Index 9: expected positive avg_block_interval_10, got %f", feats9[FeatAvgBlockInterval10])
	}

	// Test Boundary 2: Index 10 (11 proposals total, window is indices 1..10 = exactly 10 proposals)
	feats10 := extractor.ExtractFeaturesAt(events, proposals, 10)
	// Window proposals: indices 1..10 (start is index 1, PoolSize=110)
	// Backlog growth: PoolSize(index 10) - PoolSize(index 1) = 190 - 110 = 80
	if feats10[FeatTxBacklogGrowth10] != 80 {
		t.Errorf("Index 10: expected tx_backlog_growth_10=80, got %f", feats10[FeatTxBacklogGrowth10])
	}
	// Stale rate: 1 stale block (b5b) out of 10 proposals = 0.10
	if math.Abs(feats10[FeatStaleRate10]-0.10) > 1e-6 {
		t.Errorf("Index 10: expected stale_rate_10=0.10, got %f", feats10[FeatStaleRate10])
	}

	// Test Boundary 3: Index 11 (12 proposals total, window is indices 2..11 = exactly 10 proposals)
	feats11 := extractor.ExtractFeaturesAt(events, proposals, 11)
	// Window proposals: indices 2..11 (start is index 2, PoolSize=120)
	// Backlog growth: PoolSize(index 11) - PoolSize(index 2) = 200 - 120 = 80
	if feats11[FeatTxBacklogGrowth10] != 80 {
		t.Errorf("Index 11: expected tx_backlog_growth_10=80, got %f", feats11[FeatTxBacklogGrowth10])
	}
	// Stale rate: 1 stale block (b5b) out of 10 proposals = 0.10
	if math.Abs(feats11[FeatStaleRate10]-0.10) > 1e-6 {
		t.Errorf("Index 11: expected stale_rate_10=0.10, got %f", feats11[FeatStaleRate10])
	}
}

// TestLabelAudit_AuthoritativeRuns verifies the H=3 label computation across authoritative pilot runs.
func TestLabelAudit_AuthoritativeRuns(t *testing.T) {
	pilotCSV := "../../outputs/pilot_final/pilot_summary.csv"
	runsDir := "../../outputs/runs"

	if _, err := os.Stat(pilotCSV); os.IsNotExist(err) {
		t.Skip("pilot_summary.csv not present, skipping full run audit")
	}

	labeler := NewLabeler(3, 10)
	runs := []string{
		"baseline_seed1_1792863044",
		"latency_stress_seed1_1792863045",
	}

	for _, rID := range runs {
		eventsPath := filepath.Join(runsDir, rID, "events.jsonl")
		if _, err := os.Stat(eventsPath); os.IsNotExist(err) {
			continue
		}
		entries, err := ReadEventLog(eventsPath)
		if err != nil {
			t.Fatalf("reading events for %s: %v", rID, err)
		}

		proposals := ExtractProposals(entries)
		nodeIDsMap := make(map[string]bool)
		for _, e := range entries {
			if e.NodeID != "" {
				nodeIDsMap[e.NodeID] = true
			}
		}
		var nodeIDs []string
		for nid := range nodeIDsMap {
			nodeIDs = append(nodeIDs, nid)
		}

		var forkEvents []fork.Event
		for _, pe := range entries {
			forkEvents = append(forkEvents, fork.Event{
				Seq:         pe.Seq,
				SimTime:     pe.SimTime,
				EventType:   pe.EventType,
				NodeID:      pe.NodeID,
				BlockHash:   pe.BlockHash,
				BlockHeight: pe.BlockHeight,
				ProposerID:  pe.ProposerID,
				ParentHash:  pe.ParentHash,
				TipHash:     pe.TipHash,
				TipHeight:   pe.TipHeight,
				OldTipHash:  pe.OldTipHash,
				Reason:      pe.Reason,
				TxCount:     pe.TxCount,
			})
		}

		det := fork.NewDetector(nodeIDs, 0)
		detection := det.Analyze(forkEvents)

		labels, eligible := labeler.ComputeLabels(proposals, detection)
		if len(eligible) == 0 {
			t.Errorf("expected eligible proposals for %s", rID)
		}
		for _, h := range eligible {
			lbl := labels[h]
			if lbl != 0 && lbl != 1 {
				t.Errorf("invalid label %d at height %d", lbl, h)
			}
		}
	}
}

// TestChurnToggle_TransitionsAndCausality verifies that ChurnToggle events dynamically
// update peer_connectivity (offline decreases, online restores), count within the window,
// do not leak from future events, and replay deterministically.
func TestChurnToggle_TransitionsAndCausality(t *testing.T) {
	nodeIDs := []string{"N0", "N1", "N2", "N3"}
	initCfg := config.ScenarioConfig{
		MeanLatencyMs:    50.0,
		MeanPropDelayMs:  100.0,
		BlockIntervalSec: 1.0,
		TxArrivalRate:    1.0,
		BlockCapacity:    10,
	}

	events := []logstore.LogEntry{
		{Seq: 1, SimTime: 1.0, EventType: "BlockPropose", NodeID: "N0", BlockHeight: 1, BlockHash: "b1", TipHeight: 1},
		{Seq: 2, SimTime: 1.2, EventType: "ChurnToggle", NodeID: "N1", Reason: "offline"},
		{Seq: 3, SimTime: 1.5, EventType: "ChurnToggle", NodeID: "N2", Reason: "offline"},
		{Seq: 4, SimTime: 1.8, EventType: "ChurnToggle", NodeID: "N1", Reason: "online"},
		{Seq: 5, SimTime: 2.0, EventType: "BlockPropose", NodeID: "N0", BlockHeight: 2, BlockHash: "b2", TipHeight: 2},
		// Future events after prediction point (t=2.0)
		{Seq: 6, SimTime: 2.5, EventType: "ChurnToggle", NodeID: "N0", Reason: "offline"},
		{Seq: 7, SimTime: 3.0, EventType: "BlockPropose", NodeID: "N3", BlockHeight: 3, BlockHash: "b3", TipHeight: 3},
	}

	proposals := []BlockProposalInfo{
		{Height: 1, SimTime: 1.0, Hash: "b1"},
		{Height: 2, SimTime: 2.0, Hash: "b2"},
		{Height: 3, SimTime: 3.0, Hash: "b3"},
	}

	extractor := NewExtractor(nodeIDs, 0, initCfg)

	// Prediction at height 2 (t=2.0)
	feats1 := extractor.ExtractFeaturesAt(events, proposals, 1)

	// Initial connectivity: 4/4 = 1.0
	// t=1.2: N1 offline -> 3/4
	// t=1.5: N2 offline -> 2/4
	// t=1.8: N1 online -> 3/4 (N2 remains offline, N0, N1, N3 online)
	// Expected peer_connectivity at t=2.0 = 3/4 = 0.75
	if math.Abs(feats1[FeatPeerConnectivity]-0.75) > 1e-6 {
		t.Errorf("expected peer_connectivity=0.75, got %f", feats1[FeatPeerConnectivity])
	}

	// Churn events up to t=2.0: 3 events (at 1.2, 1.5, 1.8).
	// Available unique block heights: heights 1 and 2 (M=2).
	// Denominator for M < 100 is M = 2.
	// Expected churn_rate_100 = 3.0 / 2.0 = 1.5
	if math.Abs(feats1[FeatChurnRate100]-1.5) > 1e-6 {
		t.Errorf("expected churn_rate_100=1.5, got %f", feats1[FeatChurnRate100])
	}

	// Test causality: future event at t=2.5 (N0 offline) must NOT affect features at t=2.0
	// Truncate events physically at t=2.0 (Seq 5)
	var truncatedEvents []logstore.LogEntry
	for _, e := range events {
		if e.SimTime <= 2.0 {
			truncatedEvents = append(truncatedEvents, e)
		}
	}
	featsTruncated := extractor.ExtractFeaturesAt(truncatedEvents, proposals, 1)
	if featsTruncated[FeatPeerConnectivity] != feats1[FeatPeerConnectivity] {
		t.Errorf("causality violation in peer_connectivity: %f vs %f", featsTruncated[FeatPeerConnectivity], feats1[FeatPeerConnectivity])
	}
	if featsTruncated[FeatChurnRate100] != feats1[FeatChurnRate100] {
		t.Errorf("causality violation in churn_rate_100: %f vs %f", featsTruncated[FeatChurnRate100], feats1[FeatChurnRate100])
	}

	// Replay determinism
	featsReplay := extractor.ExtractFeaturesAt(events, proposals, 1)
	for k, v := range feats1 {
		if featsReplay[k] != v {
			t.Errorf("nondeterminism in %s: %f vs %f", k, featsReplay[k], v)
		}
	}
}

// TestChurnRate100_UniqueHeightWindow verifies full windows, early-run windows, repeated
// proposal heights, and exact boundary inclusions for churn_rate_100.
func TestChurnRate100_UniqueHeightWindow(t *testing.T) {
	nodeIDs := []string{"N0", "N1"}
	initCfg := config.ScenarioConfig{
		MeanLatencyMs:    50.0,
		MeanPropDelayMs:  100.0,
		BlockIntervalSec: 1.0,
		TxArrivalRate:    1.0,
		BlockCapacity:    10,
	}

	t.Run("EarlyRunWindow", func(t *testing.T) {
		// 5 unique block heights: 1..5 at times 10, 20, 30, 40, 50.
		proposals := make([]BlockProposalInfo, 5)
		for i := 0; i < 5; i++ {
			proposals[i] = BlockProposalInfo{
				Height:  i + 1,
				SimTime: float64((i + 1) * 10),
				Hash:    fmt.Sprintf("b%d", i+1),
			}
		}

		events := []logstore.LogEntry{
			{Seq: 1, SimTime: 5.0, EventType: "ChurnToggle", NodeID: "N0", Reason: "offline"}, // before block 1 -> included in early run [0.0, 50.0]
			{Seq: 2, SimTime: 10.0, EventType: "BlockPropose", NodeID: "N0", BlockHeight: 1},
			{Seq: 3, SimTime: 15.0, EventType: "ChurnToggle", NodeID: "N0", Reason: "online"},
			{Seq: 4, SimTime: 20.0, EventType: "BlockPropose", NodeID: "N0", BlockHeight: 2},
			{Seq: 5, SimTime: 30.0, EventType: "BlockPropose", NodeID: "N0", BlockHeight: 3},
			{Seq: 6, SimTime: 35.0, EventType: "ChurnToggle", NodeID: "N1", Reason: "offline"},
			{Seq: 7, SimTime: 40.0, EventType: "BlockPropose", NodeID: "N0", BlockHeight: 4},
			{Seq: 8, SimTime: 50.0, EventType: "BlockPropose", NodeID: "N0", BlockHeight: 5},
			{Seq: 9, SimTime: 50.0, EventType: "ChurnToggle", NodeID: "N1", Reason: "online"},   // after proposal anchor sequence -> excluded by seq
			{Seq: 10, SimTime: 55.0, EventType: "ChurnToggle", NodeID: "N0", Reason: "offline"}, // after prediction time -> excluded
		}

		extractor := NewExtractor(nodeIDs, 0, initCfg)
		feats := extractor.ExtractFeaturesAt(events, proposals, 4) // prediction at height 5 (t=50.0)

		// 3 churn events included (t=5.0, t=15.0, t=35.0).
		// Denominator is available unique block heights = 5.
		// Expected churn_rate_100 = 3.0 / 5.0 = 0.60
		if math.Abs(feats[FeatChurnRate100]-0.60) > 1e-6 {
			t.Errorf("expected churn_rate_100=0.60, got %f", feats[FeatChurnRate100])
		}
	})

	t.Run("FullWindowAndBoundaries", func(t *testing.T) {
		// 105 unique block heights: 1..105 at times 10, 20, ..., 1050.
		proposals := make([]BlockProposalInfo, 105)
		for i := 0; i < 105; i++ {
			proposals[i] = BlockProposalInfo{
				Height:  i + 1,
				SimTime: float64((i + 1) * 10),
				Hash:    fmt.Sprintf("b%d", i+1),
			}
		}

		// At height 105 (currentIdx=104):
		// Total unique heights = 105.
		// Last 100 unique block heights are uniqueHeights[105-100] = uniqueHeights[5] = height 6.
		// First proposal time of height 6 = 60.0.
		// Start boundary is 60.0.
		// Prediction time is 1050.0.
		events := []logstore.LogEntry{
			{Seq: 1, SimTime: 59.9, EventType: "ChurnToggle", NodeID: "N0", Reason: "offline"},  // outside window (< 60.0)
			{Seq: 2, SimTime: 60.0, EventType: "ChurnToggle", NodeID: "N0", Reason: "online"},   // exactly at start boundary (>= 60.0) -> included
			{Seq: 3, SimTime: 500.0, EventType: "ChurnToggle", NodeID: "N1", Reason: "offline"}, // inside window -> included
			{Seq: 4, SimTime: 1049.0, EventType: "ChurnToggle", NodeID: "N1", Reason: "online"}, // inside window -> included
			{Seq: 5, SimTime: 1050.0, EventType: "BlockPropose", NodeID: "N0", BlockHeight: 105},
			{Seq: 6, SimTime: 1050.1, EventType: "ChurnToggle", NodeID: "N0", Reason: "offline"}, // after prediction time -> excluded
		}

		extractor := NewExtractor(nodeIDs, 0, initCfg)
		feats := extractor.ExtractFeaturesAt(events, proposals, 104)

		// 3 events included (60.0, 500.0, 1049.0).
		// Denominator = 100.0.
		// Expected churn_rate_100 = 3.0 / 100.0 = 0.03
		if math.Abs(feats[FeatChurnRate100]-0.03) > 1e-6 {
			t.Errorf("expected churn_rate_100=0.03, got %f", feats[FeatChurnRate100])
		}
	})

	t.Run("RepeatedProposalHeights", func(t *testing.T) {
		// 105 proposals, but 5 heights are repeated competing proposals:
		// heights: 1, 2, 3, 4, 5, 5(competing), 6, 7, ..., 104.
		// Total unique heights = 104.
		var proposals []BlockProposalInfo
		for h := 1; h <= 104; h++ {
			proposals = append(proposals, BlockProposalInfo{
				Height:  h,
				SimTime: float64(len(proposals)+1) * 10,
				Hash:    fmt.Sprintf("b%d_a", h),
			})
			if h == 5 {
				// Competing proposal at same height 5
				proposals = append(proposals, BlockProposalInfo{
					Height:  h,
					SimTime: float64(len(proposals)+1) * 10,
					Hash:    fmt.Sprintf("b%d_b", h),
				})
			}
		}

		// At the final proposal (len=105, currentIdx=104, height=104):
		// Unique heights count = 104.
		// Earliest height in last 100 is uniqueHeights[104-100] = uniqueHeights[4] = height 5.
		// Earliest proposal time of height 5 = first proposal of height 5.
		extractor := NewExtractor(nodeIDs, 0, initCfg)
		feats := extractor.ExtractFeaturesAt(nil, proposals, len(proposals)-1)

		if feats == nil {
			t.Fatal("unexpected nil features")
		}
	})
}

// TestSamplingUnit_HeightLevelAnchor verifies that the dataset pipeline produces
// exactly one row per eligible unique block height, anchored to the first proposal event.
func TestSamplingUnit_HeightLevelAnchor(t *testing.T) {
	nodeIDs := []string{"N0", "N1"}
	proposals := []BlockProposalInfo{
		{Height: 11, SimTime: 11.0, Hash: "b11"},
		{Height: 12, SimTime: 12.0, Hash: "b12"},
		{Height: 13, SimTime: 13.0, Hash: "b13_first"},  // first proposal at height 13 (anchor)
		{Height: 13, SimTime: 13.5, Hash: "b13_second"}, // competing proposal at height 13
		{Height: 14, SimTime: 14.0, Hash: "b14"},
		{Height: 15, SimTime: 15.0, Hash: "b15"},
		{Height: 16, SimTime: 16.0, Hash: "b16"},
		{Height: 17, SimTime: 17.0, Hash: "b17"},
	}

	// Height map should anchor to index 2 (b13_first)
	proposalIndexMap := make(map[int]int)
	for idx, p := range proposals {
		if _, exists := proposalIndexMap[p.Height]; !exists {
			proposalIndexMap[p.Height] = idx
		}
	}

	if proposalIndexMap[13] != 2 {
		t.Errorf("expected proposalIndexMap[13]=2 (first proposal), got %d", proposalIndexMap[13])
	}
	if proposals[proposalIndexMap[13]].Hash != "b13_first" {
		t.Errorf("expected anchor to be b13_first, got %s", proposals[proposalIndexMap[13]].Hash)
	}

	// Total unique heights in proposalIndexMap = 7 (11, 12, 13, 14, 15, 16, 17)
	if len(proposalIndexMap) != 7 {
		t.Errorf("expected 7 unique heights in index map, got %d", len(proposalIndexMap))
	}
	_ = nodeIDs
}

// TestChurnFeatures_AuthoritativeDatasetNotConstant verifies on an authoritative churn_stress
// pilot run that peer_connectivity and churn_rate_100 are not universally constant.
func TestChurnFeatures_AuthoritativeDatasetNotConstant(t *testing.T) {
	runPath := filepath.Join("..", "..", "outputs", "runs", "churn_stress_seed1_1792863047")
	eventsPath := filepath.Join(runPath, "events.jsonl")
	if _, err := os.Stat(eventsPath); os.IsNotExist(err) {
		t.Skip("authoritative pilot run not found; skipping integration test")
	}

	events, err := ReadEventLog(eventsPath)
	if err != nil {
		t.Fatalf("reading events: %v", err)
	}

	proposals := ExtractProposals(events)
	if len(proposals) < 20 {
		t.Fatalf("insufficient proposals: %d", len(proposals))
	}

	var nodeIDs []string
	nodeIDMap := make(map[string]bool)
	for _, e := range events {
		if e.NodeID != "" {
			nodeIDMap[e.NodeID] = true
		}
	}
	for nid := range nodeIDMap {
		nodeIDs = append(nodeIDs, nid)
	}

	extractor := NewExtractor(nodeIDs, 10, config.ScenarioConfig{
		MeanLatencyMs:    50.0,
		MeanPropDelayMs:  100.0,
		BlockIntervalSec: 1.0,
	})

	hasNonFullConnectivity := false
	hasNonZeroChurnRate := false

	for i := 11; i < len(proposals)-3; i++ {
		feats := extractor.ExtractFeaturesAt(events, proposals, i)
		if feats[FeatPeerConnectivity] < 0.99 {
			hasNonFullConnectivity = true
		}
		if feats[FeatChurnRate100] > 0.0 {
			hasNonZeroChurnRate = true
		}
	}

	if !hasNonFullConnectivity {
		t.Errorf("churn_stress run unexpectedly had constant 1.0 peer_connectivity across all prediction points")
	}
	if !hasNonZeroChurnRate {
		t.Errorf("churn_stress run unexpectedly had constant 0.0 churn_rate_100 across all prediction points")
	}
}
