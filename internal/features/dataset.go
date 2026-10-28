package features

import (
	"bufio"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/pranav-k/forkrisk/internal/config"
	"github.com/pranav-k/forkrisk/internal/fork"
	"github.com/pranav-k/forkrisk/internal/logstore"
)

// RunExclusion records why a candidate run was excluded from dataset generation.
type RunExclusion struct {
	RunDir string `json:"run_dir"`
	Reason string `json:"reason"`
}

// DatasetMetadata records complete provenance, feature schema, and class balance.
type DatasetMetadata struct {
	GeneratedAtUTC   string         `json:"generated_at_utc"`
	DatasetVersion   string         `json:"dataset_version"`
	FeatureSchemaVer string         `json:"feature_schema_version"`
	ForecastHorizon  int            `json:"forecast_horizon"`
	WarmUpBlocks     int            `json:"warmup_blocks"`
	TotalRows        int            `json:"total_rows"`
	PositiveCount    int            `json:"positive_count"`
	NegativeCount    int            `json:"negative_count"`
	PositiveRate     float64        `json:"positive_rate"`
	IncludedRuns     []string       `json:"included_runs"`
	IncludedRunCount int            `json:"included_run_count"`
	ScenarioCounts   map[string]int `json:"scenario_counts"`
	ExcludedRuns     []RunExclusion `json:"excluded_runs"`
	DatasetSHA256    string         `json:"dataset_sha256"`
}

// PipelineConfig configures the dataset assembly pipeline.
type PipelineConfig struct {
	RunDirs      []string
	OutputDir    string
	Horizon      int
	WarmUpBlocks int
}

// GenerateDataset processes the specified runs and generates dataset.csv + dataset_metadata.json.
func GenerateDataset(cfg PipelineConfig) (*DatasetMetadata, error) {
	if cfg.Horizon <= 0 {
		cfg.Horizon = DefaultForecastHorizon
	}
	if cfg.WarmUpBlocks < 0 {
		cfg.WarmUpBlocks = 10
	}
	if err := os.MkdirAll(cfg.OutputDir, 0755); err != nil {
		return nil, fmt.Errorf("creating output dir: %w", err)
	}

	// Height-level sampling design:
	// The dataset produces exactly one sample per eligible unique run/block-height (t > warmUpBlocks, t <= N - H).
	// When multiple proposal events share the same height (e.g., competing forks), the first proposal event
	// at that height serves as the prediction-time anchor. Multiple proposals at the same height are not separate rows.
	meta := &DatasetMetadata{
		GeneratedAtUTC:   time.Now().UTC().Format(time.RFC3339),
		DatasetVersion:   "1.3",
		FeatureSchemaVer: FeatureSchemaVersion,
		ForecastHorizon:  cfg.Horizon,
		WarmUpBlocks:     cfg.WarmUpBlocks,
		ScenarioCounts:   make(map[string]int),
		IncludedRuns:     make([]string, 0),
		ExcludedRuns:     make([]RunExclusion, 0),
	}

	var allRows []DatasetRow

	// Sort run directories for absolute determinism
	sort.Strings(cfg.RunDirs)

	for _, rDir := range cfg.RunDirs {
		eventsPath := filepath.Join(rDir, "events.jsonl")
		metaPath := filepath.Join(rDir, "metadata.json")

		if _, err := os.Stat(eventsPath); os.IsNotExist(err) {
			meta.ExcludedRuns = append(meta.ExcludedRuns, RunExclusion{
				RunDir: rDir,
				Reason: "missing events.jsonl",
			})
			continue
		}
		if _, err := os.Stat(metaPath); os.IsNotExist(err) {
			meta.ExcludedRuns = append(meta.ExcludedRuns, RunExclusion{
				RunDir: rDir,
				Reason: "missing metadata.json provenance",
			})
			continue
		}

		// Read and validate metadata
		metaBytes, err := os.ReadFile(metaPath)
		if err != nil {
			meta.ExcludedRuns = append(meta.ExcludedRuns, RunExclusion{
				RunDir: rDir,
				Reason: fmt.Sprintf("unreadable metadata.json: %v", err),
			})
			continue
		}
		var runMeta logstore.RunMetadata
		if err := json.Unmarshal(metaBytes, &runMeta); err != nil {
			meta.ExcludedRuns = append(meta.ExcludedRuns, RunExclusion{
				RunDir: rDir,
				Reason: fmt.Sprintf("corrupt metadata.json: %v", err),
			})
			continue
		}

		// Extract configuration
		cfgMap, ok := runMeta.Config.(map[string]interface{})
		if !ok {
			meta.ExcludedRuns = append(meta.ExcludedRuns, RunExclusion{
				RunDir: rDir,
				Reason: "invalid config object in metadata.json",
			})
			continue
		}

		scenarioCfg := config.ScenarioConfig{
			Name: runMeta.ScenarioName,
			Seed: runMeta.Seed,
		}
		if v, ok := cfgMap["num_nodes"].(float64); ok {
			scenarioCfg.NumNodes = int(v)
		}
		if v, ok := cfgMap["block_interval_sec"].(float64); ok {
			scenarioCfg.BlockIntervalSec = v
		}
		if v, ok := cfgMap["mean_latency_ms"].(float64); ok {
			scenarioCfg.MeanLatencyMs = v
		}
		if v, ok := cfgMap["mean_prop_delay_ms"].(float64); ok {
			scenarioCfg.MeanPropDelayMs = v
		}
		if v, ok := cfgMap["tx_arrival_rate"].(float64); ok {
			scenarioCfg.TxArrivalRate = v
		}
		if v, ok := cfgMap["tx_per_block"].(float64); ok {
			scenarioCfg.TxPerBlock = int(v)
		}
		if v, ok := cfgMap["block_capacity"].(float64); ok {
			scenarioCfg.BlockCapacity = int(v)
		}
		if v, ok := cfgMap["warm_up_blocks"].(float64); ok {
			scenarioCfg.WarmUpBlocks = int(v)
		}

		// Read events
		events, err := ReadEventLog(eventsPath)
		if err != nil {
			meta.ExcludedRuns = append(meta.ExcludedRuns, RunExclusion{
				RunDir: rDir,
				Reason: fmt.Sprintf("error reading events: %v", err),
			})
			continue
		}

		proposals := ExtractProposals(events)
		if len(proposals) <= cfg.WarmUpBlocks+cfg.Horizon {
			meta.ExcludedRuns = append(meta.ExcludedRuns, RunExclusion{
				RunDir: rDir,
				Reason: fmt.Sprintf("insufficient proposals (%d <= warmup+horizon)", len(proposals)),
			})
			continue
		}

		// Collect node IDs
		nodeIDsMap := make(map[string]bool)
		for _, e := range events {
			if e.NodeID != "" {
				nodeIDsMap[e.NodeID] = true
			}
		}
		var nodeIDs []string
		for nid := range nodeIDsMap {
			nodeIDs = append(nodeIDs, nid)
		}
		sort.Strings(nodeIDs)

		// 1. Run fork detector over the full event log to obtain ground-truth outcomes
		var forkEvents []fork.Event
		for _, e := range events {
			forkEvents = append(forkEvents, fork.Event{
				Seq:         e.Seq,
				SimTime:     e.SimTime,
				EventType:   e.EventType,
				NodeID:      e.NodeID,
				BlockHash:   e.BlockHash,
				BlockHeight: e.BlockHeight,
				ProposerID:  e.ProposerID,
				ParentHash:  e.ParentHash,
				TipHash:     e.TipHash,
				TipHeight:   e.TipHeight,
				OldTipHash:  e.OldTipHash,
				Reason:      e.Reason,
				TxCount:     e.TxCount,
			})
		}
		detector := fork.NewDetector(nodeIDs, cfg.WarmUpBlocks)
		detection := detector.Analyze(forkEvents)

		// 2. Compute prediction labels
		labeler := NewLabeler(cfg.Horizon, cfg.WarmUpBlocks)
		labels, eligibleHeights := labeler.ComputeLabels(proposals, detection)

		// 3. Extract causal features for each eligible height
		extractor := NewExtractor(nodeIDs, cfg.WarmUpBlocks, scenarioCfg)

		proposalIndexMap := make(map[int]int)
		for idx, p := range proposals {
			if _, exists := proposalIndexMap[p.Height]; !exists {
				proposalIndexMap[p.Height] = idx
			}
		}

		for _, h := range eligibleHeights {
			pIdx := proposalIndexMap[h]
			proposal := proposals[pIdx]
			feats := extractor.ExtractFeaturesAt(events, proposals, pIdx)
			lbl := labels[h]

			allRows = append(allRows, DatasetRow{
				RunID:       runMeta.RunID,
				Scenario:    runMeta.ScenarioName,
				Seed:        runMeta.Seed,
				BlockHeight: h,
				SimTime:     proposal.SimTime,
				Features:    feats,
				LabelForkH:  lbl,
			})

			meta.ScenarioCounts[runMeta.ScenarioName]++
			if lbl == 1 {
				meta.PositiveCount++
			} else {
				meta.NegativeCount++
			}
		}

		meta.IncludedRuns = append(meta.IncludedRuns, runMeta.RunID)
	}

	meta.TotalRows = len(allRows)
	meta.IncludedRunCount = len(meta.IncludedRuns)
	if meta.TotalRows > 0 {
		meta.PositiveRate = float64(meta.PositiveCount) / float64(meta.TotalRows)
	}

	// Write dataset.csv
	csvPath := filepath.Join(cfg.OutputDir, "fork_risk_dataset.csv")
	csvFile, err := os.Create(csvPath)
	if err != nil {
		return nil, fmt.Errorf("creating dataset.csv: %w", err)
	}
	defer csvFile.Close()

	writer := csv.NewWriter(csvFile)
	if err := writer.Write(CSVHeader()); err != nil {
		return nil, fmt.Errorf("writing csv header: %w", err)
	}

	for _, row := range allRows {
		if err := writer.Write(row.ToCSVRow()); err != nil {
			return nil, fmt.Errorf("writing csv row: %w", err)
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, fmt.Errorf("flushing csv: %w", err)
	}

	// Compute dataset SHA-256
	h := sha256.New()
	f, err := os.Open(csvPath)
	if err == nil {
		io.Copy(h, f)
		f.Close()
		meta.DatasetSHA256 = hex.EncodeToString(h.Sum(nil))
	}

	// Write dataset_metadata.json
	metaPath := filepath.Join(cfg.OutputDir, "dataset_metadata.json")
	metaJSON, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshalling metadata: %w", err)
	}
	if err := os.WriteFile(metaPath, metaJSON, 0644); err != nil {
		return nil, fmt.Errorf("writing dataset_metadata.json: %w", err)
	}

	return meta, nil
}

// ReadEventLog reads all entries from an events.jsonl file.
func ReadEventLog(path string) ([]logstore.LogEntry, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var entries []logstore.LogEntry
	scanner := bufio.NewScanner(file)
	// Allow large lines
	buf := make([]byte, 1024*1024)
	scanner.Buffer(buf, 10*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var e logstore.LogEntry
		if err := json.Unmarshal(line, &e); err != nil {
			return nil, fmt.Errorf("parsing line: %w", err)
		}
		entries = append(entries, e)
	}
	if err := scanner.Err(); err != nil {
		return nil, scanner.Err()
	}
	return entries, nil
}
