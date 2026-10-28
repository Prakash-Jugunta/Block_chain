package node_test

import (
	"errors"
	"testing"

	"github.com/pranav-k/forkrisk/internal/node"
)

// TestNewNodeStartsAtGenesis verifies that a freshly created node has the
// genesis block as its tip (height 0).
func TestNewNodeStartsAtGenesis(t *testing.T) {
	n := node.NewNode("N0", []string{"N1"})
	genesis := node.GenesisBlock()

	if n.TipHash() != genesis.Hash {
		t.Errorf("expected tip hash %s, got %s", genesis.Hash, n.TipHash())
	}
	if n.TipHeight() != 0 {
		t.Errorf("expected tip height 0, got %d", n.TipHeight())
	}
	if n.BlockCount() != 1 {
		t.Errorf("expected 1 block (genesis), got %d", n.BlockCount())
	}
}

// TestAddBlockExtendsTip verifies that adding a valid child block updates the tip.
func TestAddBlockExtendsTip(t *testing.T) {
	n := node.NewNode("N0", nil)
	genesis := node.GenesisBlock()

	blk := node.NewBlock(1, "N0", genesis.Hash, 1.0, 3)
	changed, _, _, err := n.AddBlock(blk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !changed {
		t.Error("tip should have changed after adding a taller block")
	}
	if n.TipHash() != blk.Hash {
		t.Errorf("expected tip %s, got %s", blk.Hash, n.TipHash())
	}
	if n.TipHeight() != 1 {
		t.Errorf("expected height 1, got %d", n.TipHeight())
	}
}

// TestAddDuplicateBlockIgnored verifies that adding the same block twice is a no-op.
func TestAddDuplicateBlockIgnored(t *testing.T) {
	n := node.NewNode("N0", nil)
	genesis := node.GenesisBlock()
	blk := node.NewBlock(1, "N0", genesis.Hash, 1.0, 3)

	_, _, _, _ = n.AddBlock(blk)
	changed, _, _, err := n.AddBlock(blk)
	if err == nil && changed {
		t.Error("duplicate block should not change tip")
	}
}

// TestParentChildRelationship verifies that a chain of blocks is stored correctly.
func TestParentChildRelationship(t *testing.T) {
	n := node.NewNode("N0", nil)
	genesis := node.GenesisBlock()

	b1 := node.NewBlock(1, "N0", genesis.Hash, 1.0, 2)
	b2 := node.NewBlock(2, "N0", b1.Hash, 2.0, 3)
	b3 := node.NewBlock(3, "N0", b2.Hash, 3.0, 4)

	for _, blk := range []node.Block{b1, b2, b3} {
		if _, _, _, err := n.AddBlock(blk); err != nil {
			t.Fatalf("failed to add block at height %d: %v", blk.Height, err)
		}
	}

	if n.TipHeight() != 3 {
		t.Errorf("expected height 3, got %d", n.TipHeight())
	}
	if n.TipHash() != b3.Hash {
		t.Errorf("expected tip %s, got %s", b3.Hash, n.TipHash())
	}
}

// TestOrphanBufferingAndReprocessing verifies that a block with an unknown parent
// is buffered as an orphan rather than rejected, and then reprocessed when its parent arrives.
func TestOrphanBufferingAndReprocessing(t *testing.T) {
	n := node.NewNode("N0", nil)
	genesis := node.GenesisBlock()

	// Create parent at height 1 and child at height 2.
	parent := node.NewBlock(1, "N0", genesis.Hash, 1.0, 1)
	child := node.NewBlock(2, "N0", parent.Hash, 2.0, 1)

	// Deliver child FIRST (out of order arrival).
	directChanged, orphanChanged, recovered, err := n.AddBlock(child)
	tipChanged := directChanged || orphanChanged
	if !errors.Is(err, node.ErrOrphanBuffered) {
		t.Fatalf("expected ErrOrphanBuffered, got %v", err)
	}
	if tipChanged {
		t.Error("tip should not change when buffering an orphan block")
	}
	if len(recovered) != 0 {
		t.Errorf("expected 0 recovered blocks, got %d", len(recovered))
	}
	if !n.IsOrphan(child.Hash) {
		t.Error("child block should be in orphan buffer")
	}
	if n.OrphanCount() != 1 {
		t.Errorf("expected orphan count 1, got %d", n.OrphanCount())
	}
	if n.TipHeight() != 0 {
		t.Errorf("expected tip height 0 while orphan is buffered, got %d", n.TipHeight())
	}

	// Now deliver the parent.
	var directChanged2, orphanChanged2 bool
	directChanged2, orphanChanged2, recovered, err = n.AddBlock(parent)
	tipChanged = directChanged2 || orphanChanged2
	if err != nil {
		t.Fatalf("unexpected error adding parent: %v", err)
	}
	if !tipChanged {
		t.Error("tip should have changed after parent and reprocessed child were added")
	}
	if len(recovered) != 1 || recovered[0].Hash != child.Hash {
		t.Errorf("expected child block in recovered slice, got: %+v", recovered)
	}

	// Both parent and child should now be recognized as validated blocks.
	if !n.HasBlock(parent.Hash) {
		t.Error("parent block should be in blocks")
	}
	if !n.HasBlock(child.Hash) {
		t.Error("child block should have been promoted to blocks")
	}
	if n.IsOrphan(child.Hash) {
		t.Error("child block should no longer be in orphan buffer")
	}
	if n.OrphanCount() != 0 {
		t.Errorf("expected 0 orphans remaining, got %d", n.OrphanCount())
	}
	if n.TipHeight() != 2 {
		t.Errorf("expected final tip height 2, got %d", n.TipHeight())
	}
	if n.TipHash() != child.Hash {
		t.Errorf("expected tip hash %s, got %s", child.Hash, n.TipHash())
	}
}

// TestMultiLevelOrphanCascade verifies that a multi-level chain of orphans
// (e.g. grandchild -> child -> parent) cascades cleanly when the root parent arrives.
func TestMultiLevelOrphanCascade(t *testing.T) {
	n := node.NewNode("N0", nil)
	genesis := node.GenesisBlock()

	b1 := node.NewBlock(1, "N0", genesis.Hash, 1.0, 1)
	b2 := node.NewBlock(2, "N0", b1.Hash, 2.0, 1)
	b3 := node.NewBlock(3, "N0", b2.Hash, 3.0, 1)

	// Deliver in reverse order: b3, b2, b1
	if _, _, _, err := n.AddBlock(b3); !errors.Is(err, node.ErrOrphanBuffered) {
		t.Fatalf("expected b3 buffered as orphan, got: %v", err)
	}
	if _, _, _, err := n.AddBlock(b2); !errors.Is(err, node.ErrOrphanBuffered) {
		t.Fatalf("expected b2 buffered as orphan, got: %v", err)
	}

	if n.OrphanCount() != 2 {
		t.Fatalf("expected 2 orphans, got %d", n.OrphanCount())
	}

	// Deliver b1
	directC, orphanC, recovered, err := n.AddBlock(b1)
	tipChanged := directC || orphanC
	if err != nil {
		t.Fatalf("unexpected error adding b1: %v", err)
	}
	if !tipChanged {
		t.Error("tip should have changed after cascade")
	}
	if len(recovered) != 2 {
		t.Errorf("expected 2 recovered blocks, got %d", len(recovered))
	}

	if n.OrphanCount() != 0 {
		t.Errorf("expected 0 orphans remaining after full cascade, got %d", n.OrphanCount())
	}
	if n.TipHeight() != 3 {
		t.Errorf("expected tip height 3, got %d", n.TipHeight())
	}
	if n.TipHash() != b3.Hash {
		t.Errorf("expected tip hash %s, got %s", b3.Hash, n.TipHash())
	}
}

// TestInvalidBlockHeightRejected verifies that a block with an invalid height relative
// to its known parent is rejected with ErrInvalidBlockHeight.
func TestInvalidBlockHeightRejected(t *testing.T) {
	n := node.NewNode("N0", nil)
	genesis := node.GenesisBlock()

	// Child has height 3 instead of 1
	invalidBlk := node.NewBlock(3, "N0", genesis.Hash, 1.0, 1)
	_, _, _, err := n.AddBlock(invalidBlk)
	if !errors.Is(err, node.ErrInvalidBlockHeight) {
		t.Errorf("expected ErrInvalidBlockHeight, got: %v", err)
	}
}

// TestLongestChainSelection verifies that the node always keeps the tallest tip.
func TestLongestChainSelection(t *testing.T) {
	n := node.NewNode("N0", nil)
	genesis := node.GenesisBlock()

	// Add block at height 1 (fork A) from N0.
	forkA := node.NewBlock(1, "N0", genesis.Hash, 1.0, 1)
	if _, _, _, err := n.AddBlock(forkA); err != nil {
		t.Fatalf("forkA: %v", err)
	}

	// Add block at height 1 (fork B) from N1 — same height as forkA.
	forkB := node.NewBlock(1, "N1", genesis.Hash, 1.001, 1)
	// Tip should switch only if forkB.Hash < forkA.Hash (tie-break).
	_, _, _, _ = n.AddBlock(forkB)
	// Whichever has the smaller hash should win.
	expectedTip := forkA.Hash
	if forkB.Hash < forkA.Hash {
		expectedTip = forkB.Hash
	}
	if n.TipHash() != expectedTip {
		t.Errorf("expected tie-break winner %s, got %s", expectedTip[:8], n.TipHash()[:8])
	}

	// Now add a height-2 block extending forkA — must become the new tip
	// regardless of hash ordering.
	blk2 := node.NewBlock(2, "N0", forkA.Hash, 2.0, 1)
	changed, _, _, err := n.AddBlock(blk2)
	if err != nil {
		t.Fatalf("blk2: %v", err)
	}
	if !changed {
		t.Error("adding height-2 block should have changed the tip")
	}
	if n.TipHeight() != 2 {
		t.Errorf("expected height 2, got %d", n.TipHeight())
	}
}

// TestTieBreakByHash verifies that when two blocks have equal height, the one
// with the lexicographically smaller hash wins. This must be consistent.
func TestTieBreakByHash(t *testing.T) {
	genesis := node.GenesisBlock()

	// Create two blocks at the same height with different proposers so hashes differ.
	// We don't know in advance which hash is smaller, but the rule must be consistent.
	blkA := node.NewBlock(1, "N0", genesis.Hash, 1.0, 1)
	blkB := node.NewBlock(1, "N1", genesis.Hash, 1.0, 1)

	// Both nodes start fresh.
	n1 := node.NewNode("N1", nil)
	n2 := node.NewNode("N2", nil)

	// n1: receives A first, then B.
	_, _, _, _ = n1.AddBlock(blkA)
	_, _, _, _ = n1.AddBlock(blkB)

	// n2: receives B first, then A.
	_, _, _, _ = n2.AddBlock(blkB)
	_, _, _, _ = n2.AddBlock(blkA)

	// Both nodes should settle on the same tip regardless of arrival order.
	if n1.TipHash() != n2.TipHash() {
		t.Errorf("arrival order should not affect final tip: n1=%s n2=%s",
			n1.TipHash()[:8], n2.TipHash()[:8])
	}
}

// TestHasBlock verifies the HasBlock check.
func TestHasBlock(t *testing.T) {
	n := node.NewNode("N0", nil)
	genesis := node.GenesisBlock()

	if !n.HasBlock(genesis.Hash) {
		t.Error("node should have genesis block")
	}
	if n.HasBlock("nonexistent") {
		t.Error("node should not have a block it never received")
	}
}
