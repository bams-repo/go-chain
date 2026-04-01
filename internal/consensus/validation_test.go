// Copyright (c) 2024-2026 The Fairchain Contributors
// Fairchain is an experiment in modularity, designed to improve on the work
// of Satoshi Nakamoto and to inspire more creative genius in the space.
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

package consensus

import (
	"strings"
	"testing"
	"time"

	"github.com/bams-repo/fairchain/internal/crypto"
	"github.com/bams-repo/fairchain/internal/params"
	"github.com/bams-repo/fairchain/internal/types"
)

func makeTestBlock(height uint32, p *params.ChainParams) types.Block {
	subsidy := p.CalcSubsidy(height)
	scriptSig := minimalBIP34ScriptSig(height, []byte("test"))

	coinbase := types.Transaction{
		Version: 1,
		Inputs: []types.TxInput{
			{
				PreviousOutPoint: types.CoinbaseOutPoint,
				SignatureScript:  scriptSig,
				Sequence:         0xFFFFFFFF,
			},
		},
		Outputs: []types.TxOutput{
			{Value: subsidy, PkScript: []byte{0x00}},
		},
	}

	merkle, _ := crypto.ComputeMerkleRoot([]types.Transaction{coinbase})

	return types.Block{
		Header: types.BlockHeader{
			Version:    1,
			MerkleRoot: merkle,
			Timestamp:  1700000000 + height,
			Bits:       p.InitialBits,
		},
		Transactions: []types.Transaction{coinbase},
	}
}

func TestValidateBlockStructure(t *testing.T) {
	p := params.Regtest
	block := makeTestBlock(1, p)

	if err := ValidateBlockStructure(&block, 1, p, nil, nil); err != nil {
		t.Fatalf("valid block rejected: %v", err)
	}
}

func TestValidateBlockStructureNoCoinbase(t *testing.T) {
	p := params.Regtest
	block := types.Block{
		Header: types.BlockHeader{Version: 1, Bits: p.InitialBits},
		Transactions: []types.Transaction{
			{
				Version: 1,
				Inputs: []types.TxInput{
					{PreviousOutPoint: types.OutPoint{Hash: types.Hash{1}, Index: 0}, SignatureScript: []byte("sig"), Sequence: 0xFFFFFFFF},
				},
				Outputs: []types.TxOutput{{Value: 100, PkScript: []byte{0x00}}},
			},
		},
	}

	if err := ValidateBlockStructure(&block, 1, p, nil, nil); err == nil {
		t.Fatal("should reject block without coinbase")
	}
}

func TestValidateBlockStructureExcessiveSubsidy(t *testing.T) {
	// Coinbase value is now validated in ValidateTransactionInputs (where fees are known).
	// ValidateBlockStructure only checks for coinbase output value overflow.
	// This test verifies that a coinbase with value slightly above subsidy passes
	// structural validation (it would be caught by tx input validation with fee context).
	p := params.Regtest
	block := makeTestBlock(1, p)
	block.Transactions[0].Outputs[0].Value = p.InitialSubsidy + 1

	merkle, _ := crypto.ComputeMerkleRoot(block.Transactions)
	block.Header.MerkleRoot = merkle

	if err := ValidateBlockStructure(&block, 1, p, nil, nil); err != nil {
		t.Fatalf("structural validation should pass (coinbase cap enforced in tx validation): %v", err)
	}
}

func TestValidateBlockStructureBadMerkle(t *testing.T) {
	p := params.Regtest
	block := makeTestBlock(1, p)
	block.Header.MerkleRoot = types.Hash{0xFF}

	if err := ValidateBlockStructure(&block, 1, p, nil, nil); err == nil {
		t.Fatal("should reject block with bad merkle root")
	}
}

func TestValidateBlockStructureDuplicateTx(t *testing.T) {
	p := params.Regtest
	block := makeTestBlock(1, p)
	// Add a duplicate of the coinbase (which would have the same txid).
	block.Transactions = append(block.Transactions, block.Transactions[0])
	merkle, _ := crypto.ComputeMerkleRoot(block.Transactions)
	block.Header.MerkleRoot = merkle

	if err := ValidateBlockStructure(&block, 1, p, nil, nil); err == nil {
		t.Fatal("should reject block with duplicate txids")
	}
}

func TestValidateBlockStructureEmpty(t *testing.T) {
	p := params.Regtest
	block := types.Block{
		Header:       types.BlockHeader{Version: 1, Bits: p.InitialBits},
		Transactions: nil,
	}

	if err := ValidateBlockStructure(&block, 0, p, nil, nil); err == nil {
		t.Fatal("should reject empty block")
	}
}

func TestCalcMedianTimePast(t *testing.T) {
	headers := make(map[uint32]*types.BlockHeader)
	for i := uint32(0); i < 15; i++ {
		headers[i] = &types.BlockHeader{Timestamp: 1700000000 + i*60}
	}
	getAncestor := func(h uint32) *types.BlockHeader {
		return headers[h]
	}

	median := CalcMedianTimePast(14, getAncestor)
	// Median of timestamps at heights 4..14 (11 values).
	// Timestamps: 1700000240, ..., 1700000840. Median = 1700000540 (height 9).
	expected := uint32(1700000000 + 9*60)
	if median != expected {
		t.Fatalf("median time past = %d, want %d", median, expected)
	}
}

func TestSubsidySchedule(t *testing.T) {
	p := params.Regtest

	s0 := p.CalcSubsidy(0)
	if s0 != p.InitialSubsidy {
		t.Fatalf("subsidy at height 0 = %d, want %d", s0, p.InitialSubsidy)
	}

	s150 := p.CalcSubsidy(150) // First halving for regtest.
	if s150 != p.InitialSubsidy/2 {
		t.Fatalf("subsidy at height 150 = %d, want %d", s150, p.InitialSubsidy/2)
	}

	s300 := p.CalcSubsidy(300)
	if s300 != p.InitialSubsidy/4 {
		t.Fatalf("subsidy at height 300 = %d, want %d", s300, p.InitialSubsidy/4)
	}
}

// --- BIP-94 timewarp prevention tests ---
//
// These tests use "median-11" (matching mainnet/testnet) and construct
// ancestor headers so the MTP is low enough to allow the block timestamp
// to pass the MTP check, isolating the timewarp rule.

func timewarpAncestors(parentTS uint32) func(uint32) *types.BlockHeader {
	// Build 20 headers where most timestamps are very old (low), but the
	// parent (height 19) has a high timestamp. This simulates the timewarp
	// scenario: an attacker mines many blocks with compressed timestamps,
	// then sets a high timestamp on the last block of the epoch.
	//
	// MTP at tipHeight=19 is the median of heights 9..19 (11 blocks).
	// We set heights 0..18 to very old timestamps so MTP is low,
	// and height 19 (parent) to parentTS.
	headers := make(map[uint32]*types.BlockHeader)
	baseTS := parentTS - 50000
	for i := uint32(0); i < 19; i++ {
		headers[i] = &types.BlockHeader{Timestamp: baseTS + i*10}
	}
	headers[19] = &types.BlockHeader{Timestamp: parentTS}
	return func(h uint32) *types.BlockHeader { return headers[h] }
}

func TestTimewarpRejectedAtRetargetBoundary(t *testing.T) {
	p := &params.ChainParams{
		RetargetInterval:    20,
		MaxTimeFutureDrift:  2 * time.Hour,
		MinTimestampRule:    "median-11",
		TimewarpGracePeriod: 10 * time.Minute,
		ActivationHeights:   map[string]uint32{"timewarp": 1},
	}

	parentTS := uint32(1700002000)
	parent := &types.BlockHeader{Timestamp: parentTS}
	getAncestor := timewarpAncestors(parentTS)

	header := &types.BlockHeader{
		// 700 seconds before parent -- exceeds 600s grace
		Timestamp: parentTS - 700,
	}

	// Height 20 is a retarget boundary (20 % 20 == 0), tipHeight=19.
	err := ValidateHeaderTimestamp(header, parent, 20, uint32(1700003000), getAncestor, 19, p)
	if err == nil {
		t.Fatal("should reject timewarp at retarget boundary")
	}
	if !strings.Contains(err.Error(), "timewarp rejected") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTimewarpAcceptedWithinGrace(t *testing.T) {
	p := &params.ChainParams{
		RetargetInterval:    20,
		MaxTimeFutureDrift:  2 * time.Hour,
		MinTimestampRule:    "median-11",
		TimewarpGracePeriod: 10 * time.Minute,
		ActivationHeights:   map[string]uint32{"timewarp": 1},
	}

	parentTS := uint32(1700002000)
	parent := &types.BlockHeader{Timestamp: parentTS}
	getAncestor := timewarpAncestors(parentTS)

	header := &types.BlockHeader{
		// 500 seconds before parent -- within 600s grace
		Timestamp: parentTS - 500,
	}

	err := ValidateHeaderTimestamp(header, parent, 20, uint32(1700003000), getAncestor, 19, p)
	if err != nil {
		t.Fatalf("should accept timestamp within grace period: %v", err)
	}
}

func TestTimewarpNotEnforcedBeforeActivation(t *testing.T) {
	p := &params.ChainParams{
		RetargetInterval:    20,
		MaxTimeFutureDrift:  2 * time.Hour,
		MinTimestampRule:    "median-11",
		TimewarpGracePeriod: 10 * time.Minute,
		ActivationHeights:   map[string]uint32{"timewarp": 12000},
	}

	parentTS := uint32(1700002000)
	parent := &types.BlockHeader{Timestamp: parentTS}
	getAncestor := timewarpAncestors(parentTS)

	header := &types.BlockHeader{
		// Way before parent -- would be rejected if timewarp active
		Timestamp: parentTS - 2000,
	}

	// Height 20 is below activation height 12000.
	err := ValidateHeaderTimestamp(header, parent, 20, uint32(1700003000), getAncestor, 19, p)
	if err != nil {
		t.Fatalf("should not enforce timewarp before activation: %v", err)
	}
}

func TestTimewarpNotEnforcedOffBoundary(t *testing.T) {
	p := &params.ChainParams{
		RetargetInterval:    20,
		MaxTimeFutureDrift:  2 * time.Hour,
		MinTimestampRule:    "median-11",
		TimewarpGracePeriod: 10 * time.Minute,
		ActivationHeights:   map[string]uint32{"timewarp": 1},
	}

	parentTS := uint32(1700002000)
	parent := &types.BlockHeader{Timestamp: parentTS}
	getAncestor := timewarpAncestors(parentTS)

	header := &types.BlockHeader{
		Timestamp: parentTS + 1,
	}

	// Height 15 is NOT a retarget boundary (15 % 20 != 0).
	err := ValidateHeaderTimestamp(header, parent, 15, uint32(1700003000), getAncestor, 14, p)
	if err != nil {
		t.Fatalf("should not enforce timewarp off retarget boundary: %v", err)
	}
}

func minimalBIP34ScriptSig(height uint32, tag []byte) []byte {
	heightBytes := make([]byte, 4)
	types.PutUint32LE(heightBytes, height)
	pushLen := 4
	switch {
	case height <= 0xFF:
		pushLen = 1
	case height <= 0xFFFF:
		pushLen = 2
	case height <= 0xFFFFFF:
		pushLen = 3
	}
	sig := make([]byte, 0, 1+pushLen+len(tag))
	sig = append(sig, byte(pushLen))
	sig = append(sig, heightBytes[:pushLen]...)
	sig = append(sig, tag...)
	return sig
}
