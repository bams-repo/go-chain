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

	// Pre-mined genesis block (sha256mem PoW).
	// Coinbase: "fairchain mainnet genesis"
	// Timestamp: 1773212462 (2026-03-11T07:01:02Z)
	// Display hash: 96922ece0d092d54ba9340e2fd92ed0c725b7218a3395389b94c1263a3e9e204
	GenesisBlock: types.Block{
		Header: types.BlockHeader{
			Version:   1,
			PrevBlock: types.ZeroHash,
			MerkleRoot: types.Hash{
				0x1a, 0x43, 0xdf, 0x3e, 0xf8, 0x14, 0x0d, 0xbe,
				0x47, 0xad, 0xea, 0xdb, 0x14, 0x1b, 0xd4, 0xbb,
				0x74, 0xee, 0x7d, 0x6f, 0x81, 0x44, 0x1c, 0x4d,
				0xc0, 0x41, 0x16, 0xf1, 0xb5, 0x01, 0xdc, 0xb5,
			},
			Timestamp: 1773212462,
			Bits:      0x1e7ce359,
			Nonce:     63815,
		},
		Transactions: []types.Transaction{{
			Version: 1,
			Inputs: []types.TxInput{{
				PreviousOutPoint: types.CoinbaseOutPoint,
				SignatureScript:  []byte("fairchain mainnet genesis"),
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
		0x04, 0xe2, 0xe9, 0xa3, 0x63, 0x12, 0x4c, 0xb9,
		0x89, 0x53, 0x39, 0xa3, 0x18, 0x72, 0x5b, 0x72,
		0x0c, 0xed, 0x92, 0xfd, 0xe2, 0x40, 0x93, 0xba,
		0x54, 0x2d, 0x09, 0x0d, 0xce, 0x2e, 0x92, 0x96,
	},

	TargetBlockSpacing:  10 * time.Minute,
	RetargetInterval:    144,
	TargetTimespan:      144 * 10 * time.Minute,
	MaxTimeFutureDrift:  2 * time.Hour,
	MinTimestampRule:    "median-11",

	// Difficulty calibrated for sha256mem (~224 H/s per core).
	// 0x1e7ce359 ≈ 134K hashes ≈ 10 min on a single core.
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

	SeedNodes: []string{},

	ActivationHeights: map[string]uint32{},
}

// Testnet is the public test network with easier difficulty.
var Testnet = &ChainParams{
	Name:         "testnet",
	DataDirName:  "testnet2",
	NetworkMagic: [4]byte{0xFA, 0x1C, 0xC0, 0x02},
	DefaultPort:  19334,
	AddressPrefix: 0x6F,

	// Pre-mined genesis block (sha256mem PoW).
	// Coinbase: "fairchain testnet genesis"
	// Timestamp: 1773533803 (2026-03-15T00:16:43Z)
	// Display hash: 7258c351b08fc2fb81a87a0d7a9f8a9f082220d966e28e5e28bf99db497f8b46
	GenesisBlock: types.Block{
		Header: types.BlockHeader{
			Version:   1,
			PrevBlock: types.ZeroHash,
			MerkleRoot: types.Hash{
				0xb5, 0x8a, 0xb7, 0x94, 0xe8, 0x13, 0x5d, 0x55,
				0xf9, 0x7b, 0x93, 0x7f, 0xbb, 0x19, 0xca, 0xa3,
				0xe4, 0x3b, 0xd0, 0x3f, 0xe6, 0x0b, 0x4e, 0x08,
				0x19, 0x0a, 0xf5, 0x44, 0x52, 0xce, 0xd2, 0x3a,
			},
			Timestamp: 1773533803,
			Bits:      0x1f3a910b,
			Nonce:     285,
		},
		Transactions: []types.Transaction{{
			Version: 1,
			Inputs: []types.TxInput{{
				PreviousOutPoint: types.CoinbaseOutPoint,
				SignatureScript:  []byte("fairchain testnet genesis"),
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
		0x46, 0x8b, 0x7f, 0x49, 0xdb, 0x99, 0xbf, 0x28,
		0x5e, 0x8e, 0xe2, 0x66, 0xd9, 0x20, 0x22, 0x08,
		0x9f, 0x8a, 0x9f, 0x7a, 0x0d, 0x7a, 0xa8, 0x81,
		0xfb, 0xc2, 0x8f, 0xb0, 0x51, 0xc3, 0x58, 0x72,
	},

	TargetBlockSpacing:  5 * time.Second,
	RetargetInterval:    20,
	TargetTimespan:      20 * 5 * time.Second, // 20 blocks × 5s
	MaxTimeFutureDrift:  2 * time.Minute,
	MinTimestampRule:    "median-11",

	// Difficulty calibrated for sha256mem (~224 H/s per core).
	// 0x1f3a910b ≈ 1,119 hashes ≈ 5 sec on a single core.
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
		"45.32.196.26:19334",  // main_web
		"207.148.9.169:19334", // mining_pool
	},

	ActivationHeights: map[string]uint32{},
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

	ActivationHeights: map[string]uint32{},
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
