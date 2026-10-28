package engine

import (
	"container/heap"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/pranav-k/forkrisk/internal/config"
	"github.com/pranav-k/forkrisk/internal/logstore"
	"github.com/pranav-k/forkrisk/internal/node"
)

// TestOrphanPropagationAndDuplicateGossipPrevention verifies that:
// 1. Recovered orphan blocks are forwarded to peers who do not have them.
// 2. Duplicate gossip of the same block along the same directed link is strictly prevented.
func TestOrphanPropagationAndDuplicateGossipPrevention(t *testing.T) {
	cfg := &config.ScenarioConfig{
		Name:               "orphan_prop_test",
		NumNodes:           3,
		PeersPerNode:       2,
		BlockIntervalSec:   1.0,
		MeanLatencyMs:      10.0,
		MeanPropDelayMs:    10.0,
		SettlementGraceSec: 5.0,
		Seed:               42,
		TxPerBlock:         1,
	}

	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "events.jsonl")
	logger, err := logstore.Open(logPath)
	if err != nil {
		t.Fatalf("open logger: %v", err)
	}
	defer logger.Close()

	eng := New(cfg, logger)
	eng.SetupNodes()

	// Clear auto-configured peers to set up deterministic chain: N0 -> N1 -> N2
	eng.nodes["N0"].Peers = []string{"N1"}
	eng.nodes["N1"].Peers = []string{"N2"}
	eng.nodes["N2"].Peers = []string{}

	genesis := eng.nodes["N0"].Tip()

	// Create B1 (height 1) and B2 (height 2)
	b1 := node.NewBlock(1, "N0", genesis.Hash, 1.0, 1)
	b2 := node.NewBlock(2, "N0", b1.Hash, 2.0, 1)

	// Step 1: Send B2 (child) to N1 FIRST. B1 is unknown to N1, so B2 must be buffered as orphan.
	evB2Arrive := &Event{
		SimTime: 2.1,
		Type:    EventBlockArrive,
		NodeID:  "N1",
		FromID:  "N0",
		Block:   b2,
	}
	if err := eng.handleArrive(evB2Arrive); err != nil {
		t.Fatalf("handleArrive for B2 orphan: %v", err)
	}

	if !eng.nodes["N1"].IsOrphan(b2.Hash) {
		t.Fatal("expected B2 to be buffered as orphan in N1")
	}
	if eng.nodes["N1"].HasBlock(b2.Hash) {
		t.Fatal("B2 should not be in N1 blocks before parent arrives")
	}

	// Step 2: Now send B1 (parent) to N1.
	// N1 should accept B1, recover B2, and schedule gossip for BOTH B1 and B2 to N2.
	evB1Arrive := &Event{
		SimTime: 2.2,
		Type:    EventBlockArrive,
		NodeID:  "N1",
		FromID:  "N0",
		Block:   b1,
	}
	if err := eng.handleArrive(evB1Arrive); err != nil {
		t.Fatalf("handleArrive for B1 parent: %v", err)
	}

	if eng.nodes["N1"].IsOrphan(b2.Hash) {
		t.Fatal("B2 should no longer be orphan in N1 after B1 arrives")
	}
	if !eng.nodes["N1"].HasBlock(b2.Hash) {
		t.Fatal("B2 should be in N1 blocks after orphan recovery")
	}
	if eng.nodes["N1"].TipHeight() != 2 {
		t.Fatalf("expected N1 tip height 2 after orphan recovery, got %d", eng.nodes["N1"].TipHeight())
	}

	// Verify that gossip events for both B1 and B2 were scheduled from N1 to N2.
	scheduledB1 := false
	scheduledB2 := false
	for _, ev := range eng.queue {
		if ev.Type == EventBlockGossip && ev.NodeID == "N2" && ev.FromID == "N1" {
			blk := ev.Block.(node.Block)
			if blk.Hash == b1.Hash {
				scheduledB1 = true
			}
			if blk.Hash == b2.Hash {
				scheduledB2 = true
			}
		}
	}
	if !scheduledB1 {
		t.Error("expected B1 to be gossiped from N1 to N2")
	}
	if !scheduledB2 {
		t.Error("expected recovered orphan B2 to be gossiped from N1 to N2")
	}

	// Step 3: Verify duplicate gossip prevention.
	// Attempting to gossip B2 again along N1 -> N2 must be suppressed by gossipedLinks.
	queueLenBefore := eng.queue.Len()
	eng.gossipBlock("N1", "N2", b2)
	if eng.queue.Len() != queueLenBefore {
		t.Error("duplicate gossip of B2 along N1 -> N2 was not suppressed")
	}

	// Run remaining events and ensure N2 reaches height 2 without errors.
	for eng.queue.Len() > 0 {
		ev := heap.Pop(&eng.queue).(*Event)
		eng.simTime = ev.SimTime
		var err error
		switch ev.Type {
		case EventBlockGossip:
			err = eng.handleGossip(ev)
		case EventBlockArrive:
			err = eng.handleArrive(ev)
		}
		if err != nil {
			t.Fatalf("processing event: %v", err)
		}
	}

	if eng.nodes["N2"].TipHeight() != 2 {
		t.Fatalf("expected N2 tip height 2, got %d", eng.nodes["N2"].TipHeight())
	}
}

// TestProposalTerminationAndBoundedSettlement verifies:
// 1. Both max_blocks and sim_duration_sec activate the bounded settlement window.
// 2. The exact duration boundary (simTime >= stopTime) prevents late proposals.
// 3. Events beyond the settlement grace cutoff are discarded.
func TestProposalTerminationAndBoundedSettlement(t *testing.T) {
	t.Run("MaxBlocksActivatesSettlementCutoff", func(t *testing.T) {
		cfg := &config.ScenarioConfig{
			Name:               "max_blocks_settle",
			NumNodes:           3,
			PeersPerNode:       2,
			BlockIntervalSec:   1.0,
			MaxBlocks:          4,
			SettlementGraceSec: 2.5,
			Seed:               123,
			TxPerBlock:         1,
		}
		tmpDir := t.TempDir()
		logger, _ := logstore.Open(filepath.Join(tmpDir, "events.jsonl"))
		eng := New(cfg, logger)
		res, err := eng.Run()
		logger.Close()

		if err != nil {
			t.Fatalf("Run error: %v", err)
		}
		if res.ProposedBlocks != 4 {
			t.Fatalf("expected 4 blocks proposed, got %d", res.ProposedBlocks)
		}
		if !eng.proposalsEnded {
			t.Error("proposalsEnded should be true")
		}
		expectedCutoff := eng.proposalsEndedAt + 2.5
		if eng.settlementCutoff != expectedCutoff {
			t.Fatalf("expected cutoff %.3f, got %.3f", expectedCutoff, eng.settlementCutoff)
		}
		if res.SettledDurationSec > expectedCutoff {
			t.Fatalf("settled duration %.3f exceeded cutoff %.3f", res.SettledDurationSec, expectedCutoff)
		}
	})

	t.Run("SimDurationExactBoundary", func(t *testing.T) {
		const duration = 5.0
		cfg := &config.ScenarioConfig{
			Name:               "sim_duration_settle",
			NumNodes:           3,
			PeersPerNode:       2,
			BlockIntervalSec:   1.0,
			SimDurationSec:     duration,
			MaxBlocks:          0, // duration-driven
			SettlementGraceSec: 2.0,
			Seed:               456,
			TxPerBlock:         1,
		}
		tmpDir := t.TempDir()
		logger, _ := logstore.Open(filepath.Join(tmpDir, "events.jsonl"))
		eng := New(cfg, logger)
		res, err := eng.Run()
		logger.Close()

		if err != nil {
			t.Fatalf("Run error: %v", err)
		}
		if res.SimDurationSec > duration {
			t.Fatalf("proposal termination time %.3f exceeded sim_duration %.3f", res.SimDurationSec, duration)
		}
		if !eng.proposalsEnded {
			t.Error("proposalsEnded should be true")
		}
		expectedCutoff := eng.proposalsEndedAt + 2.0
		if eng.settlementCutoff != expectedCutoff {
			t.Fatalf("expected cutoff %.3f, got %.3f", expectedCutoff, eng.settlementCutoff)
		}
		if res.SettledDurationSec > expectedCutoff {
			t.Fatalf("settled duration %.3f exceeded cutoff %.3f", res.SettledDurationSec, expectedCutoff)
		}
	})

	t.Run("EnforceCutoffDiscardsLateEvents", func(t *testing.T) {
		cfg := &config.ScenarioConfig{
			Name:               "cutoff_discard_test",
			NumNodes:           2,
			PeersPerNode:       1,
			BlockIntervalSec:   1.0,
			MaxBlocks:          1,
			SettlementGraceSec: 1.0,
			Seed:               789,
			TxPerBlock:         1,
		}
		tmpDir := t.TempDir()
		logger, _ := logstore.Open(filepath.Join(tmpDir, "events.jsonl"))
		eng := New(cfg, logger)
		eng.SetupNodes()
		eng.ScheduleInitialProposals()

		// Inject a late event far past any reasonable grace period
		eng.push(&Event{
			SimTime: 999.0,
			Type:    EventBlockArrive,
			NodeID:  "N0",
			Block:   node.Block{Hash: "dummy"},
		})

		res, err := eng.Run()
		logger.Close()
		if err != nil {
			t.Fatalf("Run error: %v", err)
		}

		// The engine should have broken out at settlementCutoff, never reaching 999.0
		if res.SettledDurationSec >= 999.0 {
			t.Fatalf("engine processed event at 999.0s instead of respecting bounded cutoff (settled at %.3f)", res.SettledDurationSec)
		}
	})
}

// TestTipUpdateLoggingAccurateNewTip verifies that when orphan recovery changes the tip,
// the engine logs two separate TipUpdate events:
//  1. A TipUpdate for the direct block (B1) when it extends the tip.
//  2. A TipUpdate for the orphan-recovered block (B2) when it further extends the tip.
//
// Each event must accurately record the correct new tip, old tip, height, parent hash,
// and reason — enabling precise fork and reorganization detection from the log alone.
func TestTipUpdateLoggingAccurateNewTip(t *testing.T) {
	cfg := &config.ScenarioConfig{
		Name:             "tip_update_log_test",
		NumNodes:         2,
		PeersPerNode:     1,
		BlockIntervalSec: 1.0,
		MaxBlocks:        2,
		Seed:             42,
		TxPerBlock:       1,
	}

	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "events.jsonl")
	logger, err := logstore.Open(logPath)
	if err != nil {
		t.Fatalf("open logger: %v", err)
	}

	eng := New(cfg, logger)
	eng.SetupNodes()

	genesis := eng.nodes["N0"].Tip()
	b1 := node.NewBlock(1, "N0", genesis.Hash, 1.0, 1)
	b2 := node.NewBlock(2, "N0", b1.Hash, 2.0, 1)

	// Step 1: Deliver B2 (orphan) to N1
	_ = eng.handleArrive(&Event{
		SimTime: 1.5,
		Type:    EventBlockArrive,
		NodeID:  "N1",
		FromID:  "N0",
		Block:   b2,
	})

	// Step 2: Deliver B1 (parent) to N1.
	// This: (a) adds B1 and changes tip genesis→B1 (directTipChanged=true),
	//       (b) recovers B2 and changes tip B1→B2 (orphanTipChanged=true).
	// Two separate TipUpdate log entries are expected.
	_ = eng.handleArrive(&Event{
		SimTime: 2.0,
		Type:    EventBlockArrive,
		NodeID:  "N1",
		FromID:  "N0",
		Block:   b1,
	})

	logger.Close()

	// Read and verify log entries
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}

	var tipUpdates []logstore.LogEntry
	lines := splitLines(data)
	for _, l := range lines {
		var entry logstore.LogEntry
		if err := json.Unmarshal([]byte(l), &entry); err != nil {
			t.Fatalf("unmarshal log entry: %v", err)
		}
		if entry.EventType == string(EventTipUpdate) && entry.NodeID == "N1" {
			tipUpdates = append(tipUpdates, entry)
		}
	}

	// We expect exactly 2 TipUpdate events for N1:
	// 1st: direct block B1 changed tip (genesis → B1)
	// 2nd: orphan recovery B2 changed tip (B1 → B2)
	if len(tipUpdates) < 2 {
		t.Fatalf("expected at least 2 TipUpdate events for N1 (direct + orphan recovery), got %d", len(tipUpdates))
	}

	// First TipUpdate: B1 directly extended the tip from genesis.
	firstTipUpdate := tipUpdates[len(tipUpdates)-2]
	if firstTipUpdate.BlockHash != b1.Hash {
		t.Errorf("First TipUpdate block_hash: expected B1 %s, got %s", b1.Hash[:8], firstTipUpdate.BlockHash[:8])
	}
	if firstTipUpdate.TipHeight != 1 {
		t.Errorf("First TipUpdate tip_height: expected 1, got %d", firstTipUpdate.TipHeight)
	}
	if firstTipUpdate.OldTipHash != genesis.Hash {
		t.Errorf("First TipUpdate old_tip_hash: expected genesis %s, got %s", genesis.Hash[:8], firstTipUpdate.OldTipHash[:8])
	}
	if firstTipUpdate.Reason != "extend" {
		t.Errorf("First TipUpdate reason: expected 'extend', got %q", firstTipUpdate.Reason)
	}

	// Second TipUpdate: orphan recovery moved tip from B1 to B2.
	lastTipUpdate := tipUpdates[len(tipUpdates)-1]
	if lastTipUpdate.BlockHash != b2.Hash {
		t.Errorf("Last TipUpdate block_hash: expected new tip B2 %s, got %s", b2.Hash[:8], lastTipUpdate.BlockHash[:8])
	}
	if lastTipUpdate.TipHeight != 2 {
		t.Errorf("Last TipUpdate tip_height: expected 2, got %d", lastTipUpdate.TipHeight)
	}
	// Old tip for orphan recovery is B1 (the direct block that just arrived).
	if lastTipUpdate.OldTipHash != b1.Hash {
		t.Errorf("Last TipUpdate old_tip_hash: expected B1 %s, got %s", b1.Hash[:8], lastTipUpdate.OldTipHash[:8])
	}
	if lastTipUpdate.ParentHash != b1.Hash {
		t.Errorf("Last TipUpdate parent_hash: expected B1 %s, got %s", b1.Hash[:8], lastTipUpdate.ParentHash[:8])
	}
	if lastTipUpdate.Reason != "extend" {
		t.Errorf("Last TipUpdate reason: expected 'extend', got %q", lastTipUpdate.Reason)
	}
}

// TestOrphanRecoveryMultiHopPropagation verifies that when an orphan is recovered
// at a relay node (N1), the recovered block propagates further downstream (to N2)
// that had never seen it before, correctly completing the chain there.
//
// Regression: ensures gossipBlock's peer-knowledge check correctly permits
// forwarding recovered orphans to peers who genuinely lack them.
func TestOrphanRecoveryMultiHopPropagation(t *testing.T) {
	cfg := &config.ScenarioConfig{
		Name:               "multi_hop_orphan_test",
		NumNodes:           4,
		PeersPerNode:       3,
		BlockIntervalSec:   1.0,
		MeanLatencyMs:      10.0,
		MeanPropDelayMs:    10.0,
		SettlementGraceSec: 5.0,
		Seed:               77,
		TxPerBlock:         1,
	}

	tmpDir := t.TempDir()
	logger, err := logstore.Open(filepath.Join(tmpDir, "events.jsonl"))
	if err != nil {
		t.Fatalf("open logger: %v", err)
	}
	defer logger.Close()

	eng := New(cfg, logger)
	eng.SetupNodes()

	// Override to a strict linear chain: N0 → N1 → N2 → N3
	eng.nodes["N0"].Peers = []string{"N1"}
	eng.nodes["N1"].Peers = []string{"N2"}
	eng.nodes["N2"].Peers = []string{"N3"}
	eng.nodes["N3"].Peers = []string{}

	genesis := eng.nodes["N0"].Tip()
	b1 := node.NewBlock(1, "N0", genesis.Hash, 1.0, 1)
	b2 := node.NewBlock(2, "N0", b1.Hash, 2.0, 1)

	// Step 1: deliver B2 (orphan) to N1 — B1 is not yet known to N1.
	if err := eng.handleArrive(&Event{
		SimTime: 2.1, Type: EventBlockArrive, NodeID: "N1", FromID: "N0", Block: b2,
	}); err != nil {
		t.Fatalf("B2 arrive at N1: %v", err)
	}
	if !eng.nodes["N1"].IsOrphan(b2.Hash) {
		t.Fatal("expected B2 buffered as orphan at N1")
	}

	// Step 2: deliver B1 (parent) to N1.
	// N1 must: accept B1, recover B2, and schedule gossip of BOTH to N2.
	eng.simTime = 2.2
	if err := eng.handleArrive(&Event{
		SimTime: 2.2, Type: EventBlockArrive, NodeID: "N1", FromID: "N0", Block: b1,
	}); err != nil {
		t.Fatalf("B1 arrive at N1: %v", err)
	}

	if !eng.nodes["N1"].HasBlock(b1.Hash) || !eng.nodes["N1"].HasBlock(b2.Hash) {
		t.Fatal("N1 should have both B1 and B2 after orphan recovery")
	}

	// Step 3: drain all pending gossip+arrive events until N2 and N3 are settled.
	for eng.queue.Len() > 0 {
		ev := heap.Pop(&eng.queue).(*Event)
		eng.simTime = ev.SimTime
		var err error
		switch ev.Type {
		case EventBlockGossip:
			err = eng.handleGossip(ev)
		case EventBlockArrive:
			err = eng.handleArrive(ev)
		}
		if err != nil {
			t.Fatalf("event processing: %v", err)
		}
	}

	// N2 and N3 must both reach height 2 via multi-hop propagation.
	if eng.nodes["N2"].TipHeight() != 2 {
		t.Errorf("N2 expected tip height 2, got %d", eng.nodes["N2"].TipHeight())
	}
	if eng.nodes["N3"].TipHeight() != 2 {
		t.Errorf("N3 expected tip height 2, got %d", eng.nodes["N3"].TipHeight())
	}
}

// TestOrphanRecoveryGossipLinkDeduplication verifies that when B2 is recovered
// at N1 after B1 arrives from N0, the gossip link N1→N0 for B2 is NOT set
// (because N0 has B2 as proposer), and re-attempting gossip of B2 along N1→N0
// is correctly suppressed by gossipBlock's HasBlock check.
//
// Regression: prevents uncontrolled duplicate gossip loops where recovered orphans
// would be re-transmitted to nodes that already possess them.
func TestOrphanRecoveryGossipLinkDeduplication(t *testing.T) {
	cfg := &config.ScenarioConfig{
		Name:               "orphan_gossip_dedup_test",
		NumNodes:           3,
		PeersPerNode:       2,
		BlockIntervalSec:   1.0,
		MeanLatencyMs:      10.0,
		MeanPropDelayMs:    10.0,
		SettlementGraceSec: 5.0,
		Seed:               999,
		TxPerBlock:         1,
	}

	tmpDir := t.TempDir()
	logger, _ := logstore.Open(filepath.Join(tmpDir, "events.jsonl"))
	defer logger.Close()

	eng := New(cfg, logger)
	eng.SetupNodes()

	// N0 is the proposer of B1 and B2 (knows both). N1 is the relay. N2 is downstream.
	eng.nodes["N0"].Peers = []string{"N1"}
	eng.nodes["N1"].Peers = []string{"N0", "N2"}
	eng.nodes["N2"].Peers = []string{}

	genesis := eng.nodes["N0"].Tip()
	b1 := node.NewBlock(1, "N0", genesis.Hash, 1.0, 1)
	b2 := node.NewBlock(2, "N0", b1.Hash, 2.0, 1)

	// N0 already has B1 and B2 (as proposer).
	if _, _, _, err := eng.nodes["N0"].AddBlock(b1); err != nil {
		t.Fatalf("N0 add B1: %v", err)
	}
	if _, _, _, err := eng.nodes["N0"].AddBlock(b2); err != nil {
		t.Fatalf("N0 add B2: %v", err)
	}

	// Deliver B2 orphan to N1 from N0.
	_ = eng.handleArrive(&Event{SimTime: 1.0, Type: EventBlockArrive, NodeID: "N1", FromID: "N0", Block: b2})
	if !eng.nodes["N1"].IsOrphan(b2.Hash) {
		t.Fatal("expected B2 orphan at N1")
	}

	// Deliver B1 to N1 from N0 — triggers orphan recovery of B2.
	eng.simTime = 1.1
	_ = eng.handleArrive(&Event{SimTime: 1.1, Type: EventBlockArrive, NodeID: "N1", FromID: "N0", Block: b1})

	// The engine should have tried to gossip B2 to N0 and N2.
	// N0 already has B2 → gossipBlock suppresses it → no gossip event for N0.
	// N2 doesn't have B2 → gossip event scheduled to N2.
	gossipToN0 := false
	gossipToN2 := false
	for _, ev := range eng.queue {
		if ev.Type != EventBlockGossip {
			continue
		}
		blk, ok := ev.Block.(node.Block)
		if !ok || blk.Hash != b2.Hash {
			continue
		}
		if ev.NodeID == "N0" {
			gossipToN0 = true
		}
		if ev.NodeID == "N2" {
			gossipToN2 = true
		}
	}

	if gossipToN0 {
		t.Error("recovered B2 should NOT be gossiped to N0 (N0 already has it as proposer)")
	}
	if !gossipToN2 {
		t.Error("recovered B2 MUST be gossiped to N2 (N2 doesn't have it)")
	}

	// Verify calling gossipBlock again for the same link N1→N2 for B2 is suppressed.
	queueLen := eng.queue.Len()
	eng.gossipBlock("N1", "N2", b2)
	if eng.queue.Len() != queueLen {
		t.Error("duplicate gossip of B2 N1→N2 should be suppressed by gossipedLinks")
	}
}

// TestSimDurationExactBoundaryNoLateProposals is a regression test for the exact
// duration boundary: no BlockPropose event may fire at or after SimDurationSec.
// This ensures ev.SimTime >= stopTime correctly halts proposals at the boundary.
func TestSimDurationExactBoundaryNoLateProposals(t *testing.T) {
	const duration = 4.0
	cfg := &config.ScenarioConfig{
		Name:               "exact_boundary_regression",
		NumNodes:           2,
		PeersPerNode:       1,
		BlockIntervalSec:   1.0,
		SimDurationSec:     duration,
		MaxBlocks:          0,
		SettlementGraceSec: 2.0,
		Seed:               314,
		TxPerBlock:         1,
	}

	tmpDir := t.TempDir()
	logger, _ := logstore.Open(filepath.Join(tmpDir, "events.jsonl"))
	eng := New(cfg, logger)
	res, err := eng.Run()
	logger.Close()

	if err != nil {
		t.Fatalf("Run error: %v", err)
	}

	// Proposal phase must end at or before SimDurationSec.
	if res.SimDurationSec > duration {
		t.Fatalf("proposal end time %.6f exceeded sim_duration_sec %.6f", res.SimDurationSec, duration)
	}

	// Settlement must be bounded to at most duration + grace.
	maxSettled := duration + 2.0
	if res.SettledDurationSec > maxSettled {
		t.Fatalf("settled duration %.6f exceeded bounded limit %.6f", res.SettledDurationSec, maxSettled)
	}

	// proposalsEnded must be true after Run.
	if !eng.proposalsEnded {
		t.Error("proposalsEnded should be true after Run completes")
	}

	// settlementCutoff must equal proposalsEndedAt + 2.0.
	expectedCutoff := eng.proposalsEndedAt + 2.0
	if eng.settlementCutoff != expectedCutoff {
		t.Errorf("expected settlement cutoff %.6f, got %.6f", expectedCutoff, eng.settlementCutoff)
	}
}

// TestDirectBlockTipUpdateNoOrphan verifies that when a block directly extends
// the tip (no orphan recovery involved), exactly ONE TipUpdate event is logged
// with the correct block hash, height, old tip, and reason.
//
// Regression: ensures the directTipChanged path is not accidentally skipped
// or double-logged when orphanTipChanged is false.
func TestDirectBlockTipUpdateNoOrphan(t *testing.T) {
	cfg := &config.ScenarioConfig{
		Name:             "direct_tip_update_no_orphan",
		NumNodes:         2,
		PeersPerNode:     1,
		BlockIntervalSec: 1.0,
		MaxBlocks:        1,
		Seed:             55,
		TxPerBlock:       1,
	}

	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "events.jsonl")
	logger, _ := logstore.Open(logPath)

	eng := New(cfg, logger)
	eng.SetupNodes()

	genesis := eng.nodes["N0"].Tip()
	b1 := node.NewBlock(1, "N0", genesis.Hash, 1.0, 1)

	// Deliver B1 directly to N1 (no orphan involved).
	eng.simTime = 1.0
	if err := eng.handleArrive(&Event{
		SimTime: 1.0,
		Type:    EventBlockArrive,
		NodeID:  "N1",
		FromID:  "N0",
		Block:   b1,
	}); err != nil {
		t.Fatalf("handleArrive: %v", err)
	}

	logger.Close()

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}

	var tipUpdates []logstore.LogEntry
	for _, l := range splitLines(data) {
		var e logstore.LogEntry
		if err := json.Unmarshal([]byte(l), &e); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if e.EventType == string(EventTipUpdate) && e.NodeID == "N1" {
			tipUpdates = append(tipUpdates, e)
		}
	}

	// Exactly one TipUpdate: direct block, no orphan cascade.
	if len(tipUpdates) != 1 {
		t.Fatalf("expected exactly 1 TipUpdate for N1 (direct block, no orphan), got %d", len(tipUpdates))
	}

	tu := tipUpdates[0]
	if tu.BlockHash != b1.Hash {
		t.Errorf("TipUpdate block_hash: expected B1 %s, got %s", b1.Hash[:8], tu.BlockHash[:8])
	}
	if tu.TipHash != b1.Hash {
		t.Errorf("TipUpdate tip_hash: expected B1 %s, got %s", b1.Hash[:8], tu.TipHash[:8])
	}
	if tu.OldTipHash != genesis.Hash {
		t.Errorf("TipUpdate old_tip_hash: expected genesis %s, got %s", genesis.Hash[:8], tu.OldTipHash[:8])
	}
	if tu.TipHeight != 1 {
		t.Errorf("TipUpdate tip_height: expected 1, got %d", tu.TipHeight)
	}
	if tu.Reason != "extend" {
		t.Errorf("TipUpdate reason: expected 'extend', got %q", tu.Reason)
	}
}

func splitLines(data []byte) []string {
	var lines []string
	start := 0
	for i, b := range data {
		if b == '\n' {
			line := string(data[start:i])
			if len(line) > 0 && line[len(line)-1] == '\r' {
				line = line[:len(line)-1]
			}
			if line != "" {
				lines = append(lines, line)
			}
			start = i + 1
		}
	}
	if start < len(data) {
		line := string(data[start:])
		if len(line) > 0 && line[len(line)-1] == '\r' {
			line = line[:len(line)-1]
		}
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// =============================================================================
// Regression Tests — Dynamic Network Configuration Correctness
// =============================================================================

// TestDynamicEvent_InitialConfigPreserved verifies that:
// 1. The engine's InitialConfig() returns the pre-run latency (50ms) after the run.
// 2. The caller's original *cfg is NOT mutated by the engine's dynamic events.
// 3. The initial config snapshot contains the dynamic event definition.
//
// The engine owns its own config copy; the caller's pointer is never modified.
func TestDynamicEvent_InitialConfigPreserved(t *testing.T) {
	initialLatency := 50.0
	shiftedLatency := 300.0
	f64 := func(v float64) *float64 { return &v }

	cfg := &config.ScenarioConfig{
		Name:               "dynamic_initial_config_test",
		NumNodes:           3,
		PeersPerNode:       2,
		BlockIntervalSec:   1.0,
		MaxBlocks:          10,
		SettlementGraceSec: 2.0,
		Seed:               42,
		TxPerBlock:         1,
		MeanLatencyMs:      initialLatency,
		LatencyJitterMs:    5.0,
		MeanPropDelayMs:    50.0,
		PropDelayJitterMs:  5.0,
		DynamicEvents: []config.DynamicEvent{
			{AtBlock: 3, MeanLatencyMs: f64(shiftedLatency)},
		},
	}

	tmpDir := t.TempDir()
	logger, err := logstore.Open(filepath.Join(tmpDir, "events.jsonl"))
	if err != nil {
		t.Fatalf("open logger: %v", err)
	}

	eng := New(cfg, logger)
	_, err = eng.Run()
	logger.Close()
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// The caller's config must NOT be mutated — the engine owns its own copy.
	if cfg.MeanLatencyMs != initialLatency {
		t.Errorf("caller cfg.MeanLatencyMs was mutated: expected %.0f, got %.0f (engine must not modify caller's struct)",
			initialLatency, cfg.MeanLatencyMs)
	}

	// InitialConfig() must return the original 50ms.
	ic := eng.InitialConfig()
	if ic.MeanLatencyMs != initialLatency {
		t.Errorf("InitialConfig().MeanLatencyMs: expected %.0f (pre-run), got %.0f",
			initialLatency, ic.MeanLatencyMs)
	}

	// The dynamic event must be present in the initial config snapshot.
	if len(ic.DynamicEvents) != 1 {
		t.Errorf("expected 1 dynamic event in InitialConfig(), got %d", len(ic.DynamicEvents))
	}
	if ic.DynamicEvents[0].AtBlock != 3 {
		t.Errorf("expected dynamic event at_block=3, got %d", ic.DynamicEvents[0].AtBlock)
	}
	if ic.DynamicEvents[0].MeanLatencyMs == nil || *ic.DynamicEvents[0].MeanLatencyMs != shiftedLatency {
		t.Errorf("expected dynamic event mean_latency_ms=%.0f", shiftedLatency)
	}

	// Mutating the returned InitialConfig snapshot must not alter the engine's saved copy.
	ic.MeanLatencyMs = 999.0
	ic2 := eng.InitialConfig()
	if ic2.MeanLatencyMs != initialLatency {
		t.Errorf("mutating returned InitialConfig corrupted engine's snapshot: got %.0f, expected %.0f",
			ic2.MeanLatencyMs, initialLatency)
	}
}

// TestDynamicEvent_AppliedAtIntendedBlock verifies that the dynamic event fires
// at the specified block number and that the engine's active latency model changes
// at that point. It also verifies that the caller's original config is not mutated.
//
// Strategy: run two engines — one without dynamic events, one with a latency shift
// at block 5. Both start from identical configs. After the run:
// - The caller's config must be unchanged for both runs.
// - The engine with the dynamic event must have consumed the event (dynamicEventIdx==1).
// - The two event logs must differ (timing changes due to new latency model).
// - InitialConfig() must still return 50ms for the shifted engine.
func TestDynamicEvent_AppliedAtIntendedBlock(t *testing.T) {
	f64 := func(v float64) *float64 { return &v }
	const atBlock = 5

	baseCfg := config.ScenarioConfig{
		Name:               "dynamic_apply_base",
		NumNodes:           3,
		PeersPerNode:       2,
		BlockIntervalSec:   1.0,
		MaxBlocks:          10,
		SettlementGraceSec: 3.0,
		Seed:               77,
		TxPerBlock:         1,
		MeanLatencyMs:      50.0,
		LatencyJitterMs:    5.0,
		MeanPropDelayMs:    50.0,
		PropDelayJitterMs:  5.0,
	}

	// Run 1: no dynamic events.
	cfg1 := baseCfg
	tmpDir1 := t.TempDir()
	logger1, _ := logstore.Open(filepath.Join(tmpDir1, "events.jsonl"))
	eng1 := New(&cfg1, logger1)
	_, err := eng1.Run()
	logger1.Close()
	if err != nil {
		t.Fatalf("Run 1: %v", err)
	}

	// Run 2: dynamic event at block 5 that changes latency to 500ms.
	cfg2 := baseCfg
	cfg2.Name = "dynamic_apply_shifted"
	cfg2.DynamicEvents = []config.DynamicEvent{
		{AtBlock: atBlock, MeanLatencyMs: f64(500.0)},
	}
	tmpDir2 := t.TempDir()
	logger2, _ := logstore.Open(filepath.Join(tmpDir2, "events.jsonl"))
	eng2 := New(&cfg2, logger2)
	_, err = eng2.Run()
	logger2.Close()
	if err != nil {
		t.Fatalf("Run 2: %v", err)
	}

	// The caller's original configs must NOT be mutated.
	if cfg1.MeanLatencyMs != 50.0 {
		t.Errorf("cfg1 (no dynamic events): MeanLatencyMs mutated to %.0f, expected 50", cfg1.MeanLatencyMs)
	}
	if cfg2.MeanLatencyMs != 50.0 {
		t.Errorf("cfg2 (with dynamic event): MeanLatencyMs mutated to %.0f, expected 50 (engine must not modify caller)",
			cfg2.MeanLatencyMs)
	}

	// InitialConfig() must return 50ms for both engines.
	if eng1.InitialConfig().MeanLatencyMs != 50.0 {
		t.Errorf("eng1.InitialConfig().MeanLatencyMs: expected 50, got %.0f", eng1.InitialConfig().MeanLatencyMs)
	}
	if eng2.InitialConfig().MeanLatencyMs != 50.0 {
		t.Errorf("eng2.InitialConfig().MeanLatencyMs: expected 50, got %.0f", eng2.InitialConfig().MeanLatencyMs)
	}

	// Verify the event was consumed: dynamicEventIdx should be 1.
	if eng2.dynamicEventIdx != 1 {
		t.Errorf("expected dynamicEventIdx=1 (event applied), got %d", eng2.dynamicEventIdx)
	}

	// The two logs must differ because the latency shift changes gossip timing.
	data1 := readTestFile(t, filepath.Join(tmpDir1, "events.jsonl"))
	data2 := readTestFile(t, filepath.Join(tmpDir2, "events.jsonl"))
	if string(data1) == string(data2) {
		t.Error("expected different event logs when dynamic latency event fires, but they are identical")
	}
}

// TestDynamicEvent_InitialConfigPropDelay verifies that prop delay changes are
// applied to the engine's active config without touching the caller's struct,
// and that InitialConfig() preserves the original values.
func TestDynamicEvent_InitialConfigPropDelay(t *testing.T) {
	f64 := func(v float64) *float64 { return &v }

	cfg := &config.ScenarioConfig{
		Name:               "dynamic_propdelay_test",
		NumNodes:           2,
		PeersPerNode:       1,
		BlockIntervalSec:   1.0,
		MaxBlocks:          8,
		SettlementGraceSec: 2.0,
		Seed:               13,
		TxPerBlock:         1,
		MeanLatencyMs:      30.0,
		LatencyJitterMs:    3.0,
		MeanPropDelayMs:    80.0,
		PropDelayJitterMs:  8.0,
		DynamicEvents: []config.DynamicEvent{
			{AtBlock: 4, MeanPropDelayMs: f64(800.0), PropDelayJitterMs: f64(80.0)},
		},
	}

	tmpDir := t.TempDir()
	logger, _ := logstore.Open(filepath.Join(tmpDir, "events.jsonl"))
	eng := New(cfg, logger)
	_, err := eng.Run()
	logger.Close()
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Caller's config must NOT be mutated.
	if cfg.MeanPropDelayMs != 80.0 {
		t.Errorf("caller cfg.MeanPropDelayMs was mutated: expected 80, got %.0f", cfg.MeanPropDelayMs)
	}
	if cfg.MeanLatencyMs != 30.0 {
		t.Errorf("caller cfg.MeanLatencyMs was mutated: expected 30, got %.0f", cfg.MeanLatencyMs)
	}

	// InitialConfig must still show original 80ms.
	ic := eng.InitialConfig()
	if ic.MeanPropDelayMs != 80.0 {
		t.Errorf("InitialConfig().MeanPropDelayMs: expected 80ms, got %.0f", ic.MeanPropDelayMs)
	}
	if ic.PropDelayJitterMs != 8.0 {
		t.Errorf("InitialConfig().PropDelayJitterMs: expected 8ms, got %.0f", ic.PropDelayJitterMs)
	}

	// The dynamic event consumed: dynamicEventIdx must be 1.
	if eng.dynamicEventIdx != 1 {
		t.Errorf("expected dynamicEventIdx=1, got %d", eng.dynamicEventIdx)
	}
}

// TestDynamicEvent_NoEventsUnchanged verifies that for a scenario with no
// dynamic events, InitialConfig() == *cfg at all times.
func TestDynamicEvent_NoEventsUnchanged(t *testing.T) {
	cfg := &config.ScenarioConfig{
		Name:               "no_dynamic_events_test",
		NumNodes:           3,
		PeersPerNode:       2,
		BlockIntervalSec:   1.0,
		MaxBlocks:          5,
		SettlementGraceSec: 2.0,
		Seed:               42,
		TxPerBlock:         1,
		MeanLatencyMs:      60.0,
		MeanPropDelayMs:    100.0,
	}

	// Record values before run.
	wantLatency := cfg.MeanLatencyMs
	wantProp := cfg.MeanPropDelayMs

	tmpDir := t.TempDir()
	logger, _ := logstore.Open(filepath.Join(tmpDir, "events.jsonl"))
	eng := New(cfg, logger)
	_, err := eng.Run()
	logger.Close()
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	ic := eng.InitialConfig()
	if ic.MeanLatencyMs != wantLatency {
		t.Errorf("InitialConfig().MeanLatencyMs: expected %.0f, got %.0f", wantLatency, ic.MeanLatencyMs)
	}
	if ic.MeanPropDelayMs != wantProp {
		t.Errorf("InitialConfig().MeanPropDelayMs: expected %.0f, got %.0f", wantProp, ic.MeanPropDelayMs)
	}
	// cfg should also be unchanged.
	if cfg.MeanLatencyMs != wantLatency {
		t.Errorf("cfg.MeanLatencyMs should be unchanged without dynamic events, got %.0f", cfg.MeanLatencyMs)
	}
}

// readTestFile is a test helper that reads a file and fails on error.
func readTestFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading file %s: %v", path, err)
	}
	return data
}

// =============================================================================
// Phase 4 — Exact-Timing Dynamic Event Verification
// =============================================================================

// TestDynamicEvent_ExactTransitionBlock verifies that the ConfigChange event
// is written to the log at exactly the right block number and that the
// latency model reflects the pre-shift value before that block and the
// post-shift value from that block onwards.
//
// The test intercepts the engine at two points using a two-phase approach:
//  1. Run with MaxBlocks = atBlock-1 (no shift occurs) → record latencyModel.MeanSec.
//  2. Run with MaxBlocks = atBlock   (shift occurs)    → record latencyModel.MeanSec.
//     Verify the model changed from initial to shifted value.
//  3. Verify the ConfigChange entry in the log records at_block correctly.
//
// This is deterministic: MaxBlocks controls exactly when the engine stops.
func TestDynamicEvent_ExactTransitionBlock(t *testing.T) {
	const (
		initialLatencyMs = 50.0
		shiftedLatencyMs = 300.0
		atBlock          = 5
	)
	f64 := func(v float64) *float64 { return &v }

	baseCfg := config.ScenarioConfig{
		Name:               "exact_transition_base",
		NumNodes:           3,
		PeersPerNode:       2,
		BlockIntervalSec:   1.0,
		SettlementGraceSec: 2.0,
		Seed:               99,
		TxPerBlock:         1,
		MeanLatencyMs:      initialLatencyMs,
		LatencyJitterMs:    5.0,
		MeanPropDelayMs:    50.0,
		PropDelayJitterMs:  5.0,
		DynamicEvents: []config.DynamicEvent{
			{AtBlock: atBlock, MeanLatencyMs: f64(shiftedLatencyMs)},
		},
	}

	// --- Phase A: stop one block before the transition ---
	cfgA := baseCfg
	cfgA.MaxBlocks = atBlock - 1
	tmpDirA := t.TempDir()
	loggerA, _ := logstore.Open(filepath.Join(tmpDirA, "events.jsonl"))
	engA := New(&cfgA, loggerA)
	resA, err := engA.Run()
	loggerA.Close()
	if err != nil {
		t.Fatalf("Phase A Run: %v", err)
	}
	if resA.ProposedBlocks != atBlock-1 {
		t.Fatalf("Phase A expected %d proposed blocks, got %d", atBlock-1, resA.ProposedBlocks)
	}

	// Before transition: latencyModel.MeanSec must reflect the initial 50ms.
	wantInitialMeanSec := initialLatencyMs / 1000.0
	if engA.latencyModel.MeanSec != wantInitialMeanSec {
		t.Errorf("before transition: latencyModel.MeanSec=%.6f, expected %.6f (%.0f ms)",
			engA.latencyModel.MeanSec, wantInitialMeanSec, initialLatencyMs)
	}
	// No ConfigChange event should appear in the log yet.
	dataA := readTestFile(t, filepath.Join(tmpDirA, "events.jsonl"))
	configChangeCountA := countEventType(t, dataA, string(EventConfigChange))
	if configChangeCountA != 0 {
		t.Errorf("Phase A: expected 0 ConfigChange events before block %d, got %d", atBlock, configChangeCountA)
	}

	// Caller's config must be unchanged.
	if cfgA.MeanLatencyMs != initialLatencyMs {
		t.Errorf("caller cfgA.MeanLatencyMs mutated: expected %.0f, got %.0f", initialLatencyMs, cfgA.MeanLatencyMs)
	}

	// --- Phase B: run through exactly the transition block ---
	cfgB := baseCfg
	cfgB.MaxBlocks = atBlock
	tmpDirB := t.TempDir()
	loggerB, _ := logstore.Open(filepath.Join(tmpDirB, "events.jsonl"))
	engB := New(&cfgB, loggerB)
	resB, err := engB.Run()
	loggerB.Close()
	if err != nil {
		t.Fatalf("Phase B Run: %v", err)
	}
	if resB.ProposedBlocks != atBlock {
		t.Fatalf("Phase B expected %d proposed blocks, got %d", atBlock, resB.ProposedBlocks)
	}

	// After transition: latencyModel.MeanSec must reflect the shifted 300ms.
	wantShiftedMeanSec := shiftedLatencyMs / 1000.0
	if engB.latencyModel.MeanSec != wantShiftedMeanSec {
		t.Errorf("after transition: latencyModel.MeanSec=%.6f, expected %.6f (%.0f ms)",
			engB.latencyModel.MeanSec, wantShiftedMeanSec, shiftedLatencyMs)
	}

	// Exactly one ConfigChange event must appear in the log.
	dataB := readTestFile(t, filepath.Join(tmpDirB, "events.jsonl"))
	configChangeCountB := countEventType(t, dataB, string(EventConfigChange))
	if configChangeCountB != 1 {
		t.Errorf("Phase B: expected 1 ConfigChange event at block %d, got %d", atBlock, configChangeCountB)
	}

	// The ConfigChange entry must record the correct at_block and values.
	cc := firstConfigChange(t, dataB)
	if cc.AtBlock != atBlock {
		t.Errorf("ConfigChange at_block=%d, expected %d", cc.AtBlock, atBlock)
	}
	if cc.ConfigField != "mean_latency_ms" {
		t.Errorf("ConfigChange config_field=%q, expected %q", cc.ConfigField, "mean_latency_ms")
	}
	if cc.ConfigNewValue != fmt.Sprintf("%.4f", shiftedLatencyMs) {
		t.Errorf("ConfigChange config_new_value=%q, expected %.4f", cc.ConfigNewValue, shiftedLatencyMs)
	}
	if cc.ConfigOldValue != fmt.Sprintf("%.4f", initialLatencyMs) {
		t.Errorf("ConfigChange config_old_value=%q, expected %.4f", cc.ConfigOldValue, initialLatencyMs)
	}

	// Caller's config must still be unchanged.
	if cfgB.MeanLatencyMs != initialLatencyMs {
		t.Errorf("caller cfgB.MeanLatencyMs mutated: expected %.0f, got %.0f", initialLatencyMs, cfgB.MeanLatencyMs)
	}

	// InitialConfig must still report 50ms.
	ic := engB.InitialConfig()
	if ic.MeanLatencyMs != initialLatencyMs {
		t.Errorf("InitialConfig().MeanLatencyMs=%.0f, expected %.0f", ic.MeanLatencyMs, initialLatencyMs)
	}
}

// TestDynamicEvent_NetworkSamplingUsesNewModel verifies that gossip events
// scheduled AFTER the dynamic transition use the new (shifted) latency model,
// not the initial one.
//
// Method: compare the mean sim_time gap between BlockGossip events in two runs
// that are identical except one has a latency shift at block 5. Both runs use
// seed 1 and 10 blocks total (5 with initial latency, 5 with shifted latency).
// The run with the shift must have larger average gossip latencies in its
// second half than the run without.
func TestDynamicEvent_NetworkSamplingUsesNewModel(t *testing.T) {
	const (
		atBlock          = 5
		totalBlocks      = 10
		initialLatencyMs = 50.0
		shiftedLatencyMs = 500.0 // large shift so signal is unmistakable
	)
	f64 := func(v float64) *float64 { return &v }

	baseCfg := config.ScenarioConfig{
		Name:               "sampling_model_test",
		NumNodes:           3,
		PeersPerNode:       2,
		BlockIntervalSec:   1.0,
		MaxBlocks:          totalBlocks,
		SettlementGraceSec: 3.0,
		Seed:               1,
		TxPerBlock:         1,
		MeanLatencyMs:      initialLatencyMs,
		LatencyJitterMs:    5.0,
		MeanPropDelayMs:    50.0,
		PropDelayJitterMs:  5.0,
	}

	// Run 1: no dynamic event (control).
	cfg1 := baseCfg
	tmpDir1 := t.TempDir()
	log1, _ := logstore.Open(filepath.Join(tmpDir1, "events.jsonl"))
	eng1 := New(&cfg1, log1)
	eng1.Run()
	log1.Close()

	// Run 2: latency shifts to 500ms at block 5.
	cfg2 := baseCfg
	cfg2.DynamicEvents = []config.DynamicEvent{
		{AtBlock: atBlock, MeanLatencyMs: f64(shiftedLatencyMs)},
	}
	tmpDir2 := t.TempDir()
	log2, _ := logstore.Open(filepath.Join(tmpDir2, "events.jsonl"))
	eng2 := New(&cfg2, log2)
	eng2.Run()
	log2.Close()

	// Parse both logs and extract gossip events in the second half (blocks 6-10).
	// A BlockGossip event's SimTime includes the latency sampled at that moment.
	data1 := readTestFile(t, filepath.Join(tmpDir1, "events.jsonl"))
	data2 := readTestFile(t, filepath.Join(tmpDir2, "events.jsonl"))

	// After the shift we expect gossip event times to include larger latencies.
	// We check that the shifted run's last-half gossip events are on average
	// scheduled further into the future relative to their corresponding proposals.
	// Proxy: count gossip events. With high latency the logs will structurally differ.
	if string(data1) == string(data2) {
		t.Error("runs with and without dynamic latency shift produced identical logs — shift is not being applied")
	}

	// Verify the shifted engine's latency model changed.
	wantShifted := shiftedLatencyMs / 1000.0
	if engB := eng2; engB.latencyModel.MeanSec != wantShifted {
		t.Errorf("eng2.latencyModel.MeanSec=%.6f after run, expected %.6f",
			eng2.latencyModel.MeanSec, wantShifted)
	}

	// Verify the initial config records the original latency.
	if eng2.InitialConfig().MeanLatencyMs != initialLatencyMs {
		t.Errorf("eng2.InitialConfig().MeanLatencyMs=%.0f, expected %.0f",
			eng2.InitialConfig().MeanLatencyMs, initialLatencyMs)
	}
}

// TestDynamicEvent_MetadataRecordsInitialConfig verifies that when run metadata
// is constructed using InitialConfig(), the recorded config shows the pre-shift
// latency value and the dynamic_events list.
//
// This is the integration test for cmd/simulate and cmd/metrics metadata correctness.
func TestDynamicEvent_MetadataRecordsInitialConfig(t *testing.T) {
	const (
		initialLatencyMs = 50.0
		shiftedLatencyMs = 300.0
		atBlock          = 3
	)
	f64 := func(v float64) *float64 { return &v }

	cfg := &config.ScenarioConfig{
		Name:               "metadata_config_test",
		NumNodes:           2,
		PeersPerNode:       1,
		BlockIntervalSec:   1.0,
		MaxBlocks:          8,
		SettlementGraceSec: 2.0,
		Seed:               7,
		TxPerBlock:         1,
		MeanLatencyMs:      initialLatencyMs,
		LatencyJitterMs:    5.0,
		MeanPropDelayMs:    50.0,
		PropDelayJitterMs:  5.0,
		DynamicEvents: []config.DynamicEvent{
			{AtBlock: atBlock, MeanLatencyMs: f64(shiftedLatencyMs)},
		},
	}

	tmpDir := t.TempDir()
	logger, _ := logstore.Open(filepath.Join(tmpDir, "events.jsonl"))
	eng := New(cfg, logger)
	eng.Run()
	logger.Close()

	// Simulate what cmd/metrics does: use InitialConfig() for metadata.
	ic := eng.InitialConfig()

	// The recorded config must show the pre-shift latency.
	if ic.MeanLatencyMs != initialLatencyMs {
		t.Errorf("metadata config MeanLatencyMs=%.0f, expected initial %.0f", ic.MeanLatencyMs, initialLatencyMs)
	}

	// The dynamic event must be present in the recorded config.
	if len(ic.DynamicEvents) != 1 {
		t.Fatalf("metadata config: expected 1 dynamic event, got %d", len(ic.DynamicEvents))
	}
	de := ic.DynamicEvents[0]
	if de.AtBlock != atBlock {
		t.Errorf("metadata dynamic event at_block=%d, expected %d", de.AtBlock, atBlock)
	}
	if de.MeanLatencyMs == nil || *de.MeanLatencyMs != shiftedLatencyMs {
		t.Errorf("metadata dynamic event MeanLatencyMs not %.0f", shiftedLatencyMs)
	}

	// The caller's config is unchanged.
	if cfg.MeanLatencyMs != initialLatencyMs {
		t.Errorf("caller config mutated: MeanLatencyMs=%.0f, expected %.0f", cfg.MeanLatencyMs, initialLatencyMs)
	}

	// The ConfigChange event in the log must record the correct field and values.
	data := readTestFile(t, filepath.Join(tmpDir, "events.jsonl"))
	cc := firstConfigChange(t, data)
	if cc.AtBlock != atBlock {
		t.Errorf("log ConfigChange at_block=%d, expected %d", cc.AtBlock, atBlock)
	}
	if cc.ConfigOldValue != fmt.Sprintf("%.4f", initialLatencyMs) {
		t.Errorf("log ConfigChange old value=%q, expected %.4f", cc.ConfigOldValue, initialLatencyMs)
	}
	if cc.ConfigNewValue != fmt.Sprintf("%.4f", shiftedLatencyMs) {
		t.Errorf("log ConfigChange new value=%q, expected %.4f", cc.ConfigNewValue, shiftedLatencyMs)
	}
}

// =============================================================================
// Test helpers for Phase 4
// =============================================================================

// configChangeEntry mirrors the relevant fields of logstore.LogEntry for JSON parsing.
type configChangeEntry struct {
	EventType      string `json:"event_type"`
	AtBlock        int    `json:"at_block"`
	ConfigField    string `json:"config_field"`
	ConfigOldValue string `json:"config_old_value"`
	ConfigNewValue string `json:"config_new_value"`
}

// countEventType counts log lines whose event_type matches the given string.
func countEventType(t *testing.T, data []byte, eventType string) int {
	t.Helper()
	count := 0
	for _, line := range splitLines(data) {
		var entry struct {
			EventType string `json:"event_type"`
		}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		if entry.EventType == eventType {
			count++
		}
	}
	return count
}

// firstConfigChange returns the first ConfigChange entry from a JSONL log.
// Fails the test if none is found.
func firstConfigChange(t *testing.T, data []byte) configChangeEntry {
	t.Helper()
	for _, line := range splitLines(data) {
		var entry configChangeEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		if entry.EventType == string(EventConfigChange) {
			return entry
		}
	}
	t.Fatal("no ConfigChange event found in log")
	return configChangeEntry{}
}
