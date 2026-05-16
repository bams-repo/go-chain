// Copyright (c) 2024-2026 The Fairchain Contributors
// Distributed under the MIT software license.

package sha256mem

import (
	"github.com/bams-repo/fairchain/internal/types"
)

// ActivationKey is the ChainParams.ActivationHeights key for the progression-harden
// variant. Nodes pass the resolved height into NewChainHasher (0 = disabled).
const ActivationKey = "sha256mem"

// UsesVariantAt reports whether height uses the post-fork algorithm.
// activationHeight 0 means the variant is never active (mainnet default).
func UsesVariantAt(activationHeight, height uint32) bool {
	if activationHeight == 0 {
		return false
	}
	return height >= activationHeight
}

// PoWHashAtHeight selects the sha256mem implementation for the given activation gate.
func PoWHashAtHeight(activationHeight, height uint32, data []byte) types.Hash {
	if UsesVariantAt(activationHeight, height) {
		return powHashV2(data)
	}
	return powHashV1(data)
}

// ChainHasher implements algorithms.Hasher and algorithms.HeightHasher.
type ChainHasher struct {
	activationHeight uint32
}

// NewChainHasher returns a height-aware hasher. activationHeight 0 keeps v1 at all heights.
func NewChainHasher(activationHeight uint32) *ChainHasher {
	return &ChainHasher{activationHeight: activationHeight}
}

func (h *ChainHasher) Name() string { return "sha256mem" }

// PoWHash uses height 0 (v1 when activation is gated above 0).
func (h *ChainHasher) PoWHash(data []byte) types.Hash {
	return PoWHashAtHeight(h.activationHeight, 0, data)
}

func (h *ChainHasher) PoWHashAtHeight(data []byte, height uint32) types.Hash {
	return PoWHashAtHeight(h.activationHeight, height, data)
}

// Hasher is the pre-fork single-variant hasher (always v1). Used by tests and tools.
type Hasher struct{}

func New() *Hasher { return &Hasher{} }

func (h *Hasher) PoWHash(data []byte) types.Hash { return powHashV1(data) }

func (h *Hasher) Name() string { return "sha256mem" }
