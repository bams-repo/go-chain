// Copyright (c) 2024-2026 The Fairchain Contributors
// Fairchain is an experiment in modularity, designed to improve on the work
// of Satoshi Nakamoto and to inspire more creative genius in the space.
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

package chain

import (
	"sync"
	"testing"
	"time"

	"github.com/bams-repo/fairchain/internal/algorithms/sha256d"
	"github.com/bams-repo/fairchain/internal/consensus/pow"
	"github.com/bams-repo/fairchain/internal/crypto"
	bitcoindiff "github.com/bams-repo/fairchain/internal/difficulty/bitcoin"
	fcparams "github.com/bams-repo/fairchain/internal/params"
	"github.com/bams-repo/fairchain/internal/types"
)

func setupTestHeaderIndex(t *testing.T) (*HeaderIndex, *fcparams.ChainParams) {
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

	idx := NewHeaderIndex(p, engine, &genesis.Header)
	return idx, p
}

// headerNonce is used to produce unique headers when forking from the same parent.
var headerNonceMu sync.Mutex
var headerNonceCounter uint32

// mineHeader builds and mines a valid header extending the given parent.
func mineHeader(t *testing.T, parent *types.BlockHeader, p *fcparams.ChainParams) types.BlockHeader {
	t.Helper()

	headerNonceMu.Lock()
	headerNonceCounter++
	extra := headerNonceCounter
	headerNonceMu.Unlock()

	parentHash := crypto.HashBlockHeader(parent)
	// Use a unique MerkleRoot to ensure different headers from the same parent.
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

func TestHeaderIndexGenesis(t *testing.T) {
	idx, p := setupTestHeaderIndex(t)

	if idx.Count() != 1 {
		t.Fatalf("expected 1 header (genesis), got %d", idx.Count())
	}
	if idx.BestHeaderHeight() != 0 {
		t.Fatalf("expected best height 0, got %d", idx.BestHeaderHeight())
	}

	genesisHash := crypto.HashBlockHeader(&p.GenesisBlock.Header)
	if !idx.HasHeader(genesisHash) {
		t.Fatal("genesis header not found in index")
	}

	node := idx.GetHeader(genesisHash)
	if node == nil {
		t.Fatal("GetHeader returned nil for genesis")
	}
	if node.Height != 0 {
		t.Fatalf("genesis height = %d, want 0", node.Height)
	}
	if node.Parent != nil {
		t.Fatal("genesis parent should be nil")
	}
}

func TestHeaderIndexAddSequential(t *testing.T) {
	idx, p := setupTestHeaderIndex(t)

	parent := &p.GenesisBlock.Header
	now := uint32(time.Now().Unix())

	const count = 50
	for i := 0; i < count; i++ {
		hdr := mineHeader(t, parent, p)
		node, err := idx.AddHeader(&hdr, now)
		if err != nil {
			t.Fatalf("AddHeader at height %d: %v", i+1, err)
		}
		if node.Height != uint32(i+1) {
			t.Fatalf("expected height %d, got %d", i+1, node.Height)
		}
		parent = &hdr
	}

	if idx.Count() != count+1 {
		t.Fatalf("expected %d headers, got %d", count+1, idx.Count())
	}
	if idx.BestHeaderHeight() != count {
		t.Fatalf("expected best height %d, got %d", count, idx.BestHeaderHeight())
	}
}

func TestHeaderIndexAddFork(t *testing.T) {
	idx, p := setupTestHeaderIndex(t)
	now := uint32(time.Now().Unix())

	parent := &p.GenesisBlock.Header
	for i := 0; i < 5; i++ {
		hdr := mineHeader(t, parent, p)
		if _, err := idx.AddHeader(&hdr, now); err != nil {
			t.Fatalf("main chain header %d: %v", i+1, err)
		}
		parent = &hdr
	}

	forkParent := idx.GetHeaderByHeight(3)
	if forkParent == nil {
		t.Fatal("could not get header at height 3 for fork")
	}

	forkTip := &forkParent.Header
	for i := 0; i < 5; i++ {
		hdr := mineHeader(t, forkTip, p)
		if _, err := idx.AddHeader(&hdr, now); err != nil {
			t.Fatalf("fork header %d: %v", i+1, err)
		}
		forkTip = &hdr
	}

	// Fork chain: genesis + 3 common + 5 fork = height 8
	// Main chain: genesis + 5 = height 5
	// Fork has more work, so bestHeader should be on the fork.
	if idx.BestHeaderHeight() != 8 {
		t.Fatalf("expected best height 8 (fork), got %d", idx.BestHeaderHeight())
	}
}

func TestHeaderIndexRejectOrphan(t *testing.T) {
	idx, _ := setupTestHeaderIndex(t)
	now := uint32(time.Now().Unix())

	orphan := types.BlockHeader{
		Version:   1,
		PrevBlock: types.Hash{0xFF, 0xEE, 0xDD},
		Timestamp: 1700000001,
		Bits:      0x207fffff,
		Nonce:     0,
	}

	_, err := idx.AddHeader(&orphan, now)
	if err == nil {
		t.Fatal("expected error for orphan header")
	}
}

func TestHeaderIndexRejectDuplicate(t *testing.T) {
	idx, p := setupTestHeaderIndex(t)
	now := uint32(time.Now().Unix())

	hdr := mineHeader(t, &p.GenesisBlock.Header, p)
	if _, err := idx.AddHeader(&hdr, now); err != nil {
		t.Fatalf("first add: %v", err)
	}

	_, err := idx.AddHeader(&hdr, now)
	if err == nil {
		t.Fatal("expected error for duplicate header")
	}
}

func TestHeaderIndexLocator(t *testing.T) {
	idx, p := setupTestHeaderIndex(t)
	now := uint32(time.Now().Unix())

	parent := &p.GenesisBlock.Header
	for i := 0; i < 30; i++ {
		hdr := mineHeader(t, parent, p)
		if _, err := idx.AddHeader(&hdr, now); err != nil {
			t.Fatalf("AddHeader %d: %v", i+1, err)
		}
		parent = &hdr
	}

	locator := idx.HeaderLocator()
	if len(locator) == 0 {
		t.Fatal("locator is empty")
	}

	// First entry should be the tip.
	tipNode := idx.BestHeader()
	if locator[0] != tipNode.Hash {
		t.Fatal("first locator entry should be the tip hash")
	}

	// Last entry should be genesis.
	genesisHash := crypto.HashBlockHeader(&p.GenesisBlock.Header)
	if locator[len(locator)-1] != genesisHash {
		t.Fatal("last locator entry should be genesis hash")
	}

	// First 10 entries should be consecutive heights (30, 29, 28, ..., 21).
	for i := 0; i < 10 && i < len(locator)-1; i++ {
		node := idx.GetHeader(locator[i])
		nextNode := idx.GetHeader(locator[i+1])
		if node == nil || nextNode == nil {
			t.Fatalf("locator entry %d or %d not found", i, i+1)
		}
		if i < 9 && node.Height-nextNode.Height != 1 {
			t.Fatalf("locator entries %d and %d not consecutive: heights %d and %d",
				i, i+1, node.Height, nextNode.Height)
		}
	}
}

func TestHeaderIndexHeadersToFetch(t *testing.T) {
	idx, p := setupTestHeaderIndex(t)
	now := uint32(time.Now().Unix())

	parent := &p.GenesisBlock.Header
	for i := 0; i < 20; i++ {
		hdr := mineHeader(t, parent, p)
		if _, err := idx.AddHeader(&hdr, now); err != nil {
			t.Fatalf("AddHeader %d: %v", i+1, err)
		}
		parent = &hdr
	}

	hashes := idx.HeadersToFetch(5, 10)
	if len(hashes) != 10 {
		t.Fatalf("expected 10 hashes, got %d", len(hashes))
	}

	for i, h := range hashes {
		node := idx.GetHeader(h)
		if node == nil {
			t.Fatalf("hash %d not found in index", i)
		}
		expectedHeight := uint32(5 + i)
		if node.Height != expectedHeight {
			t.Fatalf("hash %d: expected height %d, got %d", i, expectedHeight, node.Height)
		}
	}
}

func TestHeaderIndexGetHeaderByHeight(t *testing.T) {
	idx, p := setupTestHeaderIndex(t)
	now := uint32(time.Now().Unix())

	parent := &p.GenesisBlock.Header
	for i := 0; i < 10; i++ {
		hdr := mineHeader(t, parent, p)
		if _, err := idx.AddHeader(&hdr, now); err != nil {
			t.Fatalf("AddHeader %d: %v", i+1, err)
		}
		parent = &hdr
	}

	for h := uint32(0); h <= 10; h++ {
		node := idx.GetHeaderByHeight(h)
		if node == nil {
			t.Fatalf("GetHeaderByHeight(%d) returned nil", h)
		}
		if node.Height != h {
			t.Fatalf("GetHeaderByHeight(%d) returned height %d", h, node.Height)
		}
	}

	if node := idx.GetHeaderByHeight(11); node != nil {
		t.Fatal("GetHeaderByHeight(11) should return nil")
	}
}

func TestHeaderIndexConcurrency(t *testing.T) {
	idx, p := setupTestHeaderIndex(t)
	now := uint32(time.Now().Unix())

	// Build a chain of 20 headers sequentially first.
	parent := &p.GenesisBlock.Header
	headers := make([]types.BlockHeader, 20)
	for i := 0; i < 20; i++ {
		headers[i] = mineHeader(t, parent, p)
		parent = &headers[i]
	}

	// Add them concurrently in order (using AddHeaders which is sequential internally).
	var wg sync.WaitGroup
	errs := make(chan error, 4)

	// Split into 4 batches of 5 added sequentially.
	// First add all sequentially since they depend on each other.
	added, err := idx.AddHeaders(headers, now)
	if err != nil {
		t.Fatalf("AddHeaders: added %d, error: %v", added, err)
	}

	// Now do concurrent reads.
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = idx.BestHeaderHeight()
				_ = idx.Count()
				_ = idx.HeaderLocator()
				_ = idx.GetHeaderByHeight(uint32(j % 21))
			}
		}()
	}

	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent error: %v", err)
	}

	if idx.BestHeaderHeight() != 20 {
		t.Fatalf("expected best height 20, got %d", idx.BestHeaderHeight())
	}
}

func TestHeaderIndexAddHeaders(t *testing.T) {
	idx, p := setupTestHeaderIndex(t)
	now := uint32(time.Now().Unix())

	parent := &p.GenesisBlock.Header
	headers := make([]types.BlockHeader, 10)
	for i := 0; i < 10; i++ {
		headers[i] = mineHeader(t, parent, p)
		parent = &headers[i]
	}

	added, err := idx.AddHeaders(headers, now)
	if err != nil {
		t.Fatalf("AddHeaders: added %d, error: %v", added, err)
	}
	if added != 10 {
		t.Fatalf("expected 10 added, got %d", added)
	}
	if idx.BestHeaderHeight() != 10 {
		t.Fatalf("expected best height 10, got %d", idx.BestHeaderHeight())
	}
}
