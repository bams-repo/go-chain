// Copyright (c) 2024-2026 The Fairchain Contributors
// Fairchain is an experiment in modularity, designed to improve on the work
// of Satoshi Nakamoto and to inspire more creative genius in the space.
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

package crypto

import (
	"crypto/sha256"

	"github.com/bams-repo/fairchain/internal/types"
)

// DoubleSHA256 computes SHA256(SHA256(data)), the standard consensus hash
// used for block identity, transaction hashing, and merkle roots.
// This is NOT the PoW hash — the PoW hash is provided by the consensus
// engine's Hasher and may use a different algorithm (e.g., Argon2id).
func DoubleSHA256(data []byte) types.Hash {
	first := sha256.Sum256(data)
	second := sha256.Sum256(first[:])
	return types.HashFromBytes(second[:]).Reversed()
}

// HashBlockHeader computes the double-SHA256 of the canonical 80-byte header.
// This is the block's identity hash used for chain indexing, prevblock references,
// and RPC responses. It is always DoubleSHA256 regardless of the PoW algorithm.
func HashBlockHeader(h *types.BlockHeader) types.Hash {
	return DoubleSHA256(h.SerializeToBytes())
}

// HashTransaction computes the double-SHA256 of the canonical transaction bytes.
func HashTransaction(tx *types.Transaction) (types.Hash, error) {
	data, err := tx.SerializeToBytes()
	if err != nil {
		return types.ZeroHash, err
	}
	return DoubleSHA256(data), nil
}
