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

	// Pre-mined genesis block (sha256mem: 64 MiB sequential fill + dual SHA256 mix).
	// Coinbase: "fairchain genesis"
	// Timestamp: 1774175035 (2026-03-22T10:23:55Z)
	// Display hash: ed66675f3d5ca4cb16b1623252fbdfe49dbde4c021277ae0b026d09823a4cf56
	// Pre-mined genesis block (sha256mem, LE hash convention).
	// Coinbase: "fairchain genesis"
	// Timestamp: 1774175035 (2026-03-22T10:23:55Z)
	// Display hash: e9ecf800a77d1f85dfd263d690e5d13d3682b72a6d9537dc33fe6cad2fb545c2
	GenesisBlock: types.Block{
		Header: types.BlockHeader{
			Version:   1,
			PrevBlock: types.ZeroHash,
			MerkleRoot: types.Hash{
				0x2f, 0xd5, 0x06, 0x2d, 0x25, 0xf4, 0x21, 0x67,
				0x59, 0x23, 0x2c, 0x8c, 0xb8, 0x65, 0x76, 0x59,
				0xbd, 0xf8, 0x54, 0xa0, 0xf0, 0x0d, 0xfa, 0x63,
				0x63, 0x9b, 0xea, 0x6d, 0x0a, 0x1e, 0xea, 0xc6,
			},
			Timestamp: 1774175035,
			Bits:      0x2007ffff,
			Nonce:     77,
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
		0xc2, 0x45, 0xb5, 0x2f, 0xad, 0x6c, 0xfe, 0x33,
		0xdc, 0x37, 0x95, 0x6d, 0x2a, 0xb7, 0x82, 0x36,
		0x3d, 0xd1, 0xe5, 0x90, 0xd6, 0x63, 0xd2, 0xdf,
		0x85, 0x1f, 0x7d, 0xa7, 0x00, 0xf8, 0xec, 0xe9,
	},

	TargetBlockSpacing:  10 * time.Minute,
	RetargetInterval:    144,
	TargetTimespan:      144 * 10 * time.Minute,
	MaxTimeFutureDrift:  2 * time.Hour,
	MinTimestampRule:    "median-11",

	// Easy initial difficulty — LWMA adjusts quickly to real hash rate.
	InitialBits:      0x2007ffff,
	MinBits:          0x207fffff,
	NoRetarget:       false,

	MaxBlockSize:     1_000_000,
	MaxBlockTxCount:  10_000,

	InitialSubsidy:          50_0000_0000,
	SubsidyHalvingInterval:  210_000,

	CoinbaseMaturity: 100,

	MaxReorgDepth: 288,

	TimewarpGracePeriod: 10 * time.Minute, // BIP-94: 600 seconds (one block spacing on mainnet)
	PeerStoreMaxSize:    4096,

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

	ActivationHeights: map[string]uint32{
		"locktime": 1,
		"timewarp": 1,
	},
}

// Testnet is the public test network.
// v1 (testnet1): 10-minute blocks, LWMA difficulty, LE hash convention.
var Testnet = &ChainParams{
	Name:         "testnet",
	DataDirName:  "testnet1",
	NetworkMagic: [4]byte{0xFA, 0x1C, 0xC0, 0x03},
	DefaultPort:  19334,
	AddressPrefix: 0x6F,

	// Pre-mined genesis block (sha256mem, LE hash convention).
	// Coinbase: "fairchain testnet1 genesis"
	// Timestamp: 1744325400 (2025-04-10T22:30:00Z)
	// Display hash: 1c0d0bec5e6687df1d67239378a42f95c89a4c547d112d8c80a33c2b81580d71
	GenesisBlock: types.Block{
		Header: types.BlockHeader{
			Version:   1,
			PrevBlock: types.ZeroHash,
			MerkleRoot: types.Hash{
				0xb1, 0x88, 0x4d, 0xb0, 0x63, 0x4b, 0xe0, 0x81,
				0x08, 0x02, 0x9f, 0x73, 0xfe, 0x53, 0xdc, 0xb0,
				0x93, 0xeb, 0x40, 0xdd, 0xf7, 0x54, 0x23, 0x6c,
				0x65, 0xbc, 0x4f, 0x2f, 0xc2, 0x1e, 0xe5, 0xe2,
			},
			Timestamp: 1744325400,
			Bits:      0x2007ffff,
			Nonce:     10,
		},
		Transactions: []types.Transaction{{
			Version: 1,
			Inputs: []types.TxInput{{
				PreviousOutPoint: types.CoinbaseOutPoint,
				SignatureScript:  []byte("fairchain testnet1 genesis"),
				Sequence:         0xFFFFFFFF,
			}},
			Outputs: []types.TxOutput{
				{
					Value:    50_0000_0000,
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
		0x71, 0x0d, 0x58, 0x81, 0x2b, 0x3c, 0xa3, 0x80,
		0x8c, 0x2d, 0x11, 0x7d, 0x54, 0x4c, 0x9a, 0xc8,
		0x95, 0x2f, 0xa4, 0x78, 0x93, 0x23, 0x67, 0x1d,
		0xdf, 0x87, 0x66, 0x5e, 0xec, 0x0b, 0x0d, 0x1c,
	},

	TargetBlockSpacing:  10 * time.Minute,
	RetargetInterval:    45, // LWMA window size: 45 blocks (~7.5 hours)
	TargetTimespan:      45 * 10 * time.Minute,
	MaxTimeFutureDrift:  2 * time.Hour,
	MinTimestampRule:    "median-11",

	InitialBits:              0x2007ffff, // Very easy — same as regtest floor
	MinBits:                  0x207fffff, // Floor: trivial difficulty
	NoRetarget:               false,
	AllowMinDifficultyBlocks: true,

	MaxBlockSize:     2_000_000,
	MaxBlockTxCount:  10_000,

	InitialSubsidy:          50_0000_0000,
	SubsidyHalvingInterval:  210_000,

	CoinbaseMaturity: 100,

	MaxReorgDepth: 1000,

	TimewarpGracePeriod: 10 * time.Minute,
	PeerStoreMaxSize:    1024,

	MaxMempoolSize:    5000,
	MinRelayTxFee:     1000,
	MinRelayTxFeeRate: 1,
	MempoolExpiry:     336 * time.Hour,

	SeedNodes: []string{
		"95.179.203.47:19334",  // seednode_london
		"207.246.117.14:19334", // seednode_miami
	},

	ActivationHeights: map[string]uint32{
		"locktime":      1,
		"mindiffblocks": 1,
		"timewarp":      1,
		"lwma_v2":       1750, // LWMA v2: N=200 window, per-block clamp, tighter FTL
	},
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

	TimewarpGracePeriod: 10 * time.Minute,
	PeerStoreMaxSize:    512,

	MaxMempoolSize:    10000,
	MinRelayTxFee:     0,
	MinRelayTxFeeRate: 0, // No fee-rate requirement on regtest
	MempoolExpiry:     1 * time.Hour, // Shorter for testing convenience

	SeedNodes: []string{},

	ActivationHeights: map[string]uint32{
		"locktime": 1,
		"timewarp": 1,
	},
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
