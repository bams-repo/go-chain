package p2p

import (
	"testing"

	"github.com/bams-repo/fairchain/internal/types"
)

// TestSeenBlocksEviction verifies that the seenBlocks bounded hash set
// correctly removes entries, allowing previously-rejected blocks to be
// re-processed. This is the core mechanism behind the TASK-02 fix:
// handleBlock calls seenBlocks.Remove for any block that fails ProcessBlock
// (except side-chain acceptance), so the block can be re-delivered by
// another peer.
func TestSeenBlocksEviction(t *testing.T) {
	set := newBoundedHashSet(100)

	blockA := types.Hash{0x01}
	blockB := types.Hash{0x02}
	blockC := types.Hash{0x03}

	// Simulate handleBlock: AddOrHas returns false on first encounter.
	if set.AddOrHas(blockA) {
		t.Fatal("blockA should not be seen on first encounter")
	}
	if set.AddOrHas(blockB) {
		t.Fatal("blockB should not be seen on first encounter")
	}

	// Both blocks are now "seen" — second delivery would be skipped.
	if !set.Has(blockA) {
		t.Fatal("blockA should be in the set after AddOrHas")
	}
	if !set.Has(blockB) {
		t.Fatal("blockB should be in the set after AddOrHas")
	}

	// Simulate handleBlock rejection path: Remove the rejected block.
	// This is what happens when ProcessBlock returns an error that is
	// NOT ErrSideChain (e.g., orphan, validation failure).
	set.Remove(blockA)

	// blockA should now be requestable again.
	if set.Has(blockA) {
		t.Fatal("blockA should NOT be in the set after Remove")
	}

	// blockB should still be seen (it was accepted).
	if !set.Has(blockB) {
		t.Fatal("blockB should still be in the set")
	}

	// Re-delivery of blockA should succeed (AddOrHas returns false).
	if set.AddOrHas(blockA) {
		t.Fatal("blockA should be processable after Remove")
	}

	// blockC was never added — should not be seen.
	if set.Has(blockC) {
		t.Fatal("blockC should not be in the set")
	}
}

// TestSeenBlocksCapacityEviction verifies FIFO eviction when the set
// reaches capacity, ensuring old entries are evicted to make room.
func TestSeenBlocksCapacityEviction(t *testing.T) {
	set := newBoundedHashSet(3)

	h1 := types.Hash{0x01}
	h2 := types.Hash{0x02}
	h3 := types.Hash{0x03}
	h4 := types.Hash{0x04}

	set.Add(h1)
	set.Add(h2)
	set.Add(h3)

	// All three should be present.
	if !set.Has(h1) || !set.Has(h2) || !set.Has(h3) {
		t.Fatal("all three hashes should be present")
	}

	// Adding a 4th should evict h1 (FIFO).
	set.Add(h4)
	if set.Has(h1) {
		t.Fatal("h1 should have been evicted (FIFO)")
	}
	if !set.Has(h2) || !set.Has(h3) || !set.Has(h4) {
		t.Fatal("h2, h3, h4 should all be present")
	}
}

// TestSeenBlocksRemoveIdempotent verifies that removing a hash that
// isn't in the set is a no-op and doesn't panic.
func TestSeenBlocksRemoveIdempotent(t *testing.T) {
	set := newBoundedHashSet(10)

	h := types.Hash{0xAA}

	// Remove before Add — should be a no-op.
	set.Remove(h)
	if set.Has(h) {
		t.Fatal("hash should not appear after removing a non-existent entry")
	}

	// Add then remove twice — second remove is a no-op.
	set.Add(h)
	set.Remove(h)
	set.Remove(h)
	if set.Has(h) {
		t.Fatal("hash should not be present after double remove")
	}
}

// TestSeenBlocksAddOrHasIdempotent verifies that AddOrHas is idempotent:
// calling it twice with the same hash returns true on the second call
// without adding a duplicate entry.
func TestSeenBlocksAddOrHasIdempotent(t *testing.T) {
	set := newBoundedHashSet(10)

	h := types.Hash{0xBB}

	if set.AddOrHas(h) {
		t.Fatal("first AddOrHas should return false")
	}
	if !set.AddOrHas(h) {
		t.Fatal("second AddOrHas should return true (already present)")
	}
}
