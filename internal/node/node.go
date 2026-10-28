// Package node manages the blockchain state for a single virtual node.
// A Node knows its local chain, its current best tip, and its connected peers.
package node

import (
	"errors"
	"fmt"
	"strings"
)

var (
	// ErrOrphanBuffered is returned when a block's parent is unknown and the
	// block has been stored in the node's orphan pool.
	ErrOrphanBuffered = errors.New("parent block unknown; block buffered as orphan")

	// ErrBlockAlreadyKnown is returned when adding a block already stored.
	ErrBlockAlreadyKnown = errors.New("block already known")

	// ErrInvalidBlockHeight is returned when a block's height != parent.Height + 1.
	ErrInvalidBlockHeight = errors.New("invalid block height")
)

// Node is a virtual blockchain participant.
// It maintains its own view of the blockchain; different nodes can temporarily
// disagree on the best tip if a network partition or propagation delay causes
// two blocks to arrive at different times.
type Node struct {
	// ID is the unique string identifier for this node, e.g. "N0", "N1".
	ID string

	// Peers is the list of node IDs this node is directly connected to.
	// Blocks are forwarded to peers when received.
	Peers []string

	// Online indicates whether this node is currently reachable.
	// When false (offline), the node cannot send or receive gossip messages.
	// Managed by the engine's churn model.
	Online bool

	// blocks is the set of all blocks this node has seen and validated, indexed by hash.
	blocks map[string]Block

	// orphans buffers received blocks whose parents have not arrived yet.
	orphans map[string]Block

	// orphansByParent maps parentHash -> list of buffered orphan blocks waiting for that parent.
	orphansByParent map[string][]Block

	// tipHash is the hash of the block at the current best chain tip.
	tipHash string

	// tipHeight is the height of the current best tip (cached for speed).
	tipHeight int

	// nextProposalAt is the simulated time when this node will next propose a block.
	// Managed by the engine.
	NextProposalAt float64
}

// NewNode creates a Node initialised with the genesis block as its tip.
// peers is the list of this node's direct connections (other node IDs).
func NewNode(id string, peers []string) *Node {
	genesis := GenesisBlock()
	n := &Node{
		ID:              id,
		Peers:           peers,
		Online:          true,
		blocks:          make(map[string]Block),
		orphans:         make(map[string]Block),
		orphansByParent: make(map[string][]Block),
		tipHash:         genesis.Hash,
		tipHeight:       genesis.Height,
	}
	n.blocks[genesis.Hash] = genesis
	return n
}

// HasBlock reports whether the node already has a validated block with the given hash.
func (n *Node) HasBlock(hash string) bool {
	_, ok := n.blocks[hash]
	return ok
}

// IsOrphan reports whether a block is currently in the orphan buffer awaiting its parent.
func (n *Node) IsOrphan(hash string) bool {
	_, ok := n.orphans[hash]
	return ok
}

// OrphanCount returns the number of orphan blocks currently buffered.
func (n *Node) OrphanCount() int {
	return len(n.orphans)
}

// GetOrphan retrieves a buffered orphan block by hash.
func (n *Node) GetOrphan(hash string) (Block, bool) {
	b, ok := n.orphans[hash]
	return b, ok
}

// AddBlock attempts to add a block to the node's local store.
//
// Behavior:
//  1. If already in n.blocks, returns false, false, nil, ErrBlockAlreadyKnown.
//  2. If already in n.orphans, returns false, false, nil, nil (already buffered).
//  3. If parent is unknown, buffers the block in n.orphans and n.orphansByParent,
//     and returns false, false, nil, ErrOrphanBuffered.
//  4. If parent is known:
//     - Validates height == parent.Height + 1.
//     - Adds to n.blocks and evaluates tip selection.
//     - Recursively reprocesses any buffered orphans waiting on this block
//     (or any newly unblocked descendants).
//     - Returns directTipChanged, orphanTipChanged, recoveredBlocks, nil.
//     - directTipChanged: the block itself directly updated the tip.
//     - orphanTipChanged: orphan cascade further updated the tip.
//
// Callers that only care whether the tip changed at all should OR both booleans.
func (n *Node) AddBlock(b Block) (directTipChanged bool, orphanTipChanged bool, recovered []Block, err error) {
	if n.HasBlock(b.Hash) {
		return false, false, nil, ErrBlockAlreadyKnown
	}
	if n.IsOrphan(b.Hash) {
		return false, false, nil, nil
	}

	// Look up the parent to validate the chain link.
	parent, ok := n.blocks[b.ParentHash]
	if !ok {
		// Buffer as orphan; do not discard.
		n.orphans[b.Hash] = b
		n.orphansByParent[b.ParentHash] = append(n.orphansByParent[b.ParentHash], b)
		return false, false, nil, ErrOrphanBuffered
	}

	// Height must be exactly parent + 1.
	if b.Height != parent.Height+1 {
		return false, false, nil, fmt.Errorf("%w: block %s has height %d but parent height is %d", ErrInvalidBlockHeight, b.Hash[:8], b.Height, parent.Height)
	}

	n.blocks[b.Hash] = b

	// Apply longest-chain rule with deterministic tie-break.
	oldTip := n.tipHash
	n.tipHash = selectBestTip(n.tipHash, b.Hash, n.tipHeight, b.Height)
	n.tipHeight = n.blocks[n.tipHash].Height
	directTipChanged = (n.tipHash != oldTip)

	// Reprocess any orphans that were waiting for this parent (and their descendants).
	// orphanTipChanged is set if any recovered orphan further extended the tip.
	orphanTipChanged, recoveredBlocks := n.reprocessOrphans(b.Hash)

	return directTipChanged, orphanTipChanged, recoveredBlocks, nil
}

// reprocessOrphans drains the orphan buffer for blocks whose parent has just arrived.
// Returns whether the node's tip was updated and the slice of recovered blocks.
func (n *Node) reprocessOrphans(parentHash string) (bool, []Block) {
	tipChanged := false
	var recovered []Block
	queue := []string{parentHash}

	for len(queue) > 0 {
		currParentHash := queue[0]
		queue = queue[1:]

		waiting, ok := n.orphansByParent[currParentHash]
		if !ok || len(waiting) == 0 {
			continue
		}
		delete(n.orphansByParent, currParentHash)

		for _, orphan := range waiting {
			delete(n.orphans, orphan.Hash)
			parentBlk, parentFound := n.blocks[currParentHash]
			if !parentFound || orphan.Height != parentBlk.Height+1 {
				// Invalid orphan height discarded
				continue
			}

			n.blocks[orphan.Hash] = orphan
			recovered = append(recovered, orphan)

			oldTip := n.tipHash
			n.tipHash = selectBestTip(n.tipHash, orphan.Hash, n.tipHeight, orphan.Height)
			n.tipHeight = n.blocks[n.tipHash].Height
			if n.tipHash != oldTip {
				tipChanged = true
			}

			// This promoted orphan might be the parent of further buffered orphans.
			queue = append(queue, orphan.Hash)
		}
	}

	return tipChanged, recovered
}

// selectBestTip compares the current tip with a candidate block and returns
// the hash of whichever should be the new tip.
//
// Rule: choose the block at the greater height.
// Tie-break: if heights are equal, choose the block with the lexicographically
// smaller hash. This is deterministic and requires no external state.
func selectBestTip(currentTipHash, candidateHash string, currentHeight, candidateHeight int) string {
	if candidateHeight > currentHeight {
		return candidateHash
	}
	if candidateHeight < currentHeight {
		return currentTipHash
	}
	// Equal height — lowest hash wins (deterministic tie-break).
	if strings.Compare(candidateHash, currentTipHash) < 0 {
		return candidateHash
	}
	return currentTipHash
}

// Tip returns the current best block (the chain tip).
func (n *Node) Tip() Block {
	return n.blocks[n.tipHash]
}

// TipHash returns the hash of the current best tip.
func (n *Node) TipHash() string {
	return n.tipHash
}

// TipHeight returns the height of the current best tip.
func (n *Node) TipHeight() int {
	return n.tipHeight
}

// BlockCount returns the total number of blocks this node knows about.
func (n *Node) BlockCount() int {
	return len(n.blocks)
}

// GetBlock retrieves a block by hash. Returns false if not found.
func (n *Node) GetBlock(hash string) (Block, bool) {
	b, ok := n.blocks[hash]
	return b, ok
}
