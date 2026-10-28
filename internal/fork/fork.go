// Package fork provides fork detection, stale-block identification, and
// reorganization tracking for the blockchain simulator.
//
// # Definitions (from the planning document §5)
//
//   - Fork Birth: the instant when two or more valid blocks exist at the same
//     height across different node views. Detected when two nodes have distinct
//     tip hashes at the same height simultaneously.
//
//   - Fork Episode: a network-level period beginning with a fork birth and ending
//     when all nodes converge to the same tip. Multiple node-level disagreements
//     during the same episode are counted as ONE fork episode.
//
//   - Stale Block: a block that was valid when proposed but is not on the final
//     longest chain after convergence. It was "wasted work."
//
//   - Reorganization (Reorg): a node switching its tip from one branch to another
//     incompatible branch. Detected via TipUpdate events with reason="reorg".
//     Reorg depth = number of blocks rolled back = oldTipHeight - commonAncestorHeight,
//     computed by walking both branches up via parent pointers until they meet.
//
//   - Node-View Divergence: transient snapshot where nodes disagree on the tip.
//     This is a measurement, not an event — sampled per block.
//
// # Fork Condition
//
// A fork condition exists when there are ≥2 distinct tip hashes among nodes
// that share the same (maximum) tip height. Nodes that are simply behind
// (at a lower height) are NOT forked — they haven't seen the block yet.
// This prevents false positives in normal operation where nodes update tips
// at slightly different simulated times.
//
// These are related but distinct:
//   - A fork episode may cause zero or more reorgs.
//   - A stale block is an outcome of a resolved fork.
//   - Node-view divergence is a snapshot measurement during an ongoing fork.
package fork

import (
	"encoding/json"
	"sort"
)

// Event represents a parsed log entry relevant to fork detection.
// This is a subset of logstore.LogEntry to keep the fork package independent.
type Event struct {
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
}

// ForkEpisode represents a network-level fork event from birth to resolution.
type ForkEpisode struct {
	// ID is a sequential identifier for this episode.
	ID int `json:"id"`

	// BirthTime is the simulated time when the fork was first detected.
	BirthTime float64 `json:"birth_time"`

	// BirthHeight is the block height at which the fork was first detected.
	BirthHeight int `json:"birth_height"`

	// ResolutionTime is the simulated time when all nodes converged.
	// -1 if unresolved at the end of the simulation.
	ResolutionTime float64 `json:"resolution_time"`

	// Duration is ResolutionTime - BirthTime. -1 if unresolved.
	Duration float64 `json:"duration"`

	// Height is the maximum block height reached by competing tips during this episode.
	Height int `json:"height"`

	// CompetingTips are the distinct tip hashes observed during this episode.
	CompetingTips []string `json:"competing_tips"`

	// InvolvedNodes lists the node IDs that participated in this fork.
	InvolvedNodes []string `json:"involved_nodes"`
}

// Reorganization represents a single node switching its tip to a different branch.
type Reorganization struct {
	// Time is the simulated time of the reorg.
	Time float64 `json:"time"`

	// NodeID is the node that experienced the reorg.
	NodeID string `json:"node_id"`

	// OldTipHash is the hash of the tip before the reorg.
	OldTipHash string `json:"old_tip_hash"`

	// NewTipHash is the hash of the new tip after the reorg.
	NewTipHash string `json:"new_tip_hash"`

	// NewTipHeight is the height of the new tip.
	NewTipHeight int `json:"new_tip_height"`

	// Depth is the number of blocks rolled back (estimated from the log).
	// Depth = oldTipHeight - commonAncestorHeight, where commonAncestorHeight
	// is approximated as min(oldTipHeight, newTipHeight) - 1 when we can't
	// trace the full chain from the log alone.
	Depth int `json:"depth"`
}

// StaleBlock represents a block that was proposed but is not on the final chain.
type StaleBlock struct {
	// Hash of the stale block.
	Hash string `json:"hash"`

	// Height of the stale block.
	Height int `json:"height"`

	// ProposerID is who created this block.
	ProposerID string `json:"proposer_id"`

	// Time is when this block was proposed.
	Time float64 `json:"time"`
}

// DetectionResult holds all fork-related analysis results.
type DetectionResult struct {
	// ForkEpisodes is the list of detected network-level fork episodes.
	ForkEpisodes []ForkEpisode `json:"fork_episodes"`

	// Reorganizations is the list of all detected reorgs.
	Reorganizations []Reorganization `json:"reorganizations"`

	// StaleBlocks is the list of blocks not on the final chain.
	StaleBlocks []StaleBlock `json:"stale_blocks"`

	// TotalBlocks is the total number of proposed blocks (post warm-up).
	TotalBlocks int `json:"total_blocks"`

	// FinalChainLength is the height of the longest chain at simulation end.
	FinalChainLength int `json:"final_chain_length"`
}

// ForkRate returns fork episodes ÷ total blocks. Returns 0 if no blocks.
func (r *DetectionResult) ForkRate() float64 {
	if r.TotalBlocks == 0 {
		return 0
	}
	return float64(len(r.ForkEpisodes)) / float64(r.TotalBlocks)
}

// StaleRate returns stale blocks ÷ total blocks. Returns 0 if no blocks.
func (r *DetectionResult) StaleRate() float64 {
	if r.TotalBlocks == 0 {
		return 0
	}
	return float64(len(r.StaleBlocks)) / float64(r.TotalBlocks)
}

// MeanReorgDepth returns the average reorg depth. Returns 0 if no reorgs.
func (r *DetectionResult) MeanReorgDepth() float64 {
	if len(r.Reorganizations) == 0 {
		return 0
	}
	total := 0
	for _, re := range r.Reorganizations {
		total += re.Depth
	}
	return float64(total) / float64(len(r.Reorganizations))
}

// MaxReorgDepth returns the maximum reorg depth. Returns 0 if no reorgs.
func (r *DetectionResult) MaxReorgDepth() int {
	max := 0
	for _, re := range r.Reorganizations {
		if re.Depth > max {
			max = re.Depth
		}
	}
	return max
}

// Detector processes a stream of simulation events and identifies fork episodes,
// stale blocks, and reorganizations.
type Detector struct {
	// nodeIDs is the set of all node IDs in the simulation.
	nodeIDs []string

	// warmUpBlocks is the number of initial blocks to skip.
	warmUpBlocks int
}

// NewDetector creates a fork detector configured for the given node IDs
// and warm-up period.
func NewDetector(nodeIDs []string, warmUpBlocks int) *Detector {
	ids := make([]string, len(nodeIDs))
	copy(ids, nodeIDs)
	sort.Strings(ids)
	return &Detector{
		nodeIDs:      ids,
		warmUpBlocks: warmUpBlocks,
	}
}

// Analyze processes a sequence of simulation events and returns fork detection results.
// Events must be in chronological order (by SimTime, then Seq).
//
// Fork condition: ≥2 distinct tip hashes exist among nodes AT THE SAME MAXIMUM
// height. Nodes at lower heights are simply behind, not forking.
func (d *Detector) Analyze(events []Event) DetectionResult {
	result := DetectionResult{}

	// Per-node state.
	nodeTips := make(map[string]string)    // nodeID -> tipHash
	nodeTipHeights := make(map[string]int) // nodeID -> tipHeight

	// Block metadata.
	proposedBlocks := make(map[string]bool)  // blockHash -> true (post warm-up)
	blockProposer := make(map[string]string) // blockHash -> proposerID
	blockTime := make(map[string]float64)    // blockHash -> proposal time
	blockHeight := make(map[string]int)      // blockHash -> height
	blockParent := make(map[string]string)   // blockHash -> parentHash

	// Fork episode tracking.
	var activeFork *forkTracker
	forkID := 0
	maxHeight := 0
	proposeCount := 0

	for _, ev := range events {
		switch ev.EventType {
		case "BlockPropose":
			proposeCount++
			isPostWarmup := false
			if ev.ProposalIndex > 0 {
				isPostWarmup = ev.ProposalIndex > d.warmUpBlocks
			} else {
				isPostWarmup = ev.BlockHeight > d.warmUpBlocks
			}
			if isPostWarmup {
				proposedBlocks[ev.BlockHash] = true
				blockProposer[ev.BlockHash] = ev.ProposerID
				blockTime[ev.BlockHash] = ev.SimTime
				blockHeight[ev.BlockHash] = ev.BlockHeight
				blockParent[ev.BlockHash] = ev.ParentHash
				result.TotalBlocks++
			}
			// Record ALL blocks for ancestry tracking regardless of warm-up.
			if ev.BlockHash != "" {
				blockHeight[ev.BlockHash] = ev.BlockHeight
				blockParent[ev.BlockHash] = ev.ParentHash
			}
			nodeTips[ev.NodeID] = ev.TipHash
			nodeTipHeights[ev.NodeID] = ev.TipHeight
			if ev.BlockHeight > maxHeight {
				maxHeight = ev.BlockHeight
			}

		case "TipUpdate":
			oldTip := nodeTips[ev.NodeID]
			nodeTips[ev.NodeID] = ev.TipHash
			nodeTipHeights[ev.NodeID] = ev.TipHeight

			if ev.BlockHeight > maxHeight {
				maxHeight = ev.BlockHeight
			}
			if ev.BlockHash != "" {
				blockHeight[ev.BlockHash] = ev.BlockHeight
				blockParent[ev.BlockHash] = ev.ParentHash
			}

			// Detect reorganizations.
			isPostWarmupReorg := false
			if ev.ProposalIndex > 0 {
				isPostWarmupReorg = ev.ProposalIndex > d.warmUpBlocks
			} else {
				isPostWarmupReorg = ev.TipHeight > d.warmUpBlocks
			}
			if ev.Reason == "reorg" && isPostWarmupReorg {
				// Determine the old tip hash: prefer ev.OldTipHash if present,
				// otherwise fall back to what we had recorded.
				oldTipHash := ev.OldTipHash
				if oldTipHash == "" {
					oldTipHash = oldTip
				}

				// Compute reorg depth by tracing both branches to their common ancestor.
				// Walk oldBranch and newBranch upward by parent pointers until they meet.
				depth := commonAncestorDepth(oldTipHash, ev.TipHash, blockParent, blockHeight)

				result.Reorganizations = append(result.Reorganizations, Reorganization{
					Time:         ev.SimTime,
					NodeID:       ev.NodeID,
					OldTipHash:   oldTipHash,
					NewTipHash:   ev.TipHash,
					NewTipHeight: ev.TipHeight,
					Depth:        depth,
				})
			}

		case "BlockArrive":
			if ev.TipHash != "" {
				nodeTips[ev.NodeID] = ev.TipHash
				nodeTipHeights[ev.NodeID] = ev.TipHeight
			}
			if ev.BlockHash != "" {
				blockHeight[ev.BlockHash] = ev.BlockHeight
				blockParent[ev.BlockHash] = ev.ParentHash
			}
		}

		// Fork condition check: only when current event involves post-warm-up proposals/height.
		currentHeight := ev.BlockHeight
		if ev.TipHeight > currentHeight {
			currentHeight = ev.TipHeight
		}
		isPostWarmupFork := false
		if ev.ProposalIndex > 0 {
			isPostWarmupFork = ev.ProposalIndex > d.warmUpBlocks
		} else {
			isPostWarmupFork = currentHeight > d.warmUpBlocks
		}
		if isPostWarmupFork {
			competingTips, forkHeight := d.findCompetingTips(nodeTips, nodeTipHeights)
			if len(competingTips) >= 2 {
				// Fork condition active.
				if activeFork == nil {
					forkID++
					activeFork = &forkTracker{
						id:          forkID,
						birthTime:   ev.SimTime,
						birthHeight: forkHeight,
						tips:        make(map[string]bool),
						nodes:       make(map[string]bool),
						height:      forkHeight,
					}
				}
				for _, tip := range competingTips {
					activeFork.tips[tip] = true
				}
				for nodeID, h := range nodeTipHeights {
					if h == forkHeight {
						activeFork.nodes[nodeID] = true
					}
				}
				if forkHeight > activeFork.height {
					activeFork.height = forkHeight
				}
			} else if activeFork != nil {
				// Converged — close episode.
				episode := activeFork.toEpisode(ev.SimTime)
				result.ForkEpisodes = append(result.ForkEpisodes, episode)
				activeFork = nil
			}
		}
	}

	// Record unresolved fork if still active.
	if activeFork != nil {
		lastTime := 0.0
		if len(events) > 0 {
			lastTime = events[len(events)-1].SimTime
		}
		episode := activeFork.toUnresolvedEpisode(lastTime)
		result.ForkEpisodes = append(result.ForkEpisodes, episode)
	}

	result.FinalChainLength = maxHeight

	// Determine final chain by tracing back from the majority tip among nodes at maxHeight.
	finalTipCounts := make(map[string]int)
	for nodeID, tip := range nodeTips {
		if tip != "" && (maxHeight == 0 || nodeTipHeights[nodeID] == maxHeight) {
			finalTipCounts[tip]++
		}
	}
	majorityTip := ""
	maxCount := 0
	for tip, count := range finalTipCounts {
		if count > maxCount || (count == maxCount && tip < majorityTip) {
			maxCount = count
			majorityTip = tip
		}
	}

	onFinalChain := make(map[string]bool)
	cursor := majorityTip
	for cursor != "" {
		onFinalChain[cursor] = true
		next := blockParent[cursor]
		if next == cursor {
			break // prevent infinite loop
		}
		cursor = next
	}

	for hash := range proposedBlocks {
		if !onFinalChain[hash] {
			result.StaleBlocks = append(result.StaleBlocks, StaleBlock{
				Hash:       hash,
				Height:     blockHeight[hash],
				ProposerID: blockProposer[hash],
				Time:       blockTime[hash],
			})
		}
	}

	sort.Slice(result.StaleBlocks, func(i, j int) bool {
		return result.StaleBlocks[i].Time < result.StaleBlocks[j].Time
	})

	return result
}

// findCompetingTips returns the set of distinct tip hashes among nodes that
// are AT THE MAXIMUM tip height. Nodes at lower heights are behind, not forked.
// Returns (competingTips, maxHeight). If ≥2 tips at same max height → fork.
func (d *Detector) findCompetingTips(nodeTips map[string]string, nodeTipHeights map[string]int) ([]string, int) {
	// Find the maximum height among all tracked nodes.
	maxH := 0
	for _, h := range nodeTipHeights {
		if h > maxH {
			maxH = h
		}
	}
	if maxH == 0 {
		return nil, 0
	}

	// Collect distinct tips among nodes AT the max height.
	seen := make(map[string]bool)
	var tips []string
	for _, nodeID := range d.nodeIDs {
		h := nodeTipHeights[nodeID]
		if h != maxH {
			continue // this node is behind; not a fork peer
		}
		tip := nodeTips[nodeID]
		if tip != "" && !seen[tip] {
			seen[tip] = true
			tips = append(tips, tip)
		}
	}
	return tips, maxH
}

// forkTracker tracks the state of an active fork episode.
type forkTracker struct {
	id          int
	birthTime   float64
	birthHeight int
	height      int
	tips        map[string]bool
	nodes       map[string]bool
}

func (ft *forkTracker) toEpisode(resolutionTime float64) ForkEpisode {
	tips := make([]string, 0, len(ft.tips))
	for t := range ft.tips {
		tips = append(tips, t)
	}
	sort.Strings(tips)

	nodes := make([]string, 0, len(ft.nodes))
	for n := range ft.nodes {
		nodes = append(nodes, n)
	}
	sort.Strings(nodes)

	return ForkEpisode{
		ID:             ft.id,
		BirthTime:      ft.birthTime,
		BirthHeight:    ft.birthHeight,
		ResolutionTime: resolutionTime,
		Duration:       resolutionTime - ft.birthTime,
		Height:         ft.height,
		CompetingTips:  tips,
		InvolvedNodes:  nodes,
	}
}

func (ft *forkTracker) toUnresolvedEpisode(lastTime float64) ForkEpisode {
	ep := ft.toEpisode(lastTime)
	ep.ResolutionTime = -1
	ep.Duration = -1
	return ep
}

// commonAncestorDepth computes the reorg depth by tracing both branches via
// parent pointers until they meet at a common ancestor.
//
// Depth = oldTipHeight - commonAncestorHeight, i.e. the number of blocks on
// the old branch that were rolled back.
//
// When parent information is incomplete (e.g. ancestors were pruned from the
// log, or the branches share a genesis before the warm-up window), the function
// returns depth=1 as a conservative minimum rather than 0 or -1.
func commonAncestorDepth(oldTip, newTip string, blockParent map[string]string, blockHeight map[string]int) int {
	if oldTip == "" || newTip == "" {
		return 1
	}
	if oldTip == newTip {
		return 0 // same block — no rollback at all
	}

	// Build the ancestry set of the old branch: walk from oldTip upward,
	// recording every block hash we visit.
	oldAncestors := make(map[string]bool)
	cursor := oldTip
	limit := 10000 // guard against cycles in corrupted data
	for cursor != "" && limit > 0 {
		oldAncestors[cursor] = true
		parent := blockParent[cursor]
		if parent == cursor || parent == "" {
			break // genesis or cycle guard
		}
		cursor = parent
		limit--
	}

	// Walk the new branch upward until we hit a hash in oldAncestors.
	cursor = newTip
	newBranchLen := 0
	limit = 10000
	for cursor != "" && limit > 0 {
		if oldAncestors[cursor] {
			// cursor is the common ancestor.
			// Depth = height of oldTip - height of common ancestor.
			oldH := blockHeight[oldTip]
			ancH := blockHeight[cursor]
			depth := oldH - ancH
			if depth < 1 {
				depth = 1
			}
			return depth
		}
		parent := blockParent[cursor]
		if parent == cursor || parent == "" {
			break
		}
		cursor = parent
		newBranchLen++
		limit--
	}

	// Fallback: we ran out of parent information before finding the LCA.
	// Use the old tip height as an upper bound (whole branch rolled back).
	oldH := blockHeight[oldTip]
	if oldH > 0 {
		return oldH
	}
	return 1 // absolute minimum
}

// ParseEvents parses JSONL-formatted event log data into a slice of Events.
func ParseEvents(data []byte) ([]Event, error) {
	var events []Event
	start := 0
	for i, b := range data {
		if b == '\n' {
			line := data[start:i]
			start = i + 1
			if len(line) == 0 {
				continue
			}
			var ev Event
			if err := json.Unmarshal(line, &ev); err != nil {
				return nil, err
			}
			events = append(events, ev)
		}
	}
	if start < len(data) {
		line := data[start:]
		if len(line) > 0 {
			var ev Event
			if err := json.Unmarshal(line, &ev); err != nil {
				return nil, err
			}
			events = append(events, ev)
		}
	}
	return events, nil
}
