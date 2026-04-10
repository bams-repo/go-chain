// Copyright (c) 2024-2026 The Fairchain Contributors
// Fairchain is an experiment in modularity, designed to improve on the work
// of Satoshi Nakamoto and to inspire more creative genius in the space.
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

package sha256d

import (
	"crypto/sha256"

	"github.com/bams-repo/fairchain/internal/types"
)

// Hasher implements DoubleSHA256 proof-of-work hashing (Bitcoin-parity default).
type Hasher struct{}

func New() *Hasher { return &Hasher{} }

func (h *Hasher) PoWHash(data []byte) types.Hash {
	first := sha256.Sum256(data)
	second := sha256.Sum256(first[:])
	return types.HashFromBytes(second[:]).Reversed()
}

func (h *Hasher) Name() string { return "sha256d" }
