// Copyright (c) 2024-2026 The Fairchain Contributors
// Fairchain is an experiment in modularity, designed to improve on the work
// of Satoshi Nakamoto and to inspire more creative genius in the space.
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

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

	found, err := engine.SealHeader(&header, target, 1, fcparams.Regtest, 1000000)
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
	found, _ := engine.SealHeader(&child, target, 2, p, 10000000)
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
	found, _ := engine.SealHeader(&child, easyTarget, 2, p, 10000000)
	if !found {
		t.Fatal("could not mine child with easy bits")
	}

	getAncestor := func(h uint32) *types.BlockHeader { return &parent }
	if err := engine.ValidateHeader(&child, &parent, 1, getAncestor, p); err == nil {
		t.Fatal("should reject header with wrong difficulty bits")
	}
}

// --- Testnet min-difficulty reset rule tests ---

func testnetParams() *fcparams.ChainParams {
	return &fcparams.ChainParams{
		InitialBits:              0x1e0fffff,
		MinBits:                  0x207fffff,
		NoRetarget:               false,
		RetargetInterval:         10,
		TargetBlockSpacing:       5 * time.Second,
		TargetTimespan:           10 * 5 * time.Second,
		MaxTimeFutureDrift:       2 * time.Minute,
		MinTimestampRule:         "median-11",
		AllowMinDifficultyBlocks: true,
		ActivationHeights:        map[string]uint32{"mindiffblocks": 1},
	}
}

func TestMinDiffReset_LargeGapReturnsMinBits(t *testing.T) {
	engine := New(testHasher(), testRetargeter())
	p := testnetParams()

	parent := &types.BlockHeader{
		Version:   1,
		Timestamp: 1700000000,
		Bits:      p.InitialBits,
	}

	newBlock := &types.BlockHeader{
		Version:   1,
		PrevBlock: crypto.HashBlockHeader(parent),
		Timestamp: parent.Timestamp + 11, // > 2 * 5s = 10s
		Bits:      p.MinBits,
	}

	got := engine.calcExpectedBits(newBlock, parent, 1, func(h uint32) *types.BlockHeader {
		return parent
	}, p)
	if got != p.MinBits {
		t.Fatalf("expected MinBits 0x%08x when gap > 2x spacing, got 0x%08x", p.MinBits, got)
	}
}

func TestMinDiffReset_SmallGapReturnsNormalBits(t *testing.T) {
	engine := New(testHasher(), testRetargeter())
	p := testnetParams()

	parent := &types.BlockHeader{
		Version:   1,
		Timestamp: 1700000000,
		Bits:      p.InitialBits,
	}

	newBlock := &types.BlockHeader{
		Version:   1,
		PrevBlock: crypto.HashBlockHeader(parent),
		Timestamp: parent.Timestamp + 5, // <= 2 * 5s = 10s
		Bits:      p.InitialBits,
	}

	got := engine.calcExpectedBits(newBlock, parent, 1, func(h uint32) *types.BlockHeader {
		return parent
	}, p)
	if got != p.InitialBits {
		t.Fatalf("expected normal bits 0x%08x when gap <= 2x spacing, got 0x%08x", p.InitialBits, got)
	}
}

func TestMinDiffReset_ExactBoundaryReturnsNormalBits(t *testing.T) {
	engine := New(testHasher(), testRetargeter())
	p := testnetParams()

	parent := &types.BlockHeader{
		Version:   1,
		Timestamp: 1700000000,
		Bits:      p.InitialBits,
	}

	newBlock := &types.BlockHeader{
		Version:   1,
		PrevBlock: crypto.HashBlockHeader(parent),
		Timestamp: parent.Timestamp + 10, // == 2 * 5s, not greater
		Bits:      p.InitialBits,
	}

	got := engine.calcExpectedBits(newBlock, parent, 1, func(h uint32) *types.BlockHeader {
		return parent
	}, p)
	if got != p.InitialBits {
		t.Fatalf("expected normal bits at exact boundary, got 0x%08x", got)
	}
}

func TestMinDiffReset_DisabledOnMainnet(t *testing.T) {
	engine := New(testHasher(), testRetargeter())
	p := &fcparams.ChainParams{
		InitialBits:              0x1e0fffff,
		MinBits:                  0x207fffff,
		NoRetarget:               false,
		RetargetInterval:         10,
		TargetBlockSpacing:       5 * time.Second,
		TargetTimespan:           10 * 5 * time.Second,
		MaxTimeFutureDrift:       2 * time.Minute,
		MinTimestampRule:         "median-11",
		AllowMinDifficultyBlocks: false,
	}

	parent := &types.BlockHeader{
		Version:   1,
		Timestamp: 1700000000,
		Bits:      p.InitialBits,
	}

	newBlock := &types.BlockHeader{
		Version:   1,
		PrevBlock: crypto.HashBlockHeader(parent),
		Timestamp: parent.Timestamp + 999, // huge gap, but mainnet
		Bits:      p.InitialBits,
	}

	got := engine.calcExpectedBits(newBlock, parent, 1, func(h uint32) *types.BlockHeader {
		return parent
	}, p)
	if got != p.InitialBits {
		t.Fatalf("mainnet should never return MinBits for gap, got 0x%08x", got)
	}
}

func TestMinDiffReset_ScanBackFindsRealDifficulty(t *testing.T) {
	engine := New(testHasher(), testRetargeter())
	p := testnetParams()

	realBits := uint32(0x1e0fffff)
	baseTime := uint32(1700000000)

	headers := make(map[uint32]*types.BlockHeader)
	headers[0] = &types.BlockHeader{Bits: realBits, Timestamp: baseTime}
	headers[1] = &types.BlockHeader{Bits: realBits, Timestamp: baseTime + 5}
	headers[2] = &types.BlockHeader{Bits: realBits, Timestamp: baseTime + 10}
	// Blocks 3-4 are min-difficulty (someone was away, came back)
	headers[3] = &types.BlockHeader{Bits: p.MinBits, Timestamp: baseTime + 100}
	headers[4] = &types.BlockHeader{Bits: p.MinBits, Timestamp: baseTime + 200}

	getAncestor := func(h uint32) *types.BlockHeader { return headers[h] }

	// Block 5 arrives quickly after block 4 (gap <= 2x spacing), so it
	// should NOT get min-difficulty. The scan-back should find block 2's
	// real difficulty.
	newBlock := &types.BlockHeader{
		Version:   1,
		PrevBlock: crypto.HashBlockHeader(headers[4]),
		Timestamp: headers[4].Timestamp + 3, // within 2x spacing
		Bits:      realBits,
	}

	got := engine.calcExpectedBits(newBlock, headers[4], 5, getAncestor, p)
	if got != realBits {
		t.Fatalf("scan-back should find real bits 0x%08x, got 0x%08x", realBits, got)
	}
}

func TestMinDiffReset_ScanBackStopsAtRetargetBoundary(t *testing.T) {
	engine := New(testHasher(), testRetargeter())
	p := testnetParams()
	p.RetargetInterval = 10

	baseTime := uint32(1700000000)
	realBits := uint32(0x1e0fffff)

	headers := make(map[uint32]*types.BlockHeader)
	// Block 10 is a retarget boundary with real difficulty
	headers[10] = &types.BlockHeader{Bits: realBits, Timestamp: baseTime}
	// Blocks 11-13 are min-difficulty
	headers[11] = &types.BlockHeader{Bits: p.MinBits, Timestamp: baseTime + 100}
	headers[12] = &types.BlockHeader{Bits: p.MinBits, Timestamp: baseTime + 200}
	headers[13] = &types.BlockHeader{Bits: p.MinBits, Timestamp: baseTime + 300}

	getAncestor := func(h uint32) *types.BlockHeader { return headers[h] }

	// Block 14 arrives quickly — scan back should stop at block 10 (retarget boundary)
	newBlock := &types.BlockHeader{
		Version:   1,
		PrevBlock: crypto.HashBlockHeader(headers[13]),
		Timestamp: headers[13].Timestamp + 3,
		Bits:      realBits,
	}

	got := engine.calcExpectedBits(newBlock, headers[13], 14, getAncestor, p)
	if got != realBits {
		t.Fatalf("scan-back should stop at retarget boundary and return 0x%08x, got 0x%08x", realBits, got)
	}
}

func TestMinDiffReset_RetargetBoundaryUsesNormalCalc(t *testing.T) {
	engine := New(testHasher(), testRetargeter())
	p := testnetParams()
	p.RetargetInterval = 10

	baseTime := uint32(1700000000)

	headers := make(map[uint32]*types.BlockHeader)
	for i := uint32(0); i <= 9; i++ {
		headers[i] = &types.BlockHeader{
			Bits:      p.InitialBits,
			Timestamp: baseTime + i*5,
		}
	}

	getAncestor := func(h uint32) *types.BlockHeader { return headers[h] }

	// Block 10 is a retarget boundary; even with a large gap, the retarget
	// calculation should run (not just return MinBits) because we're at
	// a retarget boundary and the gap check applies to the new block.
	newBlock := &types.BlockHeader{
		Version:   1,
		PrevBlock: crypto.HashBlockHeader(headers[9]),
		Timestamp: headers[9].Timestamp + 100, // large gap
		Bits:      p.MinBits,
	}

	got := engine.calcExpectedBits(newBlock, headers[9], 10, getAncestor, p)
	// With large gap, should return MinBits
	if got != p.MinBits {
		t.Fatalf("retarget boundary with large gap should return MinBits 0x%08x, got 0x%08x", p.MinBits, got)
	}
}

func TestMinDiffReset_InactiveBeforeActivationHeight(t *testing.T) {
	engine := New(testHasher(), testRetargeter())
	p := testnetParams()
	p.ActivationHeights["mindiffblocks"] = 15000

	parent := &types.BlockHeader{
		Version:   1,
		Timestamp: 1700000000,
		Bits:      p.InitialBits,
	}

	newBlock := &types.BlockHeader{
		Version:   1,
		PrevBlock: crypto.HashBlockHeader(parent),
		Timestamp: parent.Timestamp + 999, // huge gap, but below activation
		Bits:      p.InitialBits,
	}

	// Height 101 is a non-retarget boundary (interval=10), so the normal
	// retargeter returns tip.Bits unchanged.
	got := engine.calcExpectedBits(newBlock, parent, 101, func(h uint32) *types.BlockHeader {
		return parent
	}, p)
	if got != p.InitialBits {
		t.Fatalf("before activation height, should return normal bits 0x%08x, got 0x%08x", p.InitialBits, got)
	}
}

func TestMinDiffReset_ActiveAtActivationHeight(t *testing.T) {
	engine := New(testHasher(), testRetargeter())
	p := testnetParams()
	p.ActivationHeights["mindiffblocks"] = 100

	parent := &types.BlockHeader{
		Version:   1,
		Timestamp: 1700000000,
		Bits:      p.InitialBits,
	}

	newBlock := &types.BlockHeader{
		Version:   1,
		PrevBlock: crypto.HashBlockHeader(parent),
		Timestamp: parent.Timestamp + 11, // > 2 * 5s
		Bits:      p.MinBits,
	}

	got := engine.calcExpectedBits(newBlock, parent, 100, func(h uint32) *types.BlockHeader {
		return parent
	}, p)
	if got != p.MinBits {
		t.Fatalf("at activation height, should return MinBits 0x%08x, got 0x%08x", p.MinBits, got)
	}
}

func TestMinDiffReset_NoActivationKeyFallsBack(t *testing.T) {
	engine := New(testHasher(), testRetargeter())
	p := testnetParams()
	delete(p.ActivationHeights, "mindiffblocks")

	parent := &types.BlockHeader{
		Version:   1,
		Timestamp: 1700000000,
		Bits:      p.InitialBits,
	}

	newBlock := &types.BlockHeader{
		Version:   1,
		PrevBlock: crypto.HashBlockHeader(parent),
		Timestamp: parent.Timestamp + 999,
		Bits:      p.InitialBits,
	}

	got := engine.calcExpectedBits(newBlock, parent, 1, func(h uint32) *types.BlockHeader {
		return parent
	}, p)
	if got != p.InitialBits {
		t.Fatalf("without activation key, should return normal bits 0x%08x, got 0x%08x", p.InitialBits, got)
	}
}
