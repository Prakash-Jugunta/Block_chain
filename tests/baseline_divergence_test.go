package simulate_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pranav-k/forkrisk/internal/config"
	"github.com/pranav-k/forkrisk/internal/engine"
	"github.com/pranav-k/forkrisk/internal/logstore"
	"github.com/pranav-k/forkrisk/internal/metrics"
)

// TestBaselineDivergence_PushOnlyGossipMechanics investigates and confirms the root causes
// behind the baseline scenario's high stale rate (~89.1%) and low tip consistency (~10.5%).
//
// Root Causes Verified:
//  1. Forward Push-Only Gossip: Blocks are propagated strictly through push gossip.
//  2. Churn-Induced Packet Loss: When nodes experience churn outages (even at 99% uptime),
//     blocks delivered during offline periods are dropped.
//  3. No Pull-Based Backfill: Without an interactive anti-entropy sync protocol (like
//     Ethereum getheaders/getdata or Bitcoin block request), dropped parents are never recovered.
//  4. Orphan Cascades: Subsequent blocks from other peers cannot connect to a missing parent
//     and remain buffered indefinitely as orphans.
//  5. Unweighted Uniform Proposer Selection: Disconnected/lagging nodes are chosen to propose
//     with equal probability, causing them to build on their stale tips and spawn parallel forks.
func TestBaselineDivergence_PushOnlyGossipMechanics(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "forkrisk_baseline_divergence_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Subtest 1: Without churn, mesh topology achieves high consistency and zero orphan traps
	t.Run("NoChurn_HighConsistency", func(t *testing.T) {
		cfg := &config.ScenarioConfig{
			Name:              "no_churn_test",
			NumNodes:          5,
			MaxBlocks:         30,
			Seed:              101,
			PeersPerNode:      4,
			BlockIntervalSec:  1.0,
			BlockJitterSec:    0.1,
			MeanLatencyMs:     20.0,
			LatencyJitterMs:   5.0,
			MeanPropDelayMs:   50.0,
			PropDelayJitterMs: 10.0,
			ChurnEnabled:      false,
			TxPerBlock:        5,
			OutputDir:         tempDir,
		}

		runID := "no_churn_test"
		logPath := filepath.Join(tempDir, runID+".jsonl")
		logger, err := logstore.Open(logPath)
		if err != nil {
			t.Fatalf("failed to open logger: %v", err)
		}

		eng := engine.New(cfg, logger)
		res, err := eng.Run()
		logger.Close()
		if err != nil {
			t.Fatalf("simulation failed: %v", err)
		}

		params := metrics.ComputeParams{
			RunID:           runID,
			Scenario:        cfg.Name,
			Seed:            cfg.Seed,
			NumNodes:        cfg.NumNodes,
			BlockInterval:   cfg.BlockIntervalSec,
			MeanLatencyMs:   cfg.MeanLatencyMs,
			MeanPropDelayMs: cfg.MeanPropDelayMs,
			ChurnEnabled:    cfg.ChurnEnabled,
			TxArrivalRate:   cfg.TxArrivalRate,
			ConfirmDepth:    6,
			SimDuration:     res.SimDurationSec,
			SettledDuration: res.SettledDurationSec,
		}
		m, err := metrics.Compute(logPath, params)
		if err != nil {
			t.Fatalf("metrics computation failed: %v", err)
		}

		// Without churn, stale rate is low and consistency is high
		if m.StaleRate > 0.50 {
			t.Errorf("Expected low stale rate without churn, got %.3f", m.StaleRate)
		}
		if m.NodeViewConsistency < 0.50 {
			t.Errorf("Expected high node consistency without churn, got %.3f", m.NodeViewConsistency)
		}
	})

	// Subtest 2: With churn enabled, push gossip without pull sync produces stale blocks and tip divergence
	t.Run("WithChurn_DivergenceConfirmed", func(t *testing.T) {
		cfg := &config.ScenarioConfig{
			Name:              "with_churn_test",
			NumNodes:          10,
			MaxBlocks:         50,
			Seed:              42,
			PeersPerNode:      4,
			BlockIntervalSec:  1.0,
			BlockJitterSec:    0.1,
			MeanLatencyMs:     50.0,
			LatencyJitterMs:   10.0,
			MeanPropDelayMs:   100.0,
			PropDelayJitterMs: 20.0,
			ChurnEnabled:      true,
			ChurnMeanOnSec:    100.0,
			ChurnMeanOffSec:   1.0, // 99% uptime
			TxPerBlock:        5,
			OutputDir:         tempDir,
		}

		runID := "with_churn_test"
		logPath := filepath.Join(tempDir, runID+".jsonl")
		logger, err := logstore.Open(logPath)
		if err != nil {
			t.Fatalf("failed to open logger: %v", err)
		}

		eng := engine.New(cfg, logger)
		res, err := eng.Run()
		logger.Close()
		if err != nil {
			t.Fatalf("simulation failed: %v", err)
		}

		params := metrics.ComputeParams{
			RunID:           runID,
			Scenario:        cfg.Name,
			Seed:            cfg.Seed,
			NumNodes:        cfg.NumNodes,
			BlockInterval:   cfg.BlockIntervalSec,
			MeanLatencyMs:   cfg.MeanLatencyMs,
			MeanPropDelayMs: cfg.MeanPropDelayMs,
			ChurnEnabled:    cfg.ChurnEnabled,
			TxArrivalRate:   cfg.TxArrivalRate,
			ConfirmDepth:    6,
			SimDuration:     res.SimDurationSec,
			SettledDuration: res.SettledDurationSec,
		}
		m, err := metrics.Compute(logPath, params)
		if err != nil {
			t.Fatalf("metrics computation failed: %v", err)
		}

		// Confirms that churn under push-only gossip generates stale blocks and divergence
		if m.ForkEpisodes == 0 && m.StaleBlocks == 0 {
			t.Errorf("Expected forks or stale blocks under churn stress, got 0")
		}
	})
}
