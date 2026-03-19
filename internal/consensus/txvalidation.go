package consensus

import (
	"fmt"
	"time"

	"github.com/bams-repo/fairchain/internal/crypto"
	"github.com/bams-repo/fairchain/internal/params"
	"github.com/bams-repo/fairchain/internal/script"
	"github.com/bams-repo/fairchain/internal/types"
	"github.com/bams-repo/fairchain/internal/utxo"
)

// BIP68 constants for interpreting nSequence values as relative timelocks.
const (
	SequenceLockTimeDisableFlag = 1 << 31 // If set, nSequence is not interpreted as a relative locktime.
	SequenceLockTimeTypeFlag    = 1 << 22 // If set, relative locktime is in units of 512 seconds; otherwise blocks.
	SequenceLockTimeMask        = 0x0000ffff
	SequenceLockTimeGranularity = 9 // 2^9 = 512 seconds per unit for time-based relative locks.
)

// CheckTransactionFinality implements Bitcoin Core's IsFinalTx: a transaction is
// final if its LockTime is 0, or LockTime < blockHeight (or blockTime if
// LockTime >= 500_000_000), or all input sequences are 0xFFFFFFFF.
func CheckTransactionFinality(tx *types.Transaction, blockHeight uint32, blockMedianTime uint32) error {
	if tx.LockTime == 0 {
		return nil
	}

	// All sequences == 0xFFFFFFFF makes the tx final regardless of LockTime.
	allFinal := true
	for _, in := range tx.Inputs {
		if in.Sequence != 0xFFFFFFFF {
			allFinal = false
			break
		}
	}
	if allFinal {
		return nil
	}

	// LockTime >= 500_000_000 is interpreted as a Unix timestamp;
	// otherwise it's a block height. This matches Bitcoin Core's threshold.
	const lockTimeThreshold = 500_000_000

	if tx.LockTime < lockTimeThreshold {
		if tx.LockTime >= uint32(blockHeight) {
			return fmt.Errorf("transaction locktime %d not satisfied (block height %d)", tx.LockTime, blockHeight)
		}
	} else {
		if tx.LockTime >= blockMedianTime {
			return fmt.Errorf("transaction locktime %d not satisfied (median time %d)", tx.LockTime, blockMedianTime)
		}
	}

	return nil
}

// CheckSequenceLocks implements BIP68 relative locktime enforcement. For each
// input with Sequence != 0xFFFFFFFF and without the disable flag set, the
// input's UTXO must be buried by at least the specified number of blocks
// (or 512-second intervals, if the time flag is set).
func CheckSequenceLocks(tx *types.Transaction, blockHeight uint32, blockMedianTime uint32, utxoSet *utxo.Set) error {
	for inIdx, in := range tx.Inputs {
		if in.Sequence&SequenceLockTimeDisableFlag != 0 {
			continue
		}
		if in.PreviousOutPoint == types.CoinbaseOutPoint {
			continue
		}

		entry := utxoSet.Get(in.PreviousOutPoint.Hash, in.PreviousOutPoint.Index)
		if entry == nil {
			continue // UTXO existence is validated elsewhere.
		}

		if in.Sequence&SequenceLockTimeTypeFlag != 0 {
			return fmt.Errorf("tx input %d: time-based relative locktimes (BIP68) are not yet supported", inIdx)
		} else {
			// Block-based relative lock: the UTXO must have at least
			// (sequence & mask) confirmations.
			requiredBlocks := in.Sequence & SequenceLockTimeMask
			if blockHeight < entry.Height {
				return fmt.Errorf("tx input %d: block height %d < UTXO height %d", inIdx, blockHeight, entry.Height)
			}
			confirmations := blockHeight - entry.Height
			if confirmations < uint32(requiredBlocks) {
				return fmt.Errorf("tx input %d: relative locktime requires %d confirmations, have %d (UTXO at height %d, block height %d)",
					inIdx, requiredBlocks, confirmations, entry.Height, blockHeight)
			}
		}
	}
	return nil
}

// ValidateTransactionInputs checks that every non-coinbase transaction in a block
// has valid inputs against the UTXO set:
//   - each non-coinbase tx must have at least one input and one output
//   - each input references an existing, unspent output
//   - no duplicate inputs within a single transaction
//   - no duplicate spends across transactions within the block
//   - coinbase maturity is enforced
//   - no zero-value outputs
//   - input value accumulation checked for overflow
//   - total input value >= total output value (no value creation)
//   - coinbase value <= subsidy + total fees (with overflow protection)
//
// Returns the total fees collected by all non-coinbase transactions.
func ValidateTransactionInputs(block *types.Block, utxoSet *utxo.Set, height uint32, p *params.ChainParams) (uint64, error) {
	// Use block timestamp as median time proxy for locktime enforcement.
	// Bitcoin Core uses median-time-past (BIP113); block timestamp is a safe
	// upper bound since blocks must have timestamp > MTP.
	medianTime := block.Header.Timestamp
	var totalFees uint64

	// Track outpoints spent within this block to detect intra-block double spends.
	spentInBlock := make(map[[36]byte]struct{})

	for txIdx := range block.Transactions {
		tx := &block.Transactions[txIdx]

		if tx.IsCoinbase() {
			for outIdx, out := range tx.Outputs {
				if out.Value == 0 {
					return 0, fmt.Errorf("coinbase output %d: zero value not allowed", outIdx)
				}
			}
			continue
		}

		if len(tx.Inputs) == 0 {
			return 0, fmt.Errorf("tx %d: non-coinbase transaction has no inputs", txIdx)
		}
		if len(tx.Outputs) == 0 {
			return 0, fmt.Errorf("tx %d: transaction has no outputs", txIdx)
		}

		if txSize := tx.SerializeSize(); txSize > params.MaxTxSize {
			return 0, fmt.Errorf("tx %d: serialized size %d exceeds maximum %d bytes", txIdx, txSize, params.MaxTxSize)
		}

		txHash, err := crypto.HashTransaction(tx)
		if err != nil {
			return 0, fmt.Errorf("hash tx %d: %w", txIdx, err)
		}

		// Detect duplicate inputs within this single transaction.
		seenInputs := make(map[[36]byte]struct{}, len(tx.Inputs))

		var totalIn uint64
		for inIdx, in := range tx.Inputs {
			opKey := utxo.OutpointKey(in.PreviousOutPoint.Hash, in.PreviousOutPoint.Index)

			if _, dup := seenInputs[opKey]; dup {
				return 0, fmt.Errorf("tx %s input %d: duplicate input within transaction (outpoint %s:%d)",
					txHash, inIdx, in.PreviousOutPoint.Hash, in.PreviousOutPoint.Index)
			}
			seenInputs[opKey] = struct{}{}

			if _, alreadySpent := spentInBlock[opKey]; alreadySpent {
				return 0, fmt.Errorf("tx %s input %d: double-spend within block (outpoint %s:%d)",
					txHash, inIdx, in.PreviousOutPoint.Hash, in.PreviousOutPoint.Index)
			}

			entry := utxoSet.Get(in.PreviousOutPoint.Hash, in.PreviousOutPoint.Index)
			if entry == nil {
				return 0, fmt.Errorf("tx %s input %d: references missing UTXO %s:%d",
					txHash, inIdx, in.PreviousOutPoint.Hash, in.PreviousOutPoint.Index)
			}

			if entry.IsCoinbase {
				if height < entry.Height {
					return 0, fmt.Errorf("tx %s input %d: coinbase at height %d cannot be spent at height %d",
						txHash, inIdx, entry.Height, height)
				}
				maturityDepth := height - entry.Height
				if maturityDepth < p.CoinbaseMaturity {
					return 0, fmt.Errorf("tx %s input %d: coinbase output at height %d not mature (need %d confirmations, have %d)",
						txHash, inIdx, entry.Height, p.CoinbaseMaturity, maturityDepth)
				}
			}

			if totalIn+entry.Value < totalIn {
				return 0, fmt.Errorf("tx %s: input value overflow at input %d", txHash, inIdx)
			}
			if entry.Value > params.MaxMoneyValue {
				return 0, fmt.Errorf("tx %s input %d: input value %d exceeds max money %d", txHash, inIdx, entry.Value, params.MaxMoneyValue)
			}
			totalIn += entry.Value
			if totalIn > params.MaxMoneyValue {
				return 0, fmt.Errorf("tx %s: cumulative input value %d exceeds max money %d", txHash, totalIn, params.MaxMoneyValue)
			}
			spentInBlock[opKey] = struct{}{}
		}

		var totalOut uint64
		for outIdx, out := range tx.Outputs {
			if out.Value == 0 {
				return 0, fmt.Errorf("tx %s output %d: zero value not allowed", txHash, outIdx)
			}
			if out.Value > params.MaxMoneyValue {
				return 0, fmt.Errorf("tx %s output %d: value %d exceeds max money %d", txHash, outIdx, out.Value, params.MaxMoneyValue)
			}
			if totalOut+out.Value < totalOut {
				return 0, fmt.Errorf("tx %s output %d: value overflow", txHash, outIdx)
			}
			totalOut += out.Value
		}

		if totalIn < totalOut {
			return 0, fmt.Errorf("tx %s: input value %d < output value %d", txHash, totalIn, totalOut)
		}

	// Script validation: verify each input's SignatureScript satisfies the
	// referenced UTXO's PkScript. This is the spend authorization check.
	for inIdx, in := range tx.Inputs {
		entry := utxoSet.Get(in.PreviousOutPoint.Hash, in.PreviousOutPoint.Index)
		if entry == nil {
			continue // already validated above
		}
		if script.IsLegacyUnvalidatedScript(entry.PkScript) {
			continue
		}
		if err := script.Verify(in.SignatureScript, entry.PkScript, tx, inIdx); err != nil {
			return 0, fmt.Errorf("tx %s input %d: script validation failed: %w", txHash, inIdx, err)
		}
	}

	// LockTime and BIP68 relative locktime enforcement, gated by activation height.
	if locktimeHeight, ok := p.ActivationHeights["locktime"]; ok && height >= locktimeHeight {
		if err := CheckTransactionFinality(tx, height, medianTime); err != nil {
			return 0, fmt.Errorf("tx %s: %w", txHash, err)
		}
		if err := CheckSequenceLocks(tx, height, medianTime, utxoSet); err != nil {
			return 0, fmt.Errorf("tx %s: %w", txHash, err)
		}
	}

	fee := totalIn - totalOut
		if totalFees+fee < totalFees {
			return 0, fmt.Errorf("total fees overflow at tx %d", txIdx)
		}
		totalFees += fee
	}

	subsidy := p.CalcSubsidy(height)
	maxCoinbase := subsidy + totalFees
	if maxCoinbase < subsidy {
		return 0, fmt.Errorf("subsidy + fees overflow (subsidy=%d, fees=%d)", subsidy, totalFees)
	}
	var coinbaseValue uint64
	for outIdx, out := range block.Transactions[0].Outputs {
		if coinbaseValue+out.Value < coinbaseValue {
			return 0, fmt.Errorf("coinbase output %d: value accumulation overflow", outIdx)
		}
		coinbaseValue += out.Value
	}
	if coinbaseValue > maxCoinbase {
		return 0, fmt.Errorf("coinbase value %d exceeds subsidy+fees %d (subsidy=%d, fees=%d)",
			coinbaseValue, maxCoinbase, subsidy, totalFees)
	}

	return totalFees, nil
}

// ValidateSingleTransaction checks a single non-coinbase transaction against the UTXO set.
// Used for mempool admission. Returns the fee if valid.
func ValidateSingleTransaction(tx *types.Transaction, utxoSet *utxo.Set, tipHeight uint32, p *params.ChainParams) (uint64, error) {
	if tx.IsCoinbase() {
		return 0, fmt.Errorf("coinbase transactions cannot be validated individually")
	}

	if len(tx.Inputs) == 0 {
		return 0, fmt.Errorf("transaction has no inputs")
	}
	if len(tx.Outputs) == 0 {
		return 0, fmt.Errorf("transaction has no outputs")
	}

	if txSize := tx.SerializeSize(); txSize > params.MaxTxSize {
		return 0, fmt.Errorf("transaction size %d exceeds maximum %d bytes", txSize, params.MaxTxSize)
	}

	txHash, err := crypto.HashTransaction(tx)
	if err != nil {
		return 0, fmt.Errorf("hash transaction: %w", err)
	}

	// Detect duplicate inputs within this transaction.
	seenInputs := make(map[[36]byte]struct{}, len(tx.Inputs))

	spendHeight := tipHeight + 1

	var totalIn uint64
	for inIdx, in := range tx.Inputs {
		opKey := utxo.OutpointKey(in.PreviousOutPoint.Hash, in.PreviousOutPoint.Index)
		if _, dup := seenInputs[opKey]; dup {
			return 0, fmt.Errorf("tx %s input %d: duplicate input (outpoint %s:%d)",
				txHash, inIdx, in.PreviousOutPoint.Hash, in.PreviousOutPoint.Index)
		}
		seenInputs[opKey] = struct{}{}

		entry := utxoSet.Get(in.PreviousOutPoint.Hash, in.PreviousOutPoint.Index)
		if entry == nil {
			return 0, fmt.Errorf("tx %s input %d: references missing UTXO %s:%d",
				txHash, inIdx, in.PreviousOutPoint.Hash, in.PreviousOutPoint.Index)
		}

		if entry.IsCoinbase {
			if spendHeight < entry.Height {
				return 0, fmt.Errorf("tx %s input %d: coinbase at height %d cannot be spent at height %d",
					txHash, inIdx, entry.Height, spendHeight)
			}
			maturityDepth := spendHeight - entry.Height
			if maturityDepth < p.CoinbaseMaturity {
				return 0, fmt.Errorf("tx %s input %d: coinbase output at height %d not mature (need %d, have %d)",
					txHash, inIdx, entry.Height, p.CoinbaseMaturity, maturityDepth)
			}
		}

		if totalIn+entry.Value < totalIn {
			return 0, fmt.Errorf("tx %s: input value overflow at input %d", txHash, inIdx)
		}
		if entry.Value > params.MaxMoneyValue {
			return 0, fmt.Errorf("tx %s input %d: input value %d exceeds max money %d", txHash, inIdx, entry.Value, params.MaxMoneyValue)
		}
		totalIn += entry.Value
		if totalIn > params.MaxMoneyValue {
			return 0, fmt.Errorf("tx %s: cumulative input value %d exceeds max money %d", txHash, totalIn, params.MaxMoneyValue)
		}
	}

	var totalOut uint64
	for outIdx, out := range tx.Outputs {
		if out.Value == 0 {
			return 0, fmt.Errorf("tx %s output %d: zero value not allowed", txHash, outIdx)
		}
		if out.Value > params.MaxMoneyValue {
			return 0, fmt.Errorf("tx %s output %d: value %d exceeds max money %d", txHash, outIdx, out.Value, params.MaxMoneyValue)
		}
		if totalOut+out.Value < totalOut {
			return 0, fmt.Errorf("tx %s output %d: value overflow", txHash, outIdx)
		}
		totalOut += out.Value
	}

	if totalIn < totalOut {
		return 0, fmt.Errorf("tx %s: input value %d < output value %d", txHash, totalIn, totalOut)
	}

	// Script validation for mempool admission.
	for inIdx, in := range tx.Inputs {
		entry := utxoSet.Get(in.PreviousOutPoint.Hash, in.PreviousOutPoint.Index)
		if entry == nil {
			continue
		}
		if script.IsLegacyUnvalidatedScript(entry.PkScript) {
			continue
		}
		if err := script.Verify(in.SignatureScript, entry.PkScript, tx, inIdx); err != nil {
			return 0, fmt.Errorf("tx %s input %d: script validation failed: %w", txHash, inIdx, err)
		}
	}

	// LockTime and BIP68 relative locktime enforcement for mempool admission.
	if locktimeHeight, ok := p.ActivationHeights["locktime"]; ok && spendHeight >= locktimeHeight {
		// Use current time as a conservative proxy for the next block's median time.
		mempoolMedianTime := uint32(time.Now().Unix())
		if err := CheckTransactionFinality(tx, spendHeight, mempoolMedianTime); err != nil {
			return 0, fmt.Errorf("tx %s: %w", txHash, err)
		}
		if err := CheckSequenceLocks(tx, spendHeight, mempoolMedianTime, utxoSet); err != nil {
			return 0, fmt.Errorf("tx %s: %w", txHash, err)
		}
	}

	fee := totalIn - totalOut
	return fee, nil
}

// CalcTxFee computes the fee for a transaction given the UTXO set.
// Returns 0 for coinbase transactions or on overflow.
func CalcTxFee(tx *types.Transaction, utxoSet *utxo.Set) uint64 {
	if tx.IsCoinbase() {
		return 0
	}
	var totalIn uint64
	for _, in := range tx.Inputs {
		entry := utxoSet.Get(in.PreviousOutPoint.Hash, in.PreviousOutPoint.Index)
		if entry != nil {
			prev := totalIn
			totalIn += entry.Value
			if totalIn < prev {
				return 0
			}
		}
	}
	var totalOut uint64
	for _, out := range tx.Outputs {
		prev := totalOut
		totalOut += out.Value
		if totalOut < prev {
			return 0
		}
	}
	if totalIn <= totalOut {
		return 0
	}
	return totalIn - totalOut
}
