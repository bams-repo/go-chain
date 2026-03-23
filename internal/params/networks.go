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

const (
	// MaxMoneyValue is the maximum number of base units that can ever exist.
	// No single transaction output may exceed this value.
	MaxMoneyValue = 2_099_999_997_690_000

	// MaxTxSize is the maximum serialized size of a single transaction in bytes.
	// Bitcoin Core uses MAX_STANDARD_TX_WEIGHT / 4 ≈ 100,000 bytes for standard
	// transactions. This protects validation from CPU/memory exhaustion on
	// oversized transactions.
	MaxTxSize = 100_000

	// 20% premine on top of mined supply for testnet.
	TestnetPremineAmount = MaxMoneyValue / 5
)

var (
	// Hardcoded burn marker script for trackable burns/premine accounting.
	// NOTE: Script spend rules are not enforced yet in this codebase.
	TestnetBurnScript = []byte("burn:testnet:premine:v1")
)

// Genesis block coinbase messages below are consensus-critical historical data
// and must not be changed. New genesis blocks should use coinparams.NameLower.

// Mainnet is the primary network.
// Economic parameters are aligned with Bitcoin mainnet.
var Mainnet = &ChainParams{
	Name:         "mainnet",
	DataDirName:  "",
	NetworkMagic: [4]byte{0xFA, 0x1C, 0xC0, 0x01},
	DefaultPort:  19333,
	AddressPrefix: 0x00,

	// Pre-mined genesis block (sha256mem v3: 128 MiB fast-fill + SHA256-per-hop).
	// Coinbase: "fairchain genesis"
	// Timestamp: 1774175035 (2026-03-22T10:23:55Z)
	// Display hash: 7dc4fedfedf4b6c942db6e7d37577eccf661c390204105788bad3e8848f9cbdb
	GenesisBlock: types.Block{
		Header: types.BlockHeader{
			Version:   1,
			PrevBlock: types.ZeroHash,
			MerkleRoot: types.Hash{
				0xc6, 0xea, 0x1e, 0x0a, 0x6d, 0xea, 0x9b, 0x63,
				0x63, 0xfa, 0x0d, 0xf0, 0xa0, 0x54, 0xf8, 0xbd,
				0x59, 0x76, 0x65, 0xb8, 0x8c, 0x2c, 0x23, 0x59,
				0x67, 0x21, 0xf4, 0x25, 0x2d, 0x06, 0xd5, 0x2f,
			},
			Timestamp: 1774175035,
			Bits:      0x1e7ce359,
			Nonce:     805312833,
		},
		Transactions: []types.Transaction{{
			Version: 1,
			Inputs: []types.TxInput{{
				PreviousOutPoint: types.CoinbaseOutPoint,
				SignatureScript:  []byte("fairchain genesis"),
				Sequence:         0xFFFFFFFF,
			}},
			Outputs: []types.TxOutput{{
				Value:    50_0000_0000,
				PkScript: []byte{0x00},
			}},
			LockTime: 0,
		}},
	},
	GenesisHash: types.Hash{
		0xdb, 0xcb, 0xf9, 0x48, 0x88, 0x3e, 0xad, 0x8b,
		0x78, 0x05, 0x41, 0x20, 0x90, 0xc3, 0x61, 0xf6,
		0xcc, 0x7e, 0x57, 0x37, 0x7d, 0x6e, 0xdb, 0x42,
		0xc9, 0xb6, 0xf4, 0xed, 0xdf, 0xfe, 0xc4, 0x7d,
	},

	TargetBlockSpacing:  10 * time.Minute,
	RetargetInterval:    144,
	TargetTimespan:      144 * 10 * time.Minute,
	MaxTimeFutureDrift:  2 * time.Hour,
	MinTimestampRule:    "median-11",

	// Difficulty calibrated for sha256mem v3 (~27 H/s per core).
	// 0x1e7ce359 ≈ 134K hashes ≈ ~25 min on a single core.
	InitialBits:      0x1e7ce359,
	MinBits:          0x1f7fffff, // Floor ≈ 8x easier than initial; allows difficulty to recover after hash rate drops
	NoRetarget:       false,

	MaxBlockSize:     1_000_000,
	MaxBlockTxCount:  10_000,

	InitialSubsidy:          50_0000_0000,
	SubsidyHalvingInterval:  210_000,

	CoinbaseMaturity: 100,

	MaxReorgDepth: 288,

	MaxMempoolSize:    5000,
	MinRelayTxFee:     1000,
	MinRelayTxFeeRate: 1, // 1 sat/byte minimum, matching Bitcoin Core's default
	MempoolExpiry:     336 * time.Hour, // 2 weeks, matching Bitcoin Core DEFAULT_MEMPOOL_EXPIRE

	SeedNodes: []string{
		"45.32.196.26:19333",    
		"149.28.248.117:19333", 
		"78.141.227.33:19333",  
		"45.63.16.42:19333",    
	},

	ActivationHeights: map[string]uint32{"locktime": 1},
}

// Testnet is the public test network with easier difficulty.
var Testnet = &ChainParams{
	Name:         "testnet",
	DataDirName:  "testnet3",
	NetworkMagic: [4]byte{0xFA, 0x1C, 0xC0, 0x02},
	DefaultPort:  19334,
	AddressPrefix: 0x6F,

	// Pre-mined genesis block (sha256mem v3: 128 MiB fast-fill + SHA256-per-hop).
	// Coinbase: "fairchain genesis"
	// Timestamp: 1774176069 (2026-03-22T10:41:09Z)
	// Display hash: 4fc52f197f3f78db983a3421d425874f21f65b16afda368046875c66d7618012
	GenesisBlock: types.Block{
		Header: types.BlockHeader{
			Version:   1,
			PrevBlock: types.ZeroHash,
			MerkleRoot: types.Hash{
				0x6a, 0xde, 0x73, 0x5c, 0xa4, 0x68, 0xa0, 0x80,
				0x82, 0x9c, 0x27, 0x77, 0x5c, 0x87, 0x65, 0x99,
				0x96, 0x8d, 0xfb, 0x47, 0xc3, 0xa4, 0xa7, 0xbf,
				0xb3, 0x83, 0x0e, 0x8e, 0x0c, 0xe3, 0xdf, 0x29,
			},
			Timestamp: 1774176069,
			Bits:      0x1f3a910b,
			Nonce:     2415919171,
		},
		Transactions: []types.Transaction{{
			Version: 1,
			Inputs: []types.TxInput{{
				PreviousOutPoint: types.CoinbaseOutPoint,
				SignatureScript:  []byte("fairchain genesis"),
				Sequence:         0xFFFFFFFF,
			}},
			Outputs: []types.TxOutput{
				{
					Value:    50_0000_00,
					PkScript: []byte{0x00},
				},
				{
					Value:    TestnetPremineAmount,
					PkScript: TestnetBurnScript,
				},
			},
			LockTime: 0,
		}},
	},
	GenesisHash: types.Hash{
		0x12, 0x80, 0x61, 0xd7, 0x66, 0x5c, 0x87, 0x46,
		0x80, 0x36, 0xda, 0xaf, 0x16, 0x5b, 0xf6, 0x21,
		0x4f, 0x87, 0x25, 0xd4, 0x21, 0x34, 0x3a, 0x98,
		0xdb, 0x78, 0x3f, 0x7f, 0x19, 0x2f, 0xc5, 0x4f,
	},

	TargetBlockSpacing:  5 * time.Second,
	RetargetInterval:    20,
	TargetTimespan:      20 * 5 * time.Second, // 20 blocks × 5s
	MaxTimeFutureDrift:  2 * time.Minute,
	MinTimestampRule:    "median-11",

	// Difficulty calibrated for sha256mem v3 (~27 H/s per core).
	// 0x1f3a910b ≈ 570 hashes ≈ ~21 sec on a single core.
	InitialBits:      0x1f3a910b,
	MinBits:          0x207fffff, // Floor: trivial difficulty (same as regtest)
	NoRetarget:       false,

	MaxBlockSize:     2_000_000,
	MaxBlockTxCount:  10_000,

	// Economic scaling: testnet is 100x block-height accelerated relative to
	// mainnet for issuance comparisons (e.g., testnet 100,000 ~= mainnet 1,000).
	// To keep monetary state aligned by that mapping:
	//   - per-block subsidy is 1/100 of mainnet
	//   - halving interval is 100x mainnet
	InitialSubsidy:          50_0000_00,
	SubsidyHalvingInterval:  21_000_000,

	CoinbaseMaturity: 10,

	MaxReorgDepth: 1000,

	MaxMempoolSize:    5000,
	MinRelayTxFee:     100,
	MinRelayTxFeeRate: 1, // 1 sat/byte minimum
	MempoolExpiry:     336 * time.Hour, // 2 weeks, matching Bitcoin Core DEFAULT_MEMPOOL_EXPIRE

	SeedNodes: []string{
		"45.32.196.26:19334",   // main_web
		"149.28.248.117:19334", // seednode_dallas
		"78.141.227.33:19334",  // seednode_london
		"45.63.16.42:19334",    // mynta_webserver
	},

	ActivationHeights: map[string]uint32{"locktime": 1},
}

// Regtest is a local regression-test network with trivial difficulty and no retarget.
var Regtest = &ChainParams{
	Name:         "regtest",
	DataDirName:  "regtest",
	NetworkMagic: [4]byte{0xFA, 0x1C, 0xC0, 0xFF},
	DefaultPort:  19444,
	AddressPrefix: 0x6F,

	TargetBlockSpacing:  1 * time.Second,
	RetargetInterval:    1,
	TargetTimespan:      1 * time.Second,
	MaxTimeFutureDrift:  10 * time.Minute,
	MinTimestampRule:    "prev+1",

	// Very easy difficulty: top byte 0x20 = exponent 32, mantissa 0x0fffff.
	InitialBits:      0x207fffff,
	MinBits:          0x207fffff,
	NoRetarget:       true,

	MaxBlockSize:     4_000_000,
	MaxBlockTxCount:  50_000,

	InitialSubsidy:          50_0000_0000,
	SubsidyHalvingInterval:  150,

	CoinbaseMaturity: 1,

	MaxReorgDepth: 0,

	MaxMempoolSize:    10000,
	MinRelayTxFee:     0,
	MinRelayTxFeeRate: 0, // No fee-rate requirement on regtest
	MempoolExpiry:     1 * time.Hour, // Shorter for testing convenience

	SeedNodes: []string{},

	ActivationHeights: map[string]uint32{"locktime": 1},
}

// NetworkByName returns chain params by network name.
func NetworkByName(name string) *ChainParams {
	switch name {
	case "mainnet":
		return Mainnet
	case "testnet":
		return Testnet
	case "regtest":
		return Regtest
	default:
		return nil
	}
}

// InitGenesis computes and sets the genesis block and hash for the given params.
// This should be called after the genesis block has been mined (nonce found).
func InitGenesis(p *ChainParams, genesisBlock types.Block, genesisHash types.Hash) {
	p.GenesisBlock = genesisBlock
	p.GenesisHash = genesisHash
}
