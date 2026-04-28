// Copyright (c) 2024-2026 The Fairchain Contributors
// Fairchain is an experiment in modularity, designed to improve on the work
// of Satoshi Nakamoto and to inspire more creative genius in the space.
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

package consensus

import (
	"bytes"
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
			// The disable flag is already checked above, so reaching here
			// means this input has an active relative locktime. Skipping
			// would silently bypass BIP68 enforcement for unconfirmed
			// parent outputs (CPFP chains in mempool). Fail-safe: reject.
			return fmt.Errorf("tx input %d: cannot verify relative locktime — UTXO not found", inIdx)
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
// medianTimePast is the BIP113 median-time-past of the parent chain, computed
// from the preceding 11 blocks. Callers must pre-compute this via
// consensus.CalcMedianTimePast and pass it in. This matches Bitcoin Core's
// GetMedianTimePast() usage for locktime enforcement.
//
// txHashes is an optional slice of pre-computed transaction hashes. When
// non-nil, it must have the same length as block.Transactions. Pass nil to
// have hashes computed on the fly.
//
// Returns the total fees collected by all non-coinbase transactions.
func ValidateTransactionInputs(block *types.Block, utxoSet *utxo.Set, height uint32, p *params.ChainParams, medianTimePast uint32, txHashes []types.Hash) (uint64, error) {
	medianTime := medianTimePast
	var totalFees uint64

	// Track outpoints spent within this block to detect intra-block double spends.
	spentInBlock := make(map[[36]byte]struct{})

	// Track outputs created by earlier transactions in this block so that
	// later transactions can spend them (intra-block transaction chaining).
	// Bitcoin Core maintains a CCoinsViewCache that layers block-local
	// changes over the persistent UTXO set; this map serves the same role.
	createdInBlock := make(map[[36]byte]*utxo.UtxoEntry)

	for txIdx := range block.Transactions {
		tx := &block.Transactions[txIdx]

		if tx.IsCoinbase() {
			for outIdx, out := range tx.Outputs {
				if out.Value == 0 {
					return 0, fmt.Errorf("coinbase output %d: zero value not allowed", outIdx)
				}
			}
			var cbHash types.Hash
			if txHashes != nil {
				cbHash = txHashes[txIdx]
			} else {
				var err error
				cbHash, err = crypto.HashTransaction(tx)
				if err != nil {
					return 0, fmt.Errorf("hash coinbase tx: %w", err)
				}
			}
			for outIdx, out := range tx.Outputs {
				key := utxo.OutpointKey(cbHash, uint32(outIdx))
				createdInBlock[key] = &utxo.UtxoEntry{
					Value:      out.Value,
					PkScript:   out.PkScript,
					Height:     height,
					IsCoinbase: true,
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

		var txHash types.Hash
		if txHashes != nil {
			txHash = txHashes[txIdx]
		} else {
			var err error
			txHash, err = crypto.HashTransaction(tx)
			if err != nil {
				return 0, fmt.Errorf("hash tx %d: %w", txIdx, err)
			}
		}

		// Detect duplicate inputs within this single transaction.
		seenInputs := make(map[[36]byte]struct{}, len(tx.Inputs))

		// Resolved entries for each input, used for script validation below.
		resolvedEntries := make([]*utxo.UtxoEntry, len(tx.Inputs))

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

			// Resolve from in-block outputs first, then the persistent UTXO set.
			var entry *utxo.UtxoEntry
			if e, ok := createdInBlock[opKey]; ok {
				entry = e
			} else {
				entry = utxoSet.Get(in.PreviousOutPoint.Hash, in.PreviousOutPoint.Index)
			}
			if entry == nil {
				return 0, fmt.Errorf("tx %s input %d: references missing UTXO %s:%d",
					txHash, inIdx, in.PreviousOutPoint.Hash, in.PreviousOutPoint.Index)
			}

			resolvedEntries[inIdx] = entry

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
			delete(createdInBlock, opKey)
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
		if totalOut > params.MaxMoneyValue {
			return 0, fmt.Errorf("tx %s: total output value %d exceeds max money %d", txHash, totalOut, params.MaxMoneyValue)
		}

		if totalIn < totalOut {
			return 0, fmt.Errorf("tx %s: input value %d < output value %d", txHash, totalIn, totalOut)
		}

		// Register this transaction's outputs for potential intra-block spending.
		for outIdx, out := range tx.Outputs {
			key := utxo.OutpointKey(txHash, uint32(outIdx))
			createdInBlock[key] = &utxo.UtxoEntry{
				Value:      out.Value,
				PkScript:   out.PkScript,
				Height:     height,
				IsCoinbase: false,
			}
		}

		// Script validation using the entries resolved during input validation.
		for inIdx, in := range tx.Inputs {
			entry := resolvedEntries[inIdx]
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

	// Premine: at the designated height the coinbase MUST include an extra
	// output paying exactly PremineAmount to PremineScript.
	if p.PremineHeight > 0 && height == p.PremineHeight && p.PremineAmount > 0 {
		maxCoinbase += p.PremineAmount
		if err := validatePremineOutput(block.Transactions[0], p); err != nil {
			return 0, err
		}
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
//
// supplementalUtxos provides additional UTXO entries not yet in the persistent
// set (e.g., outputs of unconfirmed mempool parents for CPFP). The persistent
// utxoSet is checked first; supplementalUtxos is consulted only as a fallback.
// Pass nil when no supplemental entries are needed.
func ValidateSingleTransaction(tx *types.Transaction, utxoSet *utxo.Set, tipHeight uint32, p *params.ChainParams, supplementalUtxos map[[36]byte]*utxo.UtxoEntry) (uint64, error) {
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

	// Resolved entries for script validation below.
	resolvedEntries := make([]*utxo.UtxoEntry, len(tx.Inputs))

	var totalIn uint64
	for inIdx, in := range tx.Inputs {
		opKey := utxo.OutpointKey(in.PreviousOutPoint.Hash, in.PreviousOutPoint.Index)
		if _, dup := seenInputs[opKey]; dup {
			return 0, fmt.Errorf("tx %s input %d: duplicate input (outpoint %s:%d)",
				txHash, inIdx, in.PreviousOutPoint.Hash, in.PreviousOutPoint.Index)
		}
		seenInputs[opKey] = struct{}{}

		entry := utxoSet.Get(in.PreviousOutPoint.Hash, in.PreviousOutPoint.Index)
		if entry == nil && supplementalUtxos != nil {
			entry = supplementalUtxos[opKey]
		}
		if entry == nil {
			return 0, fmt.Errorf("tx %s input %d: references missing UTXO %s:%d",
				txHash, inIdx, in.PreviousOutPoint.Hash, in.PreviousOutPoint.Index)
		}

		resolvedEntries[inIdx] = entry

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
	if totalOut > params.MaxMoneyValue {
		return 0, fmt.Errorf("tx %s: total output value %d exceeds max money %d", txHash, totalOut, params.MaxMoneyValue)
	}

	if totalIn < totalOut {
		return 0, fmt.Errorf("tx %s: input value %d < output value %d", txHash, totalIn, totalOut)
	}

	// Script validation for mempool admission using previously resolved entries.
	for inIdx, in := range tx.Inputs {
		entry := resolvedEntries[inIdx]
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
// Returns an error if any input references a missing UTXO or on overflow.
// Returns 0 fee for coinbase transactions.
func CalcTxFee(tx *types.Transaction, utxoSet *utxo.Set) (uint64, error) {
	if tx.IsCoinbase() {
		return 0, nil
	}
	var totalIn uint64
	for inIdx, in := range tx.Inputs {
		entry := utxoSet.Get(in.PreviousOutPoint.Hash, in.PreviousOutPoint.Index)
		if entry == nil {
			return 0, fmt.Errorf("input %d: references missing UTXO %s:%d",
				inIdx, in.PreviousOutPoint.Hash, in.PreviousOutPoint.Index)
		}
		prev := totalIn
		totalIn += entry.Value
		if totalIn < prev {
			return 0, fmt.Errorf("input value overflow at input %d", inIdx)
		}
	}
	var totalOut uint64
	for outIdx, out := range tx.Outputs {
		prev := totalOut
		totalOut += out.Value
		if totalOut < prev {
			return 0, fmt.Errorf("output value overflow at output %d", outIdx)
		}
	}
	if totalIn < totalOut {
		return 0, fmt.Errorf("input value %d < output value %d", totalIn, totalOut)
	}
	return totalIn - totalOut, nil
}

// validatePremineOutput ensures the coinbase contains an output that pays
// exactly PremineAmount to PremineScript. Called only at PremineHeight.
func validatePremineOutput(cb types.Transaction, p *params.ChainParams) error {
	for _, out := range cb.Outputs {
		if out.Value == p.PremineAmount && bytes.Equal(out.PkScript, p.PremineScript) {
			return nil
		}
	}
	return fmt.Errorf("coinbase at premine height %d missing required premine output "+
		"(amount=%d)", p.PremineHeight, p.PremineAmount)
}
