// Package engine implements the single-threaded discrete-event simulation loop.
//
// How it works:
//  1. The engine holds a min-heap priority queue of Events.
//  2. Every action (block proposal, block propagation, block arrival) is an Event
//     scheduled at a future simulated time.
//  3. The main loop pops the earliest event and dispatches it.
//  4. Handlers may schedule more events (e.g. a block arrival triggers gossip to peers).
//  5. The loop stops when the heap is empty or the stop condition is reached.
//
// Because everything is single-threaded and deterministic (seeded PRNG + sequence
// numbers for tie-breaking), two runs with the same config and seed produce
// byte-identical outputs.
package engine

import (
	"container/heap"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"path/filepath"

	"github.com/pranav-k/forkrisk/internal/config"
	"github.com/pranav-k/forkrisk/internal/logstore"
	"github.com/pranav-k/forkrisk/internal/network"
	"github.com/pranav-k/forkrisk/internal/node"
)

// Engine is the simulation runtime.
type Engine struct {
	// activeCfg is the engine's own independent copy of the scenario configuration.
	// applyDynamicEvents() mutates activeCfg fields; the caller's original pointer is
	// never touched after New() returns.
	activeCfg config.ScenarioConfig

	// initialCfg is a snapshot of activeCfg taken at construction time, before any
	// dynamic events run. It is never mutated. InitialConfig() returns a copy of it.
	initialCfg config.ScenarioConfig

	nodes      map[string]*node.Node
	nodeOrder  []string // deterministic node iteration order
	queue      EventQueue
	seq        int64   // global event sequence counter
	simTime    float64 // current simulated clock in seconds
	blockCount int     // total blocks proposed so far
	logger     *logstore.Logger
	errors     []error // recorded non-fatal simulation errors

	proposalsEnded   bool
	proposalsEndedAt float64
	settlementCutoff float64

	// gossipedLinks tracks which blocks have already been gossiped along directed peer links (fromID|toID|hash)
	// to prevent duplicate gossip loops.
	gossipedLinks map[string]bool

	// Separate random streams per concern (seeded from activeCfg.Seed).
	// Keeping them separate means changing one concern doesn't affect another.
	rngProposal *rand.Rand // block jitter & proposer selection
	rngNetwork  *rand.Rand // latency sampling
	rngTx       *rand.Rand // tx count per block
	rngChurn    *rand.Rand // churn toggle scheduling (Milestone 2)

	latencyModel   network.DelayModel
	propDelayModel network.DelayModel

	// dynamicEventIdx tracks which dynamic events have already been applied.
	dynamicEventIdx int

	// txPool tracks pending transactions when block capacity is limited.
	txPool int

	// Invariant and throughput tracking: TotalOfferedTx = TotalProcessedTx + FinalPendingTx
	totalOfferedTx   int
	totalProcessedTx int
	maxPendingTx     int
}

// Result summarises the simulation after it completes.
type Result struct {
	NumNodes           int
	ProposedBlocks     int
	ProcessedEvents    int64
	SimDurationSec     float64 // simulated time when proposal phase completed
	SettledDurationSec float64 // simulated time after propagation completion (settlement)
	NodeTipHeights     map[string]int
	DivergentTips      int // number of nodes NOT on the majority tip
	ErrorCount         int
	Errors             []string
	TotalOfferedTx     int
	TotalProcessedTx   int
	MaxPendingTx       int
	FinalPendingTx     int
	OfferedTPS         float64
}

// New creates and initialises a new Engine from a scenario configuration.
// The logger must already be open.
//
// The engine deep-copies *cfg into its own independent storage. The caller's
// *cfg is never read or written after New() returns. applyDynamicEvents()
// modifies the engine's own activeCfg only. Use InitialConfig() to read the
// original pre-run configuration at any time.
func New(cfg *config.ScenarioConfig, logger *logstore.Logger) *Engine {
	// deepCopyConfig returns an independent copy of a ScenarioConfig.
	// The DynamicEvents slice and any pointer fields are explicitly duplicated
	// so that mutations to the copy cannot affect the original.
	deepCopy := func(src *config.ScenarioConfig) config.ScenarioConfig {
		dst := *src // copy all scalar fields
		// Deep-copy the DynamicEvents slice and the float64 pointers inside each element.
		if len(src.DynamicEvents) > 0 {
			dst.DynamicEvents = make([]config.DynamicEvent, len(src.DynamicEvents))
			for i, de := range src.DynamicEvents {
				dst.DynamicEvents[i] = de // copies AtBlock and nil/non-nil status
				if de.MeanLatencyMs != nil {
					v := *de.MeanLatencyMs
					dst.DynamicEvents[i].MeanLatencyMs = &v
				}
				if de.LatencyJitterMs != nil {
					v := *de.LatencyJitterMs
					dst.DynamicEvents[i].LatencyJitterMs = &v
				}
				if de.MeanPropDelayMs != nil {
					v := *de.MeanPropDelayMs
					dst.DynamicEvents[i].MeanPropDelayMs = &v
				}
				if de.PropDelayJitterMs != nil {
					v := *de.PropDelayJitterMs
					dst.DynamicEvents[i].PropDelayJitterMs = &v
				}
			}
		}
		return dst
	}

	// Both copies are independent from the caller's struct.
	active := deepCopy(cfg)
	initial := deepCopy(cfg)

	// Build per-stream PRNGs by adding fixed offsets to the base seed.
	// Using different seeds per stream ensures independence.
	rngProposal := rand.New(rand.NewPCG(uint64(active.Seed), 1))
	rngNetwork := rand.New(rand.NewPCG(uint64(active.Seed), 2))
	rngTx := rand.New(rand.NewPCG(uint64(active.Seed), 3))
	rngChurn := rand.New(rand.NewPCG(uint64(active.Seed), 4))

	latModel := network.NewDelayModel(active.MeanLatencyMs, active.LatencyJitterMs)
	propModel := network.NewDelayModel(active.MeanPropDelayMs, active.PropDelayJitterMs)

	e := &Engine{
		activeCfg:        active,
		initialCfg:       initial,
		nodes:            make(map[string]*node.Node),
		queue:            make(EventQueue, 0, 1024),
		rngProposal:      rngProposal,
		rngNetwork:       rngNetwork,
		rngTx:            rngTx,
		rngChurn:         rngChurn,
		latencyModel:     latModel,
		propDelayModel:   propModel,
		logger:           logger,
		settlementCutoff: -1.0,
		gossipedLinks:    make(map[string]bool),
	}
	heap.Init(&e.queue)
	return e
}

// InitialConfig returns an independent copy of the scenario configuration as it
// was when New() was called, before any dynamic network events were applied.
//
// The returned value is a defensive copy: mutating it does not affect the engine's
// saved snapshot. Use this for metadata recording and CSV parameter columns.
func (e *Engine) InitialConfig() config.ScenarioConfig {
	// Return a fresh deep-copy so the caller cannot modify e.initialCfg.
	snap := e.initialCfg
	if len(e.initialCfg.DynamicEvents) > 0 {
		snap.DynamicEvents = make([]config.DynamicEvent, len(e.initialCfg.DynamicEvents))
		for i, de := range e.initialCfg.DynamicEvents {
			snap.DynamicEvents[i] = de
			if de.MeanLatencyMs != nil {
				v := *de.MeanLatencyMs
				snap.DynamicEvents[i].MeanLatencyMs = &v
			}
			if de.LatencyJitterMs != nil {
				v := *de.LatencyJitterMs
				snap.DynamicEvents[i].LatencyJitterMs = &v
			}
			if de.MeanPropDelayMs != nil {
				v := *de.MeanPropDelayMs
				snap.DynamicEvents[i].MeanPropDelayMs = &v
			}
			if de.PropDelayJitterMs != nil {
				v := *de.PropDelayJitterMs
				snap.DynamicEvents[i].PropDelayJitterMs = &v
			}
		}
	}
	return snap
}

// SetupNodes creates nodes and wires up their peer connections.
// Peers are assigned in a ring topology with additional cross-connections
// to reach PeersPerNode connections per node (deterministic, no RNG needed).
func (e *Engine) SetupNodes() {
	n := e.activeCfg.NumNodes
	p := e.activeCfg.PeersPerNode

	// Create all nodes first.
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("N%d", i)
		e.nodeOrder = append(e.nodeOrder, id)
		e.nodes[id] = node.NewNode(id, nil) // peers added below
	}

	// Assign peers deterministically.
	// Each node i connects to the next p nodes (mod n) — ring + cross-links.
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("N%d", i)
		peers := make([]string, 0, p)
		for j := 1; j <= p; j++ {
			peerIdx := (i + j) % n
			peers = append(peers, fmt.Sprintf("N%d", peerIdx))
		}
		e.nodes[id].Peers = peers
	}
}

// selectProposer selects which node proposes the next block according to
// the configured proposer selection rule.
//
// Rules:
//   - "round_robin": cycles through nodes in deterministic nodeOrder.
//   - "uniform_random": draws a uniform random node index from rngProposal.
func (e *Engine) selectProposer() string {
	if len(e.nodeOrder) == 0 {
		return "N0"
	}
	if e.activeCfg.ProposerSelection == "round_robin" {
		return e.nodeOrder[e.blockCount%len(e.nodeOrder)]
	}
	// Default to uniform_random
	return e.nodeOrder[e.rngProposal.IntN(len(e.nodeOrder))]
}

// ScheduleInitialProposals seeds the event queue with the first block proposal.
// Unlike the buggy per-node loop which produced N proposals per interval, this
// schedules a single initial network proposal according to the proposer-selection rule.
func (e *Engine) ScheduleInitialProposals() {
	firstProposer := e.selectProposer()
	jitter := e.sampleJitter()
	firstTime := jitter
	if firstTime < 0 {
		firstTime = 0
	}
	e.schedulePropose(firstProposer, firstTime)
}

// scheduleChurnEvents schedules the initial churn toggle events for all nodes.
// Each node starts online; the first toggle event transitions it to offline
// after an exponentially distributed duration.
func (e *Engine) scheduleChurnEvents() {
	if !e.activeCfg.ChurnEnabled {
		return
	}
	for _, id := range e.nodeOrder {
		// Schedule the first toggle: online → offline after exponential(meanOn) time.
		delay := e.sampleExponential(e.activeCfg.ChurnMeanOnSec)
		e.push(&Event{
			SimTime: delay,
			Type:    EventChurnToggle,
			NodeID:  id,
		})
	}
}

// sampleExponential draws from Exp(mean) using the churn RNG stream.
func (e *Engine) sampleExponential(mean float64) float64 {
	if mean <= 0 {
		return 0
	}
	return -mean * math.Log(1.0-e.rngChurn.Float64())
}

// scheduleInitialTxArrival seeds the event queue with the first transaction arrival.
func (e *Engine) scheduleInitialTxArrival() {
	if e.activeCfg.TxArrivalRate <= 0 || e.activeCfg.TxPerBlock <= 0 {
		return
	}
	delay := e.sampleTxInterArrival()
	e.push(&Event{
		SimTime: delay,
		Type:    EventTxArrival,
	})
}

// sampleTxInterArrival draws an exponentially distributed inter-arrival time
// for a Poisson arrival process with rate lambda = tx_arrival_rate * tx_per_block (tx/s).
func (e *Engine) sampleTxInterArrival() float64 {
	lambda := e.activeCfg.TxArrivalRate * float64(e.activeCfg.TxPerBlock)
	if lambda <= 0 {
		return 1.0
	}
	u := e.rngTx.Float64()
	if u >= 1.0 {
		u = 0.999999999
	}
	delay := -math.Log(1.0-u) / lambda
	if delay <= 0 {
		delay = 1e-9
	}
	return delay
}

// sampleJitter draws a uniform jitter in [-BlockJitterSec, +BlockJitterSec].
func (e *Engine) sampleJitter() float64 {
	j := e.activeCfg.BlockJitterSec
	if j <= 0 {
		return 0
	}
	return (e.rngProposal.Float64()*2 - 1) * j
}

// schedulePropose adds a BlockPropose event for nodeID at the given simulated time.
func (e *Engine) schedulePropose(nodeID string, at float64) {
	if at < 0 {
		at = 0
	}
	e.push(&Event{
		SimTime: at,
		Type:    EventBlockPropose,
		NodeID:  nodeID,
	})
}

// push assigns the next sequence number and adds an event to the heap.
func (e *Engine) push(ev *Event) {
	e.seq++
	ev.Seq = e.seq
	heap.Push(&e.queue, ev)
}

// terminateProposals transitions the engine from the proposal phase to the
// settlement phase and activates the bounded settlement cutoff window.
func (e *Engine) terminateProposals(at float64) {
	if e.proposalsEnded {
		return
	}
	e.proposalsEnded = true
	e.proposalsEndedAt = at
	grace := e.activeCfg.SettlementGraceSec
	if grace <= 0 {
		grace = 5.0
	}
	e.settlementCutoff = at + grace
}

// gossipBlock schedules gossip transmission of blk from fromID to toID,
// enforcing directed link deduplication, peer knowledge checks, and preventing echo to the proposer.
// Respects peer availability: messages to/from offline nodes are dropped.
func (e *Engine) gossipBlock(fromID, toID string, blk node.Block) {
	if toID == blk.ProposerID {
		return // creator already knows its own block
	}

	// Check if sender is online (can't send if offline).
	sender, sOk := e.nodes[fromID]
	if sOk && !sender.Online {
		return
	}

	// Check if receiver is online (can't receive if offline).
	peer, ok := e.nodes[toID]
	if !ok || !peer.Online {
		return
	}

	if peer.HasBlock(blk.Hash) || peer.IsOrphan(blk.Hash) {
		return // peer already has it or buffered it
	}

	linkKey := fmt.Sprintf("%s|%s|%s", fromID, toID, blk.Hash)
	if e.gossipedLinks[linkKey] {
		return // already gossiped along this directed link
	}
	e.gossipedLinks[linkKey] = true
	latency := e.latencyModel.Sample(e.rngNetwork)
	e.push(&Event{
		SimTime: e.simTime + latency,
		Type:    EventBlockGossip,
		NodeID:  toID,
		FromID:  fromID,
		Block:   blk,
	})
}

// applyDynamicEvents checks if any dynamic network condition changes should
// be applied at the current block count and updates the delay models accordingly.
// It writes only to e.activeCfg (the engine's own copy); the caller's struct and
// e.initialCfg are never modified.
// A ConfigChange log entry is written for each field that changes.
func (e *Engine) applyDynamicEvents() {
	for e.dynamicEventIdx < len(e.activeCfg.DynamicEvents) {
		de := e.activeCfg.DynamicEvents[e.dynamicEventIdx]
		if e.blockCount < de.AtBlock {
			break
		}
		// Apply latency changes.
		if de.MeanLatencyMs != nil {
			oldLat := e.activeCfg.MeanLatencyMs
			meanLat := *de.MeanLatencyMs
			jitter := e.activeCfg.LatencyJitterMs
			if de.LatencyJitterMs != nil {
				jitter = *de.LatencyJitterMs
			}
			e.latencyModel = network.NewDelayModel(meanLat, jitter)
			e.activeCfg.MeanLatencyMs = meanLat
			e.logConfigChange("mean_latency_ms", fmt.Sprintf("%.4f", oldLat), fmt.Sprintf("%.4f", meanLat), de.AtBlock)
			if de.LatencyJitterMs != nil {
				oldJit := e.activeCfg.LatencyJitterMs
				e.activeCfg.LatencyJitterMs = jitter
				e.logConfigChange("latency_jitter_ms", fmt.Sprintf("%.4f", oldJit), fmt.Sprintf("%.4f", jitter), de.AtBlock)
			}
		} else if de.LatencyJitterMs != nil {
			oldJit := e.activeCfg.LatencyJitterMs
			jitter := *de.LatencyJitterMs
			e.latencyModel = network.NewDelayModel(e.activeCfg.MeanLatencyMs, jitter)
			e.activeCfg.LatencyJitterMs = jitter
			e.logConfigChange("latency_jitter_ms", fmt.Sprintf("%.4f", oldJit), fmt.Sprintf("%.4f", jitter), de.AtBlock)
		}

		// Apply propagation delay changes.
		if de.MeanPropDelayMs != nil {
			oldProp := e.activeCfg.MeanPropDelayMs
			meanProp := *de.MeanPropDelayMs
			jitter := e.activeCfg.PropDelayJitterMs
			if de.PropDelayJitterMs != nil {
				jitter = *de.PropDelayJitterMs
			}
			e.propDelayModel = network.NewDelayModel(meanProp, jitter)
			e.activeCfg.MeanPropDelayMs = meanProp
			e.logConfigChange("mean_prop_delay_ms", fmt.Sprintf("%.4f", oldProp), fmt.Sprintf("%.4f", meanProp), de.AtBlock)
			if de.PropDelayJitterMs != nil {
				oldJit := e.activeCfg.PropDelayJitterMs
				e.activeCfg.PropDelayJitterMs = jitter
				e.logConfigChange("prop_delay_jitter_ms", fmt.Sprintf("%.4f", oldJit), fmt.Sprintf("%.4f", jitter), de.AtBlock)
			}
		} else if de.PropDelayJitterMs != nil {
			oldJit := e.activeCfg.PropDelayJitterMs
			jitter := *de.PropDelayJitterMs
			e.propDelayModel = network.NewDelayModel(e.activeCfg.MeanPropDelayMs, jitter)
			e.activeCfg.PropDelayJitterMs = jitter
			e.logConfigChange("prop_delay_jitter_ms", fmt.Sprintf("%.4f", oldJit), fmt.Sprintf("%.4f", jitter), de.AtBlock)
		}

		e.dynamicEventIdx++
	}
}

// logConfigChange writes a ConfigChange entry to the event log.
// Errors are silently appended to e.errors to avoid disrupting the simulation.
func (e *Engine) logConfigChange(field, oldVal, newVal string, atBlock int) {
	e.seq++
	if err := e.logger.Write(logstore.LogEntry{
		Seq:            e.seq,
		SimTime:        e.simTime,
		EventType:      string(EventConfigChange),
		ConfigField:    field,
		ConfigOldValue: oldVal,
		ConfigNewValue: newVal,
		AtBlock:        atBlock,
	}); err != nil {
		e.errors = append(e.errors, fmt.Errorf("logging config change: %w", err))
	}
}

// Run executes the event loop using a two-phase termination model:
//  1. Proposal Phase: Blocks are proposed at the network-wide BlockIntervalSec rate
//     until max_blocks or sim_duration_sec is reached.
//  2. Settlement Phase: Once proposals terminate, all in-flight gossip and arrival
//     events are processed to completion (or up to SettlementGraceSec) so no
//     messages in transit are prematurely abandoned.
func (e *Engine) Run() (Result, error) {
	e.SetupNodes()
	e.ScheduleInitialProposals()
	e.scheduleChurnEvents()
	e.scheduleInitialTxArrival()

	processedEvents := int64(0)
	stopTime := e.activeCfg.SimDurationSec
	maxBlocks := e.activeCfg.MaxBlocks

	for e.queue.Len() > 0 {
		ev := heap.Pop(&e.queue).(*Event)

		// Check if the proposal phase should terminate.
		// Both max_blocks and sim_duration_sec activate the bounded settlement window.
		// Handles the exact duration boundary (ev.SimTime >= stopTime).
		if !e.proposalsEnded {
			if (maxBlocks > 0 && e.blockCount >= maxBlocks) || (stopTime > 0 && ev.SimTime >= stopTime) {
				e.terminateProposals(ev.SimTime)
			}
		}

		// Discard proposal and tx arrival events after proposal termination.
		if e.proposalsEnded && (ev.Type == EventBlockPropose || ev.Type == EventTxArrival) {
			continue
		}

		// Discard events beyond the settlement cutoff to enforce bounded settlement.
		if e.proposalsEnded && e.settlementCutoff >= 0 && ev.SimTime > e.settlementCutoff {
			break
		}

		e.simTime = ev.SimTime
		processedEvents++

		var err error
		switch ev.Type {
		case EventBlockPropose:
			err = e.handlePropose(ev)
		case EventBlockGossip:
			err = e.handleGossip(ev)
		case EventBlockArrive:
			err = e.handleArrive(ev)
		case EventChurnToggle:
			err = e.handleChurnToggle(ev)
		case EventTxArrival:
			err = e.handleTxArrival(ev)
		}
		if err != nil {
			e.errors = append(e.errors, err)
		}
	}

	if !e.proposalsEnded {
		e.terminateProposals(e.simTime)
	}

	return e.buildResult(processedEvents), nil
}

// handlePropose processes a BlockPropose event.
// The proposing node creates a new block extending its current tip,
// adds it locally, logs it, and schedules gossip to all peers with sender tracking.
// It then schedules the NEXT single proposal for the entire network.
func (e *Engine) handlePropose(ev *Event) error {
	if e.proposalsEnded {
		return nil
	}

	n, ok := e.nodes[ev.NodeID]
	if !ok {
		return fmt.Errorf("unknown proposer node %q", ev.NodeID)
	}
	tip := n.Tip()

	// Determine how many transactions from the mempool (txPool) are included in this block:
	// - If BlockCapacity == 0 (unlimited), consume the entire pending txPool.
	// - If BlockCapacity > 0 (capped), consume at most BlockCapacity transactions.
	// Invariant: totalOfferedTx = totalProcessedTx + txPool at all times.
	txCount := e.txPool
	if e.activeCfg.BlockCapacity > 0 && txCount > e.activeCfg.BlockCapacity {
		txCount = e.activeCfg.BlockCapacity
	}
	e.txPool -= txCount
	e.totalProcessedTx += txCount

	// Create the new block.
	blk := node.NewBlock(
		tip.Height+1,
		ev.NodeID,
		tip.Hash,
		e.simTime,
		txCount,
	)

	// The proposer adds the block to its own chain immediately.
	_, _, _, err := n.AddBlock(blk)
	if err != nil {
		return fmt.Errorf("proposer %s failed to add its own block: %w", ev.NodeID, err)
	}
	e.blockCount++

	// Apply any dynamic events triggered by this block count.
	e.applyDynamicEvents()

	// Log the proposal.
	if err := e.logger.Write(logstore.LogEntry{
		Seq:           ev.Seq,
		SimTime:       e.simTime,
		EventType:     string(EventBlockPropose),
		NodeID:        ev.NodeID,
		FromNode:      ev.NodeID,
		BlockHash:     blk.Hash,
		BlockHeight:   blk.Height,
		ProposalIndex: e.blockCount,
		ProposerID:    blk.ProposerID,
		ParentHash:    blk.ParentHash,
		TxCount:       blk.TxCount,
		TipHash:       blk.Hash,
		TipHeight:     blk.Height,
		PoolSize:      e.txPool,
		OfferedTx:     e.totalOfferedTx,
	}); err != nil {
		return fmt.Errorf("logging block proposal: %w", err)
	}

	// Schedule gossip to each peer with link-level deduplication.
	for _, peerID := range n.Peers {
		e.gossipBlock(ev.NodeID, peerID, blk)
	}

	// Check if this proposal satisfied max_blocks or sim_duration_sec.
	if (e.activeCfg.MaxBlocks > 0 && e.blockCount >= e.activeCfg.MaxBlocks) ||
		(e.activeCfg.SimDurationSec > 0 && e.simTime >= e.activeCfg.SimDurationSec) {
		e.terminateProposals(e.simTime)
		return nil
	}

	// Schedule the NEXT network-wide proposal.
	nextProposer := e.selectProposer()
	jitter := e.sampleJitter()
	nextPropose := e.simTime + e.activeCfg.BlockIntervalSec + jitter
	if nextPropose <= e.simTime {
		nextPropose = e.simTime + 1e-6
	}
	e.schedulePropose(nextProposer, nextPropose)

	return nil
}

// handleGossip processes a BlockGossip event.
// The block is "in transit" — we now apply the processing/propagation delay
// and schedule a BlockArrive event preserving FromID.
func (e *Engine) handleGossip(ev *Event) error {
	blk, ok := ev.Block.(node.Block)
	if !ok {
		return fmt.Errorf("gossip event on node %s has invalid block payload", ev.NodeID)
	}

	// Check if the receiving node is still online.
	recipient, nodeOk := e.nodes[ev.NodeID]
	if !nodeOk {
		return fmt.Errorf("gossip event destination node %q does not exist", ev.NodeID)
	}
	if !recipient.Online {
		// Node went offline since this gossip was scheduled — message is lost.
		return nil
	}

	// Add propagation processing delay on top of the transit latency.
	propDelay := e.propDelayModel.Sample(e.rngNetwork)
	arriveTime := e.simTime + propDelay

	if err := e.logger.Write(logstore.LogEntry{
		Seq:         ev.Seq,
		SimTime:     e.simTime,
		EventType:   string(EventBlockGossip),
		NodeID:      ev.NodeID,
		FromNode:    ev.FromID,
		BlockHash:   blk.Hash,
		BlockHeight: blk.Height,
		ProposerID:  blk.ProposerID,
		ParentHash:  blk.ParentHash,
	}); err != nil {
		return fmt.Errorf("logging block gossip: %w", err)
	}

	e.push(&Event{
		SimTime: arriveTime,
		Type:    EventBlockArrive,
		NodeID:  ev.NodeID,
		FromID:  ev.FromID,
		Block:   blk,
	})
	return nil
}

// handleArrive processes a BlockArrive event.
// The receiving node validates and adds the block.
// If its parent is unknown, it is buffered as an orphan.
// If the tip changes (directly or via orphan recovery), a TipUpdate event is logged with the new tip block.
// If invalid, an observable BlockReject event is logged.
// Arriving blocks and recovered orphans are forwarded to peers who do not already know them.
func (e *Engine) handleArrive(ev *Event) error {
	blk, ok := ev.Block.(node.Block)
	if !ok {
		return fmt.Errorf("arrive event on node %s has invalid block payload", ev.NodeID)
	}
	n, nodeExists := e.nodes[ev.NodeID]
	if !nodeExists {
		return fmt.Errorf("arrive event destination node %q does not exist", ev.NodeID)
	}

	// If node is offline, it cannot process arriving blocks — message is lost.
	if !n.Online {
		return nil
	}

	// Skip if already known in blocks or orphan pool (duplicate gossip is normal).
	if n.HasBlock(blk.Hash) || n.IsOrphan(blk.Hash) {
		return nil
	}

	oldTip := n.TipHash()
	directTipChanged, orphanTipChanged, recovered, err := n.AddBlock(blk)

	// If buffered as an orphan, log an observable orphan event and do not error.
	if errors.Is(err, node.ErrOrphanBuffered) {
		if logErr := e.logger.Write(logstore.LogEntry{
			Seq:         ev.Seq,
			SimTime:     e.simTime,
			EventType:   string(EventBlockOrphan),
			NodeID:      ev.NodeID,
			FromNode:    ev.FromID,
			BlockHash:   blk.Hash,
			BlockHeight: blk.Height,
			ProposerID:  blk.ProposerID,
			ParentHash:  blk.ParentHash,
			TxCount:     blk.TxCount,
			Reason:      "parent_unknown",
		}); logErr != nil {
			return fmt.Errorf("logging orphan event: %w", logErr)
		}
		return nil
	}

	// If block is truly rejected (e.g. invalid height != parent + 1), log BlockReject.
	if err != nil {
		if logErr := e.logger.Write(logstore.LogEntry{
			Seq:         ev.Seq,
			SimTime:     e.simTime,
			EventType:   string(EventBlockReject),
			NodeID:      ev.NodeID,
			FromNode:    ev.FromID,
			BlockHash:   blk.Hash,
			BlockHeight: blk.Height,
			ProposerID:  blk.ProposerID,
			ParentHash:  blk.ParentHash,
			Reason:      err.Error(),
		}); logErr != nil {
			return fmt.Errorf("logging reject event: %w", logErr)
		}
		return fmt.Errorf("node %s rejected block %s from %s: %w", ev.NodeID, blk.Hash[:8], ev.FromID, err)
	}

	// Log successful arrival.
	if logErr := e.logger.Write(logstore.LogEntry{
		Seq:         ev.Seq,
		SimTime:     e.simTime,
		EventType:   string(EventBlockArrive),
		NodeID:      ev.NodeID,
		FromNode:    ev.FromID,
		BlockHash:   blk.Hash,
		BlockHeight: blk.Height,
		ProposerID:  blk.ProposerID,
		ParentHash:  blk.ParentHash,
		TxCount:     blk.TxCount,
		TipHash:     n.TipHash(),
		TipHeight:   n.TipHeight(),
	}); logErr != nil {
		return fmt.Errorf("logging arrive event: %w", logErr)
	}

	// Log TipUpdate for the direct block extending (or reorging) the tip.
	// This fires when blk itself (not an orphan descendant) moved the tip.
	// When blk directly changed the tip, blk IS the new intermediate tip regardless
	// of whether orphan recovery later moves the tip further.
	if directTipChanged {
		// blk was the direct tip after AddBlock's direct step.
		// Reorg if blk's parent is not the oldTip (it extended a different branch).
		reason := "extend"
		if blk.ParentHash != oldTip {
			reason = "reorg"
		}
		e.seq++
		if logErr := e.logger.Write(logstore.LogEntry{
			Seq:         e.seq,
			SimTime:     e.simTime,
			EventType:   string(EventTipUpdate),
			NodeID:      ev.NodeID,
			FromNode:    ev.FromID,
			BlockHash:   blk.Hash,
			BlockHeight: blk.Height,
			ProposerID:  blk.ProposerID,
			ParentHash:  blk.ParentHash,
			TipHash:     blk.Hash,
			TipHeight:   blk.Height,
			OldTipHash:  oldTip,
			Reason:      reason,
		}); logErr != nil {
			return fmt.Errorf("logging tip update (direct) event: %w", logErr)
		}
	}

	// Log a separate TipUpdate when orphan recovery further extended the tip beyond blk.
	// oldTipForOrphan is the tip just before orphan recovery: blk.Hash if directTipChanged,
	// otherwise the pre-AddBlock tip (oldTip).
	if orphanTipChanged {
		newTip := n.Tip()
		oldTipForOrphan := oldTip
		if directTipChanged {
			oldTipForOrphan = blk.Hash
		}
		reason := "extend"
		if newTip.ParentHash != oldTipForOrphan {
			reason = "reorg"
		}
		e.seq++
		if logErr := e.logger.Write(logstore.LogEntry{
			Seq:         e.seq,
			SimTime:     e.simTime,
			EventType:   string(EventTipUpdate),
			NodeID:      ev.NodeID,
			FromNode:    ev.FromID,
			BlockHash:   newTip.Hash,
			BlockHeight: newTip.Height,
			ProposerID:  newTip.ProposerID,
			ParentHash:  newTip.ParentHash,
			TipHash:     newTip.Hash,
			TipHeight:   newTip.Height,
			OldTipHash:  oldTipForOrphan,
			Reason:      reason,
		}); logErr != nil {
			return fmt.Errorf("logging tip update (orphan recovery) event: %w", logErr)
		}
	}

	// Forward arriving block and any recovered orphan blocks to peers.
	// gossipBlock enforces link-level deduplication, peer knowledge check, and proposer check.
	// For the arriving block (blk): suppress echo directly back to ev.FromID.
	// For recovered orphans: also suppress sending back to ev.FromID, since ev.FromID
	// is the one who sent the parent that triggered recovery and almost certainly already
	// has or will receive the orphan via another path. gossipBlock's HasBlock/IsOrphan
	// checks provide the final safety net for any case where ev.FromID genuinely lacks
	// the recovered block.
	toForward := append([]node.Block{blk}, recovered...)
	for _, fwdBlk := range toForward {
		for _, peerID := range n.Peers {
			if peerID == ev.FromID && fwdBlk.Hash == blk.Hash {
				continue // don't echo arriving block directly back to its immediate sender
			}
			e.gossipBlock(ev.NodeID, peerID, fwdBlk)
		}
	}

	return nil
}

// handleChurnToggle processes a ChurnToggle event.
// Toggles the node's Online status and schedules the next toggle.
func (e *Engine) handleChurnToggle(ev *Event) error {
	n, ok := e.nodes[ev.NodeID]
	if !ok {
		return fmt.Errorf("churn toggle for unknown node %q", ev.NodeID)
	}

	wasOnline := n.Online
	n.Online = !n.Online

	// Determine the next state duration.
	var nextDelay float64
	if n.Online {
		// Just came online; schedule next offline transition.
		nextDelay = e.sampleExponential(e.activeCfg.ChurnMeanOnSec)
	} else {
		// Just went offline; schedule next online transition.
		nextDelay = e.sampleExponential(e.activeCfg.ChurnMeanOffSec)
	}

	// Log the toggle.
	reason := "offline"
	if n.Online {
		reason = "online"
	}
	if logErr := e.logger.Write(logstore.LogEntry{
		Seq:       ev.Seq,
		SimTime:   e.simTime,
		EventType: string(EventChurnToggle),
		NodeID:    ev.NodeID,
		Reason:    reason,
	}); logErr != nil {
		return fmt.Errorf("logging churn toggle: %w", logErr)
	}

	_ = wasOnline // suppress unused warning

	// Schedule the next toggle (even during settlement — churn continues until cutoff).
	nextTime := e.simTime + nextDelay
	if !e.proposalsEnded || (e.settlementCutoff >= 0 && nextTime <= e.settlementCutoff) {
		e.push(&Event{
			SimTime: nextTime,
			Type:    EventChurnToggle,
			NodeID:  ev.NodeID,
		})
	}

	return nil
}

// handleTxArrival processes a TxArrival event, adding 1 transaction to the mempool
// and scheduling the next arrival.
func (e *Engine) handleTxArrival(ev *Event) error {
	if e.proposalsEnded {
		return nil
	}
	e.txPool++
	e.totalOfferedTx++
	if e.txPool > e.maxPendingTx {
		e.maxPendingTx = e.txPool
	}

	// Schedule next transaction arrival.
	nextTime := e.simTime + e.sampleTxInterArrival()
	if !e.proposalsEnded {
		e.push(&Event{
			SimTime: nextTime,
			Type:    EventTxArrival,
		})
	}
	return nil
}

// buildResult assembles the final simulation summary.
func (e *Engine) buildResult(processedEvents int64) Result {
	heights := make(map[string]int, len(e.nodes))
	tipCounts := make(map[string]int) // tip hash → count of nodes on it

	for _, id := range e.nodeOrder {
		h := e.nodes[id].TipHeight()
		heights[id] = h
		tipCounts[e.nodes[id].TipHash()]++
	}

	// Find the majority tip (most nodes agree on it).
	maxCount := 0
	for _, c := range tipCounts {
		if c > maxCount {
			maxCount = c
		}
	}
	divergent := len(e.nodeOrder) - maxCount

	errStrs := make([]string, 0, len(e.errors))
	for _, err := range e.errors {
		errStrs = append(errStrs, err.Error())
	}

	offeredTPS := 0.0
	duration := e.proposalsEndedAt
	if duration <= 0 {
		duration = e.simTime
	}
	if duration > 0 {
		offeredTPS = float64(e.totalOfferedTx) / duration
	}

	return Result{
		NumNodes:           e.activeCfg.NumNodes,
		ProposedBlocks:     e.blockCount,
		ProcessedEvents:    processedEvents,
		SimDurationSec:     e.proposalsEndedAt,
		SettledDurationSec: e.simTime,
		NodeTipHeights:     heights,
		DivergentTips:      divergent,
		ErrorCount:         len(e.errors),
		Errors:             errStrs,
		TotalOfferedTx:     e.totalOfferedTx,
		TotalProcessedTx:   e.totalProcessedTx,
		MaxPendingTx:       e.maxPendingTx,
		FinalPendingTx:     e.txPool,
		OfferedTPS:         offeredTPS,
	}
}

// LogFilePath builds the standard log file path for a run.
func LogFilePath(outputDir, runID string) string {
	return filepath.Join(outputDir, runID, "events.jsonl")
}
