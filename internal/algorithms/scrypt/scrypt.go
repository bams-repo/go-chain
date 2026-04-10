// Copyright (c) 2024-2026 The Fairchain Contributors
// Fairchain is an experiment in modularity, designed to improve on the work
// of Satoshi Nakamoto and to inspire more creative genius in the space.
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

package scrypt

import (
	"golang.org/x/crypto/scrypt"

	"github.com/bams-repo/fairchain/internal/types"
)

// Consensus-critical Scrypt parameters. Changing any of these is a hard fork.
// These match Litecoin's Scrypt parameters (N=1024, r=1, p=1, keyLen=32).
const (
	N      = 1024 // CPU/memory cost parameter
	R      = 1    // block size parameter
	P      = 1    // parallelization parameter
	KeyLen = 32   // 256-bit output to match types.Hash
)

// Hasher implements Scrypt proof-of-work hashing.
// Scrypt is memory-hard, making it more ASIC-resistant than SHA256d while
// remaining faster to validate than Argon2id. Used by Litecoin and many altcoins.
type Hasher struct{}

func New() *Hasher { return &Hasher{} }

func (h *Hasher) PoWHash(data []byte) types.Hash {
	out, err := scrypt.Key(data, data, N, R, P, KeyLen)
	if err != nil {
		panic("scrypt.Key failed with consensus parameters: " + err.Error())
	}
	return types.HashFromBytes(out).Reversed()
}

func (h *Hasher) Name() string { return "scrypt" }
