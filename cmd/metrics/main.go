// Command metrics runs a simulation and appends one row to summary.csv.
//
// Usage:
//
//	go run ./cmd/metrics --scenario scenarios/baseline.yaml --seed 7
//	go run ./cmd/metrics --scenario scenarios/latency_stress.yaml --seed 1 --csv outputs/summary.csv
//
// Flags:
//
//	--scenario  Path to the YAML scenario file (required)
//	--seed      Override the random seed in the scenario file (0 = use file value)
//	--csv       Path to the summary CSV file (default: outputs/summary.csv)
//	--warmup    Warm-up blocks to skip in metric calculations (overrides scenario file)
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
	"github.com/pranav-k/forkrisk/internal/metrics"
)

func main() {
	scenarioPath := flag.String("scenario", "", "Path to the YAML scenario file (required)")
	seedOverride := flag.Int64("seed", 0, "Override the random seed (0 = use value from scenario file)")
	csvPath := flag.String("csv", "outputs/summary.csv", "Path to the summary CSV file")
	warmupOverride := flag.Int("warmup", -1, "Override warm_up_blocks (-1 = use scenario file value)")
	flag.Parse()

	if *scenarioPath == "" {
		fmt.Fprintln(os.Stderr, "Error: --scenario flag is required")
		fmt.Fprintln(os.Stderr, "Example: go run ./cmd/metrics --scenario scenarios/baseline.yaml --seed 7")
		os.Exit(1)
	}

	// Load and validate the scenario.
	cfg, err := config.LoadFromFile(*scenarioPath)
	if err != nil {
		log.Fatalf("Failed to load scenario: %v", err)
	}
	if *seedOverride != 0 {
		cfg.Seed = *seedOverride
	}
	if *warmupOverride >= 0 {
		cfg.WarmUpBlocks = *warmupOverride
	}

	// Prepare the log file.
	runID := logstore.RunID(cfg.Name, cfg.Seed)
	runDir := filepath.Join(cfg.OutputDir, runID)
	logPath := engine.LogFilePath(cfg.OutputDir, runID)
	logger, err := logstore.Open(logPath)
	if err != nil {
		log.Fatalf("Failed to open log file: %v", err)
	}

	fmt.Printf("╔════════════════════════════════════════════════╗\n")
	fmt.Printf("║  ForkRisk — Simulate + Metrics (Milestone 2)  ║\n")
	fmt.Printf("╚════════════════════════════════════════════════╝\n")
	fmt.Printf("Scenario  : %s\n", cfg.Name)
	fmt.Printf("Seed      : %d\n", cfg.Seed)
	fmt.Printf("Run ID    : %s\n", runID)
	fmt.Printf("Log file  : %s\n", logPath)
	fmt.Printf("CSV out   : %s\n\n", *csvPath)

	// Run the simulation.
	eng := engine.New(cfg, logger)
	result, err := eng.Run()
	logger.Close()
	if err != nil {
		log.Fatalf("Simulation error: %v", err)
	}

	// Capture the initial (pre-run) config recorded by the engine at initialization time.
	// The engine maintains independent copies for activeCfg (which mutates on dynamic events)
	// and initialCfg (which preserves pre-run values for metadata and reporting).
	initialCfg := eng.InitialConfig()

	// Compute log hash and write metadata.
	logHash, _ := hashFile(logPath)
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

	// Print simulation summary.
	fmt.Printf("Simulation complete\n")
	fmt.Printf("  Blocks proposed   : %d\n", result.ProposedBlocks)
	fmt.Printf("  Events processed  : %d\n", result.ProcessedEvents)
	fmt.Printf("  Sim duration      : %.3fs\n", result.SimDurationSec)
	fmt.Printf("  Settled duration  : %.3fs\n", result.SettledDurationSec)
	fmt.Printf("  Divergent tips    : %d\n", result.DivergentTips)
	fmt.Printf("  Errors            : %d\n\n", result.ErrorCount)

	nodeIDs := make([]string, 0, len(result.NodeTipHeights))
	for id := range result.NodeTipHeights {
		nodeIDs = append(nodeIDs, id)
	}
	sort.Strings(nodeIDs)
	fmt.Printf("Node tip heights:\n")
	for _, id := range nodeIDs {
		fmt.Printf("  %-6s → height %d\n", id, result.NodeTipHeights[id])
	}
	fmt.Println()

	// Compute metrics using the initial (pre-run) config parameters so that
	// the CSV columns report starting values, not post-dynamic-event values.
	p := metrics.ComputeParams{
		RunID:           runID,
		Scenario:        initialCfg.Name,
		Seed:            initialCfg.Seed,
		NumNodes:        initialCfg.NumNodes,
		BlockInterval:   initialCfg.BlockIntervalSec,
		MeanLatencyMs:   initialCfg.MeanLatencyMs,
		MeanPropDelayMs: initialCfg.MeanPropDelayMs,
		ChurnEnabled:    initialCfg.ChurnEnabled,
		TxArrivalRate:   initialCfg.TxArrivalRate,
		WarmUpBlocks:    initialCfg.WarmUpBlocks,
		ConfirmDepth:    initialCfg.ConfirmationDepth,
		SimDuration:     result.SimDurationSec,
		SettledDuration: result.SettledDurationSec,
	}

	m, err := metrics.Compute(logPath, p)
	if err != nil {
		log.Fatalf("Failed to compute metrics: %v", err)
	}

	// Print metrics summary.
	fmt.Printf("Run-level metrics\n")
	fmt.Printf("  Total blocks      : %d\n", m.TotalBlocks)
	fmt.Printf("  Fork episodes     : %d\n", m.ForkEpisodes)
	fmt.Printf("  Fork rate         : %.6f\n", m.ForkRate)
	fmt.Printf("  Stale blocks      : %d\n", m.StaleBlocks)
	fmt.Printf("  Stale rate        : %.6f\n", m.StaleRate)
	fmt.Printf("  Reorg count       : %d\n", m.ReorgCount)
	fmt.Printf("  Mean reorg depth  : %.4f\n", m.MeanReorgDepth)
	fmt.Printf("  Max reorg depth   : %d\n", m.MaxReorgDepth)
	fmt.Printf("  Prop time mean    : %.4fs\n", m.PropTimeMeanSec)
	fmt.Printf("  Prop time P95     : %.4fs\n", m.PropTimeP95Sec)
	fmt.Printf("  Conf latency mean : %.4fs\n", m.ConfLatencyMeanSec)
	fmt.Printf("  Raw TPS           : %.4f\n", m.RawTPS)
	fmt.Printf("  Goodput           : %.4f tx/s\n", m.Goodput)
	fmt.Printf("  Node view consist.: %.6f\n", m.NodeViewConsistency)
	fmt.Printf("  Peer availability : %.6f\n", m.PeerAvailability)
	fmt.Println()

	// Append to summary CSV.
	if err := metrics.AppendCSVRow(*csvPath, m); err != nil {
		log.Fatalf("Failed to write CSV: %v", err)
	}
	fmt.Printf("Row appended to: %s\n", *csvPath)
	fmt.Printf("Log SHA-256: %s\n", logHash)
}

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
