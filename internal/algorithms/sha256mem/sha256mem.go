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
const (
	Slots = 2097152
	HardenInterval = 128
	MixRounds = 32768
)
type Hasher struct{}

func New() *Hasher { return &Hasher{} }

func (h *Hasher) PoWHash(data []byte) types.Hash {
	seed := sha256.Sum256(data)

	memPtr := memPool.Get().(*[][32]byte)
	mem := *memPtr


	mem[0] = seed
	for i := 1; i < Slots; i++ {
		if i%HardenInterval == 0 {
			mem[i] = sha256.Sum256(mem[i-1][:])
		} else {
			arxFill(&mem[i], &mem[i-1], uint32(i))
		}
	}

	acc := mem[Slots-1]
	acc = mixPassA(acc, &mem)
	acc = mixPassB(acc, &mem)

	memPool.Put(memPtr)

	final := sha256.Sum256(acc[:])
	return types.Hash(final)
}

func mixPassA(acc [32]byte, mem *[][32]byte) [32]byte {
	m := *mem
	for i := 0; i < MixRounds; i++ {
		idx := binary.LittleEndian.Uint32(acc[:4]) % uint32(Slots)
		var buf [64]byte
		copy(buf[:32], acc[:])
		copy(buf[32:], m[idx][:])
		acc = sha256.Sum256(buf[:])
	}
	return acc
}

func mixPassB(acc [32]byte, mem *[][32]byte) [32]byte {
	m := *mem
	for i := 0; i < MixRounds; i++ {
		off := (i % 7) * 4
		idx := binary.LittleEndian.Uint32(acc[off:off+4]) % uint32(Slots)
		var buf [64]byte
		copy(buf[:32], acc[:])
		copy(buf[32:], m[idx][:])
		acc = sha256.Sum256(buf[:])
	}
	return acc
}
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
