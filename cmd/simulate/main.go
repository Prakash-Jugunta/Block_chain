// Command simulate runs a single blockchain network simulation.
//
// Usage:
//
//	go run ./cmd/simulate --scenario scenarios/baseline.yaml --seed 7
//
// Flags:
//
//	--scenario  Path to the YAML scenario file (required)
//	--seed      Override the random seed in the scenario file (optional)
package main

import (
	"crypto/sha256"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"time"

	"github.com/pranav-k/forkrisk/internal/config"
	"github.com/pranav-k/forkrisk/internal/engine"
	"github.com/pranav-k/forkrisk/internal/logstore"
)

func main() {
	// --- Parse CLI flags ---
	scenarioPath := flag.String("scenario", "", "Path to the YAML scenario file (required)")
	seedOverride := flag.Int64("seed", 0, "Override the random seed (0 = use value from scenario file)")
	flag.Parse()

	if *scenarioPath == "" {
		fmt.Fprintln(os.Stderr, "Error: --scenario flag is required")
		fmt.Fprintln(os.Stderr, "Example: go run ./cmd/simulate --scenario scenarios/baseline.yaml --seed 7")
		os.Exit(1)
	}

	// --- Load and validate the scenario ---
	cfg, err := config.LoadFromFile(*scenarioPath)
	if err != nil {
		log.Fatalf("Failed to load scenario: %v", err)
	}
	if *seedOverride != 0 {
		cfg.Seed = *seedOverride
	}

	// --- Prepare the log file ---
	runID := logstore.RunID(cfg.Name, cfg.Seed)
	runDir := filepath.Join(cfg.OutputDir, runID)
	logPath := engine.LogFilePath(cfg.OutputDir, runID)
	logger, err := logstore.Open(logPath)
	if err != nil {
		log.Fatalf("Failed to open log file: %v", err)
	}
	defer logger.Close()

	// --- Print startup banner ---
	fmt.Printf("╔══════════════════════════════════════════════╗\n")
	fmt.Printf("║  ForkRisk Blockchain Simulator — Milestone 1 ║\n")
	fmt.Printf("╚══════════════════════════════════════════════╝\n")
	fmt.Printf("Scenario       : %s\n", cfg.Name)
	fmt.Printf("Seed           : %d\n", cfg.Seed)
	fmt.Printf("Nodes          : %d  (peers/node: %d)\n", cfg.NumNodes, cfg.PeersPerNode)
	fmt.Printf("Proposer rule  : %s\n", cfg.ProposerSelection)
	fmt.Printf("Block interval : %.2fs (target network-wide) ± %.2fs jitter\n", cfg.BlockIntervalSec, cfg.BlockJitterSec)
	fmt.Printf("Latency        : %.0f ± %.0f ms\n", cfg.MeanLatencyMs, cfg.LatencyJitterMs)
	fmt.Printf("Prop delay     : %.0f ± %.0f ms\n", cfg.MeanPropDelayMs, cfg.PropDelayJitterMs)
	fmt.Printf("Settlement grc : %.1fs\n", cfg.SettlementGraceSec)
	if cfg.MaxBlocks > 0 {
		fmt.Printf("Stop after     : %d blocks (proposals stop; propagation settles)\n", cfg.MaxBlocks)
	} else {
		fmt.Printf("Stop after     : %.1f simulated seconds (proposals stop; propagation settles)\n", cfg.SimDurationSec)
	}
	fmt.Printf("Log file       : %s\n\n", logPath)

	// --- Run the simulation ---
	eng := engine.New(cfg, logger)
	result, err := eng.Run()
	if err != nil {
		log.Fatalf("Simulation error: %v", err)
	}
	logger.Close()

	// Capture the initial (pre-run) config for metadata — dynamic events must not
	// corrupt the recorded starting parameters.
	initialCfg := eng.InitialConfig()

	// Compute SHA-256 hash of the event log for reproducibility verification.
	logHash, err := hashFile(logPath)
	if err != nil {
		log.Printf("Warning: failed to compute log SHA-256: %v", err)
		logHash = "error"
	}

	// --- Save Run Metadata (provenance & reproducibility) ---
	meta := logstore.RunMetadata{
		RunID:              runID,
		ScenarioName:       cfg.Name,
		Seed:               cfg.Seed,
		TimestampUTC:       time.Now().UTC().Format(time.RFC3339),
		GoVersion:          runtime.Version(),
		Platform:           fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH),
		GitCommit:          "unversioned",
		Config:             initialCfg,
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
	if metaErr := logstore.WriteMetadata(runDir, meta); metaErr != nil {
		log.Printf("Warning: failed to write metadata.json: %v", metaErr)
	}

	// --- Print results ---
	fmt.Printf("══════════════ Simulation Complete ══════════════\n")
	fmt.Printf("Nodes               : %d\n", result.NumNodes)
	fmt.Printf("Blocks proposed     : %d\n", result.ProposedBlocks)
	fmt.Printf("Events processed    : %d\n", result.ProcessedEvents)
	fmt.Printf("Proposal sim time   : %.3f seconds\n", result.SimDurationSec)
	fmt.Printf("Settled sim time    : %.3f seconds\n", result.SettledDurationSec)
	fmt.Printf("Simulation errors   : %d\n", result.ErrorCount)
	if result.ErrorCount > 0 {
		for i, errStr := range result.Errors {
			if i < 5 {
				fmt.Printf("  - error: %s\n", errStr)
			}
		}
	}

	// Print tip heights in node order.
	fmt.Printf("\nNode tip heights:\n")
	nodeIDs := make([]string, 0, len(result.NodeTipHeights))
	for id := range result.NodeTipHeights {
		nodeIDs = append(nodeIDs, id)
	}
	sort.Strings(nodeIDs)
	for _, id := range nodeIDs {
		fmt.Printf("  %-6s → height %d\n", id, result.NodeTipHeights[id])
	}

	fmt.Printf("\nDivergent tips      : %d node(s) not on the majority tip\n", result.DivergentTips)
	fmt.Printf("Log SHA-256         : %s\n", logHash)
	fmt.Printf("Metadata saved      : %s\n", filepath.Join(runDir, "metadata.json"))
	fmt.Printf("═════════════════════════════════════════════════\n")
}

// hashFile computes the SHA-256 of a file's contents and returns it as hex.
// Used to verify that two runs with the same seed produce identical logs.
func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}
