// Copyright (c) 2024-2026 The Fairchain Contributors
// Fairchain is an experiment in modularity, designed to improve on the work
// of Satoshi Nakamoto and to inspire more creative genius in the space.
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

package store

import (
	"math/big"

	"github.com/bams-repo/fairchain/internal/crypto"
	"github.com/bams-repo/fairchain/internal/types"
)

// BlockStore abstracts persistent storage for blocks, headers, chain state, and UTXOs.
// Implementations must be safe for concurrent read access.
// Write operations are expected to be serialized by the caller (chain manager).
type BlockStore interface {
	// Block data (flat files).
	HasBlock(hash types.Hash) (bool, error)
	WriteBlock(hash types.Hash, block *types.Block) (fileNum, offset, size uint32, err error)
	WriteBlockNoSync(hash types.Hash, block *types.Block) (fileNum, offset, size uint32, err error)
	WriteBlockRaw(hash types.Hash, data []byte) (fileNum, offset, size uint32, err error)
	WriteBlockNoSyncRaw(hash types.Hash, data []byte) (fileNum, offset, size uint32, err error)
	ReadBlock(fileNum, offset, size uint32) (*types.Block, error)
	WriteUndo(fileNum uint32, data []byte) (offset, size uint32, err error)
	WriteUndoNoSync(fileNum uint32, data []byte) (offset, size uint32, err error)
	ReadUndo(fileNum, offset, size uint32) ([]byte, error)
	SyncBlockFiles() error

	// Block index (LevelDB).
	PutBlockIndex(hash types.Hash, rec *DiskBlockIndex) error
	PutBlockIndexBatch(hash types.Hash, rec *DiskBlockIndex) error
	FlushBlockIndex() error
	GetBlockIndex(hash types.Hash) (*DiskBlockIndex, error)
	DeleteBlockIndex(hash types.Hash) error
	ForEachBlockIndex(fn func(hash types.Hash, rec *DiskBlockIndex) error) error

	// Chain tip (stored in block index).
	GetChainTip() (types.Hash, uint32, error)
	PutChainTip(hash types.Hash, height uint32) error
	PutChainTipNoSync(hash types.Hash, height uint32) error

	// Chainstate / UTXO (LevelDB).
	PutUtxo(txHash types.Hash, index uint32, data []byte) error
	GetUtxo(txHash types.Hash, index uint32) ([]byte, error)
	DeleteUtxo(txHash types.Hash, index uint32) error
	HasUtxo(txHash types.Hash, index uint32) (bool, error)
	NewUtxoWriteBatch() *ChainstateWriteBatch
	FlushUtxoBatch(wb *ChainstateWriteBatch) error
	FlushUtxoBatchNoSync(wb *ChainstateWriteBatch) error
	GetBestBlock() (types.Hash, error)
	PutBestBlock(hash types.Hash) error
	UtxoCount() (int, error)
	ForEachUtxo(fn func(txHash types.Hash, index uint32, data []byte) error) error

	// Legacy compatibility: read a full block by hash (uses index + flat file).
	GetBlock(hash types.Hash) (*types.Block, error)
	GetHeader(hash types.Hash) (*types.BlockHeader, error)

	Close() error
}

// PeerStore abstracts persistent storage for peer addresses.
type PeerStore interface {
	// GetPeers returns known peer addresses.
	GetPeers() ([]string, error)

	// PutPeer stores a peer address (updates last-seen timestamp if exists).
	PutPeer(addr string) error

	// PutPeerBounded stores a peer address, evicting the oldest entry if the
	// store exceeds maxSize. Pass maxSize <= 0 for unlimited.
	PutPeerBounded(addr string, maxSize int) error

	// RemovePeer removes a peer address.
	RemovePeer(addr string) error

	// PeerCount returns the number of stored peer addresses.
	PeerCount() (int, error)

	// PruneOlderThan removes peer entries whose last-seen timestamp is older
	// than the given Unix timestamp. Returns the number of entries removed.
	PruneOlderThan(beforeUnix int64) (int, error)

	// Close releases storage resources.
	Close() error
}

// CalcWork delegates to the canonical crypto.CalcWork implementation.
func CalcWork(bits uint32) *big.Int {
	return crypto.CalcWork(bits)
}
