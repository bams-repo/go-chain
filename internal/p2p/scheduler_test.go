// Copyright (c) 2024-2026 The Fairchain Contributors
// Fairchain is an experiment in modularity, designed to improve on the work
// of Satoshi Nakamoto and to inspire more creative genius in the space.
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

package p2p

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/bams-repo/fairchain/internal/algorithms/sha256d"
	"github.com/bams-repo/fairchain/internal/chain"
	"github.com/bams-repo/fairchain/internal/consensus/pow"
	"github.com/bams-repo/fairchain/internal/crypto"
	bitcoindiff "github.com/bams-repo/fairchain/internal/difficulty/bitcoin"
	fcparams "github.com/bams-repo/fairchain/internal/params"
	"github.com/bams-repo/fairchain/internal/store"
	"github.com/bams-repo/fairchain/internal/types"
)

var testHeaderNonceMu sync.Mutex
var testHeaderNonceCounter uint32

func schedulerTestSetup(t *testing.T, headerCount int) (*BlockScheduler, *chain.HeaderIndex, *chain.Chain, []types.BlockHeader) {
	t.Helper()

	p := &fcparams.ChainParams{}
	*p = *fcparams.Regtest

	cfg := fcparams.GenesisConfig{
		NetworkName:     "regtest",
		CoinbaseMessage: []byte("test genesis"),
		Timestamp:       1700000000,
		Bits:            p.InitialBits,
		Version:         1,
		Reward:          p.InitialSubsidy,
		RewardScript:    []byte{0x00},
	}
	genesis := fcparams.BuildGenesisBlock(cfg)
	engine := pow.New(sha256d.New(), bitcoindiff.New())
	if err := engine.MineGenesis(&genesis); err != nil {
		t.Fatalf("mine genesis: %v", err)
	}
	genesisHash := crypto.HashBlockHeader(&genesis.Header)
	fcparams.InitGenesis(p, genesis, genesisHash)

	dir := t.TempDir()
	s, err := store.NewFileStore(
		filepath.Join(dir, "blocks"),
		filepath.Join(dir, "blocks", "index"),
		filepath.Join(dir, "chainstate"),
		p.NetworkMagic,
	)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	c := chain.New(p, engine, s, nil)
	if err := c.Init(); err != nil {
		t.Fatalf("init chain: %v", err)
	}

	idx := chain.NewHeaderIndex(p, engine, &genesis.Header)
	now := uint32(time.Now().Unix())

	parent := &genesis.Header
	headers := make([]types.BlockHeader, headerCount)
	for i := 0; i < headerCount; i++ {
		headers[i] = mineTestHeader(t, parent, p)
		if _, err := idx.AddHeader(&headers[i], now); err != nil {
			t.Fatalf("add header %d: %v", i+1, err)
		}
		parent = &headers[i]
	}

	sched := NewBlockScheduler(idx, c)
	return sched, idx, c, headers
}

func mineTestHeader(t *testing.T, parent *types.BlockHeader, p *fcparams.ChainParams) types.BlockHeader {
	t.Helper()

	testHeaderNonceMu.Lock()
	testHeaderNonceCounter++
	extra := testHeaderNonceCounter
	testHeaderNonceMu.Unlock()

	parentHash := crypto.HashBlockHeader(parent)
	var merkle types.Hash
	merkle[0] = byte(extra)
	merkle[1] = byte(extra >> 8)

	header := types.BlockHeader{
		Version:    1,
		PrevBlock:  parentHash,
		MerkleRoot: merkle,
		Timestamp:  parent.Timestamp + 1,
		Bits:       p.InitialBits,
		Nonce:      0,
	}

	target := crypto.CompactToHash(header.Bits)
	engine := pow.New(sha256d.New(), bitcoindiff.New())
	found, _ := engine.SealHeader(&header, target, 10_000_000)
	if !found {
		t.Fatal("could not mine header")
	}
	return header
}

func TestSchedulerPopulate(t *testing.T) {
	sched, _, _, headers := schedulerTestSetup(t, 10)
	sched.Populate()

	if sched.NeedCount() != len(headers) {
		t.Fatalf("expected %d needed, got %d", len(headers), sched.NeedCount())
	}
}

func TestSchedulerAssignWork(t *testing.T) {
	sched, _, _, _ := schedulerTestSetup(t, 10)
	sched.Populate()

	hashes := sched.AssignWork("peer1", 5)
	if len(hashes) != 5 {
		t.Fatalf("expected 5 assigned, got %d", len(hashes))
	}
	if sched.InFlightCount() != 5 {
		t.Fatalf("expected 5 in-flight, got %d", sched.InFlightCount())
	}
	if sched.NeedCount() != 5 {
		t.Fatalf("expected 5 remaining needed, got %d", sched.NeedCount())
	}
}

func TestSchedulerBlockReceived(t *testing.T) {
	sched, _, _, headers := schedulerTestSetup(t, 5)
	sched.Populate()

	hashes := sched.AssignWork("peer1", 5)
	if len(hashes) != 5 {
		t.Fatalf("expected 5 assigned, got %d", len(hashes))
	}

	block := &types.Block{Header: headers[0]}
	blockHash := crypto.HashBlockHeader(&headers[0])
	ok := sched.BlockReceived(blockHash, block, "peer1")
	if !ok {
		t.Fatal("BlockReceived returned false for expected block")
	}
	if sched.InFlightCount() != 4 {
		t.Fatalf("expected 4 in-flight, got %d", sched.InFlightCount())
	}
	if sched.StagingCount() != 1 {
		t.Fatalf("expected 1 staging, got %d", sched.StagingCount())
	}
}

func TestSchedulerBlockReceivedUnexpected(t *testing.T) {
	sched, _, _, _ := schedulerTestSetup(t, 5)
	sched.Populate()

	block := &types.Block{Header: types.BlockHeader{Nonce: 999}}
	ok := sched.BlockReceived(types.Hash{0xFF}, block, "peer1")
	if ok {
		t.Fatal("BlockReceived should return false for unexpected block")
	}
}

func TestSchedulerDrainReady(t *testing.T) {
	sched, idx, _, headers := schedulerTestSetup(t, 5)
	sched.Populate()

	hashes := sched.AssignWork("peer1", 5)
	if len(hashes) != 5 {
		t.Fatalf("expected 5 assigned, got %d", len(hashes))
	}

	for i := range headers {
		h := crypto.HashBlockHeader(&headers[i])
		block := &types.Block{Header: headers[i]}
		sched.BlockReceived(h, block, "peer1")
	}

	_ = idx
	ready := sched.DrainReady()
	if len(ready) != 5 {
		t.Fatalf("expected 5 ready blocks, got %d", len(ready))
	}

	for i, staged := range ready {
		expectedHash := crypto.HashBlockHeader(&headers[i])
		gotHash := crypto.HashBlockHeader(&staged.Block.Header)
		if gotHash != expectedHash {
			t.Fatalf("block %d hash mismatch", i)
		}
		if staged.PeerAddr != "peer1" {
			t.Fatalf("block %d peer = %q, want peer1", i, staged.PeerAddr)
		}
	}
}

func TestSchedulerDrainReadyGap(t *testing.T) {
	sched, _, _, headers := schedulerTestSetup(t, 5)
	sched.Populate()

	sched.AssignWork("peer1", 5)

	// Only deliver blocks 0, 1, and 3 (skip 2).
	for _, i := range []int{0, 1, 3} {
		h := crypto.HashBlockHeader(&headers[i])
		block := &types.Block{Header: headers[i]}
		sched.BlockReceived(h, block, "peer1")
	}

	ready := sched.DrainReady()
	// Should only get blocks 0 and 1 (stops at gap).
	if len(ready) != 2 {
		t.Fatalf("expected 2 ready blocks (gap at 2), got %d", len(ready))
	}
}

func TestSchedulerTimeout(t *testing.T) {
	sched, _, _, _ := schedulerTestSetup(t, 5)
	sched.requestTimeout = 1 * time.Millisecond
	sched.Populate()

	sched.AssignWork("peer1", 3)
	time.Sleep(5 * time.Millisecond)

	timedOut := sched.HandleTimeout()
	if len(timedOut) != 3 {
		t.Fatalf("expected 3 timed out, got %d", len(timedOut))
	}
	if sched.InFlightCount() != 0 {
		t.Fatalf("expected 0 in-flight after timeout, got %d", sched.InFlightCount())
	}
	// Timed-out blocks should be back in the needed queue.
	if sched.NeedCount() != 5 {
		t.Fatalf("expected 5 needed after timeout, got %d", sched.NeedCount())
	}
}

func TestSchedulerRemovePeer(t *testing.T) {
	sched, _, _, _ := schedulerTestSetup(t, 10)
	sched.Populate()

	sched.AssignWork("peer1", 5)
	sched.AssignWork("peer2", 3)

	if sched.InFlightCount() != 8 {
		t.Fatalf("expected 8 in-flight, got %d", sched.InFlightCount())
	}

	sched.RemovePeer("peer1")

	if sched.InFlightCount() != 3 {
		t.Fatalf("expected 3 in-flight after removing peer1, got %d", sched.InFlightCount())
	}
	// peer1's 5 blocks should be back in needed.
	if sched.NeedCount() != 7 {
		t.Fatalf("expected 7 needed after removing peer1, got %d", sched.NeedCount())
	}
}

func TestSchedulerMaxInFlight(t *testing.T) {
	sched, _, _, _ := schedulerTestSetup(t, 50)
	sched.maxInFlightPerPeer = 4
	sched.Populate()

	hashes := sched.AssignWork("peer1", 10)
	if len(hashes) != 4 {
		t.Fatalf("expected 4 assigned (per-peer limit), got %d", len(hashes))
	}

	hashes2 := sched.AssignWork("peer1", 10)
	if len(hashes2) != 0 {
		t.Fatalf("expected 0 assigned (peer at limit), got %d", len(hashes2))
	}
}

func TestSchedulerMaxGlobalInFlight(t *testing.T) {
	sched, _, _, _ := schedulerTestSetup(t, 50)
	sched.maxGlobalInFlight = 8
	sched.maxInFlightPerPeer = 16
	sched.Populate()

	sched.AssignWork("peer1", 5)
	hashes := sched.AssignWork("peer2", 10)
	if len(hashes) != 3 {
		t.Fatalf("expected 3 assigned (global limit 8 - 5 = 3), got %d", len(hashes))
	}
}

func TestSchedulerMaxStaging(t *testing.T) {
	sched, _, _, headers := schedulerTestSetup(t, 20)
	sched.maxStagingSize = 3
	sched.Populate()

	assigned := sched.AssignWork("peer1", 10)
	for i := 0; i < len(assigned) && i < 3; i++ {
		block := &types.Block{Header: headers[i]}
		sched.BlockReceived(assigned[i], block, "peer1")
	}

	// Staging is now at max. New assignments should be blocked.
	hashes := sched.AssignWork("peer2", 5)
	if len(hashes) != 0 {
		t.Fatalf("expected 0 assigned (staging full), got %d", len(hashes))
	}
}

func TestSchedulerComplete(t *testing.T) {
	sched, _, _, headers := schedulerTestSetup(t, 3)
	sched.Populate()

	if sched.IsComplete() {
		t.Fatal("should not be complete before any work")
	}

	assigned := sched.AssignWork("peer1", 3)
	for i, h := range assigned {
		block := &types.Block{Header: headers[i]}
		sched.BlockReceived(h, block, "peer1")
	}

	// Drain all ready blocks.
	ready := sched.DrainReady()
	if len(ready) != 3 {
		t.Fatalf("expected 3 ready, got %d", len(ready))
	}

	if !sched.IsComplete() {
		t.Fatal("should be complete after all blocks drained")
	}
}

func TestSchedulerStats(t *testing.T) {
	sched, _, _, _ := schedulerTestSetup(t, 10)
	sched.Populate()

	sched.AssignWork("peer1", 3)

	stats := sched.Stats()
	if stats.Needed != 7 {
		t.Fatalf("stats.Needed = %d, want 7", stats.Needed)
	}
	if stats.InFlight != 3 {
		t.Fatalf("stats.InFlight = %d, want 3", stats.InFlight)
	}
	if stats.Staging != 0 {
		t.Fatalf("stats.Staging = %d, want 0", stats.Staging)
	}
}
