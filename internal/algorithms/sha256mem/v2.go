// Copyright (c) 2024-2026 The Fairchain Contributors
// Distributed under the MIT software license.

package sha256mem

import (
	"crypto/sha256"
	"encoding/binary"

	"github.com/bams-repo/fairchain/internal/types"
)

const (
	slotsV2          = 2097152
	hardenIntervalV2 = 128
	mixRoundsV2      = 16384
	hardenThreshold  = 3
)

func powHashV2(data []byte) types.Hash {
	seed := sha256.Sum256(data)

	memPtr := memPool.Get().(*[][32]byte)
	mem := *memPtr

	mem[0] = seed
	for i := 1; i < slotsV2; i++ {
		if i%hardenIntervalV2 == 0 {
			mem[i] = sha256.Sum256(mem[i-1][:])
		} else {
			mem[i] = mem[i-1]
			progressionHarden(&mem[i], uint32(i))
		}
	}

	acc := mem[slotsV2-1]
	acc = mixPassA(acc, &mem, mixRoundsV2)
	acc = mixPassB(acc, &mem, mixRoundsV2)

	memPool.Put(memPtr)

	final := sha256.Sum256(acc[:])
	return types.Hash(final)
}

func progressionHarden(slot *[32]byte, index uint32) {
	selector := binary.LittleEndian.Uint32(slot[(index&7)*4:])
	if ((selector^index)&255) < hardenThreshold {
		*slot = sha256.Sum256(slot[:])
	} else {
		var next [32]byte
		arxFill(&next, slot, index)
		*slot = next
	}
}
