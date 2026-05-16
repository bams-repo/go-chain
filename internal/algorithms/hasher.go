// Copyright (c) 2024-2026 The Fairchain Contributors
// Fairchain is an experiment in modularity, designed to improve on the work
// of Satoshi Nakamoto and to inspire more creative genius in the space.
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

package algorithms

import (
	"fmt"

	"github.com/bams-repo/fairchain/internal/algorithms/argon2id"
	"github.com/bams-repo/fairchain/internal/algorithms/scrypt"
	"github.com/bams-repo/fairchain/internal/algorithms/sha256d"
	"github.com/bams-repo/fairchain/internal/algorithms/sha256mem"
	"github.com/bams-repo/fairchain/internal/params"
	"github.com/bams-repo/fairchain/internal/types"
)

// Hasher computes the proof-of-work hash for a serialized block header.
// Implementations must be deterministic: same input always produces same output.
// Implementations must be safe for concurrent use.
type Hasher interface {
	// PoWHash computes the proof-of-work hash of the given data.
	// For header validation, data is the canonical 80-byte serialized header.
	PoWHash(data []byte) types.Hash

	// Name returns the algorithm identifier (e.g., "sha256d", "argon2id").
	Name() string
}

// GetHasher returns a Hasher for the named algorithm.
// For sha256mem, pass chain params via GetHasherForChain so height-gated variants work.
func GetHasher(name string) (Hasher, error) {
	return GetHasherForChain(name, nil)
}

// GetHasherForChain returns a Hasher, binding chain params when the algorithm supports forks.
func GetHasherForChain(name string, p *params.ChainParams) (Hasher, error) {
	switch name {
	case "sha256d":
		return sha256d.New(), nil
	case "argon2id":
		return argon2id.New(), nil
	case "scrypt":
		return scrypt.New(), nil
	case "sha256mem":
		var act uint32
		if p != nil {
			act = p.ActivationHeights[sha256mem.ActivationKey]
		}
		return sha256mem.NewChainHasher(act), nil
	default:
		return nil, fmt.Errorf("unknown PoW algorithm: %q", name)
	}
}
