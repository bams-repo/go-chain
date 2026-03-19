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
	// Memory usage = Slots * 32 bytes. 65536 * 32 = 2 MiB.
	// 2 MiB fits comfortably in L2/L3 cache on phones and desktops alike,
	// while being large enough to prevent ASIC register-file tricks.
	Slots = 65536

	// MixRounds is the number of random-read mixing passes.
	// Each round does one data-dependent buffer lookup + one SHA256.
	// 64 rounds ensures the access pattern is thoroughly unpredictable.
	MixRounds = 64
)

// Hasher implements memory-hard SHA256 proof-of-work hashing.
//
// Algorithm:
//  1. Seed: SHA256(header) → mem[0]
//  2. Fill: mem[i] = SHA256(mem[i-1]) for i in 1..Slots-1
//  3. Mix: 64 rounds of data-dependent random reads from the buffer
//  4. Finalize: SHA256(accumulator) → output hash
//
// The fill phase is sequential (each slot depends on the previous),
// preventing parallel precomputation. The mix phase has an unpredictable
// access pattern (each index depends on the current accumulator value),
// preventing memory-time tradeoffs.
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

	// Phase 3: Memory-hard mixing — read pattern depends on accumulator.
	acc := mem[Slots-1]
	for i := 0; i < MixRounds; i++ {
		idx := binary.LittleEndian.Uint32(acc[:4]) % uint32(Slots)
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
