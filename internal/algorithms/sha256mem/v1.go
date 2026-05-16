// Copyright (c) 2024-2026 The Fairchain Contributors
// Distributed under the MIT software license.

package sha256mem

import (
	"crypto/sha256"
	"encoding/binary"

	"github.com/bams-repo/fairchain/internal/types"
)

const (
	slotsV1           = 2097152
	hardenIntervalV1  = 128
	mixRoundsV1       = 32768
)

func powHashV1(data []byte) types.Hash {
	seed := sha256.Sum256(data)

	memPtr := memPool.Get().(*[][32]byte)
	mem := *memPtr

	mem[0] = seed
	for i := 1; i < slotsV1; i++ {
		if i%hardenIntervalV1 == 0 {
			mem[i] = sha256.Sum256(mem[i-1][:])
		} else {
			arxFill(&mem[i], &mem[i-1], uint32(i))
		}
	}

	acc := mem[slotsV1-1]
	acc = mixPassA(acc, &mem, mixRoundsV1)
	acc = mixPassB(acc, &mem, mixRoundsV1)

	memPool.Put(memPtr)

	final := sha256.Sum256(acc[:])
	return types.Hash(final).Reversed()
}

func mixPassA(acc [32]byte, mem *[][32]byte, rounds int) [32]byte {
	m := *mem
	var buf [64]byte
	for i := 0; i < rounds; i++ {
		idx := binary.LittleEndian.Uint32(acc[:4]) % uint32(slotsV1)
		copy(buf[:32], acc[:])
		copy(buf[32:], m[idx][:])
		acc = sha256.Sum256(buf[:])
	}
	return acc
}

func mixPassB(acc [32]byte, mem *[][32]byte, rounds int) [32]byte {
	m := *mem
	var buf [64]byte
	for i := 0; i < rounds; i++ {
		off := (i % 7) * 4
		idx := binary.LittleEndian.Uint32(acc[off:off+4]) % uint32(slotsV1)
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
