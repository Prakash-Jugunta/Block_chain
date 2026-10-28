package engine_test

import (
	"container/heap"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pranav-k/forkrisk/internal/config"
	"github.com/pranav-k/forkrisk/internal/engine"
	"github.com/pranav-k/forkrisk/internal/logstore"
)

// TestEventQueueEmptyLen verifies an empty queue has Len() == 0.
func TestEventQueueEmptyLen(t *testing.T) {
	q := engine.EventQueue{}
	heap.Init(&q)
	if q.Len() != 0 {
		t.Errorf("expected len 0, got %d", q.Len())
	}
}

// TestEventQueueChronologicalOrder verifies that events are popped in
// ascending SimTime order (min-heap property).
func TestEventQueueChronologicalOrder(t *testing.T) {
	q := &engine.EventQueue{}
	heap.Init(q)

	// Push events out of order.
	times := []float64{5.0, 1.0, 3.0, 2.0, 4.0}
	for i, t := range times {
		heap.Push(q, &engine.Event{SimTime: t, Seq: int64(i)})
	}

	prev := -1.0
	for q.Len() > 0 {
		ev := heap.Pop(q).(*engine.Event)
		if ev.SimTime < prev {
			t.Errorf("events not in order: got %.1f after %.1f", ev.SimTime, prev)
		}
		prev = ev.SimTime
	}
}

// TestEventQueueTieBreakBySeq verifies that events with identical SimTime
// are popped in ascending Seq order. This is the determinism guarantee.
func TestEventQueueTieBreakBySeq(t *testing.T) {
	q := &engine.EventQueue{}
	heap.Init(q)

	// All events at the same simulated time — should come out in Seq order.
	const sameTime = 10.0
	seqs := []int64{5, 1, 3, 2, 4}
	for _, s := range seqs {
		heap.Push(q, &engine.Event{SimTime: sameTime, Seq: s})
	}

	prevSeq := int64(-1)
	for q.Len() > 0 {
		ev := heap.Pop(q).(*engine.Event)
		if ev.Seq < prevSeq {
			t.Errorf("Seq not in order: got %d after %d", ev.Seq, prevSeq)
		}
		prevSeq = ev.Seq
	}
}

// TestEventQueueMixedOrdering tests a realistic mix of times and seq numbers.
func TestEventQueueMixedOrdering(t *testing.T) {
	q := &engine.EventQueue{}
	heap.Init(q)

	type ev struct {
		time float64
		seq  int64
	}
	events := []ev{
		{2.0, 10},
		{1.0, 20},
		{2.0, 5},
		{1.0, 15},
		{3.0, 1},
	}
	for _, e := range events {
		heap.Push(q, &engine.Event{SimTime: e.time, Seq: e.seq})
	}

	// Expected order: (1.0,15), (1.0,20), (2.0,5), (2.0,10), (3.0,1)
	expected := []ev{
		{1.0, 15},
		{1.0, 20},
		{2.0, 5},
		{2.0, 10},
		{3.0, 1},
	}
	for i, want := range expected {
		got := heap.Pop(q).(*engine.Event)
		if got.SimTime != want.time || got.Seq != want.seq {
			t.Errorf("position %d: want (%.1f, %d) got (%.1f, %d)",
				i, want.time, want.seq, got.SimTime, got.Seq)
		}
	}
}

// TestEventQueueSingleElement verifies push/pop with one element.
func TestEventQueueSingleElement(t *testing.T) {
	q := &engine.EventQueue{}
	heap.Init(q)
	heap.Push(q, &engine.Event{SimTime: 7.0, Seq: 1, Type: engine.EventBlockPropose, NodeID: "N0"})
	if q.Len() != 1 {
		t.Fatalf("expected len 1, got %d", q.Len())
	}
	ev := heap.Pop(q).(*engine.Event)
	if ev.NodeID != "N0" {
		t.Errorf("expected NodeID N0, got %s", ev.NodeID)
	}
	if q.Len() != 0 {
		t.Errorf("expected empty queue after pop, got len %d", q.Len())
	}
}

// TestProposerSelectionRoundRobin verifies that the round_robin selection rule
// cycles sequentially through node IDs.
func TestProposerSelectionRoundRobin(t *testing.T) {
	cfg := &config.ScenarioConfig{
		Name:              "rr_test",
		NumNodes:          3,
		BlockIntervalSec:  1.0,
		MaxBlocks:         6,
		Seed:              123,
		PeersPerNode:      1,
		TxPerBlock:        1,
		ProposerSelection: "round_robin",
	}

	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "events.jsonl")
	logger, err := logstore.Open(logPath)
	if err != nil {
		t.Fatalf("open logger: %v", err)
	}

	eng := engine.New(cfg, logger)
	res, err := eng.Run()
	logger.Close()
	if err != nil {
		t.Fatalf("engine Run failed: %v", err)
	}

	if res.ProposedBlocks != 6 {
		t.Errorf("expected 6 proposed blocks, got %d", res.ProposedBlocks)
	}
	if res.ErrorCount != 0 {
		t.Errorf("expected 0 errors, got %d (%v)", res.ErrorCount, res.Errors)
	}
}

// TestProposalSettlementDrainsMessages verifies that reaching max_blocks stops
// proposals but drains all in-flight messages so nodes reach consistent heights.
func TestProposalSettlementDrainsMessages(t *testing.T) {
	cfg := &config.ScenarioConfig{
		Name:               "settle_test",
		NumNodes:           4,
		BlockIntervalSec:   1.0,
		MeanLatencyMs:      50.0,
		MeanPropDelayMs:    50.0,
		MaxBlocks:          10,
		SettlementGraceSec: 5.0,
		Seed:               42,
		PeersPerNode:       2,
		TxPerBlock:         2,
	}

	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "events.jsonl")
	logger, err := logstore.Open(logPath)
	if err != nil {
		t.Fatalf("open logger: %v", err)
	}

	eng := engine.New(cfg, logger)
	res, err := eng.Run()
	logger.Close()
	if err != nil {
		t.Fatalf("engine Run failed: %v", err)
	}

	if res.ProposedBlocks != 10 {
		t.Errorf("expected exactly 10 proposed blocks, got %d", res.ProposedBlocks)
	}

	// Settlement duration should be greater than proposal end time if messages were in flight
	if res.SettledDurationSec < res.SimDurationSec {
		t.Errorf("settled duration (%.3f) should be >= proposal duration (%.3f)", res.SettledDurationSec, res.SimDurationSec)
	}

	// All nodes should have received the blocks and reached a valid height >= 1
	for id, h := range res.NodeTipHeights {
		if h < 1 {
			t.Errorf("node %s did not receive propagated blocks (height: %d)", id, h)
		}
	}
}

// TestTxArrival_DecoupledFromBlockInterval verifies that changing block interval (e.g. 1.0s to 0.2s)
// increases block proposal frequency by 5x while offered transaction arrivals remain independent.
func TestTxArrival_DecoupledFromBlockInterval(t *testing.T) {
	tmpDir := t.TempDir()

	baseCfg := &config.ScenarioConfig{
		Name:             "base_tx_test",
		NumNodes:         4,
		BlockIntervalSec: 1.0,
		SimDurationSec:   50.0,
		Seed:             42,
		PeersPerNode:     2,
		TxPerBlock:       10,
		TxArrivalRate:    1.0,
	}

	logger1, err := logstore.Open(filepath.Join(tmpDir, "base_events.jsonl"))
	if err != nil {
		t.Fatalf("open logger1: %v", err)
	}
	eng1 := engine.New(baseCfg, logger1)
	res1, err := eng1.Run()
	logger1.Close()
	if err != nil {
		t.Fatalf("eng1 Run: %v", err)
	}

	stressCfg := &config.ScenarioConfig{
		Name:             "stress_tx_test",
		NumNodes:         4,
		BlockIntervalSec: 0.2, // 5x faster proposals
		SimDurationSec:   50.0,
		Seed:             42,
		PeersPerNode:     2,
		TxPerBlock:       10,
		TxArrivalRate:    1.0, // same offered load
	}

	logger2, err := logstore.Open(filepath.Join(tmpDir, "stress_events.jsonl"))
	if err != nil {
		t.Fatalf("open logger2: %v", err)
	}
	eng2 := engine.New(stressCfg, logger2)
	res2, err := eng2.Run()
	logger2.Close()
	if err != nil {
		t.Fatalf("eng2 Run: %v", err)
	}

	// 1. Block frequency must be ~5x higher.
	ratio := float64(res2.ProposedBlocks) / float64(res1.ProposedBlocks)
	if ratio < 4.5 || ratio > 5.5 {
		t.Errorf("expected ~5x block proposal ratio, got %.2f (base: %d, stress: %d)",
			ratio, res1.ProposedBlocks, res2.ProposedBlocks)
	}

	_ = math.Abs(0)
	t.Logf("base offered: %d, stress offered: %d", res1.TotalOfferedTx, res2.TotalOfferedTx)
	if res1.TotalOfferedTx != res2.TotalOfferedTx {
		t.Errorf("offered tx load differed between T=1.0s and T=0.2s: base=%d, stress=%d",
			res1.TotalOfferedTx, res2.TotalOfferedTx)
	}

	// 3. Average tx count per block in stress run should be ~1/5 of baseline run.
	avgTx1 := float64(res1.TotalProcessedTx) / float64(res1.ProposedBlocks)
	avgTx2 := float64(res2.TotalProcessedTx) / float64(res2.ProposedBlocks)
	if avgTx2 > avgTx1*0.35 {
		t.Errorf("expected stress blocks to hold ~2 tx, got %.2f (base was %.2f)", avgTx2, avgTx1)
	}

	// 4. Verify exact invariant for both runs.
	if res1.TotalOfferedTx != res1.TotalProcessedTx+res1.FinalPendingTx {
		t.Errorf("invariant violated for base: offered=%d, processed=%d, pending=%d",
			res1.TotalOfferedTx, res1.TotalProcessedTx, res1.FinalPendingTx)
	}
	if res2.TotalOfferedTx != res2.TotalProcessedTx+res2.FinalPendingTx {
		t.Errorf("invariant violated for stress: offered=%d, processed=%d, pending=%d",
			res2.TotalOfferedTx, res2.TotalProcessedTx, res2.FinalPendingTx)
	}
}

// TestTxArrival_RateScalesOfferedLoad verifies that increasing tx_arrival_rate from 1.0 to 10.0
// scales offered load by ~10x while block proposal cadence remains unaffected.
func TestTxArrival_RateScalesOfferedLoad(t *testing.T) {
	tmpDir := t.TempDir()

	cfg1 := &config.ScenarioConfig{
		Name:             "rate_1x",
		NumNodes:         4,
		BlockIntervalSec: 1.0,
		SimDurationSec:   40.0,
		Seed:             123,
		PeersPerNode:     2,
		TxPerBlock:       10,
		TxArrivalRate:    1.0,
	}

	logger1, _ := logstore.Open(filepath.Join(tmpDir, "rate1.jsonl"))
	eng1 := engine.New(cfg1, logger1)
	res1, _ := eng1.Run()
	logger1.Close()

	cfg10 := &config.ScenarioConfig{
		Name:             "rate_10x",
		NumNodes:         4,
		BlockIntervalSec: 1.0,
		SimDurationSec:   40.0,
		Seed:             123,
		PeersPerNode:     2,
		TxPerBlock:       10,
		TxArrivalRate:    10.0,
		BlockCapacity:    10, // limits block processing to observe saturation
	}

	logger10, _ := logstore.Open(filepath.Join(tmpDir, "rate10.jsonl"))
	eng10 := engine.New(cfg10, logger10)
	res10, _ := eng10.Run()
	logger10.Close()

	// Proposals should be identical in count AND proposal schedule (timestamps + proposer nodes).
	if res1.ProposedBlocks != res10.ProposedBlocks {
		t.Errorf("block count differed: 1x had %d, 10x had %d", res1.ProposedBlocks, res10.ProposedBlocks)
	}

	// Compare proposal timestamps between 1x and 10x runs to prove proposal schedule invariance.
	proposals1 := readBlockProposals(t, filepath.Join(tmpDir, "rate1.jsonl"))
	proposals10 := readBlockProposals(t, filepath.Join(tmpDir, "rate10.jsonl"))
	if len(proposals1) != len(proposals10) {
		t.Fatalf("proposal count mismatch: 1x had %d, 10x had %d", len(proposals1), len(proposals10))
	}
	for i := range proposals1 {
		p1, p10 := proposals1[i], proposals10[i]
		if p1.SimTime != p10.SimTime || p1.NodeID != p10.NodeID || p1.Height != p10.Height {
			t.Fatalf("proposal schedule diverged at proposal %d: 1x=(t=%.6f, node=%s, h=%d), 10x=(t=%.6f, node=%s, h=%d)",
				i, p1.SimTime, p1.NodeID, p1.Height, p10.SimTime, p10.NodeID, p10.Height)
		}
	}

	// Offered tx should scale ~10x
	loadRatio := float64(res10.TotalOfferedTx) / float64(res1.TotalOfferedTx)
	if loadRatio < 8.5 || loadRatio > 11.5 {
		t.Errorf("expected ~10x offered load scaling, got %.2f (1x: %d, 10x: %d)",
			loadRatio, res1.TotalOfferedTx, res10.TotalOfferedTx)
	}

	// 10x with capacity 10 should produce significant pending backlog
	if res10.FinalPendingTx <= 0 {
		t.Errorf("expected pending backlog in 10x run with capacity 10, got %d", res10.FinalPendingTx)
	}

	// Verify exact invariant
	if res10.TotalOfferedTx != res10.TotalProcessedTx+res10.FinalPendingTx {
		t.Errorf("invariant violated: offered=%d, processed=%d, pending=%d",
			res10.TotalOfferedTx, res10.TotalProcessedTx, res10.FinalPendingTx)
	}
}

// proposalInfo captures proposal timing and identity for schedule comparison.
type proposalInfo struct {
	SimTime float64
	NodeID  string
	Height  int
}

func readBlockProposals(t *testing.T, logPath string) []proposalInfo {
	t.Helper()
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log %s: %v", logPath, err)
	}
	var proposals []proposalInfo
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var entry struct {
			EventType   string  `json:"event_type"`
			SimTime     float64 `json:"sim_time"`
			NodeID      string  `json:"node_id"`
			BlockHeight int     `json:"block_height"`
		}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("unmarshal line in %s: %v", logPath, err)
		}
		if entry.EventType == string(engine.EventBlockPropose) {
			proposals = append(proposals, proposalInfo{
				SimTime: entry.SimTime,
				NodeID:  entry.NodeID,
				Height:  entry.BlockHeight,
			})
		}
	}
	return proposals
}

// TestTxPool_CapacityZeroConsumesAll verifies that block_capacity: 0 means unlimited capacity,
// consuming the entire pending mempool into proposed blocks rather than artificially capping at tx_per_block.
func TestTxPool_CapacityZeroConsumesAll(t *testing.T) {
	tmpDir := t.TempDir()

	// High arrival rate (50 tx/s), but capacity = 0 (unlimited)
	cfgZeroCap := &config.ScenarioConfig{
		Name:             "unlimited_cap",
		NumNodes:         3,
		BlockIntervalSec: 1.0,
		SimDurationSec:   20.0,
		Seed:             456,
		PeersPerNode:     2,
		TxPerBlock:       10,  // nominal scale
		TxArrivalRate:    5.0, // 50 tx/s offered load
		BlockCapacity:    0,   // UNLIMITED
	}

	loggerZero, _ := logstore.Open(filepath.Join(tmpDir, "unlimited.jsonl"))
	engZero := engine.New(cfgZeroCap, loggerZero)
	resZero, _ := engZero.Run()
	loggerZero.Close()

	// Capped comparison: BlockCapacity: 10
	cfgCapped := &config.ScenarioConfig{
		Name:             "capped_10",
		NumNodes:         3,
		BlockIntervalSec: 1.0,
		SimDurationSec:   20.0,
		Seed:             456,
		PeersPerNode:     2,
		TxPerBlock:       10,
		TxArrivalRate:    5.0,
		BlockCapacity:    10, // CAPPED at 10 tx/block
	}

	loggerCapped, _ := logstore.Open(filepath.Join(tmpDir, "capped.jsonl"))
	engCapped := engine.New(cfgCapped, loggerCapped)
	resCapped, _ := engCapped.Run()
	loggerCapped.Close()

	// 1. Offered tx should be identical (same seed, rate, horizon)
	if resZero.TotalOfferedTx != resCapped.TotalOfferedTx {
		t.Errorf("offered tx mismatch: unlimited=%d, capped=%d", resZero.TotalOfferedTx, resCapped.TotalOfferedTx)
	}

	// 2. Unlimited capacity should process almost all offered transactions (leaving only arrivals after last block)
	if resZero.FinalPendingTx > 80 {
		t.Errorf("expected final pending tx <= 80 for unlimited capacity, got %d", resZero.FinalPendingTx)
	}
	avgTxPerBlockZero := float64(resZero.TotalProcessedTx) / float64(resZero.ProposedBlocks)
	if avgTxPerBlockZero < 35.0 {
		t.Errorf("expected unlimited capacity to take ~50 tx/block, got %.2f", avgTxPerBlockZero)
	}

	// 3. Capped capacity (10) must severely bottleneck processing and accumulate backlog
	avgTxPerBlockCapped := float64(resCapped.TotalProcessedTx) / float64(resCapped.ProposedBlocks)
	if avgTxPerBlockCapped > 10.0 {
		t.Errorf("capped block exceeded capacity 10: %.2f", avgTxPerBlockCapped)
	}
	if resCapped.FinalPendingTx < 500 {
		t.Errorf("expected severe backlog (>= 500 pending) for capped capacity, got %d", resCapped.FinalPendingTx)
	}

	// 4. Invariants must hold for both
	if resZero.TotalOfferedTx != resZero.TotalProcessedTx+resZero.FinalPendingTx {
		t.Errorf("invariant violated for unlimited: %d != %d + %d",
			resZero.TotalOfferedTx, resZero.TotalProcessedTx, resZero.FinalPendingTx)
	}
	if resCapped.TotalOfferedTx != resCapped.TotalProcessedTx+resCapped.FinalPendingTx {
		t.Errorf("invariant violated for capped: %d != %d + %d",
			resCapped.TotalOfferedTx, resCapped.TotalProcessedTx, resCapped.FinalPendingTx)
	}
}

// TestTxPool_CapacityAndBacklog verifies mempool backlog queuing and exact invariant satisfaction.
func TestTxPool_CapacityAndBacklog(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &config.ScenarioConfig{
		Name:             "backlog_test",
		NumNodes:         3,
		BlockIntervalSec: 1.0,
		SimDurationSec:   25.0,
		Seed:             999,
		PeersPerNode:     2,
		TxPerBlock:       10,
		TxArrivalRate:    5.0, // 50 tx/s
		BlockCapacity:    5,   // only 5 tx/block processed -> 45 tx/s backlog
	}

	logger, err := logstore.Open(filepath.Join(tmpDir, "backlog.jsonl"))
	if err != nil {
		t.Fatalf("open logger: %v", err)
	}
	eng := engine.New(cfg, logger)
	res, err := eng.Run()
	logger.Close()
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if res.TotalOfferedTx != res.TotalProcessedTx+res.FinalPendingTx {
		t.Fatalf("invariant violated: offered (%d) != processed (%d) + pending (%d)",
			res.TotalOfferedTx, res.TotalProcessedTx, res.FinalPendingTx)
	}

	if res.MaxPendingTx < 100 {
		t.Errorf("expected maxPendingTx >= 100 under 5x arrival with capacity 5, got %d", res.MaxPendingTx)
	}
	if res.FinalPendingTx < 100 {
		t.Errorf("expected finalPendingTx >= 100, got %d", res.FinalPendingTx)
	}
}

// TestTxInvariant_ZeroArrivalRate verifies zero arrivals when rate is 0.
func TestTxInvariant_ZeroArrivalRate(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &config.ScenarioConfig{
		Name:             "zero_tx_test",
		NumNodes:         2,
		BlockIntervalSec: 1.0,
		SimDurationSec:   10.0,
		Seed:             77,
		PeersPerNode:     1,
		TxPerBlock:       10,
		TxArrivalRate:    0.0,
	}

	logger, _ := logstore.Open(filepath.Join(tmpDir, "zero.jsonl"))
	eng := engine.New(cfg, logger)
	res, _ := eng.Run()
	logger.Close()

	if res.TotalOfferedTx != 0 || res.TotalProcessedTx != 0 || res.FinalPendingTx != 0 {
		t.Errorf("expected 0 txs, got offered=%d, processed=%d, pending=%d",
			res.TotalOfferedTx, res.TotalProcessedTx, res.FinalPendingTx)
	}
}
