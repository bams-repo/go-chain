// Copyright (c) 2024-2026 The Fairchain Contributors
// Distributed under the MIT software license.

// pow-bench measures sha256mem PoW throughput and time-to-hit for a fixed
// compact difficulty (default: mainnet InitialBits). It does not connect
// to the network or load chainstate.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bams-repo/fairchain/internal/algorithms/sha256mem"
	"github.com/bams-repo/fairchain/internal/consensus/pow"
	"github.com/bams-repo/fairchain/internal/crypto"
	"github.com/bams-repo/fairchain/internal/difficulty/lwma"
	"github.com/bams-repo/fairchain/internal/params"
	"github.com/bams-repo/fairchain/internal/types"
)

func main() {
	workers := flag.Int("workers", 10, "number of parallel hashing goroutines")
	interval := flag.Duration("interval", 3*time.Second, "hashrate report interval")
	bitsStr := flag.String("bits", "", "compact difficulty: decimal or 0x hex; empty = mainnet InitialBits")
	maxDur := flag.Duration("max", 0, "stop after this duration (0 = run until a solution or full nonce space)")
	flag.Parse()

	mp := params.Mainnet
	var compact uint32
	if *bitsStr == "" {
		compact = mp.InitialBits
	} else {
		v, err := strconv.ParseUint(strings.TrimSpace(*bitsStr), 0, 32)
		if err != nil {
			fmt.Fprintf(os.Stderr, "pow-bench: invalid -bits: %v\n", err)
			os.Exit(2)
		}
		compact = uint32(v)
	}
	target := crypto.CompactToHash(compact)

	// Fixed synthetic header: same serialization shape as real mining; PoW only
	// cares about the 80-byte preimage.
	hdr := types.BlockHeader{
		Version:    1,
		PrevBlock:  mp.GenesisHash,
		MerkleRoot: mp.GenesisBlock.Header.MerkleRoot,
		Timestamp:  mp.GenesisBlock.Header.Timestamp + 1,
		Bits:       compact,
		Nonce:      0,
	}

	activation := mp.ActivationHeights[sha256mem.ActivationKey]
	engine := pow.New(sha256mem.NewChainHasher(activation), lwma.New())
	benchHeight := uint32(1)
	batchSize := uint64(32)

	ctx := context.Background()
	var cancel context.CancelFunc
	if *maxDur > 0 {
		ctx, cancel = context.WithTimeout(ctx, *maxDur)
	} else {
		ctx, cancel = context.WithCancel(ctx)
	}
	defer cancel()

	var totalHashes atomic.Uint64
	start := time.Now()
	lastCount := uint64(0)
	lastTick := start

	go func() {
		t := time.NewTicker(*interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-t.C:
				cur := totalHashes.Load()
				dt := now.Sub(lastTick).Seconds()
				if dt <= 0 {
					continue
				}
				windowH := float64(cur-lastCount) / dt
				elapsed := time.Since(start).Round(time.Millisecond)
				fmt.Printf("[%s] window %.0f H/s  cumulative %d hashes  elapsed %v  bits=0x%08x\n",
					now.Format(time.RFC3339), windowH, cur, elapsed, compact)
				lastCount = cur
				lastTick = now
			}
		}
	}()

	found := runWorkers(ctx, *workers, hdr, target, benchHeight, mp, batchSize, engine, &totalHashes)
	cancel()

	elapsed := time.Since(start)
	final := totalHashes.Load()
	if found != nil {
		fmt.Printf("FOUND nonce=%d  hashes=%d  wall=%v  bits=0x%08x\n",
			found.Nonce, final, elapsed.Round(time.Millisecond), compact)
		os.Exit(0)
	}
	fmt.Printf("STOPPED (no solution in window)  hashes=%d  wall=%v\n", final, elapsed.Round(time.Millisecond))
	os.Exit(1)
}

func runWorkers(
	ctx context.Context,
	numWorkers int,
	base types.BlockHeader,
	target types.Hash,
	height uint32,
	p *params.ChainParams,
	batchSize uint64,
	engine *pow.Engine,
	totalHashes *atomic.Uint64,
) *types.BlockHeader {
	if numWorkers < 1 {
		numWorkers = 1
	}
	rangeSize := uint64(0x100000000) / uint64(numWorkers)

	type result struct {
		hdr types.BlockHeader
	}
	resCh := make(chan result, 1)
	workerCtx, workerCancel := context.WithCancel(ctx)
	defer workerCancel()

	var wg sync.WaitGroup
	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		startNonce := uint64(w) * rangeSize
		endNonce := startNonce + rangeSize
		if w == numWorkers-1 {
			endNonce = 0x100000000
		}
		go func(start, end uint64) {
			defer wg.Done()
			h := base
			h.Nonce = uint32(start)
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
				found, hashes, err := engine.SealHeaderCounted(&h, target, height, p, batch)
				totalHashes.Add(hashes)
				if err != nil {
					return
				}
				if found {
					select {
					case resCh <- result{hdr: h}:
					default:
					}
					workerCancel()
					return
				}
				pos += batch
				h.Nonce = uint32(pos & 0xFFFFFFFF)
			}
		}(startNonce, endNonce)
	}
	go func() {
		wg.Wait()
		close(resCh)
	}()
	r, ok := <-resCh
	workerCancel()
	wg.Wait()
	if !ok {
		return nil
	}
	return &r.hdr
}
