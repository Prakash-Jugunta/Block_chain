// Package node defines the Block type, genesis block, and block hashing.
// A Block is the fundamental unit of the blockchain.
package node

import (
	"crypto/sha256"
	"fmt"
)

// Block represents a single block in the blockchain.
// All fields are immutable after creation.
type Block struct {
	// Hash is the unique identifier of this block.
	// Computed as SHA-256(height + proposerID + parentHash + createdAt + txCount).
	Hash string `json:"hash"`

	// ParentHash is the hash of the block this block extends.
	// The genesis block has ParentHash = "".
	ParentHash string `json:"parent_hash"`

	// Height is the position of this block in the chain.
	// Genesis = 0, first real block = 1, and so on.
	Height int `json:"height"`

	// ProposerID is the node ID that created this block.
	ProposerID string `json:"proposer_id"`

	// CreatedAt is the simulated time (in seconds) when this block was proposed.
	CreatedAt float64 `json:"created_at"`

	// TxCount is the number of transactions in this block.
	TxCount int `json:"tx_count"`
}

// ComputeHash returns the deterministic SHA-256 hash of a block's fields.
// The hash encodes height, proposer, parent, timestamp, and tx count.
// Result is a lowercase hexadecimal string.
func ComputeHash(height int, proposerID, parentHash string, createdAt float64, txCount int) string {
	input := fmt.Sprintf("%d|%s|%s|%.9f|%d", height, proposerID, parentHash, createdAt, txCount)
	sum := sha256.Sum256([]byte(input))
	return fmt.Sprintf("%x", sum)
}

// NewBlock creates a fully initialised Block and computes its hash.
func NewBlock(height int, proposerID, parentHash string, createdAt float64, txCount int) Block {
	h := ComputeHash(height, proposerID, parentHash, createdAt, txCount)
	return Block{
		Hash:       h,
		ParentHash: parentHash,
		Height:     height,
		ProposerID: proposerID,
		CreatedAt:  createdAt,
		TxCount:    txCount,
	}
}

// genesisHash is computed once at package init and shared by all nodes.
// Every node starts with the exact same genesis block — this is critical for
// consistent chain selection.
var genesisHash string

func init() {
	// Genesis uses sentinel values. createdAt=0 and txCount=0.
	genesisHash = ComputeHash(0, "genesis", "", 0.0, 0)
}

// GenesisBlock returns the canonical genesis block.
// Every node calls this to obtain its initial chain state.
func GenesisBlock() Block {
	return Block{
		Hash:       genesisHash,
		ParentHash: "",
		Height:     0,
		ProposerID: "genesis",
		CreatedAt:  0.0,
		TxCount:    0,
	}
}
