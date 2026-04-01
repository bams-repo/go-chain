// Copyright (c) 2024-2026 The Fairchain Contributors
// Fairchain is an experiment in modularity, designed to improve on the work
// of Satoshi Nakamoto and to inspire more creative genius in the space.
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

package p2p

import (
	"testing"
	"time"

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

// --- Per-IP addr budget tests ---

func TestAddrBudget_InitialRemaining(t *testing.T) {
	b := &addrBudget{windowStart: time.Now()}
	r := b.remaining(time.Now())
	if r != addrBudgetMax {
		t.Fatalf("initial remaining = %d, want %d", r, addrBudgetMax)
	}
}

func TestAddrBudget_ConsumeReducesRemaining(t *testing.T) {
	b := &addrBudget{windowStart: time.Now()}
	b.consume(300)
	r := b.remaining(time.Now())
	if r != addrBudgetMax-300 {
		t.Fatalf("remaining after consume(300) = %d, want %d", r, addrBudgetMax-300)
	}
}

func TestAddrBudget_ExhaustedReturnsZero(t *testing.T) {
	b := &addrBudget{windowStart: time.Now()}
	b.consume(addrBudgetMax)
	r := b.remaining(time.Now())
	if r != 0 {
		t.Fatalf("remaining after full consume = %d, want 0", r)
	}
}

func TestAddrBudget_ResetsAfterWindow(t *testing.T) {
	start := time.Now().Add(-addrBudgetWindow - time.Second)
	b := &addrBudget{count: addrBudgetMax, windowStart: start}

	r := b.remaining(time.Now())
	if r != addrBudgetMax {
		t.Fatalf("remaining after window expiry = %d, want %d (budget should reset)", r, addrBudgetMax)
	}
	if b.count != 0 {
		t.Fatalf("count should be reset to 0, got %d", b.count)
	}
}

func TestAddrBudget_DoesNotResetWithinWindow(t *testing.T) {
	start := time.Now().Add(-addrBudgetWindow + time.Hour)
	b := &addrBudget{count: 500, windowStart: start}

	r := b.remaining(time.Now())
	if r != addrBudgetMax-500 {
		t.Fatalf("remaining within window = %d, want %d", r, addrBudgetMax-500)
	}
}

func TestAddrBudget_PersistsAcrossSimulatedReconnect(t *testing.T) {
	budgets := make(map[string]*addrBudget)

	ip := "192.168.1.100"
	budgets[ip] = &addrBudget{windowStart: time.Now()}
	budgets[ip].consume(800)

	// Simulate disconnect -- budget is NOT deleted (unlike old per-peer map).
	// Simulate reconnect -- same IP, different port.
	b, ok := budgets[ip]
	if !ok {
		t.Fatal("budget should persist after simulated disconnect")
	}
	r := b.remaining(time.Now())
	if r != addrBudgetMax-800 {
		t.Fatalf("remaining after reconnect = %d, want %d (budget must persist)", r, addrBudgetMax-800)
	}
}
