// Package fork_test contains unit tests for the fork detection module.
// Tests use crafted event-log fixtures with known expected outcomes.
package fork_test

import (
	"testing"

	"github.com/pranav-k/forkrisk/internal/fork"
)

// helpers ----------------------------------------------------------------

func makePropose(seq int64, t float64, nodeID, hash, parent string, height int) fork.Event {
	return fork.Event{
		Seq:         seq,
		SimTime:     t,
		EventType:   "BlockPropose",
		NodeID:      nodeID,
		BlockHash:   hash,
		BlockHeight: height,
		ProposerID:  nodeID,
		ParentHash:  parent,
		TipHash:     hash,
		TipHeight:   height,
	}
}

func makeTipUpdate(seq int64, t float64, nodeID, hash, oldHash, parent string, height int, reason string) fork.Event {
	return fork.Event{
		Seq:         seq,
		SimTime:     t,
		EventType:   "TipUpdate",
		NodeID:      nodeID,
		BlockHash:   hash,
		BlockHeight: height,
		TipHash:     hash,
		TipHeight:   height,
		OldTipHash:  oldHash,
		ParentHash:  parent,
		Reason:      reason,
	}
}

func makeArrive(seq int64, t float64, nodeID, hash, parent string, height int, tipHash string, tipHeight int) fork.Event {
	return fork.Event{
		Seq:         seq,
		SimTime:     t,
		EventType:   "BlockArrive",
		NodeID:      nodeID,
		BlockHash:   hash,
		BlockHeight: height,
		ParentHash:  parent,
		TipHash:     tipHash,
		TipHeight:   tipHeight,
	}
}

// TestNoFork_LinearChain verifies that a single linear chain produces zero fork episodes.
// Fixture: N0 proposes blocks 1→2→3; N1 receives them all in order.
func TestNoFork_LinearChain(t *testing.T) {
	nodeIDs := []string{"N0", "N1"}
	d := fork.NewDetector(nodeIDs, 0)

	events := []fork.Event{
		makePropose(1, 1.0, "N0", "blk1", "genesis", 1),
		makeTipUpdate(2, 1.1, "N1", "blk1", "genesis", "genesis", 1, "extend"),
		makePropose(3, 2.0, "N0", "blk2", "blk1", 2),
		makeTipUpdate(4, 2.1, "N1", "blk2", "blk1", "blk1", 2, "extend"),
		makePropose(5, 3.0, "N0", "blk3", "blk2", 3),
		makeTipUpdate(6, 3.1, "N1", "blk3", "blk2", "blk2", 3, "extend"),
	}

	result := d.Analyze(events)

	if len(result.ForkEpisodes) != 0 {
		t.Errorf("expected 0 fork episodes, got %d: %+v", len(result.ForkEpisodes), result.ForkEpisodes)
	}
	if len(result.Reorganizations) != 0 {
		t.Errorf("expected 0 reorgs, got %d", len(result.Reorganizations))
	}
	if len(result.StaleBlocks) != 0 {
		t.Errorf("expected 0 stale blocks, got %d", len(result.StaleBlocks))
	}
}

// TestForkEpisode_TwoCompetingBlocks verifies that two concurrent blocks at
// the same height produce exactly ONE fork episode, not two separate episodes.
//
// Fixture:
//
//	N0 proposes blkA at height 1 (t=1.0)
//	N1 proposes blkB at height 1 (t=1.0) — competing!
//	At t=2.0, N0 receives blkB and reorgs to blkA (wins by hash tie-break)
//	At t=2.0, N1 receives blkA and reorgs to blkA (same hash tie-break)
//	→ Fork episode: 1 birth at t=1.0, resolved at t=2.0
func TestForkEpisode_TwoCompetingBlocks(t *testing.T) {
	nodeIDs := []string{"N0", "N1"}
	d := fork.NewDetector(nodeIDs, 0)

	events := []fork.Event{
		// N0 proposes blkA, N1 still has genesis.
		makePropose(1, 1.0, "N0", "blkA", "genesis", 1),
		// N1 proposes blkB, same height — fork birth!
		makePropose(2, 1.0, "N1", "blkB", "genesis", 1),
		// N0 receives blkB but keeps blkA (hash tie-break; assume blkA < blkB).
		makeTipUpdate(3, 2.0, "N0", "blkA", "genesis", "genesis", 1, "extend"),
		// N1 receives blkA and switches (blkA < blkB).
		makeTipUpdate(4, 2.0, "N1", "blkA", "blkB", "genesis", 1, "reorg"),
	}

	result := d.Analyze(events)

	if len(result.ForkEpisodes) != 1 {
		t.Fatalf("expected 1 fork episode, got %d: %+v", len(result.ForkEpisodes), result.ForkEpisodes)
	}
	ep := result.ForkEpisodes[0]
	if ep.BirthTime != 1.0 {
		t.Errorf("expected fork birth at t=1.0, got %v", ep.BirthTime)
	}
	if ep.ResolutionTime < 0 {
		t.Errorf("fork episode should be resolved, got unresolved")
	}
	if len(result.Reorganizations) != 1 {
		t.Errorf("expected 1 reorg (N1 switched from blkB to blkA), got %d", len(result.Reorganizations))
	}
	if result.Reorganizations[0].NodeID != "N1" {
		t.Errorf("expected reorg on N1, got %s", result.Reorganizations[0].NodeID)
	}
}

// TestForkEpisode_SingleEpisodeMultipleNodes verifies that multiple nodes
// diverging during the SAME timeframe is counted as ONE episode.
//
// Fixture: 4 nodes; N0 and N1 see blkA; N2 and N3 see blkB.
// All converge to blkA at t=3.0. This must be exactly ONE fork episode.
func TestForkEpisode_SingleEpisodeMultipleNodes(t *testing.T) {
	nodeIDs := []string{"N0", "N1", "N2", "N3"}
	d := fork.NewDetector(nodeIDs, 0)

	events := []fork.Event{
		makePropose(1, 1.0, "N0", "blkA", "genesis", 1),
		makePropose(2, 1.0, "N2", "blkB", "genesis", 1),
		// N1 gets blkA.
		makeTipUpdate(3, 1.5, "N1", "blkA", "genesis", "genesis", 1, "extend"),
		// N3 gets blkB.
		makeTipUpdate(4, 1.5, "N3", "blkB", "genesis", "genesis", 1, "extend"),
		// All converge to blkA at t=3.0.
		makeTipUpdate(5, 3.0, "N2", "blkA", "blkB", "genesis", 1, "reorg"),
		makeTipUpdate(6, 3.0, "N3", "blkA", "blkB", "genesis", 1, "reorg"),
	}

	result := d.Analyze(events)

	if len(result.ForkEpisodes) != 1 {
		t.Errorf("expected exactly 1 fork episode (not one per node pair), got %d", len(result.ForkEpisodes))
	}
}

// TestStaleBlock verifies that the losing block in a fork is identified as stale.
// blkA is the proposer's block, blkB loses — blkB is stale.
func TestStaleBlock(t *testing.T) {
	nodeIDs := []string{"N0", "N1"}
	d := fork.NewDetector(nodeIDs, 0)

	events := []fork.Event{
		makePropose(1, 1.0, "N0", "blkA", "genesis", 1),
		makePropose(2, 1.0, "N1", "blkB", "genesis", 1),
		// Both nodes converge to blkA (wins by hash).
		makeTipUpdate(3, 2.0, "N1", "blkA", "blkB", "genesis", 1, "reorg"),
		// blkA extends to blk2.
		makePropose(4, 3.0, "N0", "blk2", "blkA", 2),
		makeTipUpdate(5, 3.1, "N1", "blk2", "blkA", "blkA", 2, "extend"),
	}

	result := d.Analyze(events)

	// blkB was proposed but is not on the final chain (blkA → blk2).
	if len(result.StaleBlocks) != 1 {
		t.Fatalf("expected 1 stale block, got %d: %+v", len(result.StaleBlocks), result.StaleBlocks)
	}
	if result.StaleBlocks[0].Hash != "blkB" {
		t.Errorf("expected stale block blkB, got %s", result.StaleBlocks[0].Hash)
	}
}

// TestWarmUpBlocks verifies that blocks at height ≤ warmUpBlocks are excluded
// from fork/stale counts.
func TestWarmUpBlocks(t *testing.T) {
	nodeIDs := []string{"N0", "N1"}
	d := fork.NewDetector(nodeIDs, 2) // skip first 2 blocks

	events := []fork.Event{
		// Height 1 — within warm-up, should be ignored.
		makePropose(1, 0.5, "N0", "wblkA", "genesis", 1),
		makePropose(2, 0.5, "N1", "wblkB", "genesis", 1),
		makeTipUpdate(3, 1.0, "N1", "wblkA", "wblkB", "genesis", 1, "reorg"),
		// Height 2 — also warm-up.
		makePropose(4, 1.5, "N0", "wblk2A", "wblkA", 2),
		makePropose(5, 1.5, "N1", "wblk2B", "wblkA", 2),
		makeTipUpdate(6, 2.0, "N1", "wblk2A", "wblk2B", "wblkA", 2, "reorg"),
		// Height 3 — post warm-up, a linear chain.
		makePropose(7, 3.0, "N0", "blk3", "wblk2A", 3),
		makeTipUpdate(8, 3.1, "N1", "blk3", "wblk2A", "wblk2A", 3, "extend"),
	}

	result := d.Analyze(events)

	if len(result.ForkEpisodes) != 0 {
		t.Errorf("expected 0 fork episodes after warm-up filter, got %d", len(result.ForkEpisodes))
	}
	if len(result.StaleBlocks) != 0 {
		t.Errorf("expected 0 stale blocks after warm-up filter, got %d", len(result.StaleBlocks))
	}
}

// TestReorgDepthEstimate verifies that the reorg depth is estimated correctly.
// N0 is at height 3 (chain A); when it switches to chain B at height 3,
// the common ancestor is at height 2, so depth = 1.
func TestReorgDepthEstimate(t *testing.T) {
	nodeIDs := []string{"N0", "N1"}
	d := fork.NewDetector(nodeIDs, 0)

	events := []fork.Event{
		makePropose(1, 1.0, "N0", "blkA1", "genesis", 1),
		makePropose(2, 1.0, "N1", "blkB1", "genesis", 1),
		// N0 extends chain A.
		makePropose(3, 2.0, "N0", "blkA2", "blkA1", 2),
		makePropose(4, 3.0, "N0", "blkA3", "blkA2", 3),
		// N1 extends chain B.
		makePropose(5, 2.0, "N1", "blkB2", "blkB1", 2),
		makePropose(6, 3.0, "N1", "blkB3", "blkB2", 3),
		// N0 switches from blkA3 (height 3) to blkB3 (height 3) — reorg.
		makeTipUpdate(7, 4.0, "N0", "blkB3", "blkA3", "blkB2", 3, "reorg"),
	}

	result := d.Analyze(events)

	if len(result.Reorganizations) != 1 {
		t.Fatalf("expected 1 reorg, got %d", len(result.Reorganizations))
	}
	reorg := result.Reorganizations[0]
	if reorg.Depth < 1 {
		t.Errorf("expected reorg depth >= 1, got %d", reorg.Depth)
	}
}

// TestUnresolvedFork verifies that a fork active at simulation end is counted
// but marked as unresolved (ResolutionTime == -1).
func TestUnresolvedFork(t *testing.T) {
	nodeIDs := []string{"N0", "N1"}
	d := fork.NewDetector(nodeIDs, 0)

	events := []fork.Event{
		makePropose(1, 1.0, "N0", "blkA", "genesis", 1),
		makePropose(2, 1.0, "N1", "blkB", "genesis", 1),
		// No convergence — simulation ends here.
	}

	result := d.Analyze(events)

	if len(result.ForkEpisodes) != 1 {
		t.Fatalf("expected 1 (unresolved) fork episode, got %d", len(result.ForkEpisodes))
	}
	if result.ForkEpisodes[0].ResolutionTime != -1 {
		t.Errorf("expected unresolved fork (ResolutionTime=-1), got %v", result.ForkEpisodes[0].ResolutionTime)
	}
}

// TestForkRate verifies the fork rate formula: episodes / total blocks.
func TestForkRate(t *testing.T) {
	nodeIDs := []string{"N0", "N1"}
	d := fork.NewDetector(nodeIDs, 0)

	// 1 fork episode across 3 proposed blocks = rate 1/3.
	events := []fork.Event{
		makePropose(1, 1.0, "N0", "blkA", "genesis", 1),
		makePropose(2, 1.0, "N1", "blkB", "genesis", 1),
		makeTipUpdate(3, 2.0, "N1", "blkA", "blkB", "genesis", 1, "reorg"),
		makePropose(4, 3.0, "N0", "blk2", "blkA", 2),
		makeTipUpdate(5, 3.1, "N1", "blk2", "blkA", "blkA", 2, "extend"),
	}

	result := d.Analyze(events)

	// totalBlocks = 3 (blkA, blkB, blk2).
	if result.TotalBlocks != 3 {
		t.Errorf("expected 3 total blocks, got %d", result.TotalBlocks)
	}
	if len(result.ForkEpisodes) != 1 {
		t.Errorf("expected 1 fork episode, got %d", len(result.ForkEpisodes))
	}
	expectedRate := 1.0 / 3.0
	rate := result.ForkRate()
	if rate < expectedRate-0.001 || rate > expectedRate+0.001 {
		t.Errorf("expected fork rate ~%.4f, got %.4f", expectedRate, rate)
	}
}

// TestStaleRate verifies the stale block rate formula.
func TestStaleRate(t *testing.T) {
	nodeIDs := []string{"N0", "N1"}
	d := fork.NewDetector(nodeIDs, 0)

	// 1 stale block out of 3 proposed = rate 1/3.
	events := []fork.Event{
		makePropose(1, 1.0, "N0", "blkA", "genesis", 1),
		makePropose(2, 1.0, "N1", "blkB", "genesis", 1), // stale
		makeTipUpdate(3, 2.0, "N1", "blkA", "blkB", "genesis", 1, "reorg"),
		makePropose(4, 3.0, "N0", "blk2", "blkA", 2),
		makeTipUpdate(5, 3.1, "N1", "blk2", "blkA", "blkA", 2, "extend"),
	}

	result := d.Analyze(events)

	if len(result.StaleBlocks) != 1 {
		t.Fatalf("expected 1 stale block, got %d", len(result.StaleBlocks))
	}
	expected := 1.0 / 3.0
	rate := result.StaleRate()
	if rate < expected-0.001 || rate > expected+0.001 {
		t.Errorf("expected stale rate ~%.4f, got %.4f", expected, rate)
	}
}

// TestParseEvents verifies the JSONL event parser on a minimal fixture.
func TestParseEvents(t *testing.T) {
	jsonl := `{"seq":1,"sim_time":1.0,"event_type":"BlockPropose","node_id":"N0","block_hash":"abc","block_height":1,"tip_hash":"abc","tip_height":1}
{"seq":2,"sim_time":1.5,"event_type":"TipUpdate","node_id":"N1","block_hash":"abc","block_height":1,"tip_hash":"abc","tip_height":1,"old_tip_hash":"genesis","reason":"extend"}
`
	events, err := fork.ParseEvents([]byte(jsonl))
	if err != nil {
		t.Fatalf("ParseEvents error: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].EventType != "BlockPropose" {
		t.Errorf("expected first event type BlockPropose, got %s", events[0].EventType)
	}
	if events[1].Reason != "extend" {
		t.Errorf("expected second event reason 'extend', got %q", events[1].Reason)
	}
}

// TestConsecutiveForkEpisodes verifies that two separate fork episodes (with
// convergence between them) are counted as two distinct episodes.
func TestConsecutiveForkEpisodes(t *testing.T) {
	nodeIDs := []string{"N0", "N1"}
	d := fork.NewDetector(nodeIDs, 0)

	events := []fork.Event{
		// Episode 1: fork at height 1.
		makePropose(1, 1.0, "N0", "blkA1", "genesis", 1),
		makePropose(2, 1.0, "N1", "blkB1", "genesis", 1),
		makeTipUpdate(3, 2.0, "N1", "blkA1", "blkB1", "genesis", 1, "reorg"),
		// Both on blkA1 now — converged, episode 1 ends.

		// Episode 2: fork at height 2.
		makePropose(4, 3.0, "N0", "blkA2", "blkA1", 2),
		makePropose(5, 3.0, "N1", "blkB2", "blkA1", 2),
		makeTipUpdate(6, 4.0, "N1", "blkA2", "blkB2", "blkA1", 2, "reorg"),
		// Both on blkA2 — converged, episode 2 ends.
	}

	result := d.Analyze(events)

	if len(result.ForkEpisodes) != 2 {
		t.Errorf("expected 2 fork episodes, got %d", len(result.ForkEpisodes))
	}
}

// =============================================================================
// Regression Tests — Reorg Depth via Actual Common Ancestor
// =============================================================================

// TestReorgDepth_CommonAncestorExact tests that reorg depth is computed from
// the actual common ancestor, not from min(heights)-1.
//
// Chain layout:
//
//	genesis → A1 → A2 → A3   (old tip: A3 at height 3)
//	genesis → A1 → B2 → B3   (new tip: B3 at height 3)
//
// Common ancestor = A1 (height 1).
// Reorg depth = oldTipHeight - commonAncestorHeight = 3 - 1 = 2.
func TestReorgDepth_CommonAncestorExact(t *testing.T) {
	nodeIDs := []string{"N0", "N1"}
	d := fork.NewDetector(nodeIDs, 0)

	events := []fork.Event{
		// Shared chain: genesis → A1
		makePropose(1, 1.0, "N0", "A1", "genesis", 1),
		makeTipUpdate(2, 1.1, "N1", "A1", "genesis", "genesis", 1, "extend"),

		// N0 extends: A1 → A2 → A3
		makePropose(3, 2.0, "N0", "A2", "A1", 2),
		makePropose(4, 3.0, "N0", "A3", "A2", 3),

		// N1 extends: A1 → B2 → B3 (diverges at height 2)
		makePropose(5, 2.0, "N1", "B2", "A1", 2),
		makePropose(6, 3.0, "N1", "B3", "B2", 3),

		// N0 switches from A3 to B3 — reorg.
		makeTipUpdate(7, 4.0, "N0", "B3", "A3", "B2", 3, "reorg"),
	}

	result := d.Analyze(events)

	if len(result.Reorganizations) != 1 {
		t.Fatalf("expected 1 reorg, got %d", len(result.Reorganizations))
	}
	reorg := result.Reorganizations[0]
	// Common ancestor is A1 (height 1); oldTip is A3 (height 3).
	// depth = 3 - 1 = 2.
	if reorg.Depth != 2 {
		t.Errorf("expected reorg depth 2 (A3→A1→B3: rolled back 2 blocks), got %d", reorg.Depth)
	}
}

// TestReorgDepth_SingleBlockRollback tests that when old and new tips share
// a parent (diverge at the tip only), depth = 1.
//
// Chain layout:
//
//	genesis → A1   (old tip: A1 at height 1)
//	genesis → B1   (new tip: B1 at height 1, common ancestor = genesis)
//
// Common ancestor = genesis (height 0). depth = 1 - 0 = 1.
func TestReorgDepth_SingleBlockRollback(t *testing.T) {
	nodeIDs := []string{"N0", "N1"}
	d := fork.NewDetector(nodeIDs, 0)

	events := []fork.Event{
		makePropose(1, 1.0, "N0", "A1", "genesis", 1),
		makePropose(2, 1.0, "N1", "B1", "genesis", 1),
		// N0 switches from A1 to B1.
		makeTipUpdate(3, 2.0, "N0", "B1", "A1", "genesis", 1, "reorg"),
	}

	result := d.Analyze(events)

	if len(result.Reorganizations) != 1 {
		t.Fatalf("expected 1 reorg, got %d", len(result.Reorganizations))
	}
	if result.Reorganizations[0].Depth != 1 {
		t.Errorf("expected reorg depth 1, got %d", result.Reorganizations[0].Depth)
	}
}

// TestReorgDepth_ThreeBlockRollback tests depth=3 when the divergence point is
// at genesis (no shared ancestors beyond genesis).
//
//	genesis → A1 → A2 → A3   (old tip)
//	genesis → B1 → B2 → B3   (new tip)
//
// Common ancestor = genesis (height 0). depth = 3 - 0 = 3.
func TestReorgDepth_ThreeBlockRollback(t *testing.T) {
	nodeIDs := []string{"N0", "N1"}
	d := fork.NewDetector(nodeIDs, 0)

	events := []fork.Event{
		// N0 builds A-chain from genesis.
		makePropose(1, 1.0, "N0", "A1", "genesis", 1),
		makePropose(2, 2.0, "N0", "A2", "A1", 2),
		makePropose(3, 3.0, "N0", "A3", "A2", 3),

		// N1 builds B-chain from genesis (independently).
		makePropose(4, 1.0, "N1", "B1", "genesis", 1),
		makePropose(5, 2.0, "N1", "B2", "B1", 2),
		makePropose(6, 3.0, "N1", "B3", "B2", 3),

		// N0 switches from A3 to B3.
		makeTipUpdate(7, 4.0, "N0", "B3", "A3", "B2", 3, "reorg"),
	}

	result := d.Analyze(events)

	if len(result.Reorganizations) != 1 {
		t.Fatalf("expected 1 reorg, got %d", len(result.Reorganizations))
	}
	// Common ancestor is genesis (height 0); A3 is height 3.
	// depth = 3 - 0 = 3.
	if result.Reorganizations[0].Depth != 3 {
		t.Errorf("expected reorg depth 3, got %d", result.Reorganizations[0].Depth)
	}
}

// =============================================================================
// Regression Tests — Node-View Consistency
// =============================================================================

// TestNodeViewConsistency_AllAgree verifies that when all nodes always share
// the same tip, the consistency score = 1.0.
func TestNodeViewConsistency_AllAgree(t *testing.T) {
	// This test is exercised via the metrics package; here we verify
	// that no fork is detected in a fully-consistent scenario, which
	// indirectly validates the consistency measurement.
	nodeIDs := []string{"N0", "N1", "N2"}
	d := fork.NewDetector(nodeIDs, 0)

	// All three nodes always have the same tip.
	events := []fork.Event{
		makePropose(1, 1.0, "N0", "b1", "genesis", 1),
		makeTipUpdate(2, 1.1, "N1", "b1", "genesis", "genesis", 1, "extend"),
		makeTipUpdate(3, 1.1, "N2", "b1", "genesis", "genesis", 1, "extend"),
		makePropose(4, 2.0, "N1", "b2", "b1", 2),
		makeTipUpdate(5, 2.1, "N0", "b2", "b1", "b1", 2, "extend"),
		makeTipUpdate(6, 2.1, "N2", "b2", "b1", "b1", 2, "extend"),
	}

	result := d.Analyze(events)
	if len(result.ForkEpisodes) != 0 {
		t.Errorf("expected 0 fork episodes when all nodes always agree, got %d", len(result.ForkEpisodes))
	}
}

// TestForkEpisode_ExactMetrics verifies exact counts for a crafted log with
// known fork structure:
//   - 1 fork episode
//   - 1 stale block
//   - 1 reorg
//   - fork rate = 1/4
//   - stale rate = 1/4
func TestForkEpisode_ExactMetrics(t *testing.T) {
	nodeIDs := []string{"N0", "N1"}
	d := fork.NewDetector(nodeIDs, 0)

	events := []fork.Event{
		// Block 1: only N0 proposes, no fork.
		makePropose(1, 1.0, "N0", "b1", "genesis", 1),
		makeTipUpdate(2, 1.1, "N1", "b1", "genesis", "genesis", 1, "extend"),

		// Block 2: concurrent proposals → fork.
		makePropose(3, 2.0, "N0", "b2a", "b1", 2),
		makePropose(4, 2.0, "N1", "b2b", "b1", 2),

		// Convergence: N1 adopts b2a.
		makeTipUpdate(5, 3.0, "N1", "b2a", "b2b", "b1", 2, "reorg"),

		// Block 3: linear.
		makePropose(6, 4.0, "N0", "b3", "b2a", 3),
		makeTipUpdate(7, 4.1, "N1", "b3", "b2a", "b2a", 3, "extend"),
	}

	result := d.Analyze(events)

	if result.TotalBlocks != 4 {
		t.Errorf("expected 4 total blocks (b1, b2a, b2b, b3), got %d", result.TotalBlocks)
	}
	if len(result.ForkEpisodes) != 1 {
		t.Errorf("expected 1 fork episode, got %d", len(result.ForkEpisodes))
	}
	if len(result.StaleBlocks) != 1 {
		t.Errorf("expected 1 stale block (b2b), got %d", len(result.StaleBlocks))
	}
	if result.StaleBlocks[0].Hash != "b2b" {
		t.Errorf("expected stale block b2b, got %s", result.StaleBlocks[0].Hash)
	}
	if len(result.Reorganizations) != 1 {
		t.Errorf("expected 1 reorg, got %d", len(result.Reorganizations))
	}
	expectedForkRate := 1.0 / 4.0
	if got := result.ForkRate(); got < expectedForkRate-0.001 || got > expectedForkRate+0.001 {
		t.Errorf("expected fork rate %.4f, got %.4f", expectedForkRate, got)
	}
	expectedStaleRate := 1.0 / 4.0
	if got := result.StaleRate(); got < expectedStaleRate-0.001 || got > expectedStaleRate+0.001 {
		t.Errorf("expected stale rate %.4f, got %.4f", expectedStaleRate, got)
	}
}

// TestZeroEvents verifies that an empty event log produces zero metrics.
func TestZeroEvents(t *testing.T) {
	d := fork.NewDetector([]string{"N0", "N1"}, 0)
	result := d.Analyze(nil)

	if result.TotalBlocks != 0 {
		t.Errorf("expected 0 total blocks, got %d", result.TotalBlocks)
	}
	if len(result.ForkEpisodes) != 0 {
		t.Errorf("expected 0 fork episodes, got %d", len(result.ForkEpisodes))
	}
	if result.ForkRate() != 0 {
		t.Errorf("expected fork rate 0, got %v", result.ForkRate())
	}
	if result.StaleRate() != 0 {
		t.Errorf("expected stale rate 0, got %v", result.StaleRate())
	}
	if result.MeanReorgDepth() != 0 {
		t.Errorf("expected mean reorg depth 0, got %v", result.MeanReorgDepth())
	}
	if result.MaxReorgDepth() != 0 {
		t.Errorf("expected max reorg depth 0, got %d", result.MaxReorgDepth())
	}
}

// TestWarmUpExcludesReorgs verifies that reorgs whose TipHeight falls within
// the warm-up window are NOT counted.
func TestWarmUpExcludesReorgs(t *testing.T) {
	nodeIDs := []string{"N0", "N1"}
	d := fork.NewDetector(nodeIDs, 5) // warm-up = first 5 blocks

	events := []fork.Event{
		// Fork within warm-up (height 1..5 — skipped).
		makePropose(1, 0.5, "N0", "wA1", "genesis", 1),
		makePropose(2, 0.5, "N1", "wB1", "genesis", 1),
		makeTipUpdate(3, 1.0, "N1", "wA1", "wB1", "genesis", 1, "reorg"),

		// Post warm-up: simple linear chain.
		makePropose(4, 6.0, "N0", "p6", "wA1", 6),
		makeTipUpdate(5, 6.1, "N1", "p6", "wA1", "wA1", 6, "extend"),
	}

	result := d.Analyze(events)
	if len(result.Reorganizations) != 0 {
		t.Errorf("expected 0 reorgs (all within warm-up), got %d", len(result.Reorganizations))
	}
	if len(result.ForkEpisodes) != 0 {
		t.Errorf("expected 0 fork episodes (all within warm-up), got %d", len(result.ForkEpisodes))
	}
}
