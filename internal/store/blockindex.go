package store

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math/big"

	"github.com/bams-repo/fairchain/internal/types"
	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/opt"
	"github.com/syndtr/goleveldb/leveldb/util"
)

// Key prefixes matching Bitcoin Core conventions.
var (
	prefixBlock    = []byte("b") // 'b' + hash(32) -> DiskBlockIndex
	prefixFile     = []byte("f") // 'f' + fileNum(4) -> BlockFileInfo
	keyLastFile    = []byte("l") // -> last block file number (4 bytes LE)
	keyReindexFlag = []byte("R") // -> reindexing flag (1 byte)
)

// BlockStatus flags for DiskBlockIndex.
const (
	StatusValidHeader  uint32 = 1
	StatusValidTree    uint32 = 2
	StatusValidTx      uint32 = 4
	StatusValidChain   uint32 = 8
	StatusHaveData     uint32 = 16
	StatusHaveUndo     uint32 = 32
)

// DiskBlockIndex stores all metadata about a block in the index.
type DiskBlockIndex struct {
	Header    types.BlockHeader
	Height    uint32
	Status    uint32
	TxCount   uint32
	FileNum   uint32 // blk*.dat file number.
	DataPos   uint32 // Byte offset in blk*.dat (start of frame).
	DataSize  uint32 // Block data size (excluding frame header).
	UndoFile  uint32 // rev*.dat file number.
	UndoPos   uint32 // Byte offset in rev*.dat.
	UndoSize  uint32 // Undo data size.
	ChainWork *big.Int
}

// Serialize encodes a DiskBlockIndex for storage.
func (d *DiskBlockIndex) Serialize() []byte {
	var buf bytes.Buffer

	// Fixed-size fields.
	binary.Write(&buf, binary.LittleEndian, d.Height)
	binary.Write(&buf, binary.LittleEndian, d.Status)
	binary.Write(&buf, binary.LittleEndian, d.TxCount)
	binary.Write(&buf, binary.LittleEndian, d.FileNum)
	binary.Write(&buf, binary.LittleEndian, d.DataPos)
	binary.Write(&buf, binary.LittleEndian, d.DataSize)
	binary.Write(&buf, binary.LittleEndian, d.UndoFile)
	binary.Write(&buf, binary.LittleEndian, d.UndoPos)
	binary.Write(&buf, binary.LittleEndian, d.UndoSize)

	// 80-byte header.
	buf.Write(d.Header.SerializeToBytes())

	// Chain work as 32-byte big-endian.
	var workBytes [32]byte
	if d.ChainWork != nil {
		b := d.ChainWork.Bytes()
		copy(workBytes[32-len(b):], b)
	}
	buf.Write(workBytes[:])

	return buf.Bytes()
}

// DeserializeDiskBlockIndex decodes a DiskBlockIndex from stored bytes.
func DeserializeDiskBlockIndex(data []byte) (*DiskBlockIndex, error) {
	if len(data) < 36+80+32 { // 9*4=36 fixed + 80 header + 32 work
		return nil, fmt.Errorf("disk block index too short: %d bytes", len(data))
	}

	d := &DiskBlockIndex{}
	r := bytes.NewReader(data)

	binary.Read(r, binary.LittleEndian, &d.Height)
	binary.Read(r, binary.LittleEndian, &d.Status)
	binary.Read(r, binary.LittleEndian, &d.TxCount)
	binary.Read(r, binary.LittleEndian, &d.FileNum)
	binary.Read(r, binary.LittleEndian, &d.DataPos)
	binary.Read(r, binary.LittleEndian, &d.DataSize)
	binary.Read(r, binary.LittleEndian, &d.UndoFile)
	binary.Read(r, binary.LittleEndian, &d.UndoPos)
	binary.Read(r, binary.LittleEndian, &d.UndoSize)

	if err := d.Header.Deserialize(r); err != nil {
		return nil, fmt.Errorf("deserialize header: %w", err)
	}

	var workBytes [32]byte
	if _, err := r.Read(workBytes[:]); err != nil {
		return nil, fmt.Errorf("read chain work: %w", err)
	}
	d.ChainWork = new(big.Int).SetBytes(workBytes[:])

	return d, nil
}

// BlockIndex wraps a LevelDB instance for the block index.
type BlockIndex struct {
	db *leveldb.DB
}

// NewBlockIndex opens or creates a LevelDB block index at the given path.
func NewBlockIndex(path string) (*BlockIndex, error) {
	db, err := leveldb.OpenFile(path, &opt.Options{
		BlockCacheCapacity: 8 * 1024 * 1024, // 8 MiB cache.
	})
	if err != nil {
		return nil, fmt.Errorf("open block index: %w", err)
	}
	return &BlockIndex{db: db}, nil
}

func blockIndexKey(hash types.Hash) []byte {
	key := make([]byte, 1+types.HashSize)
	key[0] = 'b'
	copy(key[1:], hash[:])
	return key
}

func fileInfoKey(fileNum uint32) []byte {
	key := make([]byte, 5)
	key[0] = 'f'
	binary.LittleEndian.PutUint32(key[1:], fileNum)
	return key
}

// PutBlockIndex stores a DiskBlockIndex record with synchronous writes to
// ensure the index survives crashes. Without Sync, a crash after an
// acknowledged write can lose the block index entry.
func (bi *BlockIndex) PutBlockIndex(hash types.Hash, rec *DiskBlockIndex) error {
	return bi.db.Put(blockIndexKey(hash), rec.Serialize(), &opt.WriteOptions{Sync: true})
}

// PutBlockIndexBatch stores a DiskBlockIndex record without synchronous writes.
// Used during IBD where periodic checkpoints provide crash safety.
func (bi *BlockIndex) PutBlockIndexBatch(hash types.Hash, rec *DiskBlockIndex) error {
	return bi.db.Put(blockIndexKey(hash), rec.Serialize(), &opt.WriteOptions{Sync: false})
}

// FlushIndex forces a WAL flush by writing a no-op compaction hint.
func (bi *BlockIndex) FlushIndex() error {
	return bi.db.CompactRange(util.Range{Start: prefixBlock, Limit: nil})
}

// GetBlockIndex retrieves a DiskBlockIndex record by block hash.
func (bi *BlockIndex) GetBlockIndex(hash types.Hash) (*DiskBlockIndex, error) {
	data, err := bi.db.Get(blockIndexKey(hash), nil)
	if err != nil {
		return nil, fmt.Errorf("get block index %s: %w", hash, err)
	}
	return DeserializeDiskBlockIndex(data)
}

// HasBlock checks if a block hash exists in the index.
func (bi *BlockIndex) HasBlock(hash types.Hash) (bool, error) {
	return bi.db.Has(blockIndexKey(hash), nil)
}

// DeleteBlockIndex removes a block index entry. Used to clean up entries for
// blocks that failed validation after being tentatively written (e.g. a reorg
// candidate whose transaction inputs turned out to be invalid).
func (bi *BlockIndex) DeleteBlockIndex(hash types.Hash) error {
	return bi.db.Delete(blockIndexKey(hash), &opt.WriteOptions{Sync: true})
}

// PutLastFile stores the last block file number.
func (bi *BlockIndex) PutLastFile(fileNum uint32) error {
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], fileNum)
	return bi.db.Put(keyLastFile, buf[:], &opt.WriteOptions{Sync: true})
}

// GetLastFile retrieves the last block file number.
func (bi *BlockIndex) GetLastFile() (uint32, error) {
	data, err := bi.db.Get(keyLastFile, nil)
	if err != nil {
		return 0, err
	}
	if len(data) < 4 {
		return 0, fmt.Errorf("corrupt last file data")
	}
	return binary.LittleEndian.Uint32(data), nil
}

// BlockFileInfo stores metadata about a blk*.dat file.
type BlockFileInfo struct {
	Blocks     uint32
	Size       uint32
	UndoSize   uint32
	HeightLow  uint32
	HeightHigh uint32
	TimeLow    uint32
	TimeHigh   uint32
}

// PutFileInfo stores file metadata.
func (bi *BlockIndex) PutFileInfo(fileNum uint32, info *BlockFileInfo) error {
	var buf bytes.Buffer
	binary.Write(&buf, binary.LittleEndian, info.Blocks)
	binary.Write(&buf, binary.LittleEndian, info.Size)
	binary.Write(&buf, binary.LittleEndian, info.UndoSize)
	binary.Write(&buf, binary.LittleEndian, info.HeightLow)
	binary.Write(&buf, binary.LittleEndian, info.HeightHigh)
	binary.Write(&buf, binary.LittleEndian, info.TimeLow)
	binary.Write(&buf, binary.LittleEndian, info.TimeHigh)
	return bi.db.Put(fileInfoKey(fileNum), buf.Bytes(), &opt.WriteOptions{Sync: true})
}

// ForEachBlock iterates over all block index entries.
func (bi *BlockIndex) ForEachBlock(fn func(hash types.Hash, rec *DiskBlockIndex) error) error {
	iter := bi.db.NewIterator(util.BytesPrefix(prefixBlock), nil)
	defer iter.Release()

	for iter.Next() {
		key := iter.Key()
		if len(key) != 1+types.HashSize {
			continue
		}
		var hash types.Hash
		copy(hash[:], key[1:])

		rec, err := DeserializeDiskBlockIndex(iter.Value())
		if err != nil {
			return fmt.Errorf("deserialize block index entry: %w", err)
		}
		if err := fn(hash, rec); err != nil {
			return err
		}
	}
	return iter.Error()
}

// Close closes the LevelDB block index.
func (bi *BlockIndex) Close() error {
	return bi.db.Close()
}
