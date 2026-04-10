// Copyright (c) 2024-2026 The Fairchain Contributors
// Fairchain is an experiment in modularity, designed to improve on the work
// of Satoshi Nakamoto and to inspire more creative genius in the space.
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

package sha256mem

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/bams-repo/fairchain/internal/types"
)

func TestPoWHashDeterministic(t *testing.T) {
	h := New()
	input := []byte("test vector for sha256mem pow hash")
	got1 := h.PoWHash(input)
	got2 := h.PoWHash(input)

	if got1 == types.ZeroHash {
		t.Fatal("PoWHash returned zero hash")
	}
	if got1 != got2 {
		t.Fatal("PoWHash is not deterministic")
	}
}

func TestPoWHashDifferentInputs(t *testing.T) {
	h := New()
	a := h.PoWHash([]byte("input A"))
	b := h.PoWHash([]byte("input B"))

	if a == b {
		t.Fatal("different inputs produced the same hash")
	}
}

func TestPoWHashEmptyInput(t *testing.T) {
	h := New()
	got := h.PoWHash([]byte{})
	if got == types.ZeroHash {
		t.Fatal("PoWHash of empty input returned zero hash")
	}
}

func TestPoWHashNotPlainSHA256(t *testing.T) {
	h := New()
	input := []byte("sha256mem known vector")
	got := h.PoWHash(input)

	// Verify the output differs from plain SHA256 and SHA256d.
	// This confirms the memory fill + mixing phases affect the result.
	first := sha256.Sum256(input)
	second := sha256.Sum256(first[:])

	if types.Hash(first) == got {
		t.Fatal("output matches plain SHA256 — memory mixing has no effect")
	}
	if types.Hash(second) == got {
		t.Fatal("output matches SHA256d — memory mixing has no effect")
	}
}

func TestPoWHashKnownVector(t *testing.T) {
	h := New()

	// Locked known vector. Any change to constants, fill, mix, or
	// finalize logic will break this test.
	// BE output: c44266989d33a18aeefde8a63588433ff51d61a0afddff88fe9b533bc2d19469
	// PoWHash returns LE (reversed):
	input := []byte{}
	want, _ := hex.DecodeString("6994d1c23b539bfe88ffddafa0611df53f438835a6e8fdee8aa1339d986642c4")
	var expected types.Hash
	copy(expected[:], want)

	got := h.PoWHash(input)
	if got != expected {
		t.Fatalf("known vector mismatch\n  expected %x\n  got      %x", expected, got)
	}

	// Confirm bitwise reproducibility over many iterations.
	for i := 0; i < 10; i++ {
		got := h.PoWHash(input)
		if got != expected {
			t.Fatalf("iteration %d: hash changed\n  expected %x\n  got      %x", i, expected, got)
		}
	}
}

func TestPoWHashFillAndMixContribute(t *testing.T) {
	h := New()
	input := []byte("sha256mem dependency test")
	got := h.PoWHash(input)

	// The output must differ from a naive SHA256 chain that skips
	// the memory fill and mix phases entirely.
	seed := sha256.Sum256(input)
	chain1 := sha256.Sum256(seed[:])
	chain2 := sha256.Sum256(chain1[:])

	if types.Hash(chain2) == got {
		t.Fatal("output matches naive SHA256 chain — fill/mix has no effect")
	}
}

func TestName(t *testing.T) {
	h := New()
	if h.Name() != "sha256mem" {
		t.Fatalf("expected name sha256mem, got %s", h.Name())
	}
}

func TestConcurrentSafety(t *testing.T) {
	h := New()
	input := []byte("concurrent test data")
	expected := h.PoWHash(input)

	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			for j := 0; j < 10; j++ {
				got := h.PoWHash(input)
				if got != expected {
					t.Errorf("concurrent PoWHash mismatch")
					return
				}
			}
		}()
	}
	for i := 0; i < 10; i++ {
		<-done
	}
}

func BenchmarkPoWHash(b *testing.B) {
	h := New()
	input := []byte("benchmark input for sha256mem")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h.PoWHash(input)
	}
}

// BenchmarkPoWHashParallel runs PoWHash from GOMAXPROCS goroutines (default: all CPUs).
func BenchmarkPoWHashParallel(b *testing.B) {
	h := New()
	input := []byte("benchmark input for sha256mem")
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			h.PoWHash(input)
		}
	})
}
