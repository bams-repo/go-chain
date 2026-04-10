// Copyright (c) 2024-2026 The Fairchain Contributors
// Fairchain is an experiment in modularity, designed to improve on the work
// of Satoshi Nakamoto and to inspire more creative genius in the space.
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

package argon2id

import (
	"golang.org/x/crypto/argon2"

	"github.com/bams-repo/fairchain/internal/types"
)

// Consensus-critical Argon2id parameters. Changing any of these is a hard fork.
const (
	TimeCost    = 1   // single pass — fast enough for block validation
	MemoryCost  = 256 // 256 KiB — light for validation, heavy enough for ASIC resistance
	Parallelism = 1   // single-threaded for determinism
	KeyLen      = 32  // 256-bit output to match types.Hash
)

// Hasher implements Argon2id proof-of-work hashing.
// Uses Argon2id (RFC 9106 recommended hybrid) which combines data-independent
// memory access (ASIC resistance) with data-dependent access (GPU resistance).
type Hasher struct{}

func New() *Hasher { return &Hasher{} }

func (h *Hasher) PoWHash(data []byte) types.Hash {
	out := argon2.IDKey(data, data, TimeCost, MemoryCost, Parallelism, KeyLen)
	return types.HashFromBytes(out).Reversed()
}

func (h *Hasher) Name() string { return "argon2id" }
