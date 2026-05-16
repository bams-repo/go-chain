// Copyright (c) 2024-2026 The Fairchain Contributors
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

package wallet

import (
	"fmt"
	"sort"

	"github.com/bams-repo/fairchain/internal/coinparams"
	"github.com/bams-repo/fairchain/internal/crypto"
	"github.com/bams-repo/fairchain/internal/types"
	"github.com/bams-repo/fairchain/internal/utxo"
)

// ListTxWallet is the wallet surface required to build transaction history.
type ListTxWallet interface {
	IsOurScript(pkScript []byte) bool
	AddressVersion() byte
	FindUnspent(
		forEach func(fn func(txHash [32]byte, index uint32, value uint64, pkScript []byte, height uint32, isCoinbase bool)),
		tipHeight uint32,
	) []UnspentOutput
}

// MempoolTx is a mempool transaction plus its txid (consensus hash).
type MempoolTx struct {
	Hash types.Hash
	Tx   types.Transaction
}

// BlockByHeight loads the main-chain block at height (genesis is height 0).
type BlockByHeight func(height uint32) (*types.Block, types.Hash, error)

// PrevoutScriptLookup returns the pkScript for an outpoint in the current confirmed UTXO set, or nil.
// Used to classify mempool spends (same rule as spending a confirmed UTXO).
type PrevoutScriptLookup func(hash types.Hash, index uint32) []byte

func sortListTxResults(results []map[string]interface{}) {
	sort.Slice(results, func(i, j int) bool {
		hi := results[i]["blockheight"].(uint32)
		hj := results[j]["blockheight"].(uint32)
		if hi != hj {
			if hi == 0 {
				return true
			}
			if hj == 0 {
				return false
			}
			return hi > hj
		}
		return results[i]["vout"].(uint32) < results[j]["vout"].(uint32)
	})
}

func isLegacyScriptPlaceholder(pk []byte) bool {
	return len(pk) == 0 || (len(pk) == 1 && pk[0] == 0x00)
}

func externalSendAndDest(w ListTxWallet, tx *types.Transaction, addrVer byte) (sendTotal uint64, destAddr string) {
	for _, out := range tx.Outputs {
		if w.IsOurScript(out.PkScript) {
			continue
		}
		sendTotal += out.Value
		hashBytes := crypto.ExtractP2PKHHash(out.PkScript)
		if hashBytes != nil && destAddr == "" {
			var pkh [crypto.PubKeyHashSize]byte
			copy(pkh[:], hashBytes)
			destAddr = crypto.PubKeyHashToAddress(pkh, addrVer)
		}
	}
	return sendTotal, destAddr
}

type viewEntry struct {
	PkScript   []byte
	Value      uint64
	Height     uint32
	IsCoinbase bool
}

func applyGenesisToView(entries map[[36]byte]*viewEntry, block *types.Block) error {
	for txIdx := range block.Transactions {
		tx := &block.Transactions[txIdx]
		txHash, err := crypto.HashTransaction(tx)
		if err != nil {
			return fmt.Errorf("hash genesis tx %d: %w", txIdx, err)
		}
		for outIdx, out := range tx.Outputs {
			if isLegacyScriptPlaceholder(out.PkScript) {
				continue
			}
			key := utxo.OutpointKey(txHash, uint32(outIdx))
			entries[key] = &viewEntry{
				Value:      out.Value,
				PkScript:   out.PkScript,
				Height:     0,
				IsCoinbase: tx.IsCoinbase(),
			}
		}
	}
	return nil
}

// processBlockWithSends updates entries (UTXO view at start of block) to state after the block
// and returns list rows for confirmed sends from this block. Mirrors utxo.Set.ConnectBlock ordering.
func processBlockWithSends(
	entries map[[36]byte]*viewEntry,
	block *types.Block,
	height, tipHeight uint32,
	w ListTxWallet,
	addrVer byte,
	cbMaturity uint32,
) ([]map[string]interface{}, error) {
	type addEntry struct {
		key   [36]byte
		entry *viewEntry
	}
	var toRemove [][36]byte
	var toAdd []addEntry
	var sendRows []map[string]interface{}

	spentInBlock := make(map[[36]byte]struct{})
	createdInBlock := make(map[[36]byte]*viewEntry)

	for txIdx := range block.Transactions {
		tx := &block.Transactions[txIdx]
		txHash, err := crypto.HashTransaction(tx)
		if err != nil {
			return nil, fmt.Errorf("hash tx %d: %w", txIdx, err)
		}

		if !tx.IsCoinbase() {
			isSend := false
			for _, in := range tx.Inputs {
				key := utxo.OutpointKey(in.PreviousOutPoint.Hash, in.PreviousOutPoint.Index)
				var ent *viewEntry
				if e, ok := createdInBlock[key]; ok {
					ent = e
				} else if e, ok := entries[key]; ok {
					ent = e
				}
				if ent != nil && w.IsOurScript(ent.PkScript) {
					isSend = true
					break
				}
			}
			if isSend {
				sendTotal, destAddr := externalSendAndDest(w, tx, addrVer)
				if sendTotal > 0 {
					confs := tipHeight - height + 1
					sendRows = append(sendRows, map[string]interface{}{
						"txid":             txHash.ReverseString(),
						"vout":             uint32(0),
						"address":          destAddr,
						"category":         "send",
						"amount":           -float64(sendTotal) / float64(coinparams.CoinsPerBaseUnit),
						"confirmations":    confs,
						"blockheight":      height,
						"isCoinbase":       false,
						"maturityProgress": 1.0,
						"maturityTarget":   cbMaturity,
						"maturityStatus":   "verified",
					})
				}
			}
		}

		if !tx.IsCoinbase() {
			seenInTx := make(map[[36]byte]struct{}, len(tx.Inputs))
			for _, in := range tx.Inputs {
				key := utxo.OutpointKey(in.PreviousOutPoint.Hash, in.PreviousOutPoint.Index)

				if _, dup := seenInTx[key]; dup {
					return nil, fmt.Errorf("tx %s duplicate input", txHash.ReverseString())
				}
				seenInTx[key] = struct{}{}

				if _, already := spentInBlock[key]; already {
					return nil, fmt.Errorf("tx %s double-spend within block", txHash.ReverseString())
				}

				var entry *viewEntry
				if e, ok := createdInBlock[key]; ok {
					entry = e
				} else if e, ok := entries[key]; ok {
					entry = e
				}
				if entry == nil {
					return nil, fmt.Errorf("tx %s missing prevout %s:%d", txHash.ReverseString(), in.PreviousOutPoint.Hash.ReverseString(), in.PreviousOutPoint.Index)
				}

				spentInBlock[key] = struct{}{}
				toRemove = append(toRemove, key)
				delete(createdInBlock, key)
			}
		}

		for outIdx, out := range tx.Outputs {
			key := utxo.OutpointKey(txHash, uint32(outIdx))
			entry := &viewEntry{
				Value:      out.Value,
				PkScript:   out.PkScript,
				Height:     height,
				IsCoinbase: tx.IsCoinbase(),
			}
			createdInBlock[key] = entry
			toAdd = append(toAdd, addEntry{key: key, entry: entry})
		}
	}

	for _, key := range toRemove {
		delete(entries, key)
	}
	for _, a := range toAdd {
		if _, spent := spentInBlock[a.key]; !spent {
			entries[a.key] = a.entry
		}
	}

	return sendRows, nil
}

// appendConfirmedChainSends walks genesis..tip on the main chain, replays UTXO in memory,
// and appends one negative-amount "send" row per wallet spend (Bitcoin-style listtransactions).
func appendConfirmedChainSends(
	w ListTxWallet,
	tipHeight, cbMaturity uint32,
	addrVer byte,
	getBlock BlockByHeight,
	out *[]map[string]interface{},
) error {
	genBlock, _, err := getBlock(0)
	if err != nil {
		return fmt.Errorf("load genesis block: %w", err)
	}
	entries := make(map[[36]byte]*viewEntry)
	if err := applyGenesisToView(entries, genBlock); err != nil {
		return err
	}
	for h := uint32(1); h <= tipHeight; h++ {
		block, _, err := getBlock(h)
		if err != nil {
			return fmt.Errorf("load block at height %d: %w", h, err)
		}
		rows, err := processBlockWithSends(entries, block, h, tipHeight, w, addrVer, cbMaturity)
		if err != nil {
			return fmt.Errorf("height %d: %w", h, err)
		}
		*out = append(*out, rows...)
	}
	return nil
}

// BuildListTransactionEntries returns wallet history: UTXO-backed receives (and coinbase
// maturity), mempool receives and sends, and confirmed sends from a main-chain replay.
// Amounts are in display units (float). Rows are sorted newest block first, mempool before confirmed.
func BuildListTransactionEntries(
	w ListTxWallet,
	tipHeight uint32,
	coinbaseMaturity uint32,
	utxoIter func(func(txHash [32]byte, index uint32, value uint64, pkScript []byte, height uint32, isCoinbase bool)),
	prevoutScript PrevoutScriptLookup,
	mempoolTxs []MempoolTx,
	getBlock BlockByHeight,
) ([]map[string]interface{}, error) {
	if w == nil {
		return nil, fmt.Errorf("wallet is nil")
	}
	if getBlock == nil {
		return nil, fmt.Errorf("getBlock is nil")
	}
	if utxoIter == nil {
		return nil, fmt.Errorf("utxoIter is nil")
	}

	addrVer := w.AddressVersion()
	utxos := w.FindUnspent(utxoIter, tipHeight)
	results := make([]map[string]interface{}, 0, len(utxos)+len(mempoolTxs)+8)

	for _, u := range utxos {
		txHash := types.Hash(u.TxHash)
		category := "receive"
		maturityProgress := 1.0
		maturityStatus := "verified"
		if u.IsCoinbase {
			if u.Confirmations >= coinbaseMaturity {
				category = "generate"
			} else {
				category = "immature"
				maturityStatus = "unverified"
				if coinbaseMaturity > 0 {
					maturityProgress = float64(u.Confirmations) / float64(coinbaseMaturity)
				}
			}
		}
		results = append(results, map[string]interface{}{
			"txid":             txHash.ReverseString(),
			"vout":             u.Index,
			"address":          u.Address,
			"category":         category,
			"amount":           float64(u.Value) / float64(coinparams.CoinsPerBaseUnit),
			"confirmations":    u.Confirmations,
			"blockheight":      u.Height,
			"isCoinbase":       u.IsCoinbase,
			"maturityProgress": maturityProgress,
			"maturityTarget":   coinbaseMaturity,
			"maturityStatus":   maturityStatus,
		})
	}

	confirmedSendTx := make(map[string]struct{})
	if err := appendConfirmedChainSends(w, tipHeight, coinbaseMaturity, addrVer, getBlock, &results); err != nil {
		return nil, err
	}
	for _, row := range results {
		if row["category"] == "send" && row["blockheight"].(uint32) > 0 {
			if tid, ok := row["txid"].(string); ok {
				confirmedSendTx[tid] = struct{}{}
			}
		}
	}

	for i := range mempoolTxs {
		entry := &mempoolTxs[i]
		tx := &entry.Tx
		txHashReverse := entry.Hash.ReverseString()

		for outIdx, out := range tx.Outputs {
			if !w.IsOurScript(out.PkScript) {
				continue
			}
			hashBytes := crypto.ExtractP2PKHHash(out.PkScript)
			addr := ""
			if hashBytes != nil {
				var pkh [crypto.PubKeyHashSize]byte
				copy(pkh[:], hashBytes)
				addr = crypto.PubKeyHashToAddress(pkh, addrVer)
			}
			results = append(results, map[string]interface{}{
				"txid":             txHashReverse,
				"vout":             uint32(outIdx),
				"address":          addr,
				"category":         "receive",
				"amount":           float64(out.Value) / float64(coinparams.CoinsPerBaseUnit),
				"confirmations":    uint32(0),
				"blockheight":      uint32(0),
				"isCoinbase":       false,
				"maturityProgress": 0.0,
				"maturityTarget":   coinbaseMaturity,
				"maturityStatus":   "mempool",
			})
		}

		if _, confirmed := confirmedSendTx[txHashReverse]; confirmed {
			continue
		}
		isSend := false
		if prevoutScript != nil {
			for _, in := range tx.Inputs {
				pk := prevoutScript(in.PreviousOutPoint.Hash, in.PreviousOutPoint.Index)
				if pk != nil && w.IsOurScript(pk) {
					isSend = true
					break
				}
			}
		}
		if isSend {
			sendTotal, destAddr := externalSendAndDest(w, tx, addrVer)
			if sendTotal > 0 {
				results = append(results, map[string]interface{}{
					"txid":             txHashReverse,
					"vout":             uint32(0),
					"address":          destAddr,
					"category":         "send",
					"amount":           -float64(sendTotal) / float64(coinparams.CoinsPerBaseUnit),
					"confirmations":    uint32(0),
					"blockheight":      uint32(0),
					"isCoinbase":       false,
					"maturityProgress": 0.0,
					"maturityTarget":   coinbaseMaturity,
					"maturityStatus":   "mempool",
				})
			}
		}
	}

	sortListTxResults(results)
	return results, nil
}
