package consensus

import (
	"testing"

	"github.com/bams-repo/fairchain/internal/params"
	"github.com/bams-repo/fairchain/internal/types"
	"github.com/bams-repo/fairchain/internal/utxo"
)

func makeTestParams() *params.ChainParams {
	p := &params.ChainParams{}
	*p = *params.Regtest
	return p
}

func makeCoinbaseTx(height uint32, value uint64) types.Transaction {
	heightBytes := make([]byte, 4)
	types.PutUint32LE(heightBytes, height)
	return types.Transaction{
		Version: 1,
		Inputs: []types.TxInput{{
			PreviousOutPoint: types.CoinbaseOutPoint,
			SignatureScript:  append(heightBytes, []byte("test")...),
			Sequence:         0xFFFFFFFF,
		}},
		Outputs: []types.TxOutput{{
			Value:    value,
			PkScript: []byte{0x00},
		}},
	}
}

func addUTXO(s *utxo.Set, hash types.Hash, index uint32, value uint64, height uint32, isCoinbase bool) {
	s.Add(hash, index, &utxo.UtxoEntry{
		Value:      value,
		PkScript:   []byte{0x00},
		Height:     height,
		IsCoinbase: isCoinbase,
	})
}

func TestDoubleSpendWithinBlock(t *testing.T) {
	p := makeTestParams()
	s := utxo.NewSet()

	var txHash1 types.Hash
	txHash1[0] = 0x01
	addUTXO(s, txHash1, 0, 1000, 0, false)

	block := &types.Block{
		Transactions: []types.Transaction{
			makeCoinbaseTx(5, p.CalcSubsidy(5)),
			{
				Version: 1,
				Inputs: []types.TxInput{
					{PreviousOutPoint: types.OutPoint{Hash: txHash1, Index: 0}, Sequence: 0xFFFFFFFF},
				},
				Outputs: []types.TxOutput{{Value: 500, PkScript: []byte{0x01}}},
			},
			{
				Version: 1,
				Inputs: []types.TxInput{
					{PreviousOutPoint: types.OutPoint{Hash: txHash1, Index: 0}, Sequence: 0xFFFFFFFF},
				},
				Outputs: []types.TxOutput{{Value: 400, PkScript: []byte{0x02}}},
			},
		},
	}

	_, err := ValidateTransactionInputs(block, s, 5, p)
	if err == nil {
		t.Fatal("expected rejection for double-spend within block")
	}
}

func TestDuplicateInputsWithinTransaction(t *testing.T) {
	p := makeTestParams()
	s := utxo.NewSet()

	var txHash1 types.Hash
	txHash1[0] = 0x01
	addUTXO(s, txHash1, 0, 1000, 0, false)

	block := &types.Block{
		Transactions: []types.Transaction{
			makeCoinbaseTx(5, p.CalcSubsidy(5)),
			{
				Version: 1,
				Inputs: []types.TxInput{
					{PreviousOutPoint: types.OutPoint{Hash: txHash1, Index: 0}, Sequence: 0xFFFFFFFF},
					{PreviousOutPoint: types.OutPoint{Hash: txHash1, Index: 0}, Sequence: 0xFFFFFFFF},
				},
				Outputs: []types.TxOutput{{Value: 500, PkScript: []byte{0x01}}},
			},
		},
	}

	_, err := ValidateTransactionInputs(block, s, 5, p)
	if err == nil {
		t.Fatal("expected rejection for duplicate inputs within a single transaction")
	}
}

func TestOverspendTransaction(t *testing.T) {
	p := makeTestParams()
	s := utxo.NewSet()

	var txHash1 types.Hash
	txHash1[0] = 0x01
	addUTXO(s, txHash1, 0, 1000, 0, false)

	block := &types.Block{
		Transactions: []types.Transaction{
			makeCoinbaseTx(5, p.CalcSubsidy(5)),
			{
				Version: 1,
				Inputs: []types.TxInput{
					{PreviousOutPoint: types.OutPoint{Hash: txHash1, Index: 0}, Sequence: 0xFFFFFFFF},
				},
				Outputs: []types.TxOutput{{Value: 9999, PkScript: []byte{0x01}}},
			},
		},
	}

	_, err := ValidateTransactionInputs(block, s, 5, p)
	if err == nil {
		t.Fatal("expected rejection for overspend (output > input)")
	}
}

func TestImmatureCoinbaseSpend(t *testing.T) {
	p := makeTestParams()
	p.CoinbaseMaturity = 10

	s := utxo.NewSet()

	var cbHash types.Hash
	cbHash[0] = 0xCB
	addUTXO(s, cbHash, 0, 5000000000, 5, true)

	block := &types.Block{
		Transactions: []types.Transaction{
			makeCoinbaseTx(8, p.CalcSubsidy(8)),
			{
				Version: 1,
				Inputs: []types.TxInput{
					{PreviousOutPoint: types.OutPoint{Hash: cbHash, Index: 0}, Sequence: 0xFFFFFFFF},
				},
				Outputs: []types.TxOutput{{Value: 1000, PkScript: []byte{0x01}}},
			},
		},
	}

	_, err := ValidateTransactionInputs(block, s, 8, p)
	if err == nil {
		t.Fatal("expected rejection for immature coinbase spend (height 5, spending at height 8, maturity 10)")
	}
}

func TestMatureCoinbaseSpendAccepted(t *testing.T) {
	p := makeTestParams()
	p.CoinbaseMaturity = 10

	s := utxo.NewSet()

	var cbHash types.Hash
	cbHash[0] = 0xCB
	addUTXO(s, cbHash, 0, 5000000000, 5, true)

	block := &types.Block{
		Transactions: []types.Transaction{
			makeCoinbaseTx(20, p.CalcSubsidy(20)),
			{
				Version: 1,
				Inputs: []types.TxInput{
					{PreviousOutPoint: types.OutPoint{Hash: cbHash, Index: 0}, Sequence: 0xFFFFFFFF},
				},
				Outputs: []types.TxOutput{{Value: 1000, PkScript: []byte{0x01}}},
			},
		},
	}

	_, err := ValidateTransactionInputs(block, s, 20, p)
	if err != nil {
		t.Fatalf("expected mature coinbase spend to be accepted: %v", err)
	}
}

func TestInvalidCoinbaseReward(t *testing.T) {
	p := makeTestParams()
	s := utxo.NewSet()

	subsidy := p.CalcSubsidy(5)

	block := &types.Block{
		Transactions: []types.Transaction{
			makeCoinbaseTx(5, subsidy+1),
		},
	}

	_, err := ValidateTransactionInputs(block, s, 5, p)
	if err == nil {
		t.Fatal("expected rejection for coinbase reward exceeding subsidy (no fees)")
	}
}

func TestCoinbaseRewardWithFees(t *testing.T) {
	p := makeTestParams()
	s := utxo.NewSet()

	var txHash1 types.Hash
	txHash1[0] = 0x01
	addUTXO(s, txHash1, 0, 1000, 0, false)

	subsidy := p.CalcSubsidy(5)
	fee := uint64(100)

	block := &types.Block{
		Transactions: []types.Transaction{
			makeCoinbaseTx(5, subsidy+fee),
			{
				Version: 1,
				Inputs: []types.TxInput{
					{PreviousOutPoint: types.OutPoint{Hash: txHash1, Index: 0}, Sequence: 0xFFFFFFFF},
				},
				Outputs: []types.TxOutput{{Value: 1000 - fee, PkScript: []byte{0x01}}},
			},
		},
	}

	_, err := ValidateTransactionInputs(block, s, 5, p)
	if err != nil {
		t.Fatalf("expected valid block with coinbase = subsidy + fees: %v", err)
	}
}

func TestCoinbaseExceedingSubsidyPlusFees(t *testing.T) {
	p := makeTestParams()
	s := utxo.NewSet()

	var txHash1 types.Hash
	txHash1[0] = 0x01
	addUTXO(s, txHash1, 0, 1000, 0, false)

	subsidy := p.CalcSubsidy(5)
	fee := uint64(100)

	block := &types.Block{
		Transactions: []types.Transaction{
			makeCoinbaseTx(5, subsidy+fee+1),
			{
				Version: 1,
				Inputs: []types.TxInput{
					{PreviousOutPoint: types.OutPoint{Hash: txHash1, Index: 0}, Sequence: 0xFFFFFFFF},
				},
				Outputs: []types.TxOutput{{Value: 1000 - fee, PkScript: []byte{0x01}}},
			},
		},
	}

	_, err := ValidateTransactionInputs(block, s, 5, p)
	if err == nil {
		t.Fatal("expected rejection for coinbase exceeding subsidy + fees")
	}
}

func TestNonexistentUTXOReference(t *testing.T) {
	p := makeTestParams()
	s := utxo.NewSet()

	var fakeTxHash types.Hash
	fakeTxHash[0] = 0xFF

	block := &types.Block{
		Transactions: []types.Transaction{
			makeCoinbaseTx(5, p.CalcSubsidy(5)),
			{
				Version: 1,
				Inputs: []types.TxInput{
					{PreviousOutPoint: types.OutPoint{Hash: fakeTxHash, Index: 0}, Sequence: 0xFFFFFFFF},
				},
				Outputs: []types.TxOutput{{Value: 100, PkScript: []byte{0x01}}},
			},
		},
	}

	_, err := ValidateTransactionInputs(block, s, 5, p)
	if err == nil {
		t.Fatal("expected rejection for reference to nonexistent UTXO")
	}
}

func TestBlockWithConflictingTransactions(t *testing.T) {
	p := makeTestParams()
	s := utxo.NewSet()

	var txHash1 types.Hash
	txHash1[0] = 0x01
	addUTXO(s, txHash1, 0, 1000, 0, false)

	var txHash2 types.Hash
	txHash2[0] = 0x02
	addUTXO(s, txHash2, 0, 2000, 0, false)

	block := &types.Block{
		Transactions: []types.Transaction{
			makeCoinbaseTx(5, p.CalcSubsidy(5)),
			{
				Version: 1,
				Inputs: []types.TxInput{
					{PreviousOutPoint: types.OutPoint{Hash: txHash1, Index: 0}, Sequence: 0xFFFFFFFF},
					{PreviousOutPoint: types.OutPoint{Hash: txHash2, Index: 0}, Sequence: 0xFFFFFFFF},
				},
				Outputs: []types.TxOutput{{Value: 2500, PkScript: []byte{0x01}}},
			},
			{
				Version: 1,
				Inputs: []types.TxInput{
					{PreviousOutPoint: types.OutPoint{Hash: txHash1, Index: 0}, Sequence: 0xFFFFFFFF},
				},
				Outputs: []types.TxOutput{{Value: 500, PkScript: []byte{0x02}}},
			},
		},
	}

	_, err := ValidateTransactionInputs(block, s, 5, p)
	if err == nil {
		t.Fatal("expected rejection for conflicting transactions (tx2 spends same UTXO as tx1)")
	}
}

func TestZeroValueOutputRejected(t *testing.T) {
	p := makeTestParams()
	s := utxo.NewSet()

	var txHash1 types.Hash
	txHash1[0] = 0x01
	addUTXO(s, txHash1, 0, 1000, 0, false)

	block := &types.Block{
		Transactions: []types.Transaction{
			makeCoinbaseTx(5, p.CalcSubsidy(5)),
			{
				Version: 1,
				Inputs: []types.TxInput{
					{PreviousOutPoint: types.OutPoint{Hash: txHash1, Index: 0}, Sequence: 0xFFFFFFFF},
				},
				Outputs: []types.TxOutput{
					{Value: 0, PkScript: []byte{0x01}},
				},
			},
		},
	}

	_, err := ValidateTransactionInputs(block, s, 5, p)
	if err == nil {
		t.Fatal("expected rejection for zero-value output")
	}
}

func TestZeroValueCoinbaseOutputRejected(t *testing.T) {
	p := makeTestParams()
	s := utxo.NewSet()

	heightBytes := make([]byte, 4)
	types.PutUint32LE(heightBytes, 5)
	block := &types.Block{
		Transactions: []types.Transaction{
			{
				Version: 1,
				Inputs: []types.TxInput{{
					PreviousOutPoint: types.CoinbaseOutPoint,
					SignatureScript:  append(heightBytes, []byte("test")...),
					Sequence:         0xFFFFFFFF,
				}},
				Outputs: []types.TxOutput{
					{Value: 0, PkScript: []byte{0x00}},
				},
			},
		},
	}

	_, err := ValidateTransactionInputs(block, s, 5, p)
	if err == nil {
		t.Fatal("expected rejection for zero-value coinbase output")
	}
}

func TestNoInputsNonCoinbaseRejected(t *testing.T) {
	p := makeTestParams()
	s := utxo.NewSet()

	block := &types.Block{
		Transactions: []types.Transaction{
			makeCoinbaseTx(5, p.CalcSubsidy(5)),
			{
				Version: 1,
				Inputs:  []types.TxInput{},
				Outputs: []types.TxOutput{{Value: 100, PkScript: []byte{0x01}}},
			},
		},
	}

	_, err := ValidateTransactionInputs(block, s, 5, p)
	if err == nil {
		t.Fatal("expected rejection for non-coinbase tx with no inputs")
	}
}

func TestNoOutputsNonCoinbaseRejected(t *testing.T) {
	p := makeTestParams()
	s := utxo.NewSet()

	var txHash1 types.Hash
	txHash1[0] = 0x01
	addUTXO(s, txHash1, 0, 1000, 0, false)

	block := &types.Block{
		Transactions: []types.Transaction{
			makeCoinbaseTx(5, p.CalcSubsidy(5)),
			{
				Version: 1,
				Inputs: []types.TxInput{
					{PreviousOutPoint: types.OutPoint{Hash: txHash1, Index: 0}, Sequence: 0xFFFFFFFF},
				},
				Outputs: []types.TxOutput{},
			},
		},
	}

	_, err := ValidateTransactionInputs(block, s, 5, p)
	if err == nil {
		t.Fatal("expected rejection for non-coinbase tx with no outputs")
	}
}

func TestValidBlockAccepted(t *testing.T) {
	p := makeTestParams()
	s := utxo.NewSet()

	var txHash1 types.Hash
	txHash1[0] = 0x01
	addUTXO(s, txHash1, 0, 5000, 0, false)

	subsidy := p.CalcSubsidy(5)
	fee := uint64(500)

	block := &types.Block{
		Transactions: []types.Transaction{
			makeCoinbaseTx(5, subsidy+fee),
			{
				Version: 1,
				Inputs: []types.TxInput{
					{PreviousOutPoint: types.OutPoint{Hash: txHash1, Index: 0}, Sequence: 0xFFFFFFFF},
				},
				Outputs: []types.TxOutput{
					{Value: 3000, PkScript: []byte{0x01}},
					{Value: 1500, PkScript: []byte{0x02}},
				},
			},
		},
	}

	fees, err := ValidateTransactionInputs(block, s, 5, p)
	if err != nil {
		t.Fatalf("expected valid block to be accepted: %v", err)
	}
	if fees != fee {
		t.Fatalf("expected fees=%d, got %d", fee, fees)
	}
}

func TestSingleTxDuplicateInputRejected(t *testing.T) {
	p := makeTestParams()
	s := utxo.NewSet()

	var txHash1 types.Hash
	txHash1[0] = 0x01
	addUTXO(s, txHash1, 0, 1000, 0, false)

	tx := &types.Transaction{
		Version: 1,
		Inputs: []types.TxInput{
			{PreviousOutPoint: types.OutPoint{Hash: txHash1, Index: 0}, Sequence: 0xFFFFFFFF},
			{PreviousOutPoint: types.OutPoint{Hash: txHash1, Index: 0}, Sequence: 0xFFFFFFFF},
		},
		Outputs: []types.TxOutput{{Value: 500, PkScript: []byte{0x01}}},
	}

	_, err := ValidateSingleTransaction(tx, s, 4, p)
	if err == nil {
		t.Fatal("expected mempool rejection for duplicate inputs in single tx")
	}
}

func TestSingleTxZeroValueOutputRejected(t *testing.T) {
	p := makeTestParams()
	s := utxo.NewSet()

	var txHash1 types.Hash
	txHash1[0] = 0x01
	addUTXO(s, txHash1, 0, 1000, 0, false)

	tx := &types.Transaction{
		Version: 1,
		Inputs: []types.TxInput{
			{PreviousOutPoint: types.OutPoint{Hash: txHash1, Index: 0}, Sequence: 0xFFFFFFFF},
		},
		Outputs: []types.TxOutput{{Value: 0, PkScript: []byte{0x01}}},
	}

	_, err := ValidateSingleTransaction(tx, s, 4, p)
	if err == nil {
		t.Fatal("expected mempool rejection for zero-value output")
	}
}

func TestSingleTxOverspendRejected(t *testing.T) {
	p := makeTestParams()
	s := utxo.NewSet()

	var txHash1 types.Hash
	txHash1[0] = 0x01
	addUTXO(s, txHash1, 0, 1000, 0, false)

	tx := &types.Transaction{
		Version: 1,
		Inputs: []types.TxInput{
			{PreviousOutPoint: types.OutPoint{Hash: txHash1, Index: 0}, Sequence: 0xFFFFFFFF},
		},
		Outputs: []types.TxOutput{{Value: 9999, PkScript: []byte{0x01}}},
	}

	_, err := ValidateSingleTransaction(tx, s, 4, p)
	if err == nil {
		t.Fatal("expected mempool rejection for overspend")
	}
}

func TestSingleTxMissingUTXORejected(t *testing.T) {
	p := makeTestParams()
	s := utxo.NewSet()

	var fakeTxHash types.Hash
	fakeTxHash[0] = 0xFF

	tx := &types.Transaction{
		Version: 1,
		Inputs: []types.TxInput{
			{PreviousOutPoint: types.OutPoint{Hash: fakeTxHash, Index: 0}, Sequence: 0xFFFFFFFF},
		},
		Outputs: []types.TxOutput{{Value: 100, PkScript: []byte{0x01}}},
	}

	_, err := ValidateSingleTransaction(tx, s, 4, p)
	if err == nil {
		t.Fatal("expected mempool rejection for missing UTXO")
	}
}

func TestSingleTxImmatureCoinbaseRejected(t *testing.T) {
	p := makeTestParams()
	p.CoinbaseMaturity = 10

	s := utxo.NewSet()

	var cbHash types.Hash
	cbHash[0] = 0xCB
	addUTXO(s, cbHash, 0, 5000000000, 5, true)

	tx := &types.Transaction{
		Version: 1,
		Inputs: []types.TxInput{
			{PreviousOutPoint: types.OutPoint{Hash: cbHash, Index: 0}, Sequence: 0xFFFFFFFF},
		},
		Outputs: []types.TxOutput{{Value: 1000, PkScript: []byte{0x01}}},
	}

	_, err := ValidateSingleTransaction(tx, s, 7, p)
	if err == nil {
		t.Fatal("expected mempool rejection for immature coinbase spend")
	}
}

func TestSingleTxValidAccepted(t *testing.T) {
	p := makeTestParams()
	s := utxo.NewSet()

	var txHash1 types.Hash
	txHash1[0] = 0x01
	addUTXO(s, txHash1, 0, 5000, 0, false)

	tx := &types.Transaction{
		Version: 1,
		Inputs: []types.TxInput{
			{PreviousOutPoint: types.OutPoint{Hash: txHash1, Index: 0}, Sequence: 0xFFFFFFFF},
		},
		Outputs: []types.TxOutput{
			{Value: 3000, PkScript: []byte{0x01}},
			{Value: 1500, PkScript: []byte{0x02}},
		},
	}

	fee, err := ValidateSingleTransaction(tx, s, 4, p)
	if err != nil {
		t.Fatalf("expected valid tx to be accepted: %v", err)
	}
	if fee != 500 {
		t.Fatalf("expected fee=500, got %d", fee)
	}
}
