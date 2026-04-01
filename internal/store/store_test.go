// Copyright (c) 2024-2026 The Fairchain Contributors
// Fairchain is an experiment in modularity, designed to improve on the work
// of Satoshi Nakamoto and to inspire more creative genius in the space.
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

package store

import (
	"math/big"
	"path/filepath"
	"testing"
	"time"

	"github.com/bams-repo/fairchain/internal/crypto"
	"github.com/bams-repo/fairchain/internal/types"
)

func newTestFileStore(t *testing.T) *FileStore {
	t.Helper()
	dir := t.TempDir()
	magic := [4]byte{0xFA, 0x1C, 0xC0, 0xFF}
	s, err := NewFileStore(
		filepath.Join(dir, "blocks"),
		filepath.Join(dir, "blocks", "index"),
		filepath.Join(dir, "chainstate"),
		magic,
	)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestFileStoreBlockRoundtrip(t *testing.T) {
	s := newTestFileStore(t)

	block := types.Block{
		Header: types.BlockHeader{
			Version:   1,
			Timestamp: 1700000000,
			Bits:      0x207fffff,
			Nonce:     42,
		},
		Transactions: []types.Transaction{
			{
				Version: 1,
				Inputs: []types.TxInput{
					{PreviousOutPoint: types.CoinbaseOutPoint, SignatureScript: []byte("test"), Sequence: 0xFFFFFFFF},
				},
				Outputs: []types.TxOutput{
					{Value: 5000000000, PkScript: []byte{0x00}},
				},
			},
		},
	}

	hash := crypto.HashBlockHeader(&block.Header)

	fileNum, offset, size, err := s.WriteBlock(hash, &block)
	if err != nil {
		t.Fatalf("WriteBlock: %v", err)
	}

	rec := &DiskBlockIndex{
		Header:    block.Header,
		Height:    0,
		Status:    StatusHaveData | StatusValidHeader,
		TxCount:   1,
		FileNum:   fileNum,
		DataPos:   offset,
		DataSize:  size,
		ChainWork: big.NewInt(1),
	}
	if err := s.PutBlockIndex(hash, rec); err != nil {
		t.Fatalf("PutBlockIndex: %v", err)
	}

	has, err := s.HasBlock(hash)
	if err != nil {
		t.Fatalf("HasBlock: %v", err)
	}
	if !has {
		t.Fatal("block should exist after write")
	}

	got, err := s.GetBlock(hash)
	if err != nil {
		t.Fatalf("GetBlock: %v", err)
	}
	if got.Header.Nonce != 42 {
		t.Fatalf("nonce = %d, want 42", got.Header.Nonce)
	}
	if len(got.Transactions) != 1 {
		t.Fatalf("tx count = %d, want 1", len(got.Transactions))
	}

	hdr, err := s.GetHeader(hash)
	if err != nil {
		t.Fatalf("GetHeader: %v", err)
	}
	if hdr.Nonce != 42 {
		t.Fatalf("header nonce = %d, want 42", hdr.Nonce)
	}
}

func TestFileStoreChainTip(t *testing.T) {
	s := newTestFileStore(t)

	hash := types.Hash{0xAA, 0xBB}
	height := uint32(100)

	if err := s.PutChainTip(hash, height); err != nil {
		t.Fatalf("PutChainTip: %v", err)
	}

	gotHash, gotHeight, err := s.GetChainTip()
	if err != nil {
		t.Fatalf("GetChainTip: %v", err)
	}
	if gotHash != hash {
		t.Fatalf("tip hash = %s, want %s", gotHash, hash)
	}
	if gotHeight != height {
		t.Fatalf("tip height = %d, want %d", gotHeight, height)
	}
}

func TestFileStoreUndoRoundtrip(t *testing.T) {
	s := newTestFileStore(t)

	undoData := []byte{0x01, 0x02, 0x03, 0x04, 0x05}

	offset, size, err := s.WriteUndo(0, undoData)
	if err != nil {
		t.Fatalf("WriteUndo: %v", err)
	}

	got, err := s.ReadUndo(0, offset, size)
	if err != nil {
		t.Fatalf("ReadUndo: %v", err)
	}
	if len(got) != len(undoData) {
		t.Fatalf("undo data length = %d, want %d", len(got), len(undoData))
	}
	for i := range got {
		if got[i] != undoData[i] {
			t.Fatalf("undo data[%d] = %d, want %d", i, got[i], undoData[i])
		}
	}
}

func TestFileStoreUtxo(t *testing.T) {
	s := newTestFileStore(t)

	txHash := types.Hash{0x11, 0x22, 0x33}
	index := uint32(0)
	data := []byte{0xAA, 0xBB, 0xCC}

	if err := s.PutUtxo(txHash, index, data); err != nil {
		t.Fatalf("PutUtxo: %v", err)
	}

	has, err := s.HasUtxo(txHash, index)
	if err != nil {
		t.Fatalf("HasUtxo: %v", err)
	}
	if !has {
		t.Fatal("UTXO should exist")
	}

	got, err := s.GetUtxo(txHash, index)
	if err != nil {
		t.Fatalf("GetUtxo: %v", err)
	}
	if len(got) != 3 || got[0] != 0xAA {
		t.Fatalf("unexpected UTXO data: %x", got)
	}

	if err := s.DeleteUtxo(txHash, index); err != nil {
		t.Fatalf("DeleteUtxo: %v", err)
	}

	has, err = s.HasUtxo(txHash, index)
	if err != nil {
		t.Fatalf("HasUtxo after delete: %v", err)
	}
	if has {
		t.Fatal("UTXO should not exist after delete")
	}
}

func TestFileStoreNonExistent(t *testing.T) {
	s := newTestFileStore(t)

	has, err := s.HasBlock(types.Hash{0xFF})
	if err != nil {
		t.Fatalf("HasBlock: %v", err)
	}
	if has {
		t.Fatal("non-existent block should not exist")
	}

	_, _, err = s.GetChainTip()
	if err == nil {
		t.Fatal("GetChainTip should error when not set")
	}
}

func newTestBoltStore(t *testing.T) *BoltStore {
	t.Helper()
	dir := t.TempDir()
	s, err := NewBoltStore(dir + "/test.db")
	if err != nil {
		t.Fatalf("NewBoltStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestBoltStorePeers(t *testing.T) {
	s := newTestBoltStore(t)

	if err := s.PutPeer("127.0.0.1:19333"); err != nil {
		t.Fatalf("PutPeer: %v", err)
	}
	if err := s.PutPeer("192.168.1.1:19333"); err != nil {
		t.Fatalf("PutPeer: %v", err)
	}

	peers, err := s.GetPeers()
	if err != nil {
		t.Fatalf("GetPeers: %v", err)
	}
	if len(peers) != 2 {
		t.Fatalf("peer count = %d, want 2", len(peers))
	}

	if err := s.RemovePeer("127.0.0.1:19333"); err != nil {
		t.Fatalf("RemovePeer: %v", err)
	}

	peers, err = s.GetPeers()
	if err != nil {
		t.Fatalf("GetPeers after remove: %v", err)
	}
	if len(peers) != 1 {
		t.Fatalf("peer count after remove = %d, want 1", len(peers))
	}
}

func TestBoltStorePeerCount(t *testing.T) {
	s := newTestBoltStore(t)

	count, err := s.PeerCount()
	if err != nil {
		t.Fatalf("PeerCount: %v", err)
	}
	if count != 0 {
		t.Fatalf("initial count = %d, want 0", count)
	}

	s.PutPeer("10.0.0.1:19333")
	s.PutPeer("10.0.0.2:19333")
	s.PutPeer("10.0.0.3:19333")

	count, err = s.PeerCount()
	if err != nil {
		t.Fatalf("PeerCount: %v", err)
	}
	if count != 3 {
		t.Fatalf("count = %d, want 3", count)
	}
}

func TestBoltStorePutPeerBounded_EvictsOldest(t *testing.T) {
	s := newTestBoltStore(t)

	// Insert 3 peers with PutPeer (which stores current timestamp).
	s.PutPeer("10.0.0.1:19333")
	s.PutPeer("10.0.0.2:19333")
	s.PutPeer("10.0.0.3:19333")

	count, _ := s.PeerCount()
	if count != 3 {
		t.Fatalf("count = %d, want 3", count)
	}

	// Insert a 4th peer with maxSize=3 -- should evict the oldest.
	if err := s.PutPeerBounded("10.0.0.4:19333", 3); err != nil {
		t.Fatalf("PutPeerBounded: %v", err)
	}

	count, _ = s.PeerCount()
	if count != 3 {
		t.Fatalf("count after bounded insert = %d, want 3", count)
	}

	peers, _ := s.GetPeers()
	peerSet := make(map[string]bool)
	for _, p := range peers {
		peerSet[p] = true
	}

	if !peerSet["10.0.0.4:19333"] {
		t.Fatal("new peer should be present")
	}
}

func TestBoltStorePutPeerBounded_UpdateExisting(t *testing.T) {
	s := newTestBoltStore(t)

	s.PutPeer("10.0.0.1:19333")
	s.PutPeer("10.0.0.2:19333")

	// Re-inserting an existing peer should not increase count.
	if err := s.PutPeerBounded("10.0.0.1:19333", 2); err != nil {
		t.Fatalf("PutPeerBounded: %v", err)
	}

	count, _ := s.PeerCount()
	if count != 2 {
		t.Fatalf("count = %d, want 2 (no new entry for existing peer)", count)
	}
}

func TestBoltStorePruneOlderThan(t *testing.T) {
	s := newTestBoltStore(t)

	// Insert peers with PutPeer (current timestamp).
	s.PutPeer("10.0.0.1:19333")
	s.PutPeer("10.0.0.2:19333")

	// Prune with a cutoff in the far future -- should remove all.
	removed, err := s.PruneOlderThan(time.Now().Unix() + 86400)
	if err != nil {
		t.Fatalf("PruneOlderThan: %v", err)
	}
	if removed != 2 {
		t.Fatalf("removed = %d, want 2", removed)
	}

	count, _ := s.PeerCount()
	if count != 0 {
		t.Fatalf("count after prune = %d, want 0", count)
	}
}

func TestBoltStorePruneOlderThan_KeepsRecent(t *testing.T) {
	s := newTestBoltStore(t)

	s.PutPeer("10.0.0.1:19333")

	// Prune with a cutoff in the past -- should keep all.
	removed, err := s.PruneOlderThan(time.Now().Unix() - 86400)
	if err != nil {
		t.Fatalf("PruneOlderThan: %v", err)
	}
	if removed != 0 {
		t.Fatalf("removed = %d, want 0 (peer is recent)", removed)
	}

	count, _ := s.PeerCount()
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}
}
