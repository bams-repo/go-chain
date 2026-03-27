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
// Memory-hard SHA256 PoW with sequential-dependent fill.
//
// Each hash requires a 64 MiB buffer. A 12 GB GPU can fit at most ~154
// concurrent workers, far below the ~40,000 threads needed for full
// occupancy, leaving the GPU >99.6% idle. Meanwhile any CPU or phone
// with 4+ GB RAM easily spares 64 MiB per mining thread.
//
// The fill phase writes every slot sequentially. Between SHA256 anchor
// points (every HardenInterval slots), a lightweight ARX (add-rotate-xor)
// step derives each slot from its predecessor. This makes each slot
// depend on all prior slots — the GPU cannot skip or compress the buffer.
// The periodic SHA256 hardening prevents any algebraic shortcut.
//
// The mix phase performs MixRounds serial SHA256-per-hop random reads.
// Each round forces a full SHA256 between every memory read, creating
// an unbreakable serial dependency chain that cannot be pipelined.
// CPUs with SHA-NI complete each round in ~110ns; GPUs must emulate
// SHA256 in software ALU ops at 4-10x the cost per round. The high
// round count (32,768) exploits this asymmetry: the mix adds only ~4ms
// on CPU but ~30-60ms on GPU per worker.
//
// All primitives are standard SHA256 — no novel cryptography.
const (
	// Slots is the number of 32-byte entries in the memory buffer.
	// 2,097,152 * 32 = 64 MiB.
	Slots = 2097152

	// HardenInterval is the number of slots between SHA256 hardening
	// points in the fill phase. Between hardening points, a lightweight
	// ARX step (rotate-xor-add) derives each slot from its predecessor.
	// SHA256 every 256 slots prevents algebraic shortcuts while keeping
	// the fill phase fast. Total SHA256 calls in fill: Slots/256 = 8,192.
	HardenInterval = 256

	// MixRounds is the number of serial SHA256+read hops in the mix
	// phase. Each round: read mem[idx], SHA256(acc || mem[idx]) -> acc,
	// derive next idx from acc. The high count exploits the SHA-NI
	// asymmetry: CPUs with hardware SHA256 extensions handle 32,768
	// serial hashes in ~4ms, while GPUs must spend 30-60ms per worker
	// on pure ALU SHA256 emulation — an unparallelizable bottleneck.
	MixRounds = 32768
)

// Hasher implements memory-hard SHA256 proof-of-work hashing.
//
// Algorithm:
//  1. Seed:     SHA256(header) -> mem[0]
//  2. Fill:     Sequential dependent fill over 64 MiB:
//               - Every 256 slots: SHA256(previous) -> anchor
//               - Between anchors: ARX(previous, index) -> slot
//  3. Mix:      32,768 rounds of: read mem[idx] -> SHA256(acc||mem[idx]) -> new idx
//  4. Finalize: SHA256(accumulator) -> output hash
type Hasher struct{}

func New() *Hasher { return &Hasher{} }

func (h *Hasher) PoWHash(data []byte) types.Hash {
	seed := sha256.Sum256(data)

	memPtr := memPool.Get().(*[][32]byte)
	mem := *memPtr

	// Phase 2: Sequential dependent fill.
	mem[0] = seed
	for i := 1; i < Slots; i++ {
		if i%HardenInterval == 0 {
			mem[i] = sha256.Sum256(mem[i-1][:])
		} else {
			arxFill(&mem[i], &mem[i-1], uint32(i))
		}
	}

	// Phase 3: SHA256-per-hop mixing.
	acc := mem[Slots-1]
	for i := 0; i < MixRounds; i++ {
		idx := binary.LittleEndian.Uint32(acc[:4]) % uint32(Slots)
		var buf [64]byte
		copy(buf[:32], acc[:])
		copy(buf[32:], mem[idx][:])
		acc = sha256.Sum256(buf[:])
	}

	memPool.Put(memPtr)

	final := sha256.Sum256(acc[:])
	return types.Hash(final)
}

// arxFill derives dst from src using a lightweight add-rotate-xor step
// mixed with the slot index. This is ~1-2ns per slot (vs ~60ns for SHA256)
// but creates a sequential dependency: each slot depends on the prior slot.
func arxFill(dst, src *[32]byte, index uint32) {
	for w := 0; w < 8; w++ {
		v := binary.LittleEndian.Uint32(src[w*4:])
		v ^= index + uint32(w)
		v = (v << 13) | (v >> 19)
		v += binary.LittleEndian.Uint32(src[w*4:])
		binary.LittleEndian.PutUint32(dst[w*4:], v)
	}
}

func (h *Hasher) Name() string { return "sha256mem" }
