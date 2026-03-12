package params

import (
	"time"

	"github.com/bams-repo/fairchain/internal/types"
)

// Mainnet is the primary fairchain network.
// Economic parameters are aligned with Bitcoin mainnet.
// Genesis block will be mined with the genesis tool before launch.
var Mainnet = &ChainParams{
	Name:         "mainnet",
	NetworkMagic: [4]byte{0xFA, 0x1C, 0xC0, 0x01},
	DefaultPort:  19333,
	AddressPrefix: 0x00,

	TargetBlockSpacing:  10 * time.Minute,
	RetargetInterval:    144,                             // Difficulty adjustment every 144 blocks (~24 hours at 10-min blocks).
	TargetTimespan:      144 * 10 * time.Minute,          // 144 blocks * 10 min = 1,440 min (24 hours).
	MaxTimeFutureDrift:  2 * time.Hour,
	MinTimestampRule:    "median-11",

	InitialBits:      0x1d00ffff,
	MinBits:          0x1e0fffff, // Easier than initial — allows recovery from catastrophic hash rate loss.
	NoRetarget:       false,

	MaxBlockSize:     1_000_000,
	MaxBlockTxCount:  10_000,

	InitialSubsidy:          50_0000_0000, // 50 coins * 10^8 smallest units.
	SubsidyHalvingInterval:  210_000,      // ~4 years at 10-min blocks (210,000 * 10 min ≈ 3.99 years).

	CoinbaseMaturity: 100,

	MaxMempoolSize: 5000,
	MinRelayTxFee:  1000, // Smallest units per tx — equivalent to Bitcoin's 1000 sat minimum relay fee.

	SeedNodes: []string{},

	ActivationHeights: map[string]uint32{},
}

// Testnet is the public test network with easier difficulty.
var Testnet = &ChainParams{
	Name:         "testnet",
	NetworkMagic: [4]byte{0xFA, 0x1C, 0xC0, 0x02},
	DefaultPort:  19334,
	AddressPrefix: 0x6F,

	// Pre-mined genesis block.
	// Coinbase: "fairchain testnet genesis"
	// Timestamp: 1773212867 (2026-03-11T07:07:47Z)
	// Display hash: 000005ab078d150cbdb55eb5147c1b2d935ea71a0e19e66f249577275f1b82e2
	GenesisBlock: types.Block{
		Header: types.BlockHeader{
			Version:   1,
			PrevBlock: types.ZeroHash,
			MerkleRoot: types.Hash{
				0xb7, 0xa4, 0x2f, 0x81, 0x4d, 0x96, 0xb8, 0x12,
				0x21, 0xc0, 0x76, 0xa7, 0xe1, 0xae, 0xee, 0x4b,
				0xd6, 0xf8, 0xdf, 0xb1, 0x39, 0x92, 0xaf, 0x06,
				0x07, 0xeb, 0xef, 0xe9, 0x87, 0x77, 0xd6, 0x55,
			},
			Timestamp: 1773212867,
			Bits:      0x1e0fffff,
			Nonce:     206896,
		},
		Transactions: []types.Transaction{{
			Version: 1,
			Inputs: []types.TxInput{{
				PreviousOutPoint: types.CoinbaseOutPoint,
				SignatureScript:  []byte("fairchain testnet genesis"),
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
		0xe2, 0x82, 0x1b, 0x5f, 0x27, 0x77, 0x95, 0x24,
		0x6f, 0xe6, 0x19, 0x0e, 0x1a, 0xa7, 0x5e, 0x93,
		0x2d, 0x1b, 0x7c, 0x14, 0xb5, 0x5e, 0xb5, 0xbd,
		0x0c, 0x15, 0x8d, 0x07, 0xab, 0x05, 0x00, 0x00,
	},

	TargetBlockSpacing:  5 * time.Second,
	RetargetInterval:    20,
	TargetTimespan:      20 * 5 * time.Second, // 100s
	MaxTimeFutureDrift:  2 * time.Minute,
	MinTimestampRule:    "median-11",

	InitialBits:      0x1e0fffff,
	MinBits:          0x1e0fffff,
	NoRetarget:       false,

	MaxBlockSize:     2_000_000,
	MaxBlockTxCount:  10_000,

	InitialSubsidy:          50_0000_0000,
	SubsidyHalvingInterval:  210_000,

	CoinbaseMaturity: 10,

	MaxMempoolSize: 5000,
	MinRelayTxFee:  100,

	SeedNodes: []string{},

	ActivationHeights: map[string]uint32{},
}

// Regtest is a local regression-test network with trivial difficulty and no retarget.
var Regtest = &ChainParams{
	Name:         "regtest",
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

	MaxMempoolSize: 10000,
	MinRelayTxFee:  0,

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
