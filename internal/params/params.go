// Copyright (c) 2024-2026 The Fairchain Contributors
// Fairchain is an experiment in modularity, designed to improve on the work
// of Satoshi Nakamoto and to inspire more creative genius in the space.
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

package params

import (
	"time"

	"github.com/bams-repo/fairchain/internal/types"
)

// ChainParams defines the complete set of parameters for a blockchain network.
// All consensus-critical values are here so they can be audited in one place.
type ChainParams struct {
	// Network identity
	Name         string     // Human-readable network name.
	DataDirName  string     // Subdirectory under the root data dir (e.g. "testnet0"). Empty = root.
	NetworkMagic [4]byte    // Wire protocol magic bytes for message framing.
	DefaultPort  uint16     // Default TCP listen port.
	AddressPrefix byte      // Future: address version byte for base58/bech32.

	// Genesis
	GenesisBlock types.Block // The fully-defined genesis block.
	GenesisHash  types.Hash  // Precomputed hash of the genesis block header.

	// Block timing
	TargetBlockSpacing  time.Duration // Desired time between blocks.
	RetargetInterval    uint32        // Number of blocks between difficulty adjustments.
	TargetTimespan      time.Duration // RetargetInterval * TargetBlockSpacing.
	MaxTimeFutureDrift  time.Duration // Maximum allowed block timestamp ahead of network time.
	MinTimestampRule    string        // "median-11" or "prev+1" — determines minimum allowed timestamp.

	// Difficulty
	InitialBits              uint32 // Compact target for the genesis block and initial difficulty.
	MinBits                  uint32 // Minimum difficulty (maximum target) allowed.
	NoRetarget               bool   // If true, difficulty never changes (regtest mode).
	AllowMinDifficultyBlocks bool          // Bitcoin testnet rule: reset to MinBits after a long gap.
	MinDifficultyGap         time.Duration // Timestamp gap that triggers min-diff. 0 = 2*TargetBlockSpacing.

	// Block limits
	MaxBlockSize     uint32 // Maximum serialized block size in bytes.
	MaxBlockTxCount  uint32 // Maximum number of transactions per block.

	// Subsidy
	InitialSubsidy   uint64 // Coinbase reward for the first era (in smallest units).
	SubsidyHalvingInterval uint32 // Blocks between subsidy halvings.

	// Coinbase
	CoinbaseMaturity uint32 // Blocks before coinbase outputs are spendable.

	// Reorg safety
	MaxReorgDepth uint32 // Maximum number of blocks that can be disconnected in a single reorg. 0 = unlimited.

	// BIP-94 timewarp prevention: at retarget boundaries, the new block's
	// timestamp must not be more than TimewarpGracePeriod before the parent's
	// timestamp. Gated by ActivationHeights["timewarp"].
	TimewarpGracePeriod time.Duration

	// P2P peer store
	PeerStoreMaxSize uint32 // Maximum addresses in the persistent peer store. 0 = unlimited.

	// Mempool policy (non-consensus, but parameterized per network)
	MaxMempoolSize      uint32        // Maximum number of transactions in mempool.
	MinRelayTxFee       uint64        // Minimum absolute fee for mempool admission (smallest units).
	MinRelayTxFeeRate   uint64        // Minimum fee rate (sat/byte) for mempool admission.
	MempoolExpiry       time.Duration // Maximum age before a mempool transaction is expired. Bitcoin Core default: 336h (2 weeks).

	// Seed nodes
	SeedNodes []string // DNS seeds or static IP:port addresses for peer discovery.

	// MiningStartTime is the earliest Unix timestamp at which mining is
	// permitted. The miner sleeps until this time. 0 means no restriction.
	MiningStartTime int64

	// Future consensus upgrade activation heights (placeholders).
	// Map from feature name to activation block height.
	ActivationHeights map[string]uint32
}

// CalcSubsidy returns the block subsidy at the given height.
// Uses integer halving: subsidy = InitialSubsidy >> (height / SubsidyHalvingInterval).
func (p *ChainParams) CalcSubsidy(height uint32) uint64 {
	if p.SubsidyHalvingInterval == 0 {
		return p.InitialSubsidy
	}
	halvings := height / p.SubsidyHalvingInterval
	if halvings >= 64 {
		return 0
	}
	return p.InitialSubsidy >> halvings
}
