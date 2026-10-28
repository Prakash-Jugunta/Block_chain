// Package metrics computes run-level simulation metrics as defined in §7
// of CASE_STUDY_DEMO_PLAN.md.
//
// # Metric Definitions and Denominators
//
//   - ForkRate:          fork episodes ÷ total blocks (post warm-up)
//   - PropTimeMeanSec:   mean of (last-node arrival − proposal time) per block
//   - PropTimeP95Sec:    95th-percentile propagation time
//   - ConfLatencyMeanSec: mean time from proposal until the block is K-deep in all nodes' chains
//   - RawTPS:            total transactions produced ÷ simulated time
//   - Goodput:           finalized (non-stale) transactions ÷ simulated time
//   - StaleRate:         stale blocks ÷ total blocks (post warm-up)
//   - MeanReorgDepth:    average reorg depth (0 if no reorgs)
//   - MaxReorgDepth:     maximum reorg depth (0 if no reorgs)
//   - NodeViewConsistency: fraction of blocks where all nodes share the majority tip
//   - PeerAvailability:  time-weighted fraction of nodes that were online (1.0 if no churn)
//
// Edge cases:
//   - If total blocks = 0, all rate metrics return 0.
//   - If simulated time = 0, TPS metrics return 0.
//   - Propagation time is measured only for blocks seen by > 1 node.
//   - Confirmation latency requires K blocks after the subject block; blocks
//     near the end of the run may not be confirmable and are excluded.
package metrics

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	"github.com/pranav-k/forkrisk/internal/fork"
)

// RunMetrics holds all computed metrics for a single simulation run.
type RunMetrics struct {
	// Identifiers.
	RunID    string `json:"run_id"`
	Scenario string `json:"scenario"`
	Seed     int64  `json:"seed"`

	// Simulation parameters (for filtering and comparison).
	NumNodes         int     `json:"num_nodes"`
	BlockIntervalSec float64 `json:"block_interval_sec"`
	MeanLatencyMs    float64 `json:"mean_latency_ms"`
	MeanPropDelayMs  float64 `json:"mean_prop_delay_ms"`
	ChurnEnabled     bool    `json:"churn_enabled"`
	TxArrivalRate    float64 `json:"tx_arrival_rate"`

	// Fork metrics (§7).
	TotalBlocks    int     `json:"total_blocks"`
	ForkEpisodes   int     `json:"fork_episodes"`
	ForkRate       float64 `json:"fork_rate"`
	StaleBlocks    int     `json:"stale_blocks"`
	StaleRate      float64 `json:"stale_rate"`
	MeanReorgDepth float64 `json:"mean_reorg_depth"`
	MaxReorgDepth  int     `json:"max_reorg_depth"`
	ReorgCount     int     `json:"reorg_count"`

	// Propagation time (§7).
	PropTimeMeanSec float64 `json:"prop_time_mean_sec"`
	PropTimeP95Sec  float64 `json:"prop_time_p95_sec"`

	// Confirmation latency (§7).
	ConfLatencyMeanSec float64 `json:"conf_latency_mean_sec"`

	// Throughput (§7).
	OfferedTPS   float64 `json:"offered_tps"`
	ProcessedTPS float64 `json:"processed_tps"`
	RawTPS       float64 `json:"raw_tps"`
	Goodput      float64 `json:"goodput"`

	// Node-view consistency (§7).
	// Fraction of per-block snapshots where all nodes share the majority tip.
	NodeViewConsistency float64 `json:"node_view_consistency"`

	// Peer availability (§7).
	// Time-weighted fraction of uptime across all nodes.
	PeerAvailability float64 `json:"peer_availability"`

	// Run duration.
	SimDurationSec     float64 `json:"sim_duration_sec"`
	SettledDurationSec float64 `json:"settled_duration_sec"`
}

// logEvent is a minimal parsed log entry for metric computation.
type logEvent struct {
	Seq           int64   `json:"seq"`
	SimTime       float64 `json:"sim_time"`
	EventType     string  `json:"event_type"`
	NodeID        string  `json:"node_id"`
	BlockHash     string  `json:"block_hash,omitempty"`
	BlockHeight   int     `json:"block_height,omitempty"`
	ProposalIndex int     `json:"proposal_index,omitempty"`
	ProposerID    string  `json:"proposer_id,omitempty"`
	ParentHash    string  `json:"parent_hash,omitempty"`
	TipHash       string  `json:"tip_hash,omitempty"`
	TipHeight     int     `json:"tip_height,omitempty"`
	OldTipHash    string  `json:"old_tip_hash,omitempty"`
	Reason        string  `json:"reason,omitempty"`
	TxCount       int     `json:"tx_count,omitempty"`
	OfferedTx     int     `json:"offered_tx,omitempty"`
}

// ComputeParams holds the inputs needed to compute run metrics.
type ComputeParams struct {
	RunID           string
	Scenario        string
	Seed            int64
	NumNodes        int
	BlockInterval   float64
	MeanLatencyMs   float64
	MeanPropDelayMs float64
	ChurnEnabled    bool
	TxArrivalRate   float64
	WarmUpBlocks    int
	ConfirmDepth    int
	SimDuration     float64
	SettledDuration float64
}

// churnRecord stores a single peer availability state change.
type churnRecord struct {
	nodeID string
	time   float64
	online bool
}

// Compute reads a JSONL event log and computes all run-level metrics.
func Compute(logPath string, p ComputeParams) (RunMetrics, error) {
	data, err := os.ReadFile(logPath)
	if err != nil {
		return RunMetrics{}, fmt.Errorf("reading log %q: %w", logPath, err)
	}

	events, err := parseLogEvents(data)
	if err != nil {
		return RunMetrics{}, fmt.Errorf("parsing log: %w", err)
	}

	return computeFromEvents(events, p), nil
}

// ComputeFromData computes metrics from raw JSONL bytes (for testing).
func ComputeFromData(data []byte, p ComputeParams) (RunMetrics, error) {
	events, err := parseLogEvents(data)
	if err != nil {
		return RunMetrics{}, fmt.Errorf("parsing log data: %w", err)
	}
	return computeFromEvents(events, p), nil
}

func parseLogEvents(data []byte) ([]logEvent, error) {
	var events []logEvent
	start := 0
	for i, b := range data {
		if b == '\n' {
			line := data[start:i]
			start = i + 1
			if len(line) == 0 {
				continue
			}
			var ev logEvent
			if err := json.Unmarshal(line, &ev); err != nil {
				return nil, err
			}
			events = append(events, ev)
		}
	}
	if start < len(data) {
		line := data[start:]
		if len(line) > 0 {
			var ev logEvent
			if err := json.Unmarshal(line, &ev); err != nil {
				return nil, err
			}
			events = append(events, ev)
		}
	}
	return events, nil
}

func computeFromEvents(events []logEvent, p ComputeParams) RunMetrics {
	m := RunMetrics{
		RunID:              p.RunID,
		Scenario:           p.Scenario,
		Seed:               p.Seed,
		NumNodes:           p.NumNodes,
		BlockIntervalSec:   p.BlockInterval,
		MeanLatencyMs:      p.MeanLatencyMs,
		MeanPropDelayMs:    p.MeanPropDelayMs,
		ChurnEnabled:       p.ChurnEnabled,
		TxArrivalRate:      p.TxArrivalRate,
		SimDurationSec:     p.SimDuration,
		SettledDurationSec: p.SettledDuration,
		PeerAvailability:   1.0, // default: no churn = 100% uptime
	}

	// --- Step 1: Gather block metadata ---
	// proposalTime[hash] = sim_time when block was proposed.
	proposalTime := make(map[string]float64)
	// arrivalTimes[hash] = []sim_time of arrivals at each node.
	arrivalTimes := make(map[string][]float64)
	// blockTxCount[hash] = tx_count.
	blockTxCount := make(map[string]int)
	// blockHeight[hash] = height.
	blockHeight := make(map[string]int)
	// blockParent[hash] = parentHash.
	blockParent := make(map[string]string)
	// proposed blocks post-warm-up.
	proposedPostWarmup := make(map[string]bool)

	// For node-view consistency: per-block-proposal snapshot.
	// For each propose event, record the current tip of all nodes.
	nodeTips := make(map[string]string) // nodeID -> tipHash at latest update.
	type snapshot struct {
		blockHash string
		tips      map[string]string
	}
	var consistencySnapshots []snapshot

	var churnEvents []churnRecord
	// Initialize all nodes as online at t=0.
	nodeSet := make(map[string]bool)

	// For confirmation latency: confirmTime[hash] = simTime when block is K-deep.
	// We estimate: a block at height H is K-confirmed when we observe a proposal
	// or tip-update at height H+K.
	blockConfirmedAt := make(map[string]float64) // blockHash -> time of K-confirmation
	proposedAtHeight := make(map[int][]string)

	// Total txs on final chain (for goodput).
	// We accumulate tx counts of all proposed post-warm-up blocks and subtract stale ones.
	totalProposedTx := 0
	totalFinalisedTx := 0 // computed after stale detection.

	proposeCount := 0
	warmUpEndTime := -1.0
	lastProposeTime := -1.0
	warmUpOfferedTx := 0
	finalOfferedTx := 0

	for _, ev := range events {
		// Track all node IDs.
		if ev.NodeID != "" {
			nodeSet[ev.NodeID] = true
		}

		switch ev.EventType {
		case "BlockPropose":
			proposeCount++
			isPostWarmup := false
			if ev.ProposalIndex > 0 {
				isPostWarmup = ev.ProposalIndex > p.WarmUpBlocks
			} else {
				isPostWarmup = proposeCount > p.WarmUpBlocks
			}

			if isPostWarmup {
				lastProposeTime = ev.SimTime
				finalOfferedTx = ev.OfferedTx
				proposalTime[ev.BlockHash] = ev.SimTime
				blockTxCount[ev.BlockHash] = ev.TxCount
				blockHeight[ev.BlockHash] = ev.BlockHeight
				proposedAtHeight[ev.BlockHeight] = append(proposedAtHeight[ev.BlockHeight], ev.BlockHash)
				blockParent[ev.BlockHash] = ev.ParentHash
				proposedPostWarmup[ev.BlockHash] = true
				totalProposedTx += ev.TxCount
				// Proposer's tip update.
				nodeTips[ev.NodeID] = ev.BlockHash
				// Snapshot at proposal time.
				snap := make(map[string]string, len(nodeTips))
				for k, v := range nodeTips {
					snap[k] = v
				}
				consistencySnapshots = append(consistencySnapshots, snapshot{
					blockHash: ev.BlockHash,
					tips:      snap,
				})
			} else {
				warmUpEndTime = ev.SimTime
				warmUpOfferedTx = ev.OfferedTx
				nodeTips[ev.NodeID] = ev.BlockHash
			}

		case "BlockArrive", "BlockOrphan":
			// Both BlockArrive (attached) and BlockOrphan (buffered orphan) represent
			// physical reception of the block at the node.
			if ev.BlockHash != "" {
				arrivalTimes[ev.BlockHash] = append(arrivalTimes[ev.BlockHash], ev.SimTime)
				if ev.BlockHeight > 0 {
					blockHeight[ev.BlockHash] = ev.BlockHeight
					blockParent[ev.BlockHash] = ev.ParentHash
				}
			}
			if ev.TipHash != "" {
				nodeTips[ev.NodeID] = ev.TipHash
			}

		case "TipUpdate":
			if ev.TipHash != "" {
				nodeTips[ev.NodeID] = ev.TipHash
				if ev.BlockHeight > 0 {
					blockHeight[ev.BlockHash] = ev.BlockHeight
					blockParent[ev.BlockHash] = ev.ParentHash
				}
			}

		case "ChurnToggle":
			online := ev.Reason == "online"
			churnEvents = append(churnEvents, churnRecord{
				nodeID: ev.NodeID,
				time:   ev.SimTime,
				online: online,
			})
		}

		// Confirmation latency: when we see a block at height H+confirmDepth,
		// mark all blocks at height H as confirmed at this event's time.
		if ev.BlockHeight > p.WarmUpBlocks+p.ConfirmDepth {
			targetHeight := ev.BlockHeight - p.ConfirmDepth
			// Check if any proposed block at targetHeight is not yet confirmed.
			for _, hash := range proposedAtHeight[targetHeight] {
				if _, already := blockConfirmedAt[hash]; !already {
					blockConfirmedAt[hash] = ev.SimTime
				}
			}
		}
	}

	// --- Step 2: Fork detection ---
	forkEvents := toForkEvents(events)
	nodeIDs := sortedKeys(nodeSet)
	if p.NumNodes > 0 && len(nodeIDs) < p.NumNodes {
		// Supplement with synthesised node IDs when the log lacks events for some nodes
		// (e.g. nodes that were offline the entire run).
		existing := make(map[string]bool, len(nodeIDs))
		for _, id := range nodeIDs {
			existing[id] = true
		}
		for i := 0; i < p.NumNodes; i++ {
			id := fmt.Sprintf("N%d", i)
			if !existing[id] {
				nodeIDs = append(nodeIDs, id)
				nodeSet[id] = true
			}
		}
		sort.Strings(nodeIDs)
	}
	detector := fork.NewDetector(nodeIDs, p.WarmUpBlocks)
	forkResult := detector.Analyze(forkEvents)

	m.TotalBlocks = forkResult.TotalBlocks
	m.ForkEpisodes = len(forkResult.ForkEpisodes)
	m.ForkRate = forkResult.ForkRate()
	m.StaleBlocks = len(forkResult.StaleBlocks)
	m.StaleRate = forkResult.StaleRate()
	m.MeanReorgDepth = forkResult.MeanReorgDepth()
	m.MaxReorgDepth = forkResult.MaxReorgDepth()
	m.ReorgCount = len(forkResult.Reorganizations)

	// Build stale set for goodput.
	staleSet := make(map[string]bool)
	for _, s := range forkResult.StaleBlocks {
		staleSet[s.Hash] = true
	}

	// Compute finalised tx count.
	for hash := range proposedPostWarmup {
		if !staleSet[hash] {
			totalFinalisedTx += blockTxCount[hash]
		}
	}
	_ = totalFinalisedTx // used below

	// --- Step 3: Propagation time ---
	// Propagation time for block B is calculated as last-node arrival minus proposal time
	// (max_i(T_arr,i) - T_prop) across eligible post-warm-up blocks with recorded arrivals,
	// including recorded orphaned arrivals.
	var blockPropDelays []float64
	for hash := range proposedPostWarmup {
		pt, ok := proposalTime[hash]
		if !ok {
			continue
		}
		arrivals := arrivalTimes[hash]
		if len(arrivals) == 0 {
			continue
		}
		maxArr := -1.0
		hasValidArrival := false
		for _, a := range arrivals {
			if a >= pt {
				if !hasValidArrival || a > maxArr {
					maxArr = a
					hasValidArrival = true
				}
			}
		}
		if hasValidArrival {
			blockPropDelays = append(blockPropDelays, maxArr-pt)
		}
	}
	if len(blockPropDelays) > 0 {
		m.PropTimeMeanSec = mean(blockPropDelays)
		m.PropTimeP95Sec = percentile(blockPropDelays, 95)
	} else {
		m.PropTimeMeanSec = -1.0 // unavailable
		m.PropTimeP95Sec = -1.0  // unavailable
	}

	// --- Step 4: Confirmation latency ---
	var confLatencies []float64
	for hash, pt := range proposalTime {
		if ct, ok := blockConfirmedAt[hash]; ok {
			lat := ct - pt
			if lat >= 0 {
				confLatencies = append(confLatencies, lat)
			}
		}
	}
	if len(confLatencies) > 0 {
		m.ConfLatencyMeanSec = mean(confLatencies)
	}

	// --- Step 5: TPS / Goodput ---
	// Measurement duration is the post-warm-up proposal window.
	duration := p.SettledDuration
	if duration <= 0 {
		duration = p.SimDuration
	}
	if duration <= 0 && lastProposeTime > 0 {
		duration = lastProposeTime
	}

	measurementDuration := duration
	if p.WarmUpBlocks > 0 && warmUpEndTime >= 0 && duration > warmUpEndTime {
		measurementDuration = duration - warmUpEndTime
	}

	if measurementDuration > 0 {
		offeredInWindow := float64(finalOfferedTx - warmUpOfferedTx)
		if totalProposedTx == 0 && finalOfferedTx == 0 {
			offeredInWindow = 0
		} else if finalOfferedTx == 0 || offeredInWindow <= 0 {
			if p.TxArrivalRate > 0 && finalOfferedTx > 0 {
				offeredInWindow = p.TxArrivalRate * measurementDuration
			} else {
				offeredInWindow = float64(totalProposedTx)
			}
		}
		m.OfferedTPS = offeredInWindow / measurementDuration
		m.ProcessedTPS = float64(totalProposedTx) / measurementDuration
		m.RawTPS = m.OfferedTPS // Raw TPS: total offered/generated transactions ÷ measurement duration
		m.Goodput = float64(totalFinalisedTx) / measurementDuration
	}

	// --- Step 6: Node-view consistency ---
	// For each snapshot (taken at BlockPropose events post-warm-up), compute the
	// fraction of ALL configured nodes that share the majority tip.
	// Nodes not yet present in nodeTips at snapshot time are treated as having
	// the genesis (empty string) tip — they are counted as divergent.
	if len(consistencySnapshots) > 0 {
		totalConsistency := 0.0
		for _, snap := range consistencySnapshots {
			tipCount := make(map[string]int)
			// Count every configured node.
			for _, nid := range nodeIDs {
				tip := snap.tips[nid] // empty string for nodes not yet in the map
				tipCount[tip]++
			}
			total := len(nodeIDs)
			if total == 0 {
				continue
			}
			maxC := 0
			for _, c := range tipCount {
				if c > maxC {
					maxC = c
				}
			}
			totalConsistency += float64(maxC) / float64(total)
		}
		m.NodeViewConsistency = totalConsistency / float64(len(consistencySnapshots))
	}

	// --- Step 7: Peer availability ---
	totalRunTime := p.SettledDuration
	if totalRunTime <= 0 {
		totalRunTime = p.SimDuration
	}
	if p.ChurnEnabled && len(churnEvents) > 0 {
		m.PeerAvailability = computePeerAvailability(nodeIDs, churnEvents, totalRunTime)
	}

	return m
}

// computePeerAvailability computes the time-weighted fraction of uptime.
// All nodes start online at t=0. Each churn event toggles a node's state.
func computePeerAvailability(nodeIDs []string, churnEvents []churnRecord, totalTime float64) float64 {
	if totalTime <= 0 || len(nodeIDs) == 0 {
		return 1.0
	}

	// Track the last toggle time and state for each node.
	lastTime := make(map[string]float64)
	isOnline := make(map[string]bool)
	onlineTime := make(map[string]float64)

	for _, id := range nodeIDs {
		lastTime[id] = 0.0
		isOnline[id] = true // all start online
		onlineTime[id] = 0.0
	}

	for _, ev := range churnEvents {
		prev := lastTime[ev.nodeID]
		if isOnline[ev.nodeID] {
			onlineTime[ev.nodeID] += ev.time - prev
		}
		lastTime[ev.nodeID] = ev.time
		isOnline[ev.nodeID] = ev.online
	}

	// Accumulate final segment up to totalTime.
	for _, id := range nodeIDs {
		if isOnline[id] {
			onlineTime[id] += totalTime - lastTime[id]
		}
	}

	// Compute average across nodes.
	total := 0.0
	for _, id := range nodeIDs {
		frac := onlineTime[id] / totalTime
		if frac > 1.0 {
			frac = 1.0
		}
		total += frac
	}
	return total / float64(len(nodeIDs))
}

// toForkEvents converts logEvents to fork.Events for the detector.
func toForkEvents(events []logEvent) []fork.Event {
	result := make([]fork.Event, len(events))
	for i, ev := range events {
		result[i] = fork.Event{
			Seq:           ev.Seq,
			SimTime:       ev.SimTime,
			EventType:     ev.EventType,
			NodeID:        ev.NodeID,
			BlockHash:     ev.BlockHash,
			BlockHeight:   ev.BlockHeight,
			ProposalIndex: ev.ProposalIndex,
			ProposerID:    ev.ProposerID,
			ParentHash:    ev.ParentHash,
			TipHash:       ev.TipHash,
			TipHeight:     ev.TipHeight,
			OldTipHash:    ev.OldTipHash,
			Reason:        ev.Reason,
			TxCount:       ev.TxCount,
		}
	}
	return result
}

// mean returns the arithmetic mean of a slice. Returns 0 for empty input.
func mean(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	sum := 0.0
	for _, x := range xs {
		sum += x
	}
	return sum / float64(len(xs))
}

// percentile returns the p-th percentile of xs using linear interpolation.
// p should be in [0, 100]. Returns 0 for empty input.
func percentile(xs []float64, p float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	sorted := make([]float64, len(xs))
	copy(sorted, xs)
	sort.Float64s(sorted)

	if p <= 0 {
		return sorted[0]
	}
	if p >= 100 {
		return sorted[len(sorted)-1]
	}

	rank := (p / 100.0) * float64(len(sorted)-1)
	lo := int(math.Floor(rank))
	hi := lo + 1
	if hi >= len(sorted) {
		return sorted[len(sorted)-1]
	}
	frac := rank - float64(lo)
	return sorted[lo]*(1-frac) + sorted[hi]*frac
}

// sortedKeys returns sorted keys of a string bool map.
func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// CSVHeader returns the ordered column names for summary.csv.
func CSVHeader() []string {
	return []string{
		"run_id", "scenario", "seed",
		"num_nodes", "block_interval_sec", "mean_latency_ms", "mean_prop_delay_ms",
		"churn_enabled", "tx_arrival_rate",
		"total_blocks", "fork_episodes", "fork_rate",
		"stale_blocks", "stale_rate",
		"reorg_count", "mean_reorg_depth", "max_reorg_depth",
		"prop_time_mean_sec", "prop_time_p95_sec",
		"conf_latency_mean_sec",
		"offered_tps", "processed_tps", "raw_tps", "goodput",
		"node_view_consistency",
		"peer_availability",
		"sim_duration_sec", "settled_duration_sec",
	}
}

// CSVRow converts a RunMetrics to an ordered slice of string values.
func CSVRow(m RunMetrics) []string {
	propMeanStr := "N/A"
	if m.PropTimeMeanSec >= 0 {
		propMeanStr = fmt.Sprintf("%.6f", m.PropTimeMeanSec)
	}
	propP95Str := "N/A"
	if m.PropTimeP95Sec >= 0 {
		propP95Str = fmt.Sprintf("%.6f", m.PropTimeP95Sec)
	}

	return []string{
		m.RunID,
		m.Scenario,
		strconv.FormatInt(m.Seed, 10),
		strconv.Itoa(m.NumNodes),
		fmt.Sprintf("%.4f", m.BlockIntervalSec),
		fmt.Sprintf("%.2f", m.MeanLatencyMs),
		fmt.Sprintf("%.2f", m.MeanPropDelayMs),
		strconv.FormatBool(m.ChurnEnabled),
		fmt.Sprintf("%.4f", m.TxArrivalRate),
		strconv.Itoa(m.TotalBlocks),
		strconv.Itoa(m.ForkEpisodes),
		fmt.Sprintf("%.6f", m.ForkRate),
		strconv.Itoa(m.StaleBlocks),
		fmt.Sprintf("%.6f", m.StaleRate),
		strconv.Itoa(m.ReorgCount),
		fmt.Sprintf("%.4f", m.MeanReorgDepth),
		strconv.Itoa(m.MaxReorgDepth),
		propMeanStr,
		propP95Str,
		fmt.Sprintf("%.6f", m.ConfLatencyMeanSec),
		fmt.Sprintf("%.6f", m.OfferedTPS),
		fmt.Sprintf("%.6f", m.ProcessedTPS),
		fmt.Sprintf("%.6f", m.RawTPS),
		fmt.Sprintf("%.6f", m.Goodput),
		fmt.Sprintf("%.6f", m.NodeViewConsistency),
		fmt.Sprintf("%.6f", m.PeerAvailability),
		fmt.Sprintf("%.4f", m.SimDurationSec),
		fmt.Sprintf("%.4f", m.SettledDurationSec),
	}
}

// AppendCSVRow opens (or creates) the summary CSV and appends one metrics row.
// If the file does not exist, a header row is written first.
func AppendCSVRow(csvPath string, m RunMetrics) error {
	if err := os.MkdirAll(filepath.Dir(csvPath), 0755); err != nil {
		return fmt.Errorf("creating csv dir: %w", err)
	}

	// Check if file exists to decide whether to write header.
	writeHeader := false
	if _, err := os.Stat(csvPath); os.IsNotExist(err) {
		writeHeader = true
	}

	f, err := os.OpenFile(csvPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("opening csv %q: %w", csvPath, err)
	}
	defer f.Close()

	w := csv.NewWriter(f)
	if writeHeader {
		if err := w.Write(CSVHeader()); err != nil {
			return fmt.Errorf("writing csv header: %w", err)
		}
	}
	if err := w.Write(CSVRow(m)); err != nil {
		return fmt.Errorf("writing csv row: %w", err)
	}
	w.Flush()
	return w.Error()
}
