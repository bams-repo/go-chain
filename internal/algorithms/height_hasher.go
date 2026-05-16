// Copyright (c) 2024-2026 The Fairchain Contributors
// Distributed under the MIT software license.

package algorithms

import "github.com/bams-repo/fairchain/internal/types"

// HeightHasher extends Hasher with height-gated PoW (consensus forks).
// Activation rules are configured when the hasher is constructed.
type HeightHasher interface {
	Hasher
	PoWHashAtHeight(data []byte, height uint32) types.Hash
}
