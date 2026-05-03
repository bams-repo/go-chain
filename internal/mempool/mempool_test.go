// Copyright (c) 2024-2026 The Fairchain Contributors
// Fairchain is an experiment in modularity, designed to improve on the work
// of Satoshi Nakamoto and to inspire more creative genius in the space.
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

package mempool

import (
	"testing"
	"time"

	"github.com/bams-repo/fairchain/internal/crypto"
	"github.com/bams-repo/fairchain/internal/params"
	"github.com/bams-repo/fairchain/internal/types"
	"github.com/bams-repo/fairchain/internal/utxo"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func testParams() *params.ChainParams {
	return &params.ChainParams{
		MaxMempoolSize:    100,
		MinRelayTxFee:     0,
		MinRelayTxFeeRate: 0,
		MempoolExpiry:     336 * time.Hour,
		CoinbaseMaturity:  100,
		MaxBlockSize:      1_000_000,
		MaxBlockTxCount:   10_000,
		InitialSubsidy:    50_0000_0000,
		SubsidyHalvingInterval: 210_000,
		ActivationHeights: map[string]uint32{},
	}
}

// fundedTx creates a signed P2PKH transaction spending the given UTXO.
// Returns the transaction and the output's pkScript (for chaining).
func fundedTx(
	privKey *secp256k1.PrivateKey,
	prevHash types.Hash, prevIdx uint32,
	prevPkScript []byte, prevValue uint64,
	outputValue uint64,
) *types.Transaction {
	tx := &types.Transaction{
		Version: 1,
		Inputs: []types.TxInput{{
			PreviousOutPoint: types.OutPoint{Hash: prevHash, Index: prevIdx},
			Sequence:         0xFFFFFFFF,
		}},
		Outputs: []types.TxOutput{{
			Value:    outputValue,
			PkScript: prevPkScript,
		}},
		LockTime: 0,
	}
	sigScript, err := crypto.SignInput(tx, 0, prevPkScript, privKey)
	if err != nil {
		panic("sign failed: " + err.Error())
	}
	tx.Inputs[0].SignatureScript = sigScript
	return tx
}

// fundedTxMultiOut creates a signed P2PKH transaction with multiple outputs.
func fundedTxMultiOut(
	privKey *secp256k1.PrivateKey,
	prevHash types.Hash, prevIdx uint32,
	prevPkScript []byte,
	outputs []types.TxOutput,
) *types.Transaction {
	tx := &types.Transaction{
		Version:  1,
		Inputs:   []types.TxInput{{
			PreviousOutPoint: types.OutPoint{Hash: prevHash, Index: prevIdx},
			Sequence:         0xFFFFFFFF,
		}},
		Outputs:  outputs,
		LockTime: 0,
	}
	sigScript, err := crypto.SignInput(tx, 0, prevPkScript, privKey)
	if err != nil {
		panic("sign failed: " + err.Error())
	}
	tx.Inputs[0].SignatureScript = sigScript
	return tx
}

func mustHash(tx *types.Transaction) types.Hash {
	h, err := crypto.HashTransaction(tx)
	if err != nil {
		panic(err)
	}
	return h
}

func setupKeyAndUTXO(t *testing.T) (*secp256k1.PrivateKey, []byte, *utxo.Set, types.Hash) {
	t.Helper()
	privBytes, pubBytes, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	privKey, err := crypto.PrivKeyFromBytes(privBytes)
	if err != nil {
		t.Fatal(err)
	}
	pkScript := crypto.MakeP2PKHScriptFromPubKey(pubBytes)

	utxoSet := utxo.NewSet()
	fundingHash := types.Hash{0x01, 0x02, 0x03}
	utxoSet.Add(fundingHash, 0, &utxo.UtxoEntry{
		Value:    10_0000_0000,
		PkScript: pkScript,
		Height:   1,
	})
	return privKey, pkScript, utxoSet, fundingHash
}

// ---------------------------------------------------------------------------
// AddTx tests
// ---------------------------------------------------------------------------

func TestAddTx_ValidTransaction(t *testing.T) {
	privKey, pkScript, utxoSet, fundingHash := setupKeyAndUTXO(t)
	p := testParams()
	mp := New(p, utxoSet, func() uint32 { return 200 })

	tx := fundedTx(privKey, fundingHash, 0, pkScript, 10_0000_0000, 9_9999_0000)
	txHash, err := mp.AddTx(tx)
	if err != nil {
		t.Fatalf("AddTx failed: %v", err)
	}
	if txHash == types.ZeroHash {
		t.Fatal("expected non-zero hash")
	}
	if mp.Count() != 1 {
		t.Fatalf("expected count 1, got %d", mp.Count())
	}
	if !mp.HasTx(txHash) {
		t.Fatal("HasTx returned false for added tx")
	}
	got, ok := mp.GetTx(txHash)
	if !ok || got == nil {
		t.Fatal("GetTx returned nil for added tx")
	}
}

func TestAddTx_RejectCoinbase(t *testing.T) {
	p := testParams()
	utxoSet := utxo.NewSet()
	mp := New(p, utxoSet, func() uint32 { return 200 })

	coinbase := &types.Transaction{
		Version: 1,
		Inputs: []types.TxInput{{
			PreviousOutPoint: types.CoinbaseOutPoint,
			SignatureScript:  []byte("coinbase"),
			Sequence:         0xFFFFFFFF,
		}},
		Outputs: []types.TxOutput{{Value: 50_0000_0000, PkScript: []byte{0x00}}},
	}
	if _, err := mp.AddTx(coinbase); err == nil {
		t.Fatal("expected error for coinbase tx")
	}
}

func TestAddTx_RejectNoInputs(t *testing.T) {
	p := testParams()
	utxoSet := utxo.NewSet()
	mp := New(p, utxoSet, func() uint32 { return 200 })

	tx := &types.Transaction{
		Version: 1,
		Inputs:  []types.TxInput{},
		Outputs: []types.TxOutput{{Value: 1000, PkScript: []byte{0x01}}},
	}
	if _, err := mp.AddTx(tx); err == nil {
		t.Fatal("expected error for tx with no inputs")
	}
}

func TestAddTx_RejectNoOutputs(t *testing.T) {
	p := testParams()
	utxoSet := utxo.NewSet()
	mp := New(p, utxoSet, func() uint32 { return 200 })

	tx := &types.Transaction{
		Version: 1,
		Inputs: []types.TxInput{{
			PreviousOutPoint: types.OutPoint{Hash: types.Hash{0x01}, Index: 0},
			Sequence:         0xFFFFFFFF,
		}},
		Outputs: []types.TxOutput{},
	}
	if _, err := mp.AddTx(tx); err == nil {
		t.Fatal("expected error for tx with no outputs")
	}
}

func TestAddTx_RejectDuplicate(t *testing.T) {
	privKey, pkScript, utxoSet, fundingHash := setupKeyAndUTXO(t)
	p := testParams()
	mp := New(p, utxoSet, func() uint32 { return 200 })

	tx := fundedTx(privKey, fundingHash, 0, pkScript, 10_0000_0000, 9_9999_0000)
	if _, err := mp.AddTx(tx); err != nil {
		t.Fatalf("first AddTx failed: %v", err)
	}
	if _, err := mp.AddTx(tx); err == nil {
		t.Fatal("expected error for duplicate tx")
	}
}

func TestAddTx_RejectDoubleSpend(t *testing.T) {
	privKey, pkScript, utxoSet, fundingHash := setupKeyAndUTXO(t)
	p := testParams()
	mp := New(p, utxoSet, func() uint32 { return 200 })

	tx1 := fundedTx(privKey, fundingHash, 0, pkScript, 10_0000_0000, 9_9999_0000)
	if _, err := mp.AddTx(tx1); err != nil {
		t.Fatalf("AddTx tx1 failed: %v", err)
	}

	tx2 := fundedTx(privKey, fundingHash, 0, pkScript, 10_0000_0000, 9_9998_0000)
	if _, err := mp.AddTx(tx2); err == nil {
		t.Fatal("expected error for double-spend tx")
	}
}

func TestAddTx_RejectMissingUTXO(t *testing.T) {
	privKey, pkScript, utxoSet, _ := setupKeyAndUTXO(t)
	p := testParams()
	mp := New(p, utxoSet, func() uint32 { return 200 })

	bogusHash := types.Hash{0xDE, 0xAD}
	tx := fundedTx(privKey, bogusHash, 0, pkScript, 10_0000_0000, 9_0000_0000)
	if _, err := mp.AddTx(tx); err == nil {
		t.Fatal("expected error for missing UTXO")
	}
}

func TestAddTx_MinRelayFee(t *testing.T) {
	privKey, pkScript, utxoSet, fundingHash := setupKeyAndUTXO(t)
	p := testParams()
	p.MinRelayTxFee = 5_0000_0000
	mp := New(p, utxoSet, func() uint32 { return 200 })

	tx := fundedTx(privKey, fundingHash, 0, pkScript, 10_0000_0000, 9_9999_0000)
	if _, err := mp.AddTx(tx); err == nil {
		t.Fatal("expected error for fee below minimum relay fee")
	}
}

func TestAddTx_MinRelayFeeRate(t *testing.T) {
	privKey, pkScript, utxoSet, fundingHash := setupKeyAndUTXO(t)
	p := testParams()
	p.MinRelayTxFeeRate = 1_000_000
	mp := New(p, utxoSet, func() uint32 { return 200 })

	tx := fundedTx(privKey, fundingHash, 0, pkScript, 10_0000_0000, 9_9999_0000)
	if _, err := mp.AddTx(tx); err == nil {
		t.Fatal("expected error for fee rate below minimum")
	}
}

// ---------------------------------------------------------------------------
// RemoveTx tests
// ---------------------------------------------------------------------------

func TestRemoveTx(t *testing.T) {
	privKey, pkScript, utxoSet, fundingHash := setupKeyAndUTXO(t)
	p := testParams()
	mp := New(p, utxoSet, func() uint32 { return 200 })

	tx := fundedTx(privKey, fundingHash, 0, pkScript, 10_0000_0000, 9_9999_0000)
	txHash, _ := mp.AddTx(tx)

	mp.RemoveTx(txHash)
	if mp.Count() != 0 {
		t.Fatalf("expected count 0 after remove, got %d", mp.Count())
	}
	if mp.HasTx(txHash) {
		t.Fatal("HasTx returned true after removal")
	}
}

func TestRemoveTxs_ClearsSpentOutpoints(t *testing.T) {
	privKey, pkScript, utxoSet, fundingHash := setupKeyAndUTXO(t)
	p := testParams()
	mp := New(p, utxoSet, func() uint32 { return 200 })

	tx := fundedTx(privKey, fundingHash, 0, pkScript, 10_0000_0000, 9_9999_0000)
	txHash, _ := mp.AddTx(tx)

	if !mp.IsOutpointSpent(fundingHash, 0) {
		t.Fatal("outpoint should be marked spent")
	}

	mp.RemoveTxs([]types.Hash{txHash})
	if mp.IsOutpointSpent(fundingHash, 0) {
		t.Fatal("outpoint should no longer be spent after removal")
	}
}

// ---------------------------------------------------------------------------
// Eviction tests
// ---------------------------------------------------------------------------

func TestEviction_LowestFeeRateEvictedFirst(t *testing.T) {
	privBytes, pubBytes, _ := crypto.GenerateKeyPair()
	privKey, _ := crypto.PrivKeyFromBytes(privBytes)
	pkScript := crypto.MakeP2PKHScriptFromPubKey(pubBytes)

	utxoSet := utxo.NewSet()

	p := testParams()
	p.MaxMempoolSize = 2
	mp := New(p, utxoSet, func() uint32 { return 200 })

	var hashes [3]types.Hash
	fees := [3]uint64{1_0000, 5_0000, 3_0000}

	for i := 0; i < 3; i++ {
		fundHash := types.Hash{byte(i + 10)}
		utxoSet.Add(fundHash, 0, &utxo.UtxoEntry{
			Value:    10_0000_0000,
			PkScript: pkScript,
			Height:   1,
		})
		outputVal := 10_0000_0000 - fees[i]
		tx := fundedTx(privKey, fundHash, 0, pkScript, 10_0000_0000, outputVal)
		h, err := mp.AddTx(tx)
		if err != nil {
			t.Fatalf("AddTx %d failed: %v", i, err)
		}
		hashes[i] = h
	}

	if mp.Count() != 2 {
		t.Fatalf("expected 2 txs after eviction, got %d", mp.Count())
	}

	// The lowest fee-rate tx (tx0 with fee 1_0000) should have been evicted.
	if mp.HasTx(hashes[0]) {
		t.Fatal("lowest fee-rate tx should have been evicted")
	}
	if !mp.HasTx(hashes[1]) {
		t.Fatal("higher fee-rate tx1 should remain")
	}
	if !mp.HasTx(hashes[2]) {
		t.Fatal("higher fee-rate tx2 should remain")
	}
}

func TestEviction_ExplicitEvictLowestFeeRate(t *testing.T) {
	privKey, pkScript, utxoSet, fundingHash := setupKeyAndUTXO(t)
	p := testParams()
	mp := New(p, utxoSet, func() uint32 { return 200 })

	tx := fundedTx(privKey, fundingHash, 0, pkScript, 10_0000_0000, 9_9999_0000)
	mp.AddTx(tx)

	if !mp.EvictLowestFeeRate() {
		t.Fatal("EvictLowestFeeRate should return true when pool is non-empty")
	}
	if mp.Count() != 0 {
		t.Fatal("mempool should be empty after eviction")
	}
	if mp.EvictLowestFeeRate() {
		t.Fatal("EvictLowestFeeRate should return false when pool is empty")
	}
}

// ---------------------------------------------------------------------------
// CPFP tests
// ---------------------------------------------------------------------------

func TestCPFP_ChildSpendsUnconfirmedParent(t *testing.T) {
	privBytes, pubBytes, _ := crypto.GenerateKeyPair()
	privKey, _ := crypto.PrivKeyFromBytes(privBytes)
	pkScript := crypto.MakeP2PKHScriptFromPubKey(pubBytes)

	utxoSet := utxo.NewSet()
	fundingHash := types.Hash{0xAA}
	utxoSet.Add(fundingHash, 0, &utxo.UtxoEntry{
		Value:    20_0000_0000,
		PkScript: pkScript,
		Height:   1,
	})

	p := testParams()
	mp := New(p, utxoSet, func() uint32 { return 200 })

	// Parent tx: spends confirmed UTXO, creates an unconfirmed output.
	parentTx := fundedTx(privKey, fundingHash, 0, pkScript, 20_0000_0000, 19_9999_0000)
	parentHash, err := mp.AddTx(parentTx)
	if err != nil {
		t.Fatalf("parent AddTx failed: %v", err)
	}

	// Child tx: spends the parent's unconfirmed output (CPFP).
	childTx := fundedTx(privKey, parentHash, 0, pkScript, 19_9999_0000, 19_9998_0000)
	childHash, err := mp.AddTx(childTx)
	if err != nil {
		t.Fatalf("child AddTx (CPFP) failed: %v", err)
	}

	if mp.Count() != 2 {
		t.Fatalf("expected 2 txs, got %d", mp.Count())
	}
	if !mp.HasTx(parentHash) || !mp.HasTx(childHash) {
		t.Fatal("both parent and child should be in mempool")
	}
}

func TestCPFP_RejectChildSpendingAlreadySpentParentOutput(t *testing.T) {
	privBytes, pubBytes, _ := crypto.GenerateKeyPair()
	privKey, _ := crypto.PrivKeyFromBytes(privBytes)
	pkScript := crypto.MakeP2PKHScriptFromPubKey(pubBytes)

	utxoSet := utxo.NewSet()
	fundingHash := types.Hash{0xBB}
	utxoSet.Add(fundingHash, 0, &utxo.UtxoEntry{
		Value:    20_0000_0000,
		PkScript: pkScript,
		Height:   1,
	})

	p := testParams()
	mp := New(p, utxoSet, func() uint32 { return 200 })

	// Parent creates two outputs.
	parentTx := fundedTxMultiOut(privKey, fundingHash, 0, pkScript, []types.TxOutput{
		{Value: 10_0000_0000, PkScript: pkScript},
		{Value: 9_9999_0000, PkScript: pkScript},
	})
	parentHash, err := mp.AddTx(parentTx)
	if err != nil {
		t.Fatalf("parent AddTx failed: %v", err)
	}

	// Child 1 spends parent output 0.
	child1 := fundedTx(privKey, parentHash, 0, pkScript, 10_0000_0000, 9_9998_0000)
	if _, err := mp.AddTx(child1); err != nil {
		t.Fatalf("child1 AddTx failed: %v", err)
	}

	// Child 2 tries to spend the same parent output 0 — double spend.
	child2 := fundedTx(privKey, parentHash, 0, pkScript, 10_0000_0000, 9_9997_0000)
	if _, err := mp.AddTx(child2); err == nil {
		t.Fatal("expected double-spend rejection for second child spending same parent output")
	}
}

func TestCPFP_RejectChildSpendingNonexistentParentOutput(t *testing.T) {
	privBytes, pubBytes, _ := crypto.GenerateKeyPair()
	privKey, _ := crypto.PrivKeyFromBytes(privBytes)
	pkScript := crypto.MakeP2PKHScriptFromPubKey(pubBytes)

	utxoSet := utxo.NewSet()
	fundingHash := types.Hash{0xCC}
	utxoSet.Add(fundingHash, 0, &utxo.UtxoEntry{
		Value:    10_0000_0000,
		PkScript: pkScript,
		Height:   1,
	})

	p := testParams()
	mp := New(p, utxoSet, func() uint32 { return 200 })

	parentTx := fundedTx(privKey, fundingHash, 0, pkScript, 10_0000_0000, 9_9999_0000)
	parentHash, err := mp.AddTx(parentTx)
	if err != nil {
		t.Fatalf("parent AddTx failed: %v", err)
	}

	// Try to spend output index 5 of parent (only index 0 exists).
	childTx := fundedTx(privKey, parentHash, 5, pkScript, 9_9999_0000, 9_9998_0000)
	if _, err := mp.AddTx(childTx); err == nil {
		t.Fatal("expected error for child spending non-existent parent output index")
	}
}

// ---------------------------------------------------------------------------
// SweepInvalid tests — post-reorg eviction of txs with vanished inputs
// ---------------------------------------------------------------------------

// TestSweepInvalid_EvictsTxWithVanishedInput simulates the reorg case: a tx
// that was valid at AddTx time becomes invalid when its input UTXO is
// removed (i.e. the block that created the input was disconnected).
func TestSweepInvalid_EvictsTxWithVanishedInput(t *testing.T) {
	privKey, pkScript, utxoSet, fundingHash := setupKeyAndUTXO(t)

	p := testParams()
	mp := New(p, utxoSet, func() uint32 { return 200 })

	tx := fundedTx(privKey, fundingHash, 0, pkScript, 10_0000_0000, 9_9999_0000)
	txHash, err := mp.AddTx(tx)
	if err != nil {
		t.Fatalf("AddTx failed: %v", err)
	}
	if mp.Count() != 1 {
		t.Fatalf("expected 1 tx pre-sweep, got %d", mp.Count())
	}

	// Simulate a reorg disconnecting the block that created the funding UTXO:
	// remove the output from the live UTXO set. The tx in the mempool is now
	// referentially invalid.
	utxoSet.Remove(fundingHash, 0)

	evicted := mp.SweepInvalid()
	if evicted != 1 {
		t.Fatalf("expected 1 evicted, got %d", evicted)
	}
	if mp.HasTx(txHash) {
		t.Fatal("tx should have been evicted from mempool")
	}
	if mp.Count() != 0 {
		t.Fatalf("expected empty mempool, got %d txs", mp.Count())
	}
}

// TestSweepInvalid_PreservesValidCPFPChain ensures the sweep does not
// over-evict: a parent + child still in mempool remain after sweep when the
// parent's input is still in the UTXO set.
func TestSweepInvalid_PreservesValidCPFPChain(t *testing.T) {
	privKey, pkScript, utxoSet, fundingHash := setupKeyAndUTXO(t)

	p := testParams()
	mp := New(p, utxoSet, func() uint32 { return 200 })

	parentTx := fundedTx(privKey, fundingHash, 0, pkScript, 10_0000_0000, 9_9999_0000)
	parentHash, err := mp.AddTx(parentTx)
	if err != nil {
		t.Fatalf("parent AddTx failed: %v", err)
	}
	childTx := fundedTx(privKey, parentHash, 0, pkScript, 9_9999_0000, 9_9998_0000)
	childHash, err := mp.AddTx(childTx)
	if err != nil {
		t.Fatalf("child AddTx (CPFP) failed: %v", err)
	}

	// Sweep with no UTXO changes: nothing should be evicted.
	evicted := mp.SweepInvalid()
	if evicted != 0 {
		t.Fatalf("expected 0 evicted, got %d", evicted)
	}
	if !mp.HasTx(parentHash) || !mp.HasTx(childHash) {
		t.Fatal("CPFP parent and child should both remain after a no-op sweep")
	}
	if mp.Count() != 2 {
		t.Fatalf("expected 2 txs, got %d", mp.Count())
	}
}

// TestSweepInvalid_CascadeEvictsOrphanedChildren verifies the fixed-point
// iteration: when a parent is evicted, its CPFP children are no longer
// connectable and must be evicted in a subsequent pass.
func TestSweepInvalid_CascadeEvictsOrphanedChildren(t *testing.T) {
	privKey, pkScript, utxoSet, fundingHash := setupKeyAndUTXO(t)

	p := testParams()
	mp := New(p, utxoSet, func() uint32 { return 200 })

	// Parent (depth 0) spends the funding UTXO.
	parentTx := fundedTx(privKey, fundingHash, 0, pkScript, 10_0000_0000, 9_9999_0000)
	parentHash, err := mp.AddTx(parentTx)
	if err != nil {
		t.Fatalf("parent AddTx failed: %v", err)
	}
	// Child (depth 1) spends parent's output.
	childTx := fundedTx(privKey, parentHash, 0, pkScript, 9_9999_0000, 9_9998_0000)
	childHash, err := mp.AddTx(childTx)
	if err != nil {
		t.Fatalf("child AddTx failed: %v", err)
	}
	// Grandchild (depth 2) spends child's output.
	grandchildTx := fundedTx(privKey, childHash, 0, pkScript, 9_9998_0000, 9_9997_0000)
	grandchildHash, err := mp.AddTx(grandchildTx)
	if err != nil {
		t.Fatalf("grandchild AddTx failed: %v", err)
	}
	if mp.Count() != 3 {
		t.Fatalf("expected 3 txs in chain, got %d", mp.Count())
	}

	// Reorg: the funding UTXO disappears from the live set.
	utxoSet.Remove(fundingHash, 0)

	evicted := mp.SweepInvalid()
	if evicted != 3 {
		t.Fatalf("expected cascade eviction of all 3 chain entries, got %d", evicted)
	}
	if mp.HasTx(parentHash) || mp.HasTx(childHash) || mp.HasTx(grandchildHash) {
		t.Fatal("none of parent / child / grandchild should remain after cascade sweep")
	}
	if mp.Count() != 0 {
		t.Fatalf("expected empty mempool, got %d txs", mp.Count())
	}
}

// TestSweepInvalid_NoOpOnHealthyMempool is a paranoia check: the sweep is
// idempotent and a no-op when nothing has changed.
func TestSweepInvalid_NoOpOnHealthyMempool(t *testing.T) {
	privKey, pkScript, utxoSet, fundingHash := setupKeyAndUTXO(t)

	p := testParams()
	mp := New(p, utxoSet, func() uint32 { return 200 })

	tx := fundedTx(privKey, fundingHash, 0, pkScript, 10_0000_0000, 9_9999_0000)
	if _, err := mp.AddTx(tx); err != nil {
		t.Fatalf("AddTx failed: %v", err)
	}

	for i := 0; i < 3; i++ {
		if evicted := mp.SweepInvalid(); evicted != 0 {
			t.Fatalf("iteration %d: expected 0 evicted, got %d", i, evicted)
		}
		if mp.Count() != 1 {
			t.Fatalf("iteration %d: mempool changed unexpectedly", i)
		}
	}
}

// ---------------------------------------------------------------------------
// Persistence roundtrip tests
// ---------------------------------------------------------------------------

func TestPersistence_DumpLoadRoundtrip(t *testing.T) {
	privKey, pkScript, utxoSet, fundingHash := setupKeyAndUTXO(t)
	p := testParams()
	mp := New(p, utxoSet, func() uint32 { return 200 })

	tx := fundedTx(privKey, fundingHash, 0, pkScript, 10_0000_0000, 9_9999_0000)
	origHash, err := mp.AddTx(tx)
	if err != nil {
		t.Fatalf("AddTx failed: %v", err)
	}

	data := mp.DumpToBytes()
	if data == nil {
		t.Fatal("DumpToBytes returned nil for non-empty mempool")
	}

	// Create a fresh mempool and load the dump.
	mp2 := New(p, utxoSet, func() uint32 { return 200 })
	loaded := mp2.LoadFromBytes(data)
	if loaded != 1 {
		t.Fatalf("expected 1 loaded tx, got %d", loaded)
	}
	if !mp2.HasTx(origHash) {
		t.Fatal("loaded mempool should contain the original tx")
	}
}

func TestPersistence_EmptyDump(t *testing.T) {
	p := testParams()
	utxoSet := utxo.NewSet()
	mp := New(p, utxoSet, func() uint32 { return 200 })

	data := mp.DumpToBytes()
	if data != nil {
		t.Fatal("DumpToBytes should return nil for empty mempool")
	}

	loaded := mp.LoadFromBytes(nil)
	if loaded != 0 {
		t.Fatalf("LoadFromBytes(nil) should return 0, got %d", loaded)
	}
	loaded = mp.LoadFromBytes([]byte{})
	if loaded != 0 {
		t.Fatalf("LoadFromBytes(empty) should return 0, got %d", loaded)
	}
}

func TestPersistence_ExpiredTxsSkippedOnLoad(t *testing.T) {
	privKey, pkScript, utxoSet, fundingHash := setupKeyAndUTXO(t)
	p := testParams()
	p.MempoolExpiry = 1 * time.Hour
	mp := New(p, utxoSet, func() uint32 { return 200 })

	tx := fundedTx(privKey, fundingHash, 0, pkScript, 10_0000_0000, 9_9999_0000)
	txHash, _ := mp.AddTx(tx)

	// Backdate the entry so it appears expired.
	mp.mu.Lock()
	if entry, ok := mp.txs[txHash]; ok {
		entry.AddedAt = time.Now().Add(-2 * time.Hour)
	}
	mp.mu.Unlock()

	data := mp.DumpToBytes()

	mp2 := New(p, utxoSet, func() uint32 { return 200 })
	loaded := mp2.LoadFromBytes(data)
	if loaded != 0 {
		t.Fatalf("expected 0 loaded (expired), got %d", loaded)
	}
}

func TestPersistence_TimestampPreserved(t *testing.T) {
	privKey, pkScript, utxoSet, fundingHash := setupKeyAndUTXO(t)
	p := testParams()
	mp := New(p, utxoSet, func() uint32 { return 200 })

	tx := fundedTx(privKey, fundingHash, 0, pkScript, 10_0000_0000, 9_9999_0000)
	txHash, _ := mp.AddTx(tx)

	fixedTime := time.Now().Add(-1 * time.Hour)
	mp.mu.Lock()
	mp.txs[txHash].AddedAt = fixedTime
	mp.mu.Unlock()

	data := mp.DumpToBytes()

	mp2 := New(p, utxoSet, func() uint32 { return 200 })
	loaded := mp2.LoadFromBytes(data)
	if loaded != 1 {
		t.Fatalf("expected 1 loaded tx, got %d (count=%d)", loaded, mp2.Count())
	}

	entry, ok := mp2.GetTxEntry(txHash)
	if !ok {
		t.Fatal("tx not found after load")
	}
	if entry.AddedAt.Unix() != fixedTime.Unix() {
		t.Fatalf("timestamp not preserved: got %v, want %v", entry.AddedAt.Unix(), fixedTime.Unix())
	}
}

// ---------------------------------------------------------------------------
// Expiry tests
// ---------------------------------------------------------------------------

func TestExpireOldTxs(t *testing.T) {
	privKey, pkScript, utxoSet, fundingHash := setupKeyAndUTXO(t)
	p := testParams()
	p.MempoolExpiry = 1 * time.Hour
	mp := New(p, utxoSet, func() uint32 { return 200 })

	tx := fundedTx(privKey, fundingHash, 0, pkScript, 10_0000_0000, 9_9999_0000)
	txHash, _ := mp.AddTx(tx)

	// Should not expire a fresh tx.
	expired := mp.ExpireOldTxs()
	if expired != 0 {
		t.Fatalf("expected 0 expired, got %d", expired)
	}

	// Backdate and expire.
	mp.mu.Lock()
	mp.txs[txHash].AddedAt = time.Now().Add(-2 * time.Hour)
	mp.mu.Unlock()

	expired = mp.ExpireOldTxs()
	if expired != 1 {
		t.Fatalf("expected 1 expired, got %d", expired)
	}
	if mp.Count() != 0 {
		t.Fatal("mempool should be empty after expiry")
	}
}

func TestExpireOldTxs_DisabledWhenZero(t *testing.T) {
	privKey, pkScript, utxoSet, fundingHash := setupKeyAndUTXO(t)
	p := testParams()
	p.MempoolExpiry = 0
	mp := New(p, utxoSet, func() uint32 { return 200 })

	tx := fundedTx(privKey, fundingHash, 0, pkScript, 10_0000_0000, 9_9999_0000)
	txHash, _ := mp.AddTx(tx)

	mp.mu.Lock()
	mp.txs[txHash].AddedAt = time.Now().Add(-1000 * time.Hour)
	mp.mu.Unlock()

	expired := mp.ExpireOldTxs()
	if expired != 0 {
		t.Fatalf("expected 0 expired when MempoolExpiry=0, got %d", expired)
	}
}

// ---------------------------------------------------------------------------
// BlockTemplate tests
// ---------------------------------------------------------------------------

func TestBlockTemplate_OrderedByFeeRate(t *testing.T) {
	privBytes, pubBytes, _ := crypto.GenerateKeyPair()
	privKey, _ := crypto.PrivKeyFromBytes(privBytes)
	pkScript := crypto.MakeP2PKHScriptFromPubKey(pubBytes)

	utxoSet := utxo.NewSet()
	p := testParams()
	mp := New(p, utxoSet, func() uint32 { return 200 })

	fees := []uint64{1_0000, 5_0000, 3_0000}
	var hashes [3]types.Hash

	for i := 0; i < 3; i++ {
		fundHash := types.Hash{byte(i + 20)}
		utxoSet.Add(fundHash, 0, &utxo.UtxoEntry{
			Value:    10_0000_0000,
			PkScript: pkScript,
			Height:   1,
		})
		tx := fundedTx(privKey, fundHash, 0, pkScript, 10_0000_0000, 10_0000_0000-fees[i])
		h, err := mp.AddTx(tx)
		if err != nil {
			t.Fatalf("AddTx %d failed: %v", i, err)
		}
		hashes[i] = h
	}

	result := mp.BlockTemplate()
	if len(result.Transactions) != 3 {
		t.Fatalf("expected 3 txs in template, got %d", len(result.Transactions))
	}

	// Verify descending fee-rate order.
	for i := 1; i < len(result.Entries); i++ {
		if result.Entries[i].Fee > result.Entries[i-1].Fee {
			t.Fatalf("template not sorted by fee rate: entry %d fee %d > entry %d fee %d",
				i, result.Entries[i].Fee, i-1, result.Entries[i-1].Fee)
		}
	}

	expectedTotal := fees[0] + fees[1] + fees[2]
	if result.TotalFees != expectedTotal {
		t.Fatalf("expected total fees %d, got %d", expectedTotal, result.TotalFees)
	}
}

// ---------------------------------------------------------------------------
// Metadata and query tests
// ---------------------------------------------------------------------------

func TestTotalFees_And_TotalSize(t *testing.T) {
	privKey, pkScript, utxoSet, fundingHash := setupKeyAndUTXO(t)
	p := testParams()
	mp := New(p, utxoSet, func() uint32 { return 200 })

	tx := fundedTx(privKey, fundingHash, 0, pkScript, 10_0000_0000, 9_9999_0000)
	mp.AddTx(tx)

	if mp.TotalFees() != 1_0000 {
		t.Fatalf("expected fee 10000, got %d", mp.TotalFees())
	}
	if mp.TotalSize() <= 0 {
		t.Fatal("TotalSize should be positive")
	}
}

func TestGetTxHashes(t *testing.T) {
	privBytes, pubBytes, _ := crypto.GenerateKeyPair()
	privKey, _ := crypto.PrivKeyFromBytes(privBytes)
	pkScript := crypto.MakeP2PKHScriptFromPubKey(pubBytes)

	utxoSet := utxo.NewSet()
	p := testParams()
	mp := New(p, utxoSet, func() uint32 { return 200 })

	expected := make(map[types.Hash]bool)
	for i := 0; i < 3; i++ {
		fundHash := types.Hash{byte(i + 30)}
		utxoSet.Add(fundHash, 0, &utxo.UtxoEntry{
			Value:    10_0000_0000,
			PkScript: pkScript,
			Height:   1,
		})
		tx := fundedTx(privKey, fundHash, 0, pkScript, 10_0000_0000, 9_9999_0000)
		h, _ := mp.AddTx(tx)
		expected[h] = true
	}

	hashes := mp.GetTxHashes()
	if len(hashes) != 3 {
		t.Fatalf("expected 3 hashes, got %d", len(hashes))
	}
	for _, h := range hashes {
		if !expected[h] {
			t.Fatalf("unexpected hash %s", h)
		}
	}
}

func TestIsOutpointSpent(t *testing.T) {
	privKey, pkScript, utxoSet, fundingHash := setupKeyAndUTXO(t)
	p := testParams()
	mp := New(p, utxoSet, func() uint32 { return 200 })

	if mp.IsOutpointSpent(fundingHash, 0) {
		t.Fatal("outpoint should not be spent before any tx")
	}

	fundedTx(privKey, fundingHash, 0, pkScript, 10_0000_0000, 9_9999_0000)
	tx := fundedTx(privKey, fundingHash, 0, pkScript, 10_0000_0000, 9_9999_0000)
	mp.AddTx(tx)

	if !mp.IsOutpointSpent(fundingHash, 0) {
		t.Fatal("outpoint should be spent after AddTx")
	}
}

// ---------------------------------------------------------------------------
// GetTxEntry metadata test
// ---------------------------------------------------------------------------

func TestGetTxEntry_Metadata(t *testing.T) {
	privKey, pkScript, utxoSet, fundingHash := setupKeyAndUTXO(t)
	p := testParams()
	mp := New(p, utxoSet, func() uint32 { return 200 })

	tx := fundedTx(privKey, fundingHash, 0, pkScript, 10_0000_0000, 9_9999_0000)
	txHash, _ := mp.AddTx(tx)

	entry, ok := mp.GetTxEntry(txHash)
	if !ok {
		t.Fatal("GetTxEntry returned false")
	}
	if entry.Hash != txHash {
		t.Fatal("entry hash mismatch")
	}
	if entry.Fee != 1_0000 {
		t.Fatalf("expected fee 10000, got %d", entry.Fee)
	}
	if entry.Size <= 0 {
		t.Fatal("entry size should be positive")
	}
	if entry.FeeRate != entry.Fee/uint64(entry.Size) {
		t.Fatal("fee rate mismatch")
	}
	if entry.AddedAt.IsZero() {
		t.Fatal("AddedAt should not be zero")
	}
}
