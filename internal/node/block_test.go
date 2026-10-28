package node_test

import (
	"strings"
	"testing"

	"github.com/pranav-k/forkrisk/internal/node"
)

// TestGenesisConsistency verifies that every call to GenesisBlock() returns
// the same block with the same hash. This is critical: if two nodes start with
// different genesis blocks, they can never agree on a common chain.
func TestGenesisConsistency(t *testing.T) {
	g1 := node.GenesisBlock()
	g2 := node.GenesisBlock()
	if g1.Hash != g2.Hash {
		t.Errorf("Genesis hashes differ: %s vs %s", g1.Hash, g2.Hash)
	}
	if g1.Height != 0 {
		t.Errorf("Genesis height should be 0, got %d", g1.Height)
	}
	if g1.ParentHash != "" {
		t.Errorf("Genesis parent hash should be empty, got %q", g1.ParentHash)
	}
}

// TestGenesisHashDeterminism verifies that the hash is stable across calls.
func TestGenesisHashDeterminism(t *testing.T) {
	hashes := make([]string, 10)
	for i := range hashes {
		hashes[i] = node.GenesisBlock().Hash
	}
	for i := 1; i < len(hashes); i++ {
		if hashes[i] != hashes[0] {
			t.Errorf("Genesis hash changed on call %d: %s vs %s", i, hashes[i], hashes[0])
		}
	}
}

// TestBlockHashDeterminism verifies that the same inputs always produce the same hash.
func TestBlockHashDeterminism(t *testing.T) {
	h1 := node.ComputeHash(1, "N0", "parentabc", 1.5, 3)
	h2 := node.ComputeHash(1, "N0", "parentabc", 1.5, 3)
	if h1 != h2 {
		t.Errorf("Same inputs produced different hashes: %s vs %s", h1, h2)
	}
}

// TestBlockHashUniqueness verifies that different inputs produce different hashes.
// A hash collision here would cause the simulation to corrupt chain state.
func TestBlockHashUniqueness(t *testing.T) {
	cases := []struct {
		height    int
		proposer  string
		parent    string
		createdAt float64
		txCount   int
	}{
		{1, "N0", "parent1", 1.0, 5},
		{2, "N0", "parent1", 1.0, 5}, // different height
		{1, "N1", "parent1", 1.0, 5}, // different proposer
		{1, "N0", "parent2", 1.0, 5}, // different parent
		{1, "N0", "parent1", 2.0, 5}, // different time
		{1, "N0", "parent1", 1.0, 6}, // different txCount
	}
	hashes := make(map[string]struct{})
	for _, c := range cases {
		h := node.ComputeHash(c.height, c.proposer, c.parent, c.createdAt, c.txCount)
		if _, dup := hashes[h]; dup {
			t.Errorf("Hash collision for inputs %+v", c)
		}
		hashes[h] = struct{}{}
	}
}

// TestBlockHashIsLowercase verifies that hashes are lowercase hex (important for
// the string comparison in the tie-break rule).
func TestBlockHashIsLowercase(t *testing.T) {
	h := node.ComputeHash(1, "N0", "parent", 1.0, 3)
	if h != strings.ToLower(h) {
		t.Errorf("Hash is not lowercase: %s", h)
	}
}

// TestNewBlock verifies the NewBlock factory correctly sets all fields.
func TestNewBlock(t *testing.T) {
	genesis := node.GenesisBlock()
	blk := node.NewBlock(1, "N0", genesis.Hash, 1.0, 7)

	if blk.Height != 1 {
		t.Errorf("expected height 1, got %d", blk.Height)
	}
	if blk.ProposerID != "N0" {
		t.Errorf("expected proposer N0, got %s", blk.ProposerID)
	}
	if blk.ParentHash != genesis.Hash {
		t.Errorf("expected parent %s, got %s", genesis.Hash, blk.ParentHash)
	}
	if blk.TxCount != 7 {
		t.Errorf("expected txCount 7, got %d", blk.TxCount)
	}
	if blk.CreatedAt != 1.0 {
		t.Errorf("expected createdAt 1.0, got %f", blk.CreatedAt)
	}
	if blk.Hash == "" {
		t.Error("block hash should not be empty")
	}
	if blk.Hash == genesis.Hash {
		t.Error("child block hash should differ from genesis hash")
	}
}
