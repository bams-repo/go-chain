// Copyright (c) 2024-2026 The Fairchain Contributors
// Fairchain is an experiment in modularity, designed to improve on the work
// of Satoshi Nakamoto and to inspire more creative genius in the space.
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

package sha256mem

import (
	"crypto/sha256"
	"encoding/binary"
	"sync"

	"github.com/bams-repo/fairchain/internal/types"
)

var memPool = sync.Pool{
	New: func() any {
		buf := make([][32]byte, Slots)
		return &buf
	},
}

// Consensus-critical parameters. Changing any of these is a hard fork.
//
// Memory-hard SHA256: fills a large buffer with chained SHA256 hashes, then
// performs data-dependent random reads over it. The random access pattern
// forces miners to keep the full buffer in fast memory, making memory
// bandwidth (not raw compute) the bottleneck. This compresses the
// performance gap between phones, desktops, and ASICs.
//
// All primitives are standard SHA256 — no novel cryptography.
const (
	// Slots is the number of 32-byte entries in the memory buffer.
	// Memory usage = Slots * 32 bytes. 131072 * 32 = 4 MiB.
	// 4 MiB fits in phone L3 cache (~12 MB) with room for 2-3 mining
	// threads, while exceeding GPU per-SM L2 (~560 KB) by 7x — forcing
	// GPU threads to hit VRAM at ~300-800 ns per random read.
	Slots = 131072

	// MixRounds is the number of random-read mixing passes.
	// Each round does ChaseDepth serial pointer-chasing lookups followed
	// by one SHA256. 128 rounds * 8 hops = 1,024 serial random reads
	// per hash, creating a latency-bound chain that CPUs serve from L3
	// at ~10 ns/hop while GPUs stall at ~300-800 ns/hop.
	MixRounds = 128

	// ChaseDepth is the number of serial data-dependent memory lookups
	// per mix round. Each hop reads a slot and derives the next address
	// from its contents, creating an unpredictable pointer chain that
	// cannot be parallelized. This is the primary GPU/ASIC deterrent:
	// the latency of each hop is dictated by cache hierarchy, and CPUs
	// have 20-80x lower random-access latency than GPU VRAM.
	ChaseDepth = 8
)

// Hasher implements memory-hard SHA256 proof-of-work hashing.
//
// Algorithm:
//  1. Seed: SHA256(header) → mem[0]
//  2. Fill: mem[i] = SHA256(mem[i-1]) for i in 1..Slots-1
//  3. Mix: 128 rounds of pointer-chasing (8 serial dependent lookups)
//     followed by one SHA256 per round
//  4. Finalize: SHA256(accumulator) → output hash
//
// The fill phase is sequential (each slot depends on the previous),
// preventing parallel precomputation. The mix phase performs serial
// pointer-chasing where each address depends on the data at the
// previous address, creating a latency-bound chain that CPUs serve
// from L3 cache at ~10 ns/hop while GPUs stall at ~300-800 ns/hop.
type Hasher struct{}

func New() *Hasher { return &Hasher{} }

func (h *Hasher) PoWHash(data []byte) types.Hash {
	// Phase 1: Seed from header.
	seed := sha256.Sum256(data)

	// Phase 2: Fill memory buffer with chained SHA256 hashes.
	memPtr := memPool.Get().(*[][32]byte)
	mem := *memPtr
	mem[0] = seed
	for i := 1; i < Slots; i++ {
		mem[i] = sha256.Sum256(mem[i-1][:])
	}

	// Phase 3: Memory-hard mixing — pointer-chasing + SHA256.
	acc := mem[Slots-1]
	for i := 0; i < MixRounds; i++ {
		idx := binary.LittleEndian.Uint32(acc[:4]) % uint32(Slots)
		for hop := 0; hop < ChaseDepth; hop++ {
			idx = binary.LittleEndian.Uint32(mem[idx][:4]) % uint32(Slots)
		}
		var buf [64]byte
		copy(buf[:32], acc[:])
		copy(buf[32:], mem[idx][:])
		acc = sha256.Sum256(buf[:])
	}

	memPool.Put(memPtr)

	// Phase 4: Final hash.
	final := sha256.Sum256(acc[:])
	return types.Hash(final)
}

func (h *Hasher) Name() string { return "sha256mem" }
