// Copyright (c) 2024-2026 The Fairchain Contributors
// Fairchain is an experiment in modularity, designed to improve on the work
// of Satoshi Nakamoto and to inspire more creative genius in the space.
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

package main

import (
	"encoding/json"
	"fmt"
	"math/big"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/bams-repo/fairchain/internal/coinparams"
	"github.com/bams-repo/fairchain/internal/crypto"
	"github.com/bams-repo/fairchain/internal/types"
)

var (
	reDigits = regexp.MustCompile(`^\d+$`)
	reHex64  = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

func explorerCompactToDifficulty(bits, initialBits uint32) float64 {
	target := crypto.CompactToBig(bits)
	if target.Sign() <= 0 {
		return 0
	}
	genesisTarget := crypto.CompactToBig(initialBits)
	fDiff := new(big.Float).SetInt(genesisTarget)
	fDiff.Quo(fDiff, new(big.Float).SetInt(target))
	diff, _ := fDiff.Float64()
	return diff
}

func (a *App) blockSummaryMap(block *types.Block) map[string]interface{} {
	bc := a.node.Chain()
	blockHash := crypto.HashBlockHeader(&block.Header)
	_, tipHeight := bc.Tip()
	confirmations := int64(-1)
	blockHeight, heightErr := bc.GetBlockHeight(blockHash)
	if heightErr == nil {
		confirmations = int64(tipHeight) - int64(blockHeight) + 1
	}
	blockSize := 0
	if blockBytes, serErr := block.SerializeToBytes(); serErr == nil {
		blockSize = len(blockBytes)
	}
	txids := make([]string, len(block.Transactions))
	for i, tx := range block.Transactions {
		txHash, _ := crypto.HashTransaction(&tx)
		txids[i] = txHash.ReverseString()
	}
	initialBits := a.node.Params().InitialBits
	resp := map[string]interface{}{
		"hash":              blockHash.ReverseString(),
		"confirmations":     confirmations,
		"size":              blockSize,
		"weight":            blockSize * 4,
		"height":            blockHeight,
		"version":           block.Header.Version,
		"merkleroot":        block.Header.MerkleRoot.ReverseString(),
		"tx":                txids,
		"time":              block.Header.Timestamp,
		"nonce":             block.Header.Nonce,
		"bits":              fmt.Sprintf("%08x", block.Header.Bits),
		"difficulty":        explorerCompactToDifficulty(block.Header.Bits, initialBits),
		"previousblockhash": block.Header.PrevBlock.ReverseString(),
		"nTx":               len(block.Transactions),
	}
	if heightErr == nil && blockHeight < tipHeight {
		nextHeader, nextErr := bc.GetHeaderByHeight(blockHeight + 1)
		if nextErr == nil {
			nextHash := crypto.HashBlockHeader(nextHeader)
			resp["nextblockhash"] = nextHash.ReverseString()
		}
	}
	return resp
}

// ExplorerChainOverview returns tip, chain, and mempool stats for the explorer dashboard.
func (a *App) ExplorerChainOverview() (map[string]interface{}, error) {
	if a.node == nil {
		return nil, fmt.Errorf("node not initialized")
	}
	bc := a.node.Chain()
	info := bc.GetChainInfo()
	entries := a.node.Mempool().GetAllEntries()
	var mempoolBytes int
	for _, e := range entries {
		mempoolBytes += e.Size
	}
	return map[string]interface{}{
		"network":           info.Network,
		"height":            info.Height,
		"bestblockhash":     info.BestHash.ReverseString(),
		"genesisblockhash":  info.GenesisHash.ReverseString(),
		"difficulty":        info.Difficulty,
		"bits":              fmt.Sprintf("%08x", info.Bits),
		"chainwork":         fmt.Sprintf("%064x", info.Chainwork),
		"mediantime":        info.MedianTimePast,
		"retarget_epoch":    info.RetargetEpoch,
		"epoch_progress":    info.EpochProgress,
		"epoch_blocks_left": info.EpochBlocksLeft,
		"retarget_interval": info.RetargetInterval,
		"verificationprogress": info.VerificationProg,
		"mempool_tx":        len(entries),
		"mempool_bytes":     mempoolBytes,
		"display_ticker":    coinparams.Ticker,
		"display_decimals":  coinparams.Decimals,
	}, nil
}

// ExplorerRecentBlocks returns the last blocks on the active chain (newest first).
func (a *App) ExplorerRecentBlocks(limit int) ([]map[string]interface{}, error) {
	if a.node == nil {
		return nil, fmt.Errorf("node not initialized")
	}
	if limit < 1 {
		limit = 1
	}
	if limit > 200 {
		limit = 200
	}
	bc := a.node.Chain()
	_, tip := bc.Tip()
	out := make([]map[string]interface{}, 0, limit)
	for h := tip; len(out) < limit; {
		block, _, err := bc.GetBlockByHeight(h)
		if err != nil {
			break
		}
		out = append(out, a.blockSummaryMap(block))
		if h == 0 {
			break
		}
		h--
	}
	return out, nil
}

// ExplorerGetBlock loads a block by decimal height or by reverse-hex block hash.
func (a *App) ExplorerGetBlock(query string) (map[string]interface{}, error) {
	if a.node == nil {
		return nil, fmt.Errorf("node not initialized")
	}
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, fmt.Errorf("empty query")
	}
	bc := a.node.Chain()
	if reDigits.MatchString(q) {
		h64, err := strconv.ParseUint(q, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("invalid height: %w", err)
		}
		block, _, err := bc.GetBlockByHeight(uint32(h64))
		if err != nil {
			return nil, err
		}
		return a.blockSummaryMap(block), nil
	}
	hash, err := types.HashFromReverseHex(q)
	if err != nil {
		return nil, fmt.Errorf("invalid block hash: %w", err)
	}
	block, err := bc.GetBlock(hash)
	if err != nil {
		return nil, err
	}
	return a.blockSummaryMap(block), nil
}

// ExplorerGetTransaction returns a verbose transaction (same shape as getrawtransaction verbose).
func (a *App) ExplorerGetTransaction(txid string) (map[string]interface{}, error) {
	if a.node == nil {
		return nil, fmt.Errorf("node not initialized")
	}
	rpcSrv := a.node.RPCServer()
	if rpcSrv == nil {
		return nil, fmt.Errorf("RPC server not running")
	}
	t := strings.TrimSpace(txid)
	if t == "" {
		return nil, fmt.Errorf("empty txid")
	}
	txidRaw, err := json.Marshal(t)
	if err != nil {
		return nil, err
	}
	params := []json.RawMessage{txidRaw, json.RawMessage(`true`)}
	res, rpcErr := rpcSrv.DispatchRPC("getrawtransaction", params)
	if rpcErr != nil {
		return nil, fmt.Errorf("%s", rpcErr.Message)
	}
	m, ok := res.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("unexpected RPC result type")
	}
	return m, nil
}

// ExplorerMempoolSlice returns mempool transactions sorted by fee rate (highest first).
func (a *App) ExplorerMempoolSlice(limit int) ([]map[string]interface{}, error) {
	if a.node == nil {
		return nil, fmt.Errorf("node not initialized")
	}
	if limit < 1 {
		limit = 1
	}
	if limit > 500 {
		limit = 500
	}
	entries := a.node.Mempool().GetAllEntries()
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].FeeRate != entries[j].FeeRate {
			return entries[i].FeeRate > entries[j].FeeRate
		}
		return entries[i].AddedAt.After(entries[j].AddedAt)
	})
	n := limit
	if len(entries) < n {
		n = len(entries)
	}
	out := make([]map[string]interface{}, 0, n)
	for i := 0; i < n; i++ {
		e := entries[i]
		out = append(out, map[string]interface{}{
			"txid":    e.Hash.ReverseString(),
			"fee":     e.Fee,
			"size":    e.Size,
			"feerate": e.FeeRate,
			"time":    e.AddedAt.Unix(),
			"vouts":   len(e.Tx.Outputs),
			"vins":    len(e.Tx.Inputs),
		})
	}
	return out, nil
}

// ExplorerSearch resolves a height, block hash, or txid string.
func (a *App) ExplorerSearch(query string) (map[string]interface{}, error) {
	if a.node == nil {
		return nil, fmt.Errorf("node not initialized")
	}
	q := strings.TrimSpace(strings.ToLower(query))
	if q == "" {
		return nil, fmt.Errorf("empty search")
	}
	if reDigits.MatchString(q) {
		block, err := a.ExplorerGetBlock(q)
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{"kind": "block", "block": block}, nil
	}
	if len(q) == 64 && reHex64.MatchString(q) {
		if block, err := a.ExplorerGetBlock(q); err == nil {
			return map[string]interface{}{"kind": "block", "block": block}, nil
		}
		tx, err := a.ExplorerGetTransaction(q)
		if err != nil {
			return nil, fmt.Errorf("no block or transaction found for %q", query)
		}
		return map[string]interface{}{"kind": "transaction", "transaction": tx}, nil
	}
	return nil, fmt.Errorf("enter a block height, 64-character hex hash, or txid")
}
