// Copyright (c) 2024-2026 The Fairchain Contributors
// Fairchain is an experiment in modularity, designed to improve on the work
// of Satoshi Nakamoto and to inspire more creative genius in the space.
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

package miner

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bams-repo/fairchain/internal/chain"
	"github.com/bams-repo/fairchain/internal/coinparams"
	"github.com/bams-repo/fairchain/internal/consensus"
	"github.com/bams-repo/fairchain/internal/crypto"
	"github.com/bams-repo/fairchain/internal/logging"
	"github.com/bams-repo/fairchain/internal/mempool"
	"github.com/bams-repo/fairchain/internal/params"
	"github.com/bams-repo/fairchain/internal/types"
)

// TimeSource provides network-adjusted time for block timestamp construction.
type TimeSource interface {
	Now() int64
}

type localClock struct{}

func (localClock) Now() int64 { return time.Now().Unix() }

// countedSealer is implemented by engines that report actual hash counts.
type countedSealer interface {
	SealHeaderCounted(header *types.BlockHeader, target types.Hash, maxIterations uint64) (found bool, hashes uint64, err error)
}

// Miner builds block templates and searches for valid PoW solutions.
type Miner struct {
	chain        *chain.Chain
	engine       consensus.Engine
	mempool      *mempool.Mempool
	params       *params.ChainParams
	rewardScript []byte
	timeSource   TimeSource
	onBlock      func(*types.Block)
	workers      int

	hashCount     atomic.Uint64
	hashrate      atomic.Uint64
	hashrateReady atomic.Bool

	ewmaMu        sync.Mutex
	ewmaRate      float64   // EWMA of hashes/sec
	lastSnapCount uint64    // hashCount at previous snapshot
	lastSnapTime  time.Time // time of previous snapshot
	snapCount     int       // number of snapshots taken (for readiness)
}

// New creates a new Miner. ts may be nil, in which case raw local time is used.
func New(c *chain.Chain, e consensus.Engine, mp *mempool.Mempool, p *params.ChainParams, rewardScript []byte, ts TimeSource, onBlock func(*types.Block)) *Miner {
	if ts == nil {
		ts = localClock{}
	}
	workers := runtime.NumCPU()
	if workers < 1 {
		workers = 1
	}
	return &Miner{
		chain:        c,
		engine:       e,
		mempool:      mp,
		params:       p,
		rewardScript: rewardScript,
		timeSource:   ts,
		onBlock:      onBlock,
		workers:      workers,
	}
}

// Hashrate returns the approximate hashes per second (EWMA, ~60s time constant).
func (m *Miner) Hashrate() uint64 {
	return m.hashrate.Load()
}

// HashrateReady returns true once enough samples exist for a meaningful average.
func (m *Miner) HashrateReady() bool {
	return m.hashrateReady.Load()
}

// Run starts the mining loop. It blocks until ctx is cancelled.
func (m *Miner) Run(ctx context.Context) {
	logging.L.Info("starting mining loop", "component", "miner", "workers", m.workers)

	m.ewmaMu.Lock()
	m.ewmaRate = 0
	m.lastSnapCount = m.hashCount.Load()
	m.lastSnapTime = time.Now()
	m.snapCount = 0
	m.ewmaMu.Unlock()
	m.hashrateReady.Store(false)
	m.hashrate.Store(0)

	go func() {
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.snapshotHashrate()
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			logging.L.Info("stopping mining loop", "component", "miner")
			m.hashrate.Store(0)
			m.hashrateReady.Store(false)
			return
		default:
		}

		block, err := m.MineOne(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			logging.L.Error("mining error", "component", "miner", "error", err)
			time.Sleep(time.Second)
			continue
		}

		if block != nil && m.onBlock != nil {
			m.onBlock(block)
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// ewmaAlpha controls smoothing. With a 3-second sample interval this gives
// an effective time constant of ~60 seconds: alpha = 1 - exp(-3/60) ≈ 0.049.
const ewmaAlpha = 0.049

func (m *Miner) snapshotHashrate() {
	now := time.Now()
	current := m.hashCount.Load()

	m.ewmaMu.Lock()
	dt := now.Sub(m.lastSnapTime).Seconds()
	if dt <= 0 {
		m.ewmaMu.Unlock()
		return
	}

	instantRate := float64(current-m.lastSnapCount) / dt
	m.lastSnapCount = current
	m.lastSnapTime = now
	m.snapCount++

	if m.snapCount == 1 {
		m.ewmaRate = instantRate
	} else {
		m.ewmaRate = ewmaAlpha*instantRate + (1-ewmaAlpha)*m.ewmaRate
	}

	rate := m.ewmaRate
	ready := m.snapCount >= 4
	m.ewmaMu.Unlock()

	m.hashrate.Store(uint64(rate))
	if ready {
		m.hashrateReady.Store(true)
	}
}

// MineOne builds a template and attempts to mine a single block using all
// available CPU cores. Each worker searches a distinct nonce range. If the
// full nonce space is exhausted, the extraNonce/timestamp are bumped and
// the search restarts (matching Bitcoin Core's inner mining loop).
func (m *Miner) MineOne(ctx context.Context) (*types.Block, error) {
	tipHash, tipHeight := m.chain.Tip()
	tipHeader, err := m.chain.TipHeader()
	if err != nil {
		return nil, fmt.Errorf("get tip header: %w", err)
	}

	newHeight := tipHeight + 1
	subsidy := m.params.CalcSubsidy(newHeight)

	tmpl := m.mempool.BlockTemplate()

	const headerSize = 80
	const coinbaseEstimate = 150
	maxTxCount := int(m.params.MaxBlockTxCount)
	maxSize := int(m.params.MaxBlockSize)
	if maxTxCount <= 1 {
		maxTxCount = 0
	} else {
		maxTxCount--
	}

	var includedTxs []*types.Transaction
	var totalFees uint64
	blockSize := headerSize + coinbaseEstimate
	for i, tx := range tmpl.Transactions {
		txSize := tmpl.Entries[i].Size
		if maxTxCount > 0 && len(includedTxs) >= maxTxCount {
			break
		}
		if maxSize > 0 && blockSize+txSize > maxSize {
			break
		}
		includedTxs = append(includedTxs, tx)
		totalFees += tmpl.Entries[i].Fee
		blockSize += txSize
	}

	extraNonce := uint32(0)

	for {
		select {
		case <-ctx.Done():
			return nil, nil
		default:
		}

		// Abort if the chain tip changed (new block arrived from network).
		currentTip, _ := m.chain.Tip()
		if currentTip != tipHash {
			return nil, nil
		}

		coinbaseTx := m.buildCoinbaseWithExtra(newHeight, subsidy+totalFees, extraNonce)

		txs := make([]types.Transaction, 0, 1+len(includedTxs))
		txs = append(txs, coinbaseTx)
		for _, tx := range includedTxs {
			txs = append(txs, *tx)
		}

		merkle, err := crypto.ComputeMerkleRoot(txs)
		if err != nil {
			return nil, fmt.Errorf("compute merkle root: %w", err)
		}

		ts := uint32(m.timeSource.Now())
		if ts <= tipHeader.Timestamp {
			ts = tipHeader.Timestamp + 1
		}

		header := types.BlockHeader{
			Version:    1,
			PrevBlock:  tipHash,
			MerkleRoot: merkle,
			Timestamp:  ts,
			Nonce:      0,
		}

		if err := m.engine.PrepareHeader(&header, tipHeader, tipHeight, m.chain.GetAncestor, m.params); err != nil {
			return nil, fmt.Errorf("prepare header: %w", err)
		}

		target := crypto.CompactToHash(header.Bits)

		block, found := m.searchNonceSpace(ctx, header, target, txs, tipHash)
		if found {
			return block, nil
		}

		// Nonce space exhausted — bump extraNonce and retry with new merkle root.
		extraNonce++
	}
}

// searchNonceSpace splits the 32-bit nonce space across all workers and
// returns the solved block if any worker finds a valid nonce.
func (m *Miner) searchNonceSpace(ctx context.Context, header types.BlockHeader, target types.Hash, txs []types.Transaction, tipHash types.Hash) (*types.Block, bool) {
	numWorkers := m.workers
	rangeSize := uint64(0x100000000) / uint64(numWorkers)
	// Keep batches tiny for memory-hard PoW. sha256mem runs at ~1 H/s
	// per core, so even a batch of 128 delays stale-work detection by
	// minutes. With 5-second block targets we must check for new tips
	// after every few hashes to avoid mining on a stale parent.
	const batchSize = uint64(4)

	type result struct {
		header types.BlockHeader
	}

	workerCtx, workerCancel := context.WithCancel(ctx)
	defer workerCancel()

	resultCh := make(chan result, 1)
	var wg sync.WaitGroup

	cs, hasCountedSealer := m.engine.(countedSealer)

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		startNonce := uint64(w) * rangeSize
		endNonce := startNonce + rangeSize
		if w == numWorkers-1 {
			endNonce = 0x100000000
		}

		go func(wHeader types.BlockHeader, start, end uint64) {
			defer wg.Done()
			wHeader.Nonce = uint32(start)
			pos := start

			for pos < end {
				select {
				case <-workerCtx.Done():
					return
				default:
				}

				remaining := end - pos
				batch := batchSize
				if remaining < batch {
					batch = remaining
				}

				if hasCountedSealer {
					found, hashes, sealErr := cs.SealHeaderCounted(&wHeader, target, batch)
					m.hashCount.Add(hashes)
					if sealErr != nil {
						return
					}
					if found {
						select {
						case resultCh <- result{header: wHeader}:
						default:
						}
						workerCancel()
						return
					}
				} else {
					found, sealErr := m.engine.SealHeader(&wHeader, target, batch)
					m.hashCount.Add(batch)
					if sealErr != nil {
						return
					}
					if found {
						select {
						case resultCh <- result{header: wHeader}:
						default:
						}
						workerCancel()
						return
					}
				}

				pos += batch
				wHeader.Nonce = uint32(pos & 0xFFFFFFFF)

				// Check if chain tip changed (stale work).
				currentTip, _ := m.chain.Tip()
				if currentTip != tipHash {
					return
				}
			}
		}(header, startNonce, endNonce)
	}

	go func() {
		wg.Wait()
		close(resultCh)
	}()

	res, ok := <-resultCh
	workerCancel()
	wg.Wait()

	if !ok {
		return nil, false
	}

	block := &types.Block{
		Header:       res.header,
		Transactions: txs,
	}
	blockHash := crypto.HashBlockHeader(&block.Header)
	currentTip, currentHeight := m.chain.Tip()
	if currentTip != tipHash {
		logging.L.Debug("discarding stale block (tip moved during mining)",
			"component", "miner", "hash", blockHash.ReverseString(),
			"built_on_height", currentHeight)
		return nil, false
	}
	logging.L.Info("found block", "component", "miner", "hash", blockHash.ReverseString(), "height", currentHeight+1, "nonce", res.header.Nonce)
	return block, true
}

func (m *Miner) buildCoinbase(height uint32, subsidy uint64) types.Transaction {
	return m.buildCoinbaseWithExtra(height, subsidy, 0)
}

func (m *Miner) buildCoinbaseWithExtra(height uint32, subsidy uint64, extraNonce uint32) types.Transaction {
	// BIP34: encode height as a CScript push — [pushLen][height LE bytes][extraNonce LE][tag].
	pushLen := minimalHeightPushLen(height)
	heightBytes := make([]byte, 4)
	types.PutUint32LE(heightBytes, height)
	msg := make([]byte, 0, 1+pushLen+4+len(coinparams.CoinbaseTag))
	msg = append(msg, byte(pushLen))
	msg = append(msg, heightBytes[:pushLen]...)
	if extraNonce > 0 {
		extraBytes := make([]byte, 4)
		types.PutUint32LE(extraBytes, extraNonce)
		msg = append(msg, extraBytes...)
	}
	msg = append(msg, []byte(coinparams.CoinbaseTag)...)

	return types.Transaction{
		Version: 1,
		Inputs: []types.TxInput{
			{
				PreviousOutPoint: types.CoinbaseOutPoint,
				SignatureScript:  msg,
				Sequence:         0xFFFFFFFF,
			},
		},
		Outputs: []types.TxOutput{
			{
				Value:    subsidy,
				PkScript: m.rewardScript,
			},
		},
		LockTime: 0,
	}
}

func minimalHeightPushLen(height uint32) int {
	switch {
	case height <= 0xFF:
		return 1
	case height <= 0xFFFF:
		return 2
	case height <= 0xFFFFFF:
		return 3
	default:
		return 4
	}
}
