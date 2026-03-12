package store

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/bams-repo/fairchain/internal/types"
	bolt "go.etcd.io/bbolt"
)

// Bucket names for bbolt storage.
var (
	blocksBucket      = []byte("blocks")
	headersBucket     = []byte("headers")
	heightIndexBucket = []byte("heightindex") // height(uint32 BE) -> hash
	hashIndexBucket   = []byte("hashindex")   // hash -> height(uint32 BE)
	chainStateBucket  = []byte("chainstate")
	peersBucket       = []byte("peers")
	utxoBucket        = []byte("utxos")    // outpoint(36) -> serialized UtxoEntry
	undoBucket        = []byte("undodata") // block hash(32) -> serialized BlockUndoData
)

var chainTipKey = []byte("tip")

// BoltStore implements PeerStore using bbolt.
// It also provides legacy block storage methods for migration.
type BoltStore struct {
	db *bolt.DB
}

var _ PeerStore = (*BoltStore)(nil)

// NewBoltStore opens or creates a bbolt database at the given path.
func NewBoltStore(path string) (*BoltStore, error) {
	db, err := bolt.Open(path, 0600, &bolt.Options{})
	if err != nil {
		return nil, fmt.Errorf("open bolt db: %w", err)
	}

	err = db.Update(func(tx *bolt.Tx) error {
		for _, bucket := range [][]byte{
			blocksBucket, headersBucket, heightIndexBucket,
			hashIndexBucket, chainStateBucket, peersBucket,
			utxoBucket, undoBucket,
		} {
			if _, err := tx.CreateBucketIfNotExists(bucket); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("init buckets: %w", err)
	}

	return &BoltStore{db: db}, nil
}

func (s *BoltStore) Close() error {
	return s.db.Close()
}

// PeerStore methods

func (s *BoltStore) GetPeers() ([]string, error) {
	var peers []string
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(peersBucket)
		return b.ForEach(func(k, _ []byte) error {
			peers = append(peers, string(k))
			return nil
		})
	})
	return peers, err
}

func (s *BoltStore) PutPeer(addr string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(peersBucket).Put([]byte(addr), []byte{1})
	})
}

func (s *BoltStore) RemovePeer(addr string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(peersBucket).Delete([]byte(addr))
	})
}

// Legacy block storage methods (used by migration tool).

// LegacyHasBlock checks if a block exists in the legacy bbolt store.
func (s *BoltStore) LegacyHasBlock(hash types.Hash) (bool, error) {
	var found bool
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(blocksBucket)
		found = b.Get(hash[:]) != nil
		return nil
	})
	return found, err
}

// LegacyGetBlock retrieves a full block from the legacy bbolt store.
func (s *BoltStore) LegacyGetBlock(hash types.Hash) (*types.Block, error) {
	var block types.Block
	err := s.db.View(func(tx *bolt.Tx) error {
		data := tx.Bucket(blocksBucket).Get(hash[:])
		if data == nil {
			return fmt.Errorf("block %s not found", hash)
		}
		return block.Deserialize(bytes.NewReader(data))
	})
	if err != nil {
		return nil, err
	}
	return &block, nil
}

// LegacyGetHeader retrieves a block header from the legacy bbolt store.
func (s *BoltStore) LegacyGetHeader(hash types.Hash) (*types.BlockHeader, error) {
	var header types.BlockHeader
	err := s.db.View(func(tx *bolt.Tx) error {
		data := tx.Bucket(headersBucket).Get(hash[:])
		if data == nil {
			return fmt.Errorf("header %s not found", hash)
		}
		return header.Deserialize(bytes.NewReader(data))
	})
	if err != nil {
		return nil, err
	}
	return &header, nil
}

// LegacyGetBlockByHeight retrieves a block hash at the given height from legacy store.
func (s *BoltStore) LegacyGetBlockByHeight(height uint32) (types.Hash, error) {
	var hash types.Hash
	err := s.db.View(func(tx *bolt.Tx) error {
		key := heightToBytes(height)
		data := tx.Bucket(heightIndexBucket).Get(key)
		if data == nil {
			return fmt.Errorf("no block at height %d", height)
		}
		if len(data) != types.HashSize {
			return fmt.Errorf("corrupt height index at %d", height)
		}
		copy(hash[:], data)
		return nil
	})
	return hash, err
}

// LegacyGetChainTip returns the chain tip from the legacy bbolt store.
func (s *BoltStore) LegacyGetChainTip() (types.Hash, uint32, error) {
	var hash types.Hash
	var height uint32
	err := s.db.View(func(tx *bolt.Tx) error {
		data := tx.Bucket(chainStateBucket).Get(chainTipKey)
		if data == nil {
			return fmt.Errorf("chain tip not set")
		}
		if len(data) != types.HashSize+4 {
			return fmt.Errorf("corrupt chain tip data")
		}
		copy(hash[:], data[:types.HashSize])
		height = binary.BigEndian.Uint32(data[types.HashSize:])
		return nil
	})
	return hash, height, err
}

// LegacyGetUndoData retrieves undo data from the legacy bbolt store.
func (s *BoltStore) LegacyGetUndoData(blockHash types.Hash) ([]byte, error) {
	var result []byte
	err := s.db.View(func(tx *bolt.Tx) error {
		data := tx.Bucket(undoBucket).Get(blockHash[:])
		if data == nil {
			return fmt.Errorf("undo data not found for %s", blockHash)
		}
		result = make([]byte, len(data))
		copy(result, data)
		return nil
	})
	return result, err
}

// LegacyHasData checks if the legacy store has any block data.
func (s *BoltStore) LegacyHasData() bool {
	hasData := false
	s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(chainStateBucket)
		if b != nil && b.Get(chainTipKey) != nil {
			hasData = true
		}
		return nil
	})
	return hasData
}

func heightToBytes(h uint32) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, h)
	return b
}
