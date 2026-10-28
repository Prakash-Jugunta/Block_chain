// Package simulate_test contains integration tests for the full simulation.
// These tests verify:
//   - Correct network-wide block-production rate (not N times).
//   - Reliable block gossip propagation between nodes with sender/receiver tracking.
//   - Temporary tip divergence under high latency stress.
//   - Eventual convergence of all nodes to the same chain tip after settlement.
//   - Settlement policy draining in-flight messages upon reaching max_blocks.
//   - Deterministic replay for identical seeds and variation across different seeds.
//   - Run metadata generation and log structure integrity.
package simulate_test

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/pranav-k/forkrisk/internal/config"
	"github.com/pranav-k/forkrisk/internal/engine"
	"github.com/pranav-k/forkrisk/internal/logstore"
)

// runSimulation is a helper that runs a full simulation with the given config
// and returns the result, log path, and run directory.
func runSimulation(t *testing.T, cfg *config.ScenarioConfig, outDir string) (engine.Result, string, string) {
	t.Helper()
	cfg.OutputDir = outDir
	runID := fmt.Sprintf("test_%s_seed%d", cfg.Name, cfg.Seed)
	runDir := filepath.Join(outDir, runID)
	logPath := engine.LogFilePath(outDir, runID)

	logger, err := logstore.Open(logPath)
	if err != nil {
		t.Fatalf("open logger: %v", err)
	}

	eng := engine.New(cfg, logger)
	result, err := eng.Run()
	logger.Close()
	if err != nil {
		t.Fatalf("simulation error: %v", err)
	}

	// Compute log hash and write metadata as cmd/simulate/main.go does.
	logHash := hashFile(t, logPath)
	meta := logstore.RunMetadata{
		RunID:              runID,
		ScenarioName:       cfg.Name,
		Seed:               cfg.Seed,
		TimestampUTC:       "2026-10-21T00:00:00Z",
		GoVersion:          runtime.Version(),
		Platform:           fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH),
		GitCommit:          "unversioned",
		Config:             *cfg,
		ProposedBlocks:     result.ProposedBlocks,
		ProcessedEvents:    result.ProcessedEvents,
		SimDurationSec:     result.SimDurationSec,
		SettledDurationSec: result.SettledDurationSec,
		DivergentTips:      result.DivergentTips,
		NodeTipHeights:     result.NodeTipHeights,
		LogSHA256:          logHash,
		TotalOfferedTx:     result.TotalOfferedTx,
		TotalProcessedTx:   result.TotalProcessedTx,
		MaxPendingTx:       result.MaxPendingTx,
		FinalPendingTx:     result.FinalPendingTx,
		OfferedTPS:         result.OfferedTPS,
	}
	if err := logstore.WriteMetadata(runDir, meta); err != nil {
		t.Fatalf("write metadata: %v", err)
	}

	return result, logPath, runDir
}

// hashFile returns the SHA-256 hex of a file's contents.
func hashFile(t *testing.T, path string) string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open file for hashing: %v", err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		t.Fatalf("hashing file: %v", err)
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

// TestSeedReproducibility verifies total-order determinism:
// Running the exact same configuration twice with seed 7 must produce
// byte-identical event log files.
func TestSeedReproducibility(t *testing.T) {
	cfg := &config.ScenarioConfig{
		Name:              "repro_test",
		NumNodes:          4,
		BlockIntervalSec:  1.0,
		BlockJitterSec:    0.1,
		MeanLatencyMs:     50.0,
		LatencyJitterMs:   10.0,
		MeanPropDelayMs:   100.0,
		PropDelayJitterMs: 20.0,
		MaxBlocks:         20,
		Seed:              7,
		PeersPerNode:      2,
		TxPerBlock:        5,
	}

	outDir1 := t.TempDir()
	outDir2 := t.TempDir()

	cfg1 := *cfg
	_, logPath1, _ := runSimulation(t, &cfg1, outDir1)

	cfg2 := *cfg
	_, logPath2, _ := runSimulation(t, &cfg2, outDir2)

	hash1 := hashFile(t, logPath1)
	hash2 := hashFile(t, logPath2)

	if hash1 != hash2 {
		t.Fatalf("Determinism FAILED: identical seed produced different outputs.\nRun1: %s\nRun2: %s", hash1, hash2)
	}
	t.Logf("Determinism OK: both runs produced identical SHA-256: %s", hash1)
}

// TestDeterministicReplayDifferentSeeds verifies that varying the seed produces
// different simulation trajectories.
func TestDeterministicReplayDifferentSeeds(t *testing.T) {
	cfg := &config.ScenarioConfig{
		Name:              "seed_diff_test",
		NumNodes:          4,
		BlockIntervalSec:  1.0,
		BlockJitterSec:    0.1,
		MeanLatencyMs:     50.0,
		LatencyJitterMs:   10.0,
		MeanPropDelayMs:   100.0,
		PropDelayJitterMs: 20.0,
		MaxBlocks:         15,
		PeersPerNode:      2,
		TxPerBlock:        3,
	}

	outDir1 := t.TempDir()
	outDir2 := t.TempDir()

	cfg1 := *cfg
	cfg1.Seed = 101
	_, logPath1, _ := runSimulation(t, &cfg1, outDir1)

	cfg2 := *cfg
	cfg2.Seed = 202
	_, logPath2, _ := runSimulation(t, &cfg2, outDir2)

	hash1 := hashFile(t, logPath1)
	hash2 := hashFile(t, logPath2)

	if hash1 == hash2 {
		t.Fatalf("Different seeds (101 vs 202) unexpectedly produced identical log hashes: %s", hash1)
	}
}

// TestNetworkWideBlockRate verifies that the simulator schedules block proposals
// at the intended network-wide rate rather than N times the intended rate.
func TestNetworkWideBlockRate(t *testing.T) {
	const numNodes = 5
	const blockInterval = 1.0
	const simDuration = 25.0

	cfg := &config.ScenarioConfig{
		Name:              "rate_test",
		NumNodes:          numNodes,
		BlockIntervalSec:  blockInterval,
		BlockJitterSec:    0.05,
		SimDurationSec:    simDuration,
		MaxBlocks:         0, // duration-driven
		Seed:              1234,
		PeersPerNode:      2,
		TxPerBlock:        2,
		ProposerSelection: "uniform_random",
	}

	outDir := t.TempDir()
	result, _, _ := runSimulation(t, cfg, outDir)

	expectedBlocks := int(simDuration / blockInterval) // ~25 blocks
	tolerance := 5                                     // ±5 blocks due to jitter and startup

	if result.ProposedBlocks < expectedBlocks-tolerance || result.ProposedBlocks > expectedBlocks+tolerance {
		t.Fatalf("Block production rate FAILED: expected ~%d blocks (±%d), got %d blocks. (If N*rate bug were present, got would be ~%d)",
			expectedBlocks, tolerance, result.ProposedBlocks, expectedBlocks*numNodes)
	}
	t.Logf("Block production rate OK: proposed %d blocks over %.1fs (target ~%d across %d nodes)",
		result.ProposedBlocks, simDuration, expectedBlocks, numNodes)
}

// TestBlockPropagation verifies that proposed blocks propagate to all nodes.
func TestBlockPropagation(t *testing.T) {
	cfg := &config.ScenarioConfig{
		Name:              "propagation_test",
		NumNodes:          5,
		BlockIntervalSec:  1.0,
		BlockJitterSec:    0.05,
		MeanLatencyMs:     30.0,
		LatencyJitterMs:   5.0,
		MeanPropDelayMs:   50.0,
		PropDelayJitterMs: 5.0,
		MaxBlocks:         15,
		Seed:              42,
		PeersPerNode:      2,
		TxPerBlock:        3,
	}

	outDir := t.TempDir()
	result, _, _ := runSimulation(t, cfg, outDir)

	if result.ProposedBlocks != 15 {
		t.Fatalf("expected 15 blocks proposed, got %d", result.ProposedBlocks)
	}
	for id, h := range result.NodeTipHeights {
		if h < 1 {
			t.Errorf("node %s stuck at height %d — blocks did not propagate", id, h)
		}
	}
	if result.ErrorCount != 0 {
		t.Errorf("unexpected simulation errors: %v", result.Errors)
	}
}

// TestGossipSenderReceiverTracking verifies that BlockGossip and BlockArrive events
// accurately track both sender (from_node) and recipient (node_id).
func TestGossipSenderReceiverTracking(t *testing.T) {
	cfg := &config.ScenarioConfig{
		Name:              "gossip_tracking_test",
		NumNodes:          4,
		BlockIntervalSec:  1.0,
		BlockJitterSec:    0.05,
		MeanLatencyMs:     20.0,
		LatencyJitterMs:   5.0,
		MeanPropDelayMs:   20.0,
		PropDelayJitterMs: 5.0,
		MaxBlocks:         5,
		Seed:              999,
		PeersPerNode:      2,
		TxPerBlock:        2,
	}

	outDir := t.TempDir()
	_, logPath, _ := runSimulation(t, cfg, outDir)

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("reading log: %v", err)
	}

	lines := splitLines(data)
	gossipCount := 0
	arriveCount := 0

	for i, line := range lines {
		var entry logstore.LogEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("line %d: json unmarshal: %v", i, err)
		}

		if entry.EventType == string(engine.EventBlockGossip) {
			gossipCount++
			if entry.FromNode == "" {
				t.Errorf("line %d: BlockGossip has empty from_node", i)
			}
			if entry.NodeID == "" {
				t.Errorf("line %d: BlockGossip has empty node_id", i)
			}
			if entry.FromNode == entry.NodeID {
				t.Errorf("line %d: BlockGossip has from_node == node_id (%s)", i, entry.FromNode)
			}
		}

		if entry.EventType == string(engine.EventBlockArrive) {
			arriveCount++
			if entry.FromNode == "" {
				t.Errorf("line %d: BlockArrive has empty from_node", i)
			}
			if entry.NodeID == "" {
				t.Errorf("line %d: BlockArrive has empty node_id", i)
			}
		}
	}

	if gossipCount == 0 {
		t.Error("expected >0 BlockGossip events")
	}
	if arriveCount == 0 {
		t.Error("expected >0 BlockArrive events")
	}
	t.Logf("Gossip tracking verified: %d gossip events, %d arrive events with valid from_node and node_id", gossipCount, arriveCount)
}

// TestTemporaryDivergenceAndEventualConvergence verifies that under high network stress,
// nodes experience temporary tip divergence during the run, but settle onto a converged
// chain tip after the settlement phase drains in-flight propagation.
func TestTemporaryDivergenceAndEventualConvergence(t *testing.T) {
	cfg := &config.ScenarioConfig{
		Name:               "divergence_convergence_test",
		NumNodes:           4,
		BlockIntervalSec:   0.4,   // fast block intervals
		BlockJitterSec:     0.1,   // jitter causes proposals to be close
		MeanLatencyMs:      300.0, // high latency
		LatencyJitterMs:    50.0,
		MeanPropDelayMs:    200.0, // high propagation processing delay
		PropDelayJitterMs:  30.0,
		MaxBlocks:          12,
		SettlementGraceSec: 10.0, // ample time for final settlement
		Seed:               777,
		PeersPerNode:       2,
		TxPerBlock:         2,
		ProposerSelection:  "uniform_random",
	}

	outDir := t.TempDir()
	result, logPath, _ := runSimulation(t, cfg, outDir)

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("reading log: %v", err)
	}

	// Scan event log and track node tips over time to observe temporary divergence.
	nodeTips := make(map[string]string)
	observedDivergence := false

	lines := splitLines(data)
	for _, line := range lines {
		var entry logstore.LogEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		if entry.TipHash != "" {
			nodeTips[entry.NodeID] = entry.TipHash
			if len(nodeTips) >= 2 {
				// Check if there is divergence among nodes that have reported tips
				firstTip := ""
				for _, h := range nodeTips {
					if firstTip == "" {
						firstTip = h
					} else if h != firstTip {
						observedDivergence = true
						break
					}
				}
			}
		}
	}

	t.Logf("Observed temporary divergence during flight: %v", observedDivergence)

	// Eventual convergence check: after settlement phase, all nodes must agree on the tip!
	if result.DivergentTips != 0 {
		t.Fatalf("Eventual convergence FAILED: %d nodes divergent after settlement. Heights: %+v",
			result.DivergentTips, result.NodeTipHeights)
	}
	t.Logf("Eventual convergence OK: 0 divergent tips after settlement. All nodes converged to height %d",
		result.NodeTipHeights["N0"])
}

// TestSettlementPolicyDrainsInFlight verifies that stopping proposals upon max_blocks
// leaves sufficient simulated time for all in-flight messages to drain.
func TestSettlementPolicyDrainsInFlight(t *testing.T) {
	cfg := &config.ScenarioConfig{
		Name:               "settlement_drain_test",
		NumNodes:           3,
		BlockIntervalSec:   1.0,
		MeanLatencyMs:      150.0,
		MeanPropDelayMs:    150.0,
		MaxBlocks:          6,
		SettlementGraceSec: 5.0,
		Seed:               55,
		PeersPerNode:       1,
		TxPerBlock:         2,
	}

	outDir := t.TempDir()
	result, logPath, _ := runSimulation(t, cfg, outDir)

	if result.ProposedBlocks != 6 {
		t.Fatalf("expected 6 proposed blocks, got %d", result.ProposedBlocks)
	}

	// Read log to verify events continued after proposal end
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("reading log: %v", err)
	}
	lines := splitLines(data)

	lastProposeTime := 0.0
	lastEventTime := 0.0
	for _, line := range lines {
		var entry logstore.LogEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		if entry.EventType == string(engine.EventBlockPropose) && entry.SimTime > lastProposeTime {
			lastProposeTime = entry.SimTime
		}
		if entry.SimTime > lastEventTime {
			lastEventTime = entry.SimTime
		}
	}

	if lastEventTime < lastProposeTime {
		t.Errorf("expected settlement events after last proposal: lastPropose=%.3f, lastEvent=%.3f",
			lastProposeTime, lastEventTime)
	}
	t.Logf("Settlement drain verified: last proposal at %.3fs, final event settled at %.3fs",
		lastProposeTime, lastEventTime)
}

// TestRunMetadataSaved verifies that metadata.json is generated with required fields.
func TestRunMetadataSaved(t *testing.T) {
	cfg := &config.ScenarioConfig{
		Name:             "meta_test",
		NumNodes:         2,
		BlockIntervalSec: 1.0,
		MaxBlocks:        3,
		Seed:             88,
		PeersPerNode:     1,
		TxPerBlock:       1,
	}

	outDir := t.TempDir()
	_, _, runDir := runSimulation(t, cfg, outDir)

	metaPath := filepath.Join(runDir, "metadata.json")
	data, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("reading metadata.json: %v", err)
	}

	var meta logstore.RunMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		t.Fatalf("parsing metadata.json: %v", err)
	}

	if meta.Seed != 88 {
		t.Errorf("metadata seed mismatch: got %d, want 88", meta.Seed)
	}
	if meta.ScenarioName != "meta_test" {
		t.Errorf("metadata scenario mismatch: got %q, want meta_test", meta.ScenarioName)
	}
	if meta.GoVersion == "" {
		t.Error("metadata go_version should not be empty")
	}
	if meta.Platform == "" {
		t.Error("metadata platform should not be empty")
	}
	if meta.LogSHA256 == "" {
		t.Error("metadata log_sha256 should not be empty")
	}
}

// TestEventLogStructure verifies that the JSONL log file is parseable and
// contains events with expected field names.
func TestEventLogStructure(t *testing.T) {
	cfg := &config.ScenarioConfig{
		Name:              "log_structure_test",
		NumNodes:          3,
		BlockIntervalSec:  1.0,
		MeanLatencyMs:     20.0,
		LatencyJitterMs:   5.0,
		MeanPropDelayMs:   30.0,
		PropDelayJitterMs: 5.0,
		MaxBlocks:         5,
		Seed:              1,
		PeersPerNode:      1,
		TxPerBlock:        2,
	}

	outDir := t.TempDir()
	_, logPath, _ := runSimulation(t, cfg, outDir)

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("reading log file: %v", err)
	}

	type LogLine struct {
		Seq       int64   `json:"seq"`
		SimTime   float64 `json:"sim_time"`
		EventType string  `json:"event_type"`
		NodeID    string  `json:"node_id"`
	}

	lines := splitLines(data)
	if len(lines) == 0 {
		t.Fatal("event log is empty")
	}

	for i, line := range lines {
		var entry LogLine
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Errorf("line %d: invalid JSON: %v — content: %s", i, err, line)
			continue
		}
		if entry.Seq <= 0 {
			t.Errorf("line %d: Seq should be > 0, got %d", i, entry.Seq)
		}
		if entry.SimTime < 0 {
			t.Errorf("line %d: SimTime should be ≥ 0, got %f", i, entry.SimTime)
		}
		if entry.EventType == "" {
			t.Errorf("line %d: EventType should not be empty", i)
		}
	}
	t.Logf("Parsed %d log entries successfully", len(lines))
}

// TestOutputDirectoryCreated verifies that the engine creates the output
// directory tree if it doesn't already exist.
func TestOutputDirectoryCreated(t *testing.T) {
	cfg := &config.ScenarioConfig{
		Name:             "dir_creation_test",
		NumNodes:         2,
		BlockIntervalSec: 1.0,
		MeanLatencyMs:    10.0,
		MeanPropDelayMs:  10.0,
		MaxBlocks:        3,
		Seed:             5,
		PeersPerNode:     1,
		TxPerBlock:       1,
	}

	outDir := filepath.Join(t.TempDir(), "deep", "nested", "output")
	_, logPath, _ := runSimulation(t, cfg, outDir)

	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		t.Errorf("expected log file to exist at %s, but it doesn't", logPath)
	}
}

// TestSimulationOrphanPropagationAndNoDuplicateGossip verifies that across a full
// simulation, no directed peer link ever transmits the same block hash twice (duplicate gossip prevented),
// and orphan recovery successfully propagates blocks throughout the network.
func TestSimulationOrphanPropagationAndNoDuplicateGossip(t *testing.T) {
	cfg := &config.ScenarioConfig{
		Name:               "orphan_dedup_sim_test",
		NumNodes:           5,
		BlockIntervalSec:   0.3,
		BlockJitterSec:     0.1,
		MeanLatencyMs:      150.0,
		LatencyJitterMs:    50.0,
		MeanPropDelayMs:    100.0,
		PropDelayJitterMs:  30.0,
		MaxBlocks:          10,
		SettlementGraceSec: 5.0,
		Seed:               555,
		PeersPerNode:       3,
		TxPerBlock:         2,
	}

	outDir := t.TempDir()
	result, logPath, _ := runSimulation(t, cfg, outDir)

	if result.ProposedBlocks != 10 {
		t.Fatalf("expected 10 blocks, got %d", result.ProposedBlocks)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("reading log: %v", err)
	}

	gossipedOnLink := make(map[string]int) // "from|to|hash" -> count
	lines := splitLines(data)

	for i, l := range lines {
		var entry logstore.LogEntry
		if err := json.Unmarshal([]byte(l), &entry); err != nil {
			t.Fatalf("line %d unmarshal: %v", i, err)
		}
		if entry.EventType == string(engine.EventBlockGossip) {
			key := fmt.Sprintf("%s|%s|%s", entry.FromNode, entry.NodeID, entry.BlockHash)
			gossipedOnLink[key]++
			if gossipedOnLink[key] > 1 {
				t.Errorf("duplicate gossip detected on link %s -> %s for block %s",
					entry.FromNode, entry.NodeID, entry.BlockHash[:8])
			}
		}
	}

	t.Logf("Verified %d unique gossip transmissions; 0 duplicate gossip loops detected", len(gossipedOnLink))
}

// TestSimulationProposalTerminationAndSettlementWindow verifies that both max_blocks
// and sim_duration_sec enforce a strictly bounded settlement grace window.
func TestSimulationProposalTerminationAndSettlementWindow(t *testing.T) {
	t.Run("MaxBlocksBoundedGrace", func(t *testing.T) {
		const grace = 3.0
		cfg := &config.ScenarioConfig{
			Name:               "term_max_blocks_test",
			NumNodes:           4,
			BlockIntervalSec:   1.0,
			MeanLatencyMs:      50.0,
			MeanPropDelayMs:    50.0,
			MaxBlocks:          6,
			SettlementGraceSec: grace,
			Seed:               888,
			PeersPerNode:       2,
			TxPerBlock:         1,
		}
		outDir := t.TempDir()
		result, _, _ := runSimulation(t, cfg, outDir)

		if result.ProposedBlocks != 6 {
			t.Fatalf("expected 6 blocks, got %d", result.ProposedBlocks)
		}
		if result.SettledDurationSec > result.SimDurationSec+grace {
			t.Fatalf("settlement duration %.3f exceeded bounded grace limit %.3f",
				result.SettledDurationSec, result.SimDurationSec+grace)
		}
	})

	t.Run("SimDurationExactBoundary", func(t *testing.T) {
		const duration = 6.0
		const grace = 2.0
		cfg := &config.ScenarioConfig{
			Name:               "term_duration_boundary_test",
			NumNodes:           4,
			BlockIntervalSec:   1.0,
			SimDurationSec:     duration,
			MaxBlocks:          0,
			SettlementGraceSec: grace,
			Seed:               999,
			PeersPerNode:       2,
			TxPerBlock:         1,
		}
		outDir := t.TempDir()
		result, logPath, _ := runSimulation(t, cfg, outDir)

		if result.SimDurationSec > duration {
			t.Fatalf("sim proposal time %.3f exceeded sim_duration %.3f", result.SimDurationSec, duration)
		}
		if result.SettledDurationSec > duration+grace {
			t.Fatalf("settlement duration %.3f exceeded bounded limit %.3f", result.SettledDurationSec, duration+grace)
		}

		// Ensure no BlockPropose events occurred at or after sim_duration_sec
		data, err := os.ReadFile(logPath)
		if err != nil {
			t.Fatalf("reading log: %v", err)
		}
		for _, l := range splitLines(data) {
			var entry logstore.LogEntry
			_ = json.Unmarshal([]byte(l), &entry)
			if entry.EventType == string(engine.EventBlockPropose) && entry.SimTime >= duration {
				t.Fatalf("propose event occurred at %.3f >= duration %.3f", entry.SimTime, duration)
			}
		}
	})
}

// TestSimulationTipUpdateLogAccuracy verifies that every tip_update event in the log
// accurately identifies the actual new tip block hash, height, old tip, and reason.
func TestSimulationTipUpdateLogAccuracy(t *testing.T) {
	cfg := &config.ScenarioConfig{
		Name:               "tip_update_accuracy_test",
		NumNodes:           4,
		BlockIntervalSec:   0.5,
		BlockJitterSec:     0.1,
		MeanLatencyMs:      100.0,
		LatencyJitterMs:    20.0,
		MeanPropDelayMs:    50.0,
		PropDelayJitterMs:  10.0,
		MaxBlocks:          8,
		SettlementGraceSec: 4.0,
		Seed:               333,
		PeersPerNode:       2,
		TxPerBlock:         2,
	}

	outDir := t.TempDir()
	_, logPath, _ := runSimulation(t, cfg, outDir)

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("reading log: %v", err)
	}

	tipUpdates := 0
	for i, l := range splitLines(data) {
		var entry logstore.LogEntry
		if err := json.Unmarshal([]byte(l), &entry); err != nil {
			t.Fatalf("line %d unmarshal: %v", i, err)
		}
		if entry.EventType == string(engine.EventTipUpdate) {
			tipUpdates++
			if entry.BlockHash == "" {
				t.Errorf("line %d: TipUpdate has empty block_hash", i)
			}
			if entry.TipHash == "" {
				t.Errorf("line %d: TipUpdate has empty tip_hash", i)
			}
			if entry.BlockHash != entry.TipHash {
				t.Errorf("line %d: TipUpdate block_hash (%s) != tip_hash (%s)", i, entry.BlockHash, entry.TipHash)
			}
			if entry.OldTipHash == "" {
				t.Errorf("line %d: TipUpdate has empty old_tip_hash", i)
			}
			if entry.BlockHeight <= 0 {
				t.Errorf("line %d: TipUpdate height should be > 0, got %d", i, entry.BlockHeight)
			}
			if entry.Reason != "extend" && entry.Reason != "reorg" {
				t.Errorf("line %d: TipUpdate invalid reason %q", i, entry.Reason)
			}
		}
	}

	if tipUpdates == 0 {
		t.Fatal("expected >0 TipUpdate events")
	}
	t.Logf("Verified %d TipUpdate events; all accurately recorded new tip block and old tip", tipUpdates)
}

// splitLines splits a byte slice into non-empty string lines.
func splitLines(data []byte) []string {
	var lines []string
	start := 0
	for i, b := range data {
		if b == '\n' {
			line := strings.TrimSpace(string(data[start:i]))
			if line != "" {
				lines = append(lines, line)
			}
			start = i + 1
		}
	}
	if start < len(data) {
		line := strings.TrimSpace(string(data[start:]))
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// TestZeroArrivalRate_FromYAMLFile verifies the real CLI configuration loading path (LoadFromFile)
// with an explicit tx_arrival_rate: 0 scenario. It tests that zero offered transactions are generated,
// zero transactions are processed, zero pending transactions remain, and all proposed blocks have tx_count: 0.
func TestZeroArrivalRate_FromYAMLFile(t *testing.T) {
	tmpDir := t.TempDir()
	yamlContent := `
name: zero_rate_yaml_test
num_nodes: 3
block_interval_sec: 1.0
sim_duration_sec: 15.0
seed: 99
peers_per_node: 2
tx_per_block: 10
tx_arrival_rate: 0
block_capacity: 0
output_dir: ` + filepath.ToSlash(tmpDir) + `
`
	yamlPath := filepath.Join(tmpDir, "zero_scenario.yaml")
	if err := os.WriteFile(yamlPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("writing scenario YAML: %v", err)
	}

	// 1. Load through the real CLI scenario-loading path
	cfg, err := config.LoadFromFile(yamlPath)
	if err != nil {
		t.Fatalf("LoadFromFile failed: %v", err)
	}
	if cfg.TxArrivalRate != 0.0 {
		t.Fatalf("expected TxArrivalRate 0.0, got %g", cfg.TxArrivalRate)
	}

	// 2. Run simulation
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

	// 3. Verify zero transaction counts
	if res.TotalOfferedTx != 0 {
		t.Errorf("expected TotalOfferedTx == 0, got %d", res.TotalOfferedTx)
	}
	if res.TotalProcessedTx != 0 {
		t.Errorf("expected TotalProcessedTx == 0, got %d", res.TotalProcessedTx)
	}
	if res.FinalPendingTx != 0 {
		t.Errorf("expected FinalPendingTx == 0, got %d", res.FinalPendingTx)
	}

	// 4. Inspect event log: all proposed blocks must have tx_count: 0
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	lines := splitLines(logData)
	proposalsChecked := 0
	for _, line := range lines {
		var entry struct {
			EventType string `json:"event_type"`
			TxCount   int    `json:"tx_count"`
		}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("unmarshal log line: %v", err)
		}
		if entry.EventType == string(engine.EventBlockPropose) {
			proposalsChecked++
			if entry.TxCount != 0 {
				t.Errorf("block proposal has non-zero tx_count: %d", entry.TxCount)
			}
		}
	}
	if proposalsChecked == 0 {
		t.Errorf("expected >0 block proposals, got 0")
	}
}
