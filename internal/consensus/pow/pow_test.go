package pow

import (
	"testing"
	"time"

	"github.com/bams-repo/fairchain/internal/algorithms/sha256d"
	"github.com/bams-repo/fairchain/internal/crypto"
	bitcoindiff "github.com/bams-repo/fairchain/internal/difficulty/bitcoin"
	fcparams "github.com/bams-repo/fairchain/internal/params"
	"github.com/bams-repo/fairchain/internal/types"
)

func testHasher() *sha256d.Hasher    { return sha256d.New() }
func testRetargeter() *bitcoindiff.Retargeter { return bitcoindiff.New() }

func TestMineGenesisRegtest(t *testing.T) {
	cfg := fcparams.GenesisConfig{
		NetworkName:     "regtest",
		CoinbaseMessage: []byte("test genesis"),
		Timestamp:       1700000000,
		Bits:            0x207fffff, // Very easy.
		Version:         1,
		Reward:          5000000000,
		RewardScript:    []byte{0x00},
	}

	engine := New(testHasher(), testRetargeter())

	block := fcparams.BuildGenesisBlock(cfg)
	if err := engine.MineGenesis(&block); err != nil {
		t.Fatalf("MineGenesis: %v", err)
	}

	powHash := testHasher().PoWHash(block.Header.SerializeToBytes())
	target := crypto.CompactToHash(block.Header.Bits)
	if !powHash.LessOrEqual(target) {
		t.Fatal("mined genesis PoW hash does not meet target")
	}

	block2 := fcparams.BuildGenesisBlock(cfg)
	if err := engine.MineGenesis(&block2); err != nil {
		t.Fatalf("MineGenesis2: %v", err)
	}

	hash2 := crypto.HashBlockHeader(&block2.Header)
	hash := crypto.HashBlockHeader(&block.Header)
	if hash != hash2 {
		t.Fatalf("genesis mining not reproducible: %s != %s", hash, hash2)
	}
	if block.Header.Nonce != block2.Header.Nonce {
		t.Fatalf("genesis nonce not reproducible: %d != %d", block.Header.Nonce, block2.Header.Nonce)
	}
}

func TestSealHeader(t *testing.T) {
	engine := New(testHasher(), testRetargeter())
	header := types.BlockHeader{
		Version:   1,
		Timestamp: 1700000000,
		Bits:      0x207fffff,
		Nonce:     0,
	}
	target := crypto.CompactToHash(header.Bits)

	found, err := engine.SealHeader(&header, target, 1000000)
	if err != nil {
		t.Fatalf("SealHeader: %v", err)
	}
	if !found {
		t.Fatal("SealHeader should find a solution with easy difficulty")
	}

	powHash := testHasher().PoWHash(header.SerializeToBytes())
	if !powHash.LessOrEqual(target) {
		t.Fatal("sealed header PoW hash does not meet target")
	}
}

func TestCalcNextBitsNoRetarget(t *testing.T) {
	engine := New(testHasher(), testRetargeter())
	p := fcparams.Regtest

	tip := &types.BlockHeader{Bits: p.InitialBits, Timestamp: 1700000000}
	bits := engine.CalcNextBits(tip, 10, nil, p)
	if bits != p.InitialBits {
		t.Fatalf("regtest should not retarget: got 0x%08x, want 0x%08x", bits, p.InitialBits)
	}
}

func TestCalcNextBitsRetarget(t *testing.T) {
	engine := New(testHasher(), testRetargeter())
	p := &fcparams.ChainParams{
		InitialBits:      0x1e0fffff,
		MinBits:          0x1e0fffff,
		NoRetarget:       false,
		RetargetInterval: 10,
		TargetTimespan:   10 * time.Minute,
	}

	baseTime := uint32(1700000000)
	headers := make(map[uint32]*types.BlockHeader)
	for i := uint32(0); i <= 10; i++ {
		headers[i] = &types.BlockHeader{
			Bits:      p.InitialBits,
			Timestamp: baseTime + i*60,
		}
	}

	getAncestor := func(h uint32) *types.BlockHeader {
		return headers[h]
	}

	bits := engine.CalcNextBits(headers[9], 9, getAncestor, p)
	if bits != p.InitialBits {
		t.Logf("bits changed at retarget with matching timespan: 0x%08x -> 0x%08x", p.InitialBits, bits)
	}
}

func TestValidateHeader(t *testing.T) {
	engine := New(testHasher(), testRetargeter())
	p := fcparams.Regtest

	parent := types.BlockHeader{
		Version:   1,
		Timestamp: 1700000000,
		Bits:      p.InitialBits,
		Nonce:     0,
	}

	parentHash := crypto.HashBlockHeader(&parent)

	child := types.BlockHeader{
		Version:   1,
		PrevBlock: parentHash,
		Timestamp: 1700000001,
		Bits:      p.InitialBits,
		Nonce:     0,
	}

	target := crypto.CompactToHash(child.Bits)
	found, _ := engine.SealHeader(&child, target, 10000000)
	if !found {
		t.Fatal("could not mine child block")
	}

	getAncestor := func(h uint32) *types.BlockHeader { return &parent }
	if err := engine.ValidateHeader(&child, &parent, 1, getAncestor, p); err != nil {
		t.Fatalf("ValidateHeader: %v", err)
	}
}

func TestValidateHeaderBadPrevHash(t *testing.T) {
	engine := New(testHasher(), testRetargeter())
	p := fcparams.Regtest

	parent := types.BlockHeader{Version: 1, Timestamp: 1700000000, Bits: p.InitialBits}
	child := types.BlockHeader{
		Version:   1,
		PrevBlock: types.Hash{0xFF},
		Timestamp: 1700000001,
		Bits:      p.InitialBits,
	}

	getAncestor := func(h uint32) *types.BlockHeader { return &parent }
	if err := engine.ValidateHeader(&child, &parent, 1, getAncestor, p); err == nil {
		t.Fatal("should reject header with wrong prev hash")
	}
}

func TestValidateHeaderWrongBits(t *testing.T) {
	engine := New(testHasher(), testRetargeter())
	p := &fcparams.ChainParams{
		InitialBits:      0x1e0fffff,
		MinBits:          0x1e0fffff,
		NoRetarget:       true,
		RetargetInterval: 20,
		TargetTimespan:   20 * time.Minute,
	}

	parent := types.BlockHeader{
		Version:   1,
		Timestamp: 1700000000,
		Bits:      p.InitialBits,
	}
	parentHash := crypto.HashBlockHeader(&parent)

	wrongBits := uint32(0x207fffff)
	child := types.BlockHeader{
		Version:   1,
		PrevBlock: parentHash,
		Timestamp: 1700000001,
		Bits:      wrongBits,
	}

	easyTarget := crypto.CompactToHash(child.Bits)
	found, _ := engine.SealHeader(&child, easyTarget, 10000000)
	if !found {
		t.Fatal("could not mine child with easy bits")
	}

	getAncestor := func(h uint32) *types.BlockHeader { return &parent }
	if err := engine.ValidateHeader(&child, &parent, 1, getAncestor, p); err == nil {
		t.Fatal("should reject header with wrong difficulty bits")
	}
}
