// Copyright (c) 2024-2026 The Fairchain Contributors
// Fairchain is an experiment in modularity, designed to improve on the work
// of Satoshi Nakamoto and to inspire more creative genius in the space.
// Distributed under the MIT software license.

// Package sha256mem implements the flagship memory-hard PoW used by Fairchain.
//
// The design favors CPUs over GPUs: each hash builds a large scratchpad (Slots)
// with periodic serial SHA-256 hardening, then runs two long mixing passes where
// each step depends on the previous SHA-256 output (data-dependent indexing).
// From a configurable activation height (testnet), the fill phase uses
// progression harden (probabilistic SHA vs ARX) and fewer mix rounds; the name
// remains "sha256mem" on the wire.
package sha256mem

import "sync"

var memPool = sync.Pool{
	New: func() any {
		buf := make([][32]byte, slotsV1)
		return &buf
	},
}
