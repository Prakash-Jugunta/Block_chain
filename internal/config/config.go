// Package config handles loading and validating simulation scenario configurations.
// Scenarios are defined as YAML files in the scenarios/ directory.
package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// DynamicEvent represents a mid-run parameter change for dynamic network experiments.
type DynamicEvent struct {
	// AtBlock specifies the block number at which this change takes effect.
	AtBlock int `yaml:"at_block" json:"at_block"`

	// MeanLatencyMs overrides the mean latency when this event fires.
	MeanLatencyMs *float64 `yaml:"mean_latency_ms,omitempty" json:"mean_latency_ms,omitempty"`

	// LatencyJitterMs overrides the latency jitter when this event fires.
	LatencyJitterMs *float64 `yaml:"latency_jitter_ms,omitempty" json:"latency_jitter_ms,omitempty"`

	// MeanPropDelayMs overrides the propagation delay when this event fires.
	MeanPropDelayMs *float64 `yaml:"mean_prop_delay_ms,omitempty" json:"mean_prop_delay_ms,omitempty"`

	// PropDelayJitterMs overrides the propagation jitter when this event fires.
	PropDelayJitterMs *float64 `yaml:"prop_delay_jitter_ms,omitempty" json:"prop_delay_jitter_ms,omitempty"`
}

// ScenarioConfig holds all parameters for a single simulation run.
// Every field maps to a YAML key in the scenario file.
type ScenarioConfig struct {
	// Name is a human-readable label for this scenario (e.g. "baseline").
	Name string `yaml:"name" json:"name"`

	// NumNodes is the number of virtual blockchain nodes to simulate.
	// Must be ≥ 2.
	NumNodes int `yaml:"num_nodes" json:"num_nodes"`

	// BlockIntervalSec is the network-wide target time (in simulated seconds)
	// between successive block proposals across the network.
	BlockIntervalSec float64 `yaml:"block_interval_sec" json:"block_interval_sec"`

	// BlockJitterSec is the ±uniform jitter applied to BlockIntervalSec.
	// A value of 0.0 means perfectly regular proposals.
	BlockJitterSec float64 `yaml:"block_jitter_sec" json:"block_jitter_sec"`

	// ProposerSelection specifies the rule used to select a node for each block proposal.
	// Options: "uniform_random" (default), "round_robin".
	ProposerSelection string `yaml:"proposer_selection" json:"proposer_selection"`

	// SettlementGraceSec is the maximum simulated time allowed after proposal
	// termination for in-flight gossip and arrival messages to settle.
	SettlementGraceSec float64 `yaml:"settlement_grace_sec" json:"settlement_grace_sec"`

	// MeanLatencyMs is the mean one-way network latency in milliseconds
	// between any two connected peers (Gamma distributed).
	MeanLatencyMs float64 `yaml:"mean_latency_ms" json:"mean_latency_ms"`

	// LatencyJitterMs is the standard deviation of the latency (sets Gamma shape).
	LatencyJitterMs float64 `yaml:"latency_jitter_ms" json:"latency_jitter_ms"`

	// MeanPropDelayMs is the additional propagation processing delay at the
	// receiving peer, in milliseconds.
	MeanPropDelayMs float64 `yaml:"mean_prop_delay_ms" json:"mean_prop_delay_ms"`

	// PropDelayJitterMs is the std-dev of the propagation delay.
	PropDelayJitterMs float64 `yaml:"prop_delay_jitter_ms" json:"prop_delay_jitter_ms"`

	// SimDurationSec is how many simulated seconds to run proposals.
	// The simulation proposal phase stops when simulated time exceeds this value.
	SimDurationSec float64 `yaml:"sim_duration_sec" json:"sim_duration_sec"`

	// MaxBlocks, if > 0, stops block proposals after this many blocks
	// have been proposed (whichever of SimDurationSec / MaxBlocks is reached first).
	MaxBlocks int `yaml:"max_blocks" json:"max_blocks"`

	// Seed is the random seed for this run; overridable via --seed CLI flag.
	Seed int64 `yaml:"seed" json:"seed"`

	// OutputDir is the directory where event logs are written.
	// Defaults to "outputs/runs" relative to the working directory.
	OutputDir string `yaml:"output_dir" json:"output_dir"`

	// PeersPerNode is the number of peer connections each node maintains.
	// Must be ≥ 1 and < NumNodes.
	PeersPerNode int `yaml:"peers_per_node" json:"peers_per_node"`

	// TxPerBlock is the number of transactions included in each proposed block.
	// Uniform random from [1, TxPerBlock].
	TxPerBlock int `yaml:"tx_per_block" json:"tx_per_block"`

	// --- Milestone 2 fields ---

	// ChurnEnabled activates the Markov on/off peer availability model.
	ChurnEnabled bool `yaml:"churn_enabled" json:"churn_enabled"`

	// ChurnMeanOnSec is the mean duration (in simulated seconds) a node stays online.
	// Used as the rate parameter for the exponential distribution.
	ChurnMeanOnSec float64 `yaml:"churn_mean_on_sec" json:"churn_mean_on_sec"`

	// ChurnMeanOffSec is the mean duration (in simulated seconds) a node stays offline.
	ChurnMeanOffSec float64 `yaml:"churn_mean_off_sec" json:"churn_mean_off_sec"`

	// TxArrivalRate is the transaction arrival rate multiplier.
	// 1.0 = baseline (TxPerBlock txs per block interval).
	// Higher values model increased transaction load independent of block production.
	// 0.0 explicitly disables transaction arrivals.
	TxArrivalRate float64 `yaml:"tx_arrival_rate" json:"tx_arrival_rate"`

	// TxArrivalRateExplicit indicates whether tx_arrival_rate was explicitly specified
	// in the scenario configuration (even if set to 0.0), rather than omitted.
	TxArrivalRateExplicit bool `yaml:"-" json:"-"`

	// BlockCapacity is the maximum number of transactions a block can include.
	// When tx arrival produces more txs than capacity, the excess is queued (saturation).
	// 0 = unlimited (backward-compatible).
	BlockCapacity int `yaml:"block_capacity" json:"block_capacity"`

	// WarmUpBlocks is the number of initial blocks to exclude from metrics.
	// Defaults to 0 for short runs. The planning doc suggests 1000 for production runs.
	WarmUpBlocks int `yaml:"warm_up_blocks" json:"warm_up_blocks"`

	// ConfirmationDepth (K) is the number of blocks required for confirmation.
	// Used to compute confirmation latency metric. Default: 6.
	ConfirmationDepth int `yaml:"confirmation_depth" json:"confirmation_depth"`

	// DynamicEvents is an ordered list of mid-run parameter changes.
	// Each event fires when the specified block number is reached.
	DynamicEvents []DynamicEvent `yaml:"dynamic_events,omitempty" json:"dynamic_events,omitempty"`
}

// UnmarshalYAML implements custom YAML unmarshalling for ScenarioConfig to distinguish
// an omitted tx_arrival_rate (which defaults to 1.0) from an explicit value (such as 0.0).
func (c *ScenarioConfig) UnmarshalYAML(value *yaml.Node) error {
	type rawConfig ScenarioConfig
	if err := value.Decode((*rawConfig)(c)); err != nil {
		return err
	}
	// Check if tx_arrival_rate was explicitly specified in the YAML mapping.
	for i := 0; i < len(value.Content)-1; i += 2 {
		if value.Content[i].Value == "tx_arrival_rate" {
			c.TxArrivalRateExplicit = true
			break
		}
	}
	return nil
}

// Defaults fills in zero-value fields with sensible baseline values.
// Call this after unmarshalling from YAML, before Validate().
func (c *ScenarioConfig) Defaults() {
	if c.Name == "" {
		c.Name = "unnamed"
	}
	if c.NumNodes == 0 {
		c.NumNodes = 10
	}
	if c.BlockIntervalSec == 0 {
		c.BlockIntervalSec = 1.0
	}
	if c.ProposerSelection == "" {
		c.ProposerSelection = "uniform_random"
	}
	if c.SettlementGraceSec <= 0 {
		c.SettlementGraceSec = 5.0
	}
	if c.MeanLatencyMs == 0 {
		c.MeanLatencyMs = 50.0
	}
	if c.LatencyJitterMs == 0 {
		c.LatencyJitterMs = 10.0
	}
	if c.MeanPropDelayMs == 0 {
		c.MeanPropDelayMs = 100.0
	}
	if c.PropDelayJitterMs == 0 {
		c.PropDelayJitterMs = 20.0
	}
	if c.SimDurationSec == 0 && c.MaxBlocks == 0 {
		c.SimDurationSec = 100.0 // short default for quick testing
	}
	if c.Seed == 0 {
		c.Seed = time.Now().UnixNano()
	}
	if c.OutputDir == "" {
		c.OutputDir = "outputs/runs"
	}
	if c.PeersPerNode == 0 {
		c.PeersPerNode = 3
	}
	if c.TxPerBlock == 0 {
		c.TxPerBlock = 10
	}
	// Milestone 2 defaults
	if c.ChurnMeanOnSec == 0 {
		c.ChurnMeanOnSec = 100.0 // ~99% uptime with 1s off period
	}
	if c.ChurnMeanOffSec == 0 {
		c.ChurnMeanOffSec = 1.0
	}
	if !c.TxArrivalRateExplicit && c.TxArrivalRate == 0 {
		c.TxArrivalRate = 1.0
	}
	if c.ConfirmationDepth == 0 {
		c.ConfirmationDepth = 6
	}
}

// Validate checks that the configuration values are logically consistent.
// Returns an error describing the first problem found.
func (c *ScenarioConfig) Validate() error {
	if c.NumNodes < 2 {
		return fmt.Errorf("num_nodes must be ≥ 2, got %d", c.NumNodes)
	}
	if c.BlockIntervalSec <= 0 {
		return fmt.Errorf("block_interval_sec must be > 0, got %g", c.BlockIntervalSec)
	}
	if c.BlockJitterSec < 0 {
		return fmt.Errorf("block_jitter_sec must be ≥ 0, got %g", c.BlockJitterSec)
	}
	if c.ProposerSelection != "" && c.ProposerSelection != "uniform_random" && c.ProposerSelection != "round_robin" {
		return fmt.Errorf("proposer_selection must be 'uniform_random' or 'round_robin', got %q", c.ProposerSelection)
	}
	if c.SettlementGraceSec < 0 {
		return fmt.Errorf("settlement_grace_sec must be ≥ 0, got %g", c.SettlementGraceSec)
	}
	if c.MeanLatencyMs < 0 {
		return fmt.Errorf("mean_latency_ms must be ≥ 0, got %g", c.MeanLatencyMs)
	}
	if c.MeanPropDelayMs < 0 {
		return fmt.Errorf("mean_prop_delay_ms must be ≥ 0, got %g", c.MeanPropDelayMs)
	}
	if c.SimDurationSec <= 0 && c.MaxBlocks <= 0 {
		return fmt.Errorf("at least one of sim_duration_sec or max_blocks must be > 0")
	}
	if c.PeersPerNode < 1 {
		return fmt.Errorf("peers_per_node must be ≥ 1, got %d", c.PeersPerNode)
	}
	if c.PeersPerNode >= c.NumNodes {
		return fmt.Errorf("peers_per_node (%d) must be < num_nodes (%d)", c.PeersPerNode, c.NumNodes)
	}
	if c.TxPerBlock < 1 {
		return fmt.Errorf("tx_per_block must be ≥ 1, got %d", c.TxPerBlock)
	}
	// Milestone 2 validation
	if c.ChurnEnabled {
		if c.ChurnMeanOnSec <= 0 {
			return fmt.Errorf("churn_mean_on_sec must be > 0 when churn is enabled, got %g", c.ChurnMeanOnSec)
		}
		if c.ChurnMeanOffSec <= 0 {
			return fmt.Errorf("churn_mean_off_sec must be > 0 when churn is enabled, got %g", c.ChurnMeanOffSec)
		}
	}
	if c.TxArrivalRate < 0 {
		return fmt.Errorf("tx_arrival_rate must be ≥ 0, got %g", c.TxArrivalRate)
	}
	if c.BlockCapacity < 0 {
		return fmt.Errorf("block_capacity must be ≥ 0, got %d", c.BlockCapacity)
	}
	if c.ConfirmationDepth < 1 {
		return fmt.Errorf("confirmation_depth must be ≥ 1, got %d", c.ConfirmationDepth)
	}
	if c.WarmUpBlocks < 0 {
		return fmt.Errorf("warm_up_blocks must be ≥ 0, got %d", c.WarmUpBlocks)
	}
	// Validate dynamic events
	for i, de := range c.DynamicEvents {
		if de.AtBlock <= 0 {
			return fmt.Errorf("dynamic_events[%d].at_block must be > 0, got %d", i, de.AtBlock)
		}
		if i > 0 && de.AtBlock <= c.DynamicEvents[i-1].AtBlock {
			return fmt.Errorf("dynamic_events must be in ascending at_block order; event %d (block %d) <= event %d (block %d)",
				i, de.AtBlock, i-1, c.DynamicEvents[i-1].AtBlock)
		}
	}
	return nil
}

// LoadFromFile reads a YAML scenario file, applies defaults, and validates it.
func LoadFromFile(path string) (*ScenarioConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading scenario file %q: %w", path, err)
	}
	cfg := &ScenarioConfig{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing YAML %q: %w", path, err)
	}
	cfg.Defaults()
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid scenario %q: %w", path, err)
	}
	return cfg, nil
}
