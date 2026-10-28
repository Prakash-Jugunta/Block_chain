// Package logstore provides append-only structured event logging to JSONL files.
// Each line of the output file is a valid JSON object representing one event.
// The format is designed to be stable across simulation runs so that the same
// seed produces byte-identical logs.
package logstore

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// LogEntry is one record written to the JSONL event log.
// All fields use json tags so the output column names are stable.
type LogEntry struct {
	// Seq is the global event sequence number (monotonically increasing).
	Seq int64 `json:"seq"`

	// SimTime is the simulated clock in seconds when this event fired.
	SimTime float64 `json:"sim_time"`

	// EventType matches the engine.EventType constants (e.g. "BlockPropose").
	EventType string `json:"event_type"`

	// NodeID is the node that processed this event.
	NodeID string `json:"node_id"`

	// BlockHash is the hex hash of the block involved.
	BlockHash string `json:"block_hash,omitempty"`

	// BlockHeight is the height of the involved block.
	BlockHeight int `json:"block_height,omitempty"`

	// ProposalIndex is the 1-based global proposal sequence number across the network.
	ProposalIndex int `json:"proposal_index,omitempty"`

	// ProposerID is who originally created the block.
	ProposerID string `json:"proposer_id,omitempty"`

	// ParentHash is the parent block's hash.
	ParentHash string `json:"parent_hash,omitempty"`

	// TxCount is the number of transactions in the block.
	TxCount int `json:"tx_count,omitempty"`

	// FromNode is the sending node (for gossip/arrive events).
	FromNode string `json:"from_node,omitempty"`

	// TipHash is the node's new tip hash after processing (for TipUpdate events).
	TipHash string `json:"tip_hash,omitempty"`

	// TipHeight is the height of the new tip.
	TipHeight int `json:"tip_height,omitempty"`

	// OldTipHash is the previous tip before the tip update.
	OldTipHash string `json:"old_tip_hash,omitempty"`

	// Reason records rejection reasons, orphan causes, or reorg notes.
	Reason string `json:"reason,omitempty"`

	// ConfigField is the name of the configuration field that was changed
	// (used in ConfigChange events, e.g. "mean_latency_ms").
	ConfigField string `json:"config_field,omitempty"`

	// ConfigOldValue is the previous value of the changed field (as a string).
	ConfigOldValue string `json:"config_old_value,omitempty"`

	// ConfigNewValue is the new value of the changed field (as a string).
	ConfigNewValue string `json:"config_new_value,omitempty"`

	// AtBlock is the block number that triggered a ConfigChange event.
	AtBlock int `json:"at_block,omitempty"`

	// PoolSize is the size of the mempool (pending txs) after block consumption.
	PoolSize int `json:"pool_size,omitempty"`

	// OfferedTx is the total cumulative transactions offered so far.
	OfferedTx int `json:"offered_tx,omitempty"`
}

// Logger writes structured JSONL log entries to a file.
type Logger struct {
	file    *os.File
	encoder *json.Encoder
}

// Open creates (or truncates) the log file at the given path and returns a Logger.
// Parent directories are created automatically.
func Open(path string) (*Logger, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("creating log directory: %w", err)
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("creating log file %q: %w", path, err)
	}
	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false) // keep hashes readable
	return &Logger{file: f, encoder: enc}, nil
}

// Write appends one LogEntry to the file as a JSON line.
func (l *Logger) Write(entry LogEntry) error {
	return l.encoder.Encode(entry)
}

// Close flushes and closes the underlying file.
func (l *Logger) Close() error {
	return l.file.Close()
}

// RunID builds a deterministic run identifier from scenario name and seed.
// The Unix timestamp is appended only for uniqueness when re-running the same seed.
func RunID(scenario string, seed int64) string {
	return fmt.Sprintf("%s_seed%d_%d", scenario, seed, time.Now().Unix())
}

// RunMetadata stores complete run provenance and parameters for reproducibility.
type RunMetadata struct {
	RunID              string         `json:"run_id"`
	ScenarioName       string         `json:"scenario_name"`
	Seed               int64          `json:"seed"`
	TimestampUTC       string         `json:"timestamp_utc"`
	GoVersion          string         `json:"go_version"`
	Platform           string         `json:"platform"`
	GitCommit          string         `json:"git_commit"`
	Config             interface{}    `json:"config"`
	ProposedBlocks     int            `json:"proposed_blocks"`
	ProcessedEvents    int64          `json:"processed_events"`
	SimDurationSec     float64        `json:"sim_duration_sec"`
	SettledDurationSec float64        `json:"settled_duration_sec"`
	DivergentTips      int            `json:"divergent_tips"`
	NodeTipHeights     map[string]int `json:"node_tip_heights"`
	LogSHA256          string         `json:"log_sha256"`
	TotalOfferedTx     int            `json:"total_offered_tx"`
	TotalProcessedTx   int            `json:"total_processed_tx"`
	MaxPendingTx       int            `json:"max_pending_tx"`
	FinalPendingTx     int            `json:"final_pending_tx"`
	OfferedTPS         float64        `json:"offered_tps"`
}

// WriteMetadata writes the run metadata to metadata.json in the specified directory.
func WriteMetadata(dir string, meta RunMetadata) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating metadata dir: %w", err)
	}
	path := filepath.Join(dir, "metadata.json")
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling metadata: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}
