package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pranav-k/forkrisk/internal/config"
)

// TestDefaultsApplied verifies that zero-value fields are filled by Defaults().
func TestDefaultsApplied(t *testing.T) {
	cfg := &config.ScenarioConfig{}
	cfg.Defaults()

	if cfg.NumNodes == 0 {
		t.Error("NumNodes should have a default")
	}
	if cfg.BlockIntervalSec == 0 {
		t.Error("BlockIntervalSec should have a default")
	}
	if cfg.MeanLatencyMs == 0 {
		t.Error("MeanLatencyMs should have a default")
	}
	if cfg.OutputDir == "" {
		t.Error("OutputDir should have a default")
	}
	if cfg.PeersPerNode == 0 {
		t.Error("PeersPerNode should have a default")
	}
}

// TestValidateRejectsFewerThanTwoNodes verifies that NumNodes < 2 is rejected.
func TestValidateRejectsFewerThanTwoNodes(t *testing.T) {
	cfg := &config.ScenarioConfig{
		NumNodes:         1,
		BlockIntervalSec: 1.0,
		SimDurationSec:   10.0,
		PeersPerNode:     1,
		TxPerBlock:       1,
	}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for num_nodes=1, got nil")
	}
}

// TestValidateRejectsZeroBlockInterval checks that block_interval_sec=0 fails.
func TestValidateRejectsZeroBlockInterval(t *testing.T) {
	cfg := &config.ScenarioConfig{
		NumNodes:         5,
		BlockIntervalSec: 0,
		SimDurationSec:   10.0,
		PeersPerNode:     2,
		TxPerBlock:       1,
	}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for block_interval_sec=0, got nil")
	}
}

// TestValidateRejectsNoStopCondition verifies that both sim_duration_sec=0
// and max_blocks=0 is rejected.
func TestValidateRejectsNoStopCondition(t *testing.T) {
	cfg := &config.ScenarioConfig{
		NumNodes:         5,
		BlockIntervalSec: 1.0,
		SimDurationSec:   0,
		MaxBlocks:        0,
		PeersPerNode:     2,
		TxPerBlock:       1,
	}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error when neither sim_duration_sec nor max_blocks is set")
	}
}

// TestValidateRejectsTooManyPeers checks peers_per_node >= num_nodes.
func TestValidateRejectsTooManyPeers(t *testing.T) {
	cfg := &config.ScenarioConfig{
		NumNodes:         3,
		BlockIntervalSec: 1.0,
		SimDurationSec:   10.0,
		PeersPerNode:     3, // must be < NumNodes
		TxPerBlock:       1,
	}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for peers_per_node >= num_nodes")
	}
}

// TestValidAccepts a valid configuration without error.
func TestValidAccepts(t *testing.T) {
	cfg := &config.ScenarioConfig{
		NumNodes:          5,
		BlockIntervalSec:  1.0,
		SimDurationSec:    30.0,
		PeersPerNode:      2,
		TxPerBlock:        5,
		MeanLatencyMs:     50,
		MeanPropDelayMs:   100,
		ConfirmationDepth: 1,
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("unexpected validation error: %v", err)
	}
}

// TestValidateProposerSelection verifies validation of proposer selection options.
func TestValidateProposerSelection(t *testing.T) {
	cfg := &config.ScenarioConfig{
		NumNodes:          5,
		BlockIntervalSec:  1.0,
		SimDurationSec:    10.0,
		PeersPerNode:      2,
		TxPerBlock:        1,
		ConfirmationDepth: 1,
		ProposerSelection: "invalid_rule",
	}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for invalid proposer_selection, got nil")
	}

	cfg.ProposerSelection = "round_robin"
	if err := cfg.Validate(); err != nil {
		t.Errorf("expected round_robin to be valid, got: %v", err)
	}

	cfg.ProposerSelection = "uniform_random"
	if err := cfg.Validate(); err != nil {
		t.Errorf("expected uniform_random to be valid, got: %v", err)
	}
}

// TestValidateSettlementGrace verifies settlement grace validation.
func TestValidateSettlementGrace(t *testing.T) {
	cfg := &config.ScenarioConfig{
		NumNodes:           5,
		BlockIntervalSec:   1.0,
		SimDurationSec:     10.0,
		PeersPerNode:       2,
		TxPerBlock:         1,
		ConfirmationDepth:  1,
		SettlementGraceSec: -1.0,
	}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for negative settlement_grace_sec, got nil")
	}
}

// TestLoadFromFile verifies that a YAML file is correctly loaded, defaults applied,
// and validated without error.
func TestLoadFromFile(t *testing.T) {
	// Write a minimal valid YAML to a temp file.
	content := `
name: test
num_nodes: 4
block_interval_sec: 1.0
sim_duration_sec: 10.0
peers_per_node: 2
tx_per_block: 3
mean_latency_ms: 50
mean_prop_delay_ms: 100
`
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("writing temp file: %v", err)
	}

	cfg, err := config.LoadFromFile(path)
	if err != nil {
		t.Fatalf("LoadFromFile failed: %v", err)
	}
	if cfg.Name != "test" {
		t.Errorf("expected name 'test', got %q", cfg.Name)
	}
	if cfg.NumNodes != 4 {
		t.Errorf("expected 4 nodes, got %d", cfg.NumNodes)
	}
}

// TestLoadFromFileMissingFile verifies that a missing file returns an error.
func TestLoadFromFileMissingFile(t *testing.T) {
	_, err := config.LoadFromFile("nonexistent_file.yaml")
	if err == nil {
		t.Error("expected error for missing file, got nil")
	}
}

// TestLoadFromFileInvalidYAML verifies that a config that fails validation
// (here: num_nodes < 2) returns an error from LoadFromFile.
func TestLoadFromFileInvalidYAML(t *testing.T) {
	// This YAML is syntactically valid but semantically invalid: num_nodes=1 fails validation.
	badContent := `
name: bad
num_nodes: 1
block_interval_sec: 1.0
sim_duration_sec: 10.0
peers_per_node: 1
tx_per_block: 1
`
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "bad.yaml")
	if err := os.WriteFile(path, []byte(badContent), 0644); err != nil {
		t.Fatalf("writing temp file: %v", err)
	}
	_, err := config.LoadFromFile(path)
	if err == nil {
		t.Error("expected error for num_nodes=1 (invalid config), got nil")
	}
}

// TestLoadFromFileSyntaxError verifies that truly malformed YAML (bad indentation/tabs) returns an error.
func TestLoadFromFileSyntaxError(t *testing.T) {
	// YAML with a tab character where spaces are required — this is a YAML syntax error.
	badYAML := "name: test\n\tnum_nodes: 5\n"
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "syntax_bad.yaml")
	if err := os.WriteFile(path, []byte(badYAML), 0644); err != nil {
		t.Fatalf("writing temp file: %v", err)
	}
	_, err := config.LoadFromFile(path)
	if err == nil {
		t.Error("expected error for YAML with tab indentation syntax error, got nil")
	}
}

// =============================================================================
// Scenario Consistency Tests (Phase 3)
// =============================================================================

// TestScenarioConsistency loads every main scenario YAML from scenarios/ and
// checks that each stress scenario changes only its designated variable(s)
// relative to the baseline. Any unintended change is a test failure.
//
// This test must be run from the project root (go test ./...) so that
// "../../scenarios" resolves correctly from the package directory.
func TestScenarioConsistency(t *testing.T) {
	// Resolve path to scenarios/ directory from this package directory.
	scenariosDir := filepath.Join("..", "..", "scenarios")

	load := func(name string) *config.ScenarioConfig {
		t.Helper()
		path := filepath.Join(scenariosDir, name+".yaml")
		cfg, err := config.LoadFromFile(path)
		if err != nil {
			t.Fatalf("loading %s: %v", name, err)
		}
		return cfg
	}

	baseline := load("baseline")

	// For each stress scenario, define which fields are intentionally different
	// from the baseline. All other baseline fields must match.
	type scenarioCheck struct {
		name          string
		allowedDiffs  []string // field names whose difference from baseline is expected
		expectedChurn bool     // churn_enabled expected value
	}

	checks := []scenarioCheck{
		{
			name:          "latency_stress",
			allowedDiffs:  []string{"name", "mean_latency_ms", "latency_jitter_ms"},
			expectedChurn: true,
		},
		{
			name:          "propagation_stress",
			allowedDiffs:  []string{"name", "mean_prop_delay_ms", "prop_delay_jitter_ms", "settlement_grace_sec"},
			expectedChurn: true,
		},
		{
			name:          "churn_stress",
			allowedDiffs:  []string{"name", "churn_mean_on_sec", "churn_mean_off_sec"},
			expectedChurn: true,
		},
		{
			name:          "block_rate_stress",
			allowedDiffs:  []string{"name", "block_interval_sec", "block_jitter_sec"},
			expectedChurn: true,
		},
		{
			name:          "tx_load_stress",
			allowedDiffs:  []string{"name", "tx_arrival_rate", "block_capacity"},
			expectedChurn: true,
		},
		{
			name: "combined_stress",
			allowedDiffs: []string{
				"name", "block_interval_sec", "block_jitter_sec",
				"mean_latency_ms", "latency_jitter_ms",
				"mean_prop_delay_ms", "prop_delay_jitter_ms",
				"settlement_grace_sec",
			},
			expectedChurn: true,
		},
		{
			name:          "dynamic_network",
			allowedDiffs:  []string{"name", "dynamic_events"},
			expectedChurn: true,
		},
	}

	for _, sc := range checks {
		t.Run(sc.name, func(t *testing.T) {
			stress := load(sc.name)

			// Churn must always match the expected value.
			if stress.ChurnEnabled != sc.expectedChurn {
				t.Errorf("%s: churn_enabled=%v, expected %v (must match baseline for controlled experiment)",
					sc.name, stress.ChurnEnabled, sc.expectedChurn)
			}

			// Build allowedDiffs set for quick lookup.
			allowed := make(map[string]bool)
			for _, f := range sc.allowedDiffs {
				allowed[f] = true
			}

			// Check all fields that are NOT in allowedDiffs must equal the baseline.
			type field struct {
				name      string
				base, got interface{}
			}
			fields := []field{
				{"num_nodes", baseline.NumNodes, stress.NumNodes},
				{"block_interval_sec", baseline.BlockIntervalSec, stress.BlockIntervalSec},
				{"block_jitter_sec", baseline.BlockJitterSec, stress.BlockJitterSec},
				{"mean_latency_ms", baseline.MeanLatencyMs, stress.MeanLatencyMs},
				{"latency_jitter_ms", baseline.LatencyJitterMs, stress.LatencyJitterMs},
				{"mean_prop_delay_ms", baseline.MeanPropDelayMs, stress.MeanPropDelayMs},
				{"prop_delay_jitter_ms", baseline.PropDelayJitterMs, stress.PropDelayJitterMs},
				{"sim_duration_sec", baseline.SimDurationSec, stress.SimDurationSec},
				{"max_blocks", baseline.MaxBlocks, stress.MaxBlocks},
				{"seed", baseline.Seed, stress.Seed},
				{"output_dir", baseline.OutputDir, stress.OutputDir},
				{"proposer_selection", baseline.ProposerSelection, stress.ProposerSelection},
				{"peers_per_node", baseline.PeersPerNode, stress.PeersPerNode},
				{"tx_per_block", baseline.TxPerBlock, stress.TxPerBlock},
				{"churn_mean_on_sec", baseline.ChurnMeanOnSec, stress.ChurnMeanOnSec},
				{"churn_mean_off_sec", baseline.ChurnMeanOffSec, stress.ChurnMeanOffSec},
				{"tx_arrival_rate", baseline.TxArrivalRate, stress.TxArrivalRate},
				{"block_capacity", baseline.BlockCapacity, stress.BlockCapacity},
				{"warm_up_blocks", baseline.WarmUpBlocks, stress.WarmUpBlocks},
				{"confirmation_depth", baseline.ConfirmationDepth, stress.ConfirmationDepth},
				{"settlement_grace_sec", baseline.SettlementGraceSec, stress.SettlementGraceSec},
				{"churn_enabled", baseline.ChurnEnabled, stress.ChurnEnabled},
			}
			for _, f := range fields {
				if allowed[f.name] {
					continue // intentional difference
				}
				if f.base != f.got {
					t.Errorf("%s field %q: expected baseline value %v, got %v (unintended difference)",
						sc.name, f.name, f.base, f.got)
				}
			}

			// Check dynamic events definitions.
			if allowed["dynamic_events"] {
				// Verify dynamic_events for dynamic_network scenario explicitly.
				if sc.name == "dynamic_network" {
					if len(stress.DynamicEvents) != 1 {
						t.Errorf("dynamic_network: expected 1 dynamic event, got %d", len(stress.DynamicEvents))
					} else {
						ev := stress.DynamicEvents[0]
						if ev.AtBlock != 150 {
							t.Errorf("dynamic_network: expected event at block 150, got %d", ev.AtBlock)
						}
						if ev.MeanLatencyMs == nil || *ev.MeanLatencyMs != 300.0 {
							t.Errorf("dynamic_network: expected mean_latency_ms 300.0, got %v", ev.MeanLatencyMs)
						}
						if ev.LatencyJitterMs == nil || *ev.LatencyJitterMs != 30.0 {
							t.Errorf("dynamic_network: expected latency_jitter_ms 30.0, got %v", ev.LatencyJitterMs)
						}
					}
				}
			} else {
				if len(stress.DynamicEvents) != len(baseline.DynamicEvents) {
					t.Errorf("%s: expected %d dynamic events (matching baseline), got %d",
						sc.name, len(baseline.DynamicEvents), len(stress.DynamicEvents))
				}
			}
		})
	}
}

// TestLoadFromFile_TxArrivalRateSemantics verifies that:
// 1. An explicit tx_arrival_rate: 0 (or 0.0) in YAML remains 0.0 after LoadFromFile.
// 2. An omitted tx_arrival_rate receives the default of 1.0.
// 3. A negative tx_arrival_rate is rejected with a validation error.
func TestLoadFromFile_TxArrivalRateSemantics(t *testing.T) {
	tmpDir := t.TempDir()

	writeYAML := func(name, content string) string {
		p := filepath.Join(tmpDir, name)
		if err := os.WriteFile(p, []byte(content), 0644); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
		return p
	}

	baseContent := `
name: test_rate
num_nodes: 4
block_interval_sec: 1.0
sim_duration_sec: 10.0
peers_per_node: 2
tx_per_block: 5
mean_latency_ms: 50.0
mean_prop_delay_ms: 100.0
`

	// 1. Explicit zero: tx_arrival_rate: 0
	pZero := writeYAML("zero.yaml", baseContent+"tx_arrival_rate: 0\n")
	cfgZero, err := config.LoadFromFile(pZero)
	if err != nil {
		t.Fatalf("failed to load zero.yaml: %v", err)
	}
	if cfgZero.TxArrivalRate != 0.0 {
		t.Errorf("expected explicit tx_arrival_rate: 0 to remain 0.0, got %g", cfgZero.TxArrivalRate)
	}
	if !cfgZero.TxArrivalRateExplicit {
		t.Errorf("expected TxArrivalRateExplicit to be true for explicit 0")
	}

	// 1b. Explicit zero float: tx_arrival_rate: 0.0
	pZeroFloat := writeYAML("zero_float.yaml", baseContent+"tx_arrival_rate: 0.0\n")
	cfgZeroFloat, err := config.LoadFromFile(pZeroFloat)
	if err != nil {
		t.Fatalf("failed to load zero_float.yaml: %v", err)
	}
	if cfgZeroFloat.TxArrivalRate != 0.0 {
		t.Errorf("expected explicit tx_arrival_rate: 0.0 to remain 0.0, got %g", cfgZeroFloat.TxArrivalRate)
	}

	// 2. Omitted: should receive default 1.0
	pOmitted := writeYAML("omitted.yaml", baseContent)
	cfgOmitted, err := config.LoadFromFile(pOmitted)
	if err != nil {
		t.Fatalf("failed to load omitted.yaml: %v", err)
	}
	if cfgOmitted.TxArrivalRate != 1.0 {
		t.Errorf("expected omitted tx_arrival_rate to default to 1.0, got %g", cfgOmitted.TxArrivalRate)
	}

	// 3. Negative: should be rejected by Validate()
	pNegative := writeYAML("negative.yaml", baseContent+"tx_arrival_rate: -0.5\n")
	_, err = config.LoadFromFile(pNegative)
	if err == nil {
		t.Errorf("expected negative tx_arrival_rate to be rejected by Validate(), but got nil error")
	}
}
