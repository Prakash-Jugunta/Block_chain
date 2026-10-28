// Package metrics_test contains unit tests for the run-level metrics module.
package metrics_test

import (
	"math"
	"strings"
	"testing"

	"github.com/pranav-k/forkrisk/internal/metrics"
)

// defaultParams builds a ComputeParams for tests.
func defaultParams() metrics.ComputeParams {
	return metrics.ComputeParams{
		RunID:           "test_run",
		Scenario:        "test",
		Seed:            1,
		NumNodes:        2,
		BlockInterval:   1.0,
		MeanLatencyMs:   50.0,
		MeanPropDelayMs: 100.0,
		ChurnEnabled:    false,
		TxArrivalRate:   1.0,
		WarmUpBlocks:    0,
		ConfirmDepth:    2,
		SimDuration:     10.0,
		SettledDuration: 10.5,
	}
}

// approxEqual checks float equality within a tolerance.
func approxEqual(a, b, tol float64) bool {
	return math.Abs(a-b) <= tol
}

// buildJSONL builds a minimal JSONL log for a linear 2-node chain.
// N0 proposes blocks, N1 receives them via arrivals and tip updates.
func linearChainJSONL(blocks int) string {
	var sb strings.Builder
	// Use simple short hashes for readability.
	hashes := []string{"genesis", "b1", "b2", "b3", "b4", "b5", "b6", "b7", "b8", "b9", "b10"}
	for i := 1; i <= blocks && i < len(hashes); i++ {
		parent := hashes[i-1]
		hash := hashes[i]
		t := float64(i)
		// Propose event.
		sb.WriteString(`{"seq":` + itoa(int64(i*10)) + `,"sim_time":` + ftoa(t) + `,"event_type":"BlockPropose","node_id":"N0","block_hash":"` + hash + `","block_height":` + itoa(int64(i)) + `,"proposer_id":"N0","parent_hash":"` + parent + `","tip_hash":"` + hash + `","tip_height":` + itoa(int64(i)) + `,"tx_count":5}` + "\n")
		// Arrive event for N1 (includes parent_hash for chain tracing).
		sb.WriteString(`{"seq":` + itoa(int64(i*10+1)) + `,"sim_time":` + ftoa(t+0.1) + `,"event_type":"BlockArrive","node_id":"N1","block_hash":"` + hash + `","block_height":` + itoa(int64(i)) + `,"parent_hash":"` + parent + `","tip_hash":"` + hash + `","tip_height":` + itoa(int64(i)) + `,"tx_count":5}` + "\n")
		// TipUpdate for N1 (includes parent_hash).
		sb.WriteString(`{"seq":` + itoa(int64(i*10+2)) + `,"sim_time":` + ftoa(t+0.1) + `,"event_type":"TipUpdate","node_id":"N1","block_hash":"` + hash + `","block_height":` + itoa(int64(i)) + `,"tip_hash":"` + hash + `","tip_height":` + itoa(int64(i)) + `,"old_tip_hash":"` + parent + `","parent_hash":"` + parent + `","reason":"extend"}` + "\n")
	}
	return sb.String()
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	result := ""
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	for n > 0 {
		result = string(rune('0'+n%10)) + result
		n /= 10
	}
	if neg {
		result = "-" + result
	}
	return result
}

func ftoa(f float64) string {
	// Simple float formatter for test JSON.
	return strings.TrimRight(strings.TrimRight(
		strings.Replace(string(appendFloat(f)), "+", "", -1),
		"0"),
		".")
}

func appendFloat(f float64) []byte {
	// Format to 3 decimal places.
	result := make([]byte, 0, 20)
	i := int64(f)
	frac := int64((f-float64(i))*1000 + 0.5)
	s := itoa(i)
	result = append(result, []byte(s)...)
	result = append(result, '.')
	fs := itoa(frac)
	for len(fs) < 3 {
		fs = "0" + fs
	}
	result = append(result, []byte(fs)...)
	return result
}

// TestMetricsLinearChain verifies basic metrics on a simple linear chain
// where no forks occur. Key assertions:
//   - ForkRate = 0
//   - StaleRate = 0
//   - TotalBlocks matches proposed block count
//   - PropTimeMean > 0 (propagation delay observed)
//   - NodeViewConsistency = 1.0 (always agree)
func TestMetricsLinearChain(t *testing.T) {
	data := []byte(linearChainJSONL(5))
	p := defaultParams()
	p.SettledDuration = 6.0

	m, err := metrics.ComputeFromData(data, p)
	if err != nil {
		t.Fatalf("ComputeFromData error: %v", err)
	}

	if m.ForkRate != 0 {
		t.Errorf("expected fork rate 0, got %v", m.ForkRate)
	}
	if m.StaleRate != 0 {
		t.Errorf("expected stale rate 0, got %v", m.StaleRate)
	}
	if m.TotalBlocks != 5 {
		t.Errorf("expected 5 total blocks, got %d", m.TotalBlocks)
	}
	if m.PropTimeMeanSec <= 0 {
		t.Errorf("expected positive prop time mean, got %v", m.PropTimeMeanSec)
	}
	// With SettledDuration=6.0 and 5*5=25 total txs, raw TPS = 25/6.
	expectedTPS := 25.0 / 6.0
	if !approxEqual(m.RawTPS, expectedTPS, 0.1) {
		t.Errorf("expected raw TPS ~%.2f, got %.4f", expectedTPS, m.RawTPS)
	}
}

// TestMetricsNodeViewConsistency verifies that when all nodes share the same tip,
// NodeViewConsistency = 1.0.
func TestMetricsNodeViewConsistency(t *testing.T) {
	// Linear chain: all nodes always share the same tip at proposal time.
	data := []byte(linearChainJSONL(4))
	p := defaultParams()
	p.SettledDuration = 5.0

	m, err := metrics.ComputeFromData(data, p)
	if err != nil {
		t.Fatalf("error: %v", err)
	}

	// At each proposal, N1's tip has already been updated (TipUpdate before the next Propose).
	// So at each propose snapshot, both N0 and N1 should be on the same tip.
	if m.NodeViewConsistency < 0.5 {
		t.Errorf("expected node view consistency ≥ 0.5, got %v", m.NodeViewConsistency)
	}
}

// TestMetricsPeerAvailability_NoChurn verifies that without churn events,
// peer availability equals 1.0.
func TestMetricsPeerAvailability_NoChurn(t *testing.T) {
	data := []byte(linearChainJSONL(3))
	p := defaultParams()
	p.ChurnEnabled = false

	m, err := metrics.ComputeFromData(data, p)
	if err != nil {
		t.Fatalf("error: %v", err)
	}

	if m.PeerAvailability != 1.0 {
		t.Errorf("expected peer availability 1.0 with no churn, got %v", m.PeerAvailability)
	}
}

// TestMetricsPeerAvailability_WithChurn verifies that churn events reduce
// peer availability below 1.0.
func TestMetricsPeerAvailability_WithChurn(t *testing.T) {
	// N0 goes offline at t=2.0, back online at t=4.0, totalTime=10.
	// N0 online fraction: (2.0 + (10-4)) / 10 = 8/10 = 0.8
	// N1 always online: 1.0
	// Average: 0.9
	var sb strings.Builder
	// Include N0 and N1 in events so nodeSet tracks both.
	sb.WriteString(`{"seq":1,"sim_time":0.5,"event_type":"BlockPropose","node_id":"N0","block_hash":"b1","block_height":1,"proposer_id":"N0","parent_hash":"genesis","tip_hash":"b1","tip_height":1,"tx_count":1}` + "\n")
	sb.WriteString(`{"seq":2,"sim_time":0.6,"event_type":"BlockArrive","node_id":"N1","block_hash":"b1","block_height":1,"parent_hash":"genesis","tip_hash":"b1","tip_height":1}` + "\n")
	sb.WriteString(`{"seq":3,"sim_time":2.0,"event_type":"ChurnToggle","node_id":"N0","reason":"offline"}` + "\n")
	sb.WriteString(`{"seq":4,"sim_time":4.0,"event_type":"ChurnToggle","node_id":"N0","reason":"online"}` + "\n")

	p := defaultParams()
	p.ChurnEnabled = true
	p.SettledDuration = 10.0

	m, err := metrics.ComputeFromData([]byte(sb.String()), p)
	if err != nil {
		t.Fatalf("error: %v", err)
	}

	// Expected: N0 online 8/10, N1 online 10/10; avg = 0.9.
	expected := 0.9
	if !approxEqual(m.PeerAvailability, expected, 0.01) {
		t.Errorf("expected peer availability ~%.2f, got %.4f", expected, m.PeerAvailability)
	}
}

// TestPropagationTimeP95 verifies that P95 propagation time ≥ mean.
func TestPropagationTimeP95(t *testing.T) {
	data := []byte(linearChainJSONL(5))
	p := defaultParams()
	p.SettledDuration = 6.0

	m, err := metrics.ComputeFromData(data, p)
	if err != nil {
		t.Fatalf("error: %v", err)
	}

	if m.PropTimeP95Sec < m.PropTimeMeanSec {
		t.Errorf("P95 (%.4f) should be >= mean (%.4f)", m.PropTimeP95Sec, m.PropTimeMeanSec)
	}
}

// TestCSVHeader verifies that the CSV header has the expected columns.
func TestCSVHeader(t *testing.T) {
	hdr := metrics.CSVHeader()
	expected := []string{"run_id", "fork_rate", "stale_rate", "raw_tps", "goodput", "peer_availability"}
	hdrSet := make(map[string]bool)
	for _, h := range hdr {
		hdrSet[h] = true
	}
	for _, col := range expected {
		if !hdrSet[col] {
			t.Errorf("expected column %q in CSV header", col)
		}
	}
}

// TestCSVRowLength verifies that CSVRow produces the same number of values as CSVHeader.
func TestCSVRowLength(t *testing.T) {
	m := metrics.RunMetrics{
		RunID:    "test",
		Scenario: "baseline",
		Seed:     1,
	}
	hdr := metrics.CSVHeader()
	row := metrics.CSVRow(m)
	if len(row) != len(hdr) {
		t.Errorf("CSVRow length %d != CSVHeader length %d", len(row), len(hdr))
	}
}

// TestMetricsRunID verifies that the RunID and Scenario fields are preserved.
func TestMetricsRunID(t *testing.T) {
	data := []byte(linearChainJSONL(2))
	p := defaultParams()
	p.RunID = "my_run_123"
	p.Scenario = "latency_stress"

	m, err := metrics.ComputeFromData(data, p)
	if err != nil {
		t.Fatalf("error: %v", err)
	}

	if m.RunID != "my_run_123" {
		t.Errorf("expected RunID 'my_run_123', got %q", m.RunID)
	}
	if m.Scenario != "latency_stress" {
		t.Errorf("expected Scenario 'latency_stress', got %q", m.Scenario)
	}
}

// =============================================================================
// Regression Tests — Exact metric values against known event logs
// =============================================================================

// buildForkJSONL builds a JSONL log for a 2-node scenario with exactly one fork:
//
//	N0 proposes b1, b2a (winning), b3.
//	N1 proposes b2b (stale).
//	N1 reorgs from b2b → b2a at t=3.
//
// Expected: 4 total blocks, 1 fork episode, 1 stale block, 1 reorg, fork rate=0.25.
func buildForkJSONL() string {
	lines := []string{
		// b1 — both nodes agree
		`{"seq":1,"sim_time":1.0,"event_type":"BlockPropose","node_id":"N0","block_hash":"b1","block_height":1,"proposer_id":"N0","parent_hash":"genesis","tip_hash":"b1","tip_height":1,"tx_count":5}`,
		`{"seq":2,"sim_time":1.1,"event_type":"BlockArrive","node_id":"N1","block_hash":"b1","block_height":1,"parent_hash":"genesis","tip_hash":"b1","tip_height":1,"tx_count":5}`,
		`{"seq":3,"sim_time":1.1,"event_type":"TipUpdate","node_id":"N1","block_hash":"b1","block_height":1,"tip_hash":"b1","tip_height":1,"old_tip_hash":"genesis","parent_hash":"genesis","reason":"extend"}`,
		// Fork at height 2: N0 proposes b2a, N1 proposes b2b simultaneously
		`{"seq":4,"sim_time":2.0,"event_type":"BlockPropose","node_id":"N0","block_hash":"b2a","block_height":2,"proposer_id":"N0","parent_hash":"b1","tip_hash":"b2a","tip_height":2,"tx_count":5}`,
		`{"seq":5,"sim_time":2.0,"event_type":"BlockPropose","node_id":"N1","block_hash":"b2b","block_height":2,"proposer_id":"N1","parent_hash":"b1","tip_hash":"b2b","tip_height":2,"tx_count":5}`,
		// N1 reorgs to b2a at t=3
		`{"seq":6,"sim_time":3.0,"event_type":"TipUpdate","node_id":"N1","block_hash":"b2a","block_height":2,"tip_hash":"b2a","tip_height":2,"old_tip_hash":"b2b","parent_hash":"b1","reason":"reorg"}`,
		// b3 — linear after convergence
		`{"seq":7,"sim_time":4.0,"event_type":"BlockPropose","node_id":"N0","block_hash":"b3","block_height":3,"proposer_id":"N0","parent_hash":"b2a","tip_hash":"b3","tip_height":3,"tx_count":5}`,
		`{"seq":8,"sim_time":4.1,"event_type":"BlockArrive","node_id":"N1","block_hash":"b3","block_height":3,"parent_hash":"b2a","tip_hash":"b3","tip_height":3,"tx_count":5}`,
		`{"seq":9,"sim_time":4.1,"event_type":"TipUpdate","node_id":"N1","block_hash":"b3","block_height":3,"tip_hash":"b3","tip_height":3,"old_tip_hash":"b2a","parent_hash":"b2a","reason":"extend"}`,
	}
	result := ""
	for _, l := range lines {
		result += l + "\n"
	}
	return result
}

// TestMetricsForkLog_ExactCounts verifies that exact counts (fork episodes,
// stale blocks, reorgs, rates) are correct for a manually constructed event log.
func TestMetricsForkLog_ExactCounts(t *testing.T) {
	data := []byte(buildForkJSONL())
	p := defaultParams()
	p.NumNodes = 2
	p.SimDuration = 5.0
	p.SettledDuration = 5.0

	m, err := metrics.ComputeFromData(data, p)
	if err != nil {
		t.Fatalf("ComputeFromData error: %v", err)
	}

	if m.TotalBlocks != 4 {
		t.Errorf("expected 4 total blocks, got %d", m.TotalBlocks)
	}
	if m.ForkEpisodes != 1 {
		t.Errorf("expected 1 fork episode, got %d", m.ForkEpisodes)
	}
	if m.StaleBlocks != 1 {
		t.Errorf("expected 1 stale block (b2b), got %d", m.StaleBlocks)
	}
	if m.ReorgCount != 1 {
		t.Errorf("expected 1 reorg, got %d", m.ReorgCount)
	}
	expectedForkRate := 1.0 / 4.0
	if !approxEqual(m.ForkRate, expectedForkRate, 0.001) {
		t.Errorf("expected fork rate %.4f, got %.6f", expectedForkRate, m.ForkRate)
	}
	expectedStaleRate := 1.0 / 4.0
	if !approxEqual(m.StaleRate, expectedStaleRate, 0.001) {
		t.Errorf("expected stale rate %.4f, got %.6f", expectedStaleRate, m.StaleRate)
	}
}

// TestMetricsGoodput_ExcludesStale verifies that goodput excludes tx from stale blocks.
//
// Setup: 4 total blocks, 1 stale (b2b, tx_count=5). 3 on-chain (b1, b2a, b3, each 5 tx).
// Total on-chain tx = 15; simDuration = 5.0s; goodput = 15/5 = 3.0.
// Raw TPS = 20/5 = 4.0 (includes stale block).
func TestMetricsGoodput_ExcludesStale(t *testing.T) {
	data := []byte(buildForkJSONL())
	p := defaultParams()
	p.NumNodes = 2
	p.SimDuration = 5.0
	p.SettledDuration = 5.0

	m, err := metrics.ComputeFromData(data, p)
	if err != nil {
		t.Fatalf("ComputeFromData error: %v", err)
	}

	expectedRaw := 20.0 / 5.0 // 4 blocks × 5 tx ÷ 5s
	if !approxEqual(m.RawTPS, expectedRaw, 0.1) {
		t.Errorf("expected raw TPS %.2f, got %.4f", expectedRaw, m.RawTPS)
	}
	expectedGoodput := 15.0 / 5.0 // 3 on-chain blocks × 5 tx ÷ 5s
	if !approxEqual(m.Goodput, expectedGoodput, 0.1) {
		t.Errorf("expected goodput %.2f, got %.4f", expectedGoodput, m.Goodput)
	}
	if m.Goodput >= m.RawTPS {
		t.Errorf("goodput (%.4f) should be less than raw TPS (%.4f) when stale blocks exist",
			m.Goodput, m.RawTPS)
	}
}

// TestMetricsReorgDepth_ExactValue verifies MeanReorgDepth and MaxReorgDepth
// for a single reorg where the depth is known.
//
// N0 is at b2a (height 2); switches to b2b (height 2). Common ancestor = b1 (height 1).
// depth = 2 - 1 = 1.
func TestMetricsReorgDepth_ExactValue(t *testing.T) {
	data := []byte(buildForkJSONL())
	p := defaultParams()
	p.NumNodes = 2
	p.SimDuration = 5.0
	p.SettledDuration = 5.0

	m, err := metrics.ComputeFromData(data, p)
	if err != nil {
		t.Fatalf("ComputeFromData error: %v", err)
	}

	if !approxEqual(m.MeanReorgDepth, 1.0, 0.001) {
		t.Errorf("expected mean reorg depth 1.0, got %.4f", m.MeanReorgDepth)
	}
	if m.MaxReorgDepth != 1 {
		t.Errorf("expected max reorg depth 1, got %d", m.MaxReorgDepth)
	}
}

// TestMetricsZeroEvents verifies that an empty log returns zero metrics without errors.
func TestMetricsZeroEvents(t *testing.T) {
	p := defaultParams()
	p.SimDuration = 10.0
	p.SettledDuration = 10.0

	m, err := metrics.ComputeFromData([]byte(""), p)
	if err != nil {
		t.Fatalf("error on empty log: %v", err)
	}
	if m.ForkRate != 0 {
		t.Errorf("expected fork rate 0, got %v", m.ForkRate)
	}
	if m.StaleRate != 0 {
		t.Errorf("expected stale rate 0, got %v", m.StaleRate)
	}
	if m.TotalBlocks != 0 {
		t.Errorf("expected 0 total blocks, got %d", m.TotalBlocks)
	}
	if m.RawTPS != 0 {
		t.Errorf("expected raw TPS 0, got %v", m.RawTPS)
	}
}

// TestMetricsWarmUp_ExcludesEarlyBlocks verifies that blocks at height ≤ warmup
// are excluded from TotalBlocks and fork detection.
//
// JSONL has 3 blocks at heights 1, 2, 3. With warm_up_blocks=2, only block at
// height 3 is counted → TotalBlocks=1.
func TestMetricsWarmUp_ExcludesEarlyBlocks(t *testing.T) {
	data := []byte(linearChainJSONL(3))
	p := defaultParams()
	p.WarmUpBlocks = 2
	p.SimDuration = 4.0
	p.SettledDuration = 4.0

	m, err := metrics.ComputeFromData(data, p)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	// Only block at height 3 is post-warm-up.
	if m.TotalBlocks != 1 {
		t.Errorf("expected 1 total block post-warm-up, got %d", m.TotalBlocks)
	}
	if m.ForkRate != 0 {
		t.Errorf("expected fork rate 0 (no forks), got %v", m.ForkRate)
	}
}

// TestPropagationTime_AttachedDelayedBlock verifies that a successfully attached block
// calculates propagation delay accurately.
func TestPropagationTime_AttachedDelayedBlock(t *testing.T) {
	log := `{"seq":1,"sim_time":1.0,"event_type":"BlockPropose","node_id":"N0","block_hash":"b1","block_height":1,"proposal_index":1,"proposer_id":"N0","parent_hash":"genesis","tx_count":10}
{"seq":2,"sim_time":1.350,"event_type":"BlockArrive","node_id":"N1","block_hash":"b1","block_height":1,"parent_hash":"genesis","tx_count":10}
`
	p := defaultParams()
	p.WarmUpBlocks = 0
	p.SimDuration = 2.0
	p.SettledDuration = 2.0

	m, err := metrics.ComputeFromData([]byte(log), p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !approxEqual(m.PropTimeMeanSec, 0.350, 1e-4) {
		t.Errorf("expected prop time 0.350s, got %f", m.PropTimeMeanSec)
	}
	if !approxEqual(m.PropTimeP95Sec, 0.350, 1e-4) {
		t.Errorf("expected p95 prop time 0.350s, got %f", m.PropTimeP95Sec)
	}
}

// TestPropagationTime_OrphanDelayedBlock verifies that an arriving block with an unknown parent
// (logged as BlockOrphan) is correctly included in physical propagation time measurement.
func TestPropagationTime_OrphanDelayedBlock(t *testing.T) {
	log := `{"seq":1,"sim_time":1.0,"event_type":"BlockPropose","node_id":"N0","block_hash":"b2","block_height":2,"proposal_index":1,"proposer_id":"N0","parent_hash":"b1","tx_count":10}
{"seq":2,"sim_time":1.420,"event_type":"BlockOrphan","node_id":"N1","block_hash":"b2","block_height":2,"parent_hash":"b1","reason":"parent_unknown"}
`
	p := defaultParams()
	p.WarmUpBlocks = 0
	p.SimDuration = 2.0
	p.SettledDuration = 2.0

	m, err := metrics.ComputeFromData([]byte(log), p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !approxEqual(m.PropTimeMeanSec, 0.420, 1e-4) {
		t.Errorf("expected orphan prop time 0.420s, got %f", m.PropTimeMeanSec)
	}
}

// TestPropagationTime_NoEligibleObservations verifies that when a run has no eligible
// arrival observations, the metric reports as unavailable (-1.0) rather than numerical zero.
func TestPropagationTime_NoEligibleObservations(t *testing.T) {
	log := `{"seq":1,"sim_time":1.0,"event_type":"BlockPropose","node_id":"N0","block_hash":"b1","block_height":1,"proposal_index":1,"proposer_id":"N0","parent_hash":"genesis","tx_count":10}
`
	p := defaultParams()
	p.WarmUpBlocks = 0
	p.SimDuration = 2.0
	p.SettledDuration = 2.0

	m, err := metrics.ComputeFromData([]byte(log), p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.PropTimeMeanSec != -1.0 {
		t.Errorf("expected PropTimeMeanSec = -1.0 (unavailable), got %f", m.PropTimeMeanSec)
	}
	if m.PropTimeP95Sec != -1.0 {
		t.Errorf("expected PropTimeP95Sec = -1.0 (unavailable), got %f", m.PropTimeP95Sec)
	}
}

// TestPropagationTime_MeanAndP95Delay verifies the calculation of mean and p95 across
// multiple arrival delays.
func TestPropagationTime_MeanAndP95Delay(t *testing.T) {
	// Propose 20 blocks with known delays: 0.10, 0.11, ..., 0.29 sec
	var sb strings.Builder
	for i := 1; i <= 20; i++ {
		h := itoa(int64(i))
		pt := float64(i)
		arrTime := pt + 0.10 + float64(i-1)*0.01 // delays from 0.10 to 0.29
		sb.WriteString(`{"seq":` + itoa(int64(i*2)) + `,"sim_time":` + ftoa(pt) + `,"event_type":"BlockPropose","node_id":"N0","block_hash":"b` + h + `","block_height":` + h + `,"proposal_index":` + h + `,"proposer_id":"N0","parent_hash":"genesis","tx_count":10}` + "\n")
		sb.WriteString(`{"seq":` + itoa(int64(i*2+1)) + `,"sim_time":` + ftoa(arrTime) + `,"event_type":"BlockArrive","node_id":"N1","block_hash":"b` + h + `","block_height":` + h + `,"parent_hash":"genesis","tx_count":10}` + "\n")
	}

	p := defaultParams()
	p.WarmUpBlocks = 0
	p.SimDuration = 25.0
	p.SettledDuration = 25.0

	m, err := metrics.ComputeFromData([]byte(sb.String()), p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Expected mean delay = (0.10 + 0.29) / 2 = 0.195
	if !approxEqual(m.PropTimeMeanSec, 0.195, 1e-3) {
		t.Errorf("expected mean delay 0.195, got %f", m.PropTimeMeanSec)
	}
	// Expected p95 delay ≈ 0.2805
	if m.PropTimeP95Sec < 0.27 || m.PropTimeP95Sec > 0.295 {
		t.Errorf("expected p95 delay around 0.28, got %f", m.PropTimeP95Sec)
	}
}

// TestPropagationTime_WarmUpBoundary verifies that proposals occurring before the
// warm-up boundary are excluded from propagation statistics, even if their arrivals
// occur after the warm-up boundary.
func TestPropagationTime_WarmUpBoundary(t *testing.T) {
	log := `{"seq":1,"sim_time":1.0,"event_type":"BlockPropose","node_id":"N0","block_hash":"b1","block_height":1,"proposal_index":1,"proposer_id":"N0","parent_hash":"genesis","tx_count":10}
{"seq":2,"sim_time":1.8,"event_type":"BlockArrive","node_id":"N1","block_hash":"b1","block_height":1,"parent_hash":"genesis","tx_count":10}
{"seq":3,"sim_time":2.0,"event_type":"BlockPropose","node_id":"N0","block_hash":"b2","block_height":2,"proposal_index":2,"proposer_id":"N0","parent_hash":"b1","tx_count":10}
{"seq":4,"sim_time":2.1,"event_type":"BlockArrive","node_id":"N1","block_hash":"b2","block_height":2,"parent_hash":"b1","tx_count":10}
`
	p := defaultParams()
	p.WarmUpBlocks = 1 // b1 is warmup (proposal_index 1 <= 1)
	p.SimDuration = 3.0
	p.SettledDuration = 3.0

	m, err := metrics.ComputeFromData([]byte(log), p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Only b2 (post-warmup) has delay 2.1 - 2.0 = 0.1s. b1's 0.8s must be excluded.
	if !approxEqual(m.PropTimeMeanSec, 0.100, 1e-4) {
		t.Errorf("expected only post-warmup prop time 0.100s, got %f", m.PropTimeMeanSec)
	}
}

// TestThroughput_HandConstructedConservation tests raw TPS, finalized goodput,
// warm-up measurement interval, and transaction conservation against a known log.
func TestThroughput_HandConstructedConservation(t *testing.T) {
	// 5 proposals, 1 per second (t=1, 2, 3, 4, 5).
	// WarmUpBlocks = 2 (proposals 1 and 2 discarded).
	// Post-warmup proposals: 3, 4, 5 (total 3 blocks).
	// Proposal 3 has 10 txs (offered_tx: 30).
	// Proposal 4 has 10 txs (offered_tx: 40, becomes stale block).
	// Proposal 5 has 10 txs (offered_tx: 50, extends 3).
	// Post-warmup duration: t=5.0 - t=2.0 = 3.0s.
	// Offered txs post-warmup = 50 - 20 = 30 txs.
	// Proposed txs post-warmup = 30 txs.
	// Finalized txs post-warmup = 20 txs (proposal 4 is stale).
	// Expected OfferedTPS = RawTPS = 30 / 3.0 = 10.0 tx/s.
	// Expected ProcessedTPS = 30 / 3.0 = 10.0 tx/s.
	// Expected Goodput = 20 / 3.0 = 6.6667 tx/s.
	log := `{"seq":1,"sim_time":1.0,"event_type":"BlockPropose","node_id":"N0","block_hash":"b1","block_height":1,"proposal_index":1,"proposer_id":"N0","parent_hash":"genesis","tx_count":10,"offered_tx":10}
{"seq":2,"sim_time":1.1,"event_type":"BlockArrive","node_id":"N1","block_hash":"b1","block_height":1,"parent_hash":"genesis","tx_count":10}
{"seq":3,"sim_time":2.0,"event_type":"BlockPropose","node_id":"N0","block_hash":"b2","block_height":2,"proposal_index":2,"proposer_id":"N0","parent_hash":"b1","tx_count":10,"offered_tx":20}
{"seq":4,"sim_time":2.1,"event_type":"BlockArrive","node_id":"N1","block_hash":"b2","block_height":2,"parent_hash":"b1","tx_count":10}
{"seq":5,"sim_time":3.0,"event_type":"BlockPropose","node_id":"N0","block_hash":"b3","block_height":3,"proposal_index":3,"proposer_id":"N0","parent_hash":"b2","tx_count":10,"offered_tx":30}
{"seq":6,"sim_time":3.1,"event_type":"BlockArrive","node_id":"N1","block_hash":"b3","block_height":3,"parent_hash":"b2","tx_count":10}
{"seq":7,"sim_time":4.0,"event_type":"BlockPropose","node_id":"N1","block_hash":"b4_stale","block_height":4,"proposal_index":4,"proposer_id":"N1","parent_hash":"b3","tx_count":10,"offered_tx":40}
{"seq":8,"sim_time":5.0,"event_type":"BlockPropose","node_id":"N0","block_hash":"b4_main","block_height":4,"proposal_index":5,"proposer_id":"N0","parent_hash":"b3","tx_count":10,"offered_tx":50}
{"seq":9,"sim_time":5.1,"event_type":"BlockArrive","node_id":"N1","block_hash":"b4_main","block_height":4,"parent_hash":"b3","tx_count":10}
{"seq":10,"sim_time":5.1,"event_type":"TipUpdate","node_id":"N1","block_hash":"b4_main","block_height":4,"tip_hash":"b4_main","tip_height":4,"parent_hash":"b3","reason":"reorg"}
`
	p := defaultParams()
	p.WarmUpBlocks = 2
	p.SimDuration = 5.0
	p.SettledDuration = 5.0

	m, err := metrics.ComputeFromData([]byte(log), p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if m.TotalBlocks != 3 {
		t.Errorf("expected 3 total blocks post-warmup, got %d", m.TotalBlocks)
	}
	if !approxEqual(m.OfferedTPS, 10.0, 1e-3) {
		t.Errorf("expected OfferedTPS = 10.0, got %f", m.OfferedTPS)
	}
	if !approxEqual(m.ProcessedTPS, 10.0, 1e-3) {
		t.Errorf("expected ProcessedTPS = 10.0, got %f", m.ProcessedTPS)
	}
	if !approxEqual(m.RawTPS, 10.0, 1e-3) {
		t.Errorf("expected RawTPS = 10.0, got %f", m.RawTPS)
	}
	if !approxEqual(m.Goodput, 20.0/3.0, 1e-3) {
		t.Errorf("expected Goodput = 6.6667, got %f", m.Goodput)
	}
	if m.Goodput >= m.RawTPS {
		t.Errorf("invariant violated: Goodput (%f) >= RawTPS (%f)", m.Goodput, m.RawTPS)
	}
}

// TestPropagationTime_PerBlockMaxArrival tests that propagation time uses per-block last-node
// arrival minus proposal time (max_i(T_arr,i) - T_prop) across eligible blocks with differing
// numbers of receivers per block, including orphan arrivals.
func TestPropagationTime_PerBlockMaxArrival(t *testing.T) {
	// Block 1 (t=1.0): 3 receivers at t=1.1, 1.2, 1.3 -> max delay = 0.3s
	// Block 2 (t=2.0): 1 receiver at t=2.5            -> max delay = 0.5s
	// Block 3 (t=3.0): 2 receivers, one orphan at t=3.4, one arrival at t=3.2 -> max delay = 0.4s
	// Block 4 (t=4.0): 0 receivers (unreachable/dropped) -> excluded from delay sample
	// Post-warmup blocks with arrivals: 3 blocks (delays: 0.3, 0.5, 0.4)
	// Expected Mean Delay = (0.3 + 0.5 + 0.4) / 3 = 0.400s
	// Note: If pooled across all 6 arrival events (0.1, 0.2, 0.3, 0.5, 0.4, 0.2), mean would be 1.7 / 6 = 0.283s.
	// Per-block max calculation must produce exactly 0.400s!
	log := `{"seq":1,"sim_time":1.0,"event_type":"BlockPropose","node_id":"N0","block_hash":"b1","block_height":1,"proposal_index":1,"proposer_id":"N0","parent_hash":"genesis","tx_count":5}
{"seq":2,"sim_time":1.1,"event_type":"BlockArrive","node_id":"N1","block_hash":"b1","block_height":1,"parent_hash":"genesis"}
{"seq":3,"sim_time":1.2,"event_type":"BlockArrive","node_id":"N2","block_hash":"b1","block_height":1,"parent_hash":"genesis"}
{"seq":4,"sim_time":1.3,"event_type":"BlockArrive","node_id":"N3","block_hash":"b1","block_height":1,"parent_hash":"genesis"}
{"seq":5,"sim_time":2.0,"event_type":"BlockPropose","node_id":"N0","block_hash":"b2","block_height":2,"proposal_index":2,"proposer_id":"N0","parent_hash":"b1","tx_count":5}
{"seq":6,"sim_time":2.5,"event_type":"BlockArrive","node_id":"N1","block_hash":"b2","block_height":2,"parent_hash":"b1"}
{"seq":7,"sim_time":3.0,"event_type":"BlockPropose","node_id":"N0","block_hash":"b3","block_height":3,"proposal_index":3,"proposer_id":"N0","parent_hash":"b2","tx_count":5}
{"seq":8,"sim_time":3.2,"event_type":"BlockArrive","node_id":"N1","block_hash":"b3","block_height":3,"parent_hash":"b2"}
{"seq":9,"sim_time":3.4,"event_type":"BlockOrphan","node_id":"N2","block_hash":"b3","block_height":3,"parent_hash":"b2"}
{"seq":10,"sim_time":4.0,"event_type":"BlockPropose","node_id":"N0","block_hash":"b4","block_height":4,"proposal_index":4,"proposer_id":"N0","parent_hash":"b3","tx_count":5}
`
	p := defaultParams()
	p.WarmUpBlocks = 0
	p.SimDuration = 5.0
	p.SettledDuration = 5.0

	m, err := metrics.ComputeFromData([]byte(log), p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Expected mean = 0.400s
	if !approxEqual(m.PropTimeMeanSec, 0.400, 1e-4) {
		t.Errorf("expected per-block max propagation delay mean = 0.400, got %f (pooled would be 0.283)", m.PropTimeMeanSec)
	}
	// Sorted delays: [0.3, 0.4, 0.5]
	// p95 = sorted[1]*(0.1) + sorted[2]*(0.9) = 0.4*0.1 + 0.5*0.9 = 0.490
	if !approxEqual(m.PropTimeP95Sec, 0.490, 1e-3) {
		t.Errorf("expected p95 delay = 0.490, got %f", m.PropTimeP95Sec)
	}
}

// TestThroughput_OfferedVsProcessedVsGoodput tests transaction backlog, where offered TPS
// exceeds block capacity, ensuring distinct OfferedTPS, ProcessedTPS, and Goodput values.
func TestThroughput_OfferedVsProcessedVsGoodput(t *testing.T) {
	// Post-warmup proposals:
	// WarmUpBlocks = 1 (b1 proposal discarded)
	// b2 (t=2.0): 10 tx, offered_tx = 200
	// b3 (t=3.0): 10 tx, offered_tx = 300
	// b4_stale (t=4.0): 10 tx, offered_tx = 400 (stale)
	// b4_canon (t=5.0): 10 tx, offered_tx = 500 (canonical)
	// Post-warmup window: t=5.0 - t=1.0 = 4.0s
	// Offered in window: 500 - 100 = 400 tx -> OfferedTPS = 400 / 4.0 = 100.0 tx/s
	// Processed in window: 4 blocks * 10 tx = 40 tx -> ProcessedTPS = 40 / 4.0 = 10.0 tx/s
	// Finalized (canonical) in window: 3 blocks * 10 tx = 30 tx -> Goodput = 30 / 4.0 = 7.5 tx/s
	log := `{"seq":1,"sim_time":1.0,"event_type":"BlockPropose","node_id":"N0","block_hash":"b1","block_height":1,"proposal_index":1,"proposer_id":"N0","parent_hash":"genesis","tx_count":10,"offered_tx":100}
{"seq":2,"sim_time":1.1,"event_type":"BlockArrive","node_id":"N1","block_hash":"b1","block_height":1,"parent_hash":"genesis","tx_count":10}
{"seq":3,"sim_time":2.0,"event_type":"BlockPropose","node_id":"N0","block_hash":"b2","block_height":2,"proposal_index":2,"proposer_id":"N0","parent_hash":"b1","tx_count":10,"offered_tx":200}
{"seq":4,"sim_time":2.1,"event_type":"BlockArrive","node_id":"N1","block_hash":"b2","block_height":2,"parent_hash":"b1","tx_count":10}
{"seq":5,"sim_time":3.0,"event_type":"BlockPropose","node_id":"N0","block_hash":"b3","block_height":3,"proposal_index":3,"proposer_id":"N0","parent_hash":"b2","tx_count":10,"offered_tx":300}
{"seq":6,"sim_time":3.1,"event_type":"BlockArrive","node_id":"N1","block_hash":"b3","block_height":3,"parent_hash":"b2","tx_count":10}
{"seq":7,"sim_time":4.0,"event_type":"BlockPropose","node_id":"N1","block_hash":"b4_stale","block_height":4,"proposal_index":4,"proposer_id":"N1","parent_hash":"b3","tx_count":10,"offered_tx":400}
{"seq":8,"sim_time":5.0,"event_type":"BlockPropose","node_id":"N0","block_hash":"b4_canon","block_height":4,"proposal_index":5,"proposer_id":"N0","parent_hash":"b3","tx_count":10,"offered_tx":500}
{"seq":9,"sim_time":5.1,"event_type":"BlockArrive","node_id":"N1","block_hash":"b4_canon","block_height":4,"parent_hash":"b3","tx_count":10}
{"seq":10,"sim_time":5.1,"event_type":"TipUpdate","node_id":"N1","block_hash":"b4_canon","block_height":4,"tip_hash":"b4_canon","tip_height":4,"parent_hash":"b3","reason":"reorg"}
`
	p := defaultParams()
	p.WarmUpBlocks = 1
	p.SimDuration = 5.0
	p.SettledDuration = 5.0

	m, err := metrics.ComputeFromData([]byte(log), p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !approxEqual(m.OfferedTPS, 100.0, 1e-3) {
		t.Errorf("expected OfferedTPS = 100.0, got %f", m.OfferedTPS)
	}
	if !approxEqual(m.ProcessedTPS, 10.0, 1e-3) {
		t.Errorf("expected ProcessedTPS = 10.0, got %f", m.ProcessedTPS)
	}
	if !approxEqual(m.RawTPS, 100.0, 1e-3) {
		t.Errorf("expected RawTPS = 100.0, got %f", m.RawTPS)
	}
	if !approxEqual(m.Goodput, 7.5, 1e-3) {
		t.Errorf("expected Goodput = 7.5, got %f", m.Goodput)
	}
	if !(m.OfferedTPS > m.ProcessedTPS && m.ProcessedTPS > m.Goodput) {
		t.Errorf("expected OfferedTPS > ProcessedTPS > Goodput, got %f, %f, %f", m.OfferedTPS, m.ProcessedTPS, m.Goodput)
	}
}
