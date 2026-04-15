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
	// Display hash: 1fcb291c4ffa3c9877b325d42f06e187edfae37417e1d2058b626233429aeea6
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
			Bits:      0x1f147ade,
			Nonce:     1101,
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
		0xa6, 0xee, 0x9a, 0x42, 0x33, 0x62, 0x62, 0x8b,
		0x05, 0xd2, 0xe1, 0x17, 0x74, 0xe3, 0xfa, 0xed,
		0x87, 0xe1, 0x06, 0x2f, 0xd4, 0x25, 0xb3, 0x77,
		0x98, 0x3c, 0xfa, 0x4f, 0x1c, 0x29, 0xcb, 0x1f,
	},

	TargetBlockSpacing:  10 * time.Minute,
	RetargetInterval:    144,
	TargetTimespan:      144 * 10 * time.Minute,
	MaxTimeFutureDrift:  2 * time.Hour,
	MinTimestampRule:    "median-11",

	// 100x harder than trivial — LWMA adjusts quickly to real hash rate.
	InitialBits:      0x1f147ade,
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

	MiningStartTime: 1777338000, // 2026-04-27 18:00:00 PDT — mainnet mining begins

	ActivationHeights: map[string]uint32{
		"locktime": 1,
		"timewarp": 1,
	},
}

// Testnet is the public test network.
// v10 (testnet10): 20-second blocks, LWMA difficulty, LE hash convention.
var Testnet = &ChainParams{
	Name:         "testnet",
	DataDirName:  "testnet10",
	NetworkMagic: [4]byte{0xFA, 0x1C, 0xC0, 0x03},
	DefaultPort:  19334,
	AddressPrefix: 0x6F,

	// Pre-mined genesis block (sha256mem, LE hash convention).
	// Coinbase: "fairchain testnet1 genesis"
	// Timestamp: 1744325400 (2025-04-10T22:30:00Z)
	// Display hash: 242c396cebc08d71ed6de2cd1b17f2641f34d0edd42b57f79dc02f5be286c0c1
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
			Bits:      0x1f666659,
			Nonce:     1289,
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
		0xc1, 0xc0, 0x86, 0xe2, 0x5b, 0x2f, 0xc0, 0x9d,
		0xf7, 0x57, 0x2b, 0xd4, 0xed, 0xd0, 0x34, 0x1f,
		0x64, 0xf2, 0x17, 0x1b, 0xcd, 0xe2, 0x6d, 0xed,
		0x71, 0x8d, 0xc0, 0xeb, 0x6c, 0x39, 0x2c, 0x24,
	},

	TargetBlockSpacing:  20 * time.Second,
	RetargetInterval:    60, // LWMA window size N=60 per zawy12
	TargetTimespan:      60 * 20 * time.Second,
	MaxTimeFutureDrift:  60 * time.Second, // N*T/20 = 60*20/20 = 60s per zawy12
	MinTimestampRule:    "median-11",

	InitialBits:              0x1f666659, // 20x harder than original 0x2007ffff
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
