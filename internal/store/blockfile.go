package store

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/bams-repo/fairchain/internal/crypto"
	"github.com/bams-repo/fairchain/internal/types"
)

const (
	maxBlockFileSize = 128 * 1024 * 1024 // 128 MiB per blk/rev file.
	blockFileDigits  = 5                 // blk00000.dat format.
)

// BlockFileManager handles reading and writing blk*.dat and rev*.dat flat files
// following Bitcoin Core conventions.
type BlockFileManager struct {
	mu       sync.Mutex
	dir      string   // blocks/ directory path.
	magic    [4]byte  // Network magic bytes for record framing.
	curFile  uint32   // Current block file number being written to.
	curSize  int64    // Current size of the active block file.
}

// NewBlockFileManager opens or creates a block file manager for the given directory.
func NewBlockFileManager(blocksDir string, magic [4]byte) (*BlockFileManager, error) {
	bfm := &BlockFileManager{
		dir:   blocksDir,
		magic: magic,
	}

	// Find the highest existing block file to resume appending.
	for i := uint32(0); ; i++ {
		path := bfm.blkPath(i)
		info, err := os.Stat(path)
		if os.IsNotExist(err) {
			if i == 0 {
				bfm.curFile = 0
				bfm.curSize = 0
			}
			break
		}
		if err != nil {
			return nil, fmt.Errorf("stat block file %s: %w", path, err)
		}
		bfm.curFile = i
		bfm.curSize = info.Size()
	}

	return bfm, nil
}

func (bfm *BlockFileManager) blkPath(fileNum uint32) string {
	return filepath.Join(bfm.dir, fmt.Sprintf("blk%0*d.dat", blockFileDigits, fileNum))
}

func (bfm *BlockFileManager) revPath(fileNum uint32) string {
	return filepath.Join(bfm.dir, fmt.Sprintf("rev%0*d.dat", blockFileDigits, fileNum))
}

// WriteBlock serializes and appends a block to the current blk*.dat file.
// Returns the file number, byte offset, and data size (excluding framing).
func (bfm *BlockFileManager) WriteBlock(block *types.Block) (fileNum uint32, offset uint32, size uint32, err error) {
	data, err := block.SerializeToBytes()
	if err != nil {
		return 0, 0, 0, fmt.Errorf("serialize block: %w", err)
	}

	bfm.mu.Lock()
	defer bfm.mu.Unlock()

	// Rotate to a new file if the current one would exceed the size limit.
	frameSize := int64(8 + len(data)) // 4 magic + 4 size + data
	if bfm.curSize > 0 && bfm.curSize+frameSize > maxBlockFileSize {
		bfm.curFile++
		bfm.curSize = 0
	}

	fileNum = bfm.curFile
	offset = uint32(bfm.curSize)
	size = uint32(len(data))

	path := bfm.blkPath(fileNum)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("open block file: %w", err)
	}
	defer f.Close()

	preWritePos, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("seek to end of block file: %w", err)
	}

	record := make([]byte, 8+len(data))
	copy(record[:4], bfm.magic[:])
	binary.LittleEndian.PutUint32(record[4:8], uint32(len(data)))
	copy(record[8:], data)

	n, writeErr := f.Write(record)
	if writeErr != nil || n != len(record) {
		// Partial write — truncate back to the pre-write position so we
		// don't leave a half-written record that would corrupt reads.
		_ = f.Truncate(preWritePos)
		_ = f.Sync()
		if writeErr != nil {
			return 0, 0, 0, fmt.Errorf("write block record: %w", writeErr)
		}
		return 0, 0, 0, fmt.Errorf("short write to block file: wrote %d of %d bytes", n, len(record))
	}
	if err := f.Sync(); err != nil {
		_ = f.Truncate(preWritePos)
		_ = f.Sync()
		return 0, 0, 0, fmt.Errorf("sync block file: %w", err)
	}

	bfm.curSize += frameSize
	return fileNum, offset, size, nil
}

// WriteBlockNoSync is identical to WriteBlock but skips the fsync call.
// Used during IBD where periodic checkpoints provide crash safety.
func (bfm *BlockFileManager) WriteBlockNoSync(block *types.Block) (fileNum uint32, offset uint32, size uint32, err error) {
	data, err := block.SerializeToBytes()
	if err != nil {
		return 0, 0, 0, fmt.Errorf("serialize block: %w", err)
	}

	bfm.mu.Lock()
	defer bfm.mu.Unlock()

	frameSize := int64(8 + len(data))
	if bfm.curSize > 0 && bfm.curSize+frameSize > maxBlockFileSize {
		bfm.curFile++
		bfm.curSize = 0
	}

	fileNum = bfm.curFile
	offset = uint32(bfm.curSize)
	size = uint32(len(data))

	path := bfm.blkPath(fileNum)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("open block file: %w", err)
	}
	defer f.Close()

	preWritePos, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("seek to end of block file: %w", err)
	}

	record := make([]byte, 8+len(data))
	copy(record[:4], bfm.magic[:])
	binary.LittleEndian.PutUint32(record[4:8], uint32(len(data)))
	copy(record[8:], data)

	n, writeErr := f.Write(record)
	if writeErr != nil || n != len(record) {
		_ = f.Truncate(preWritePos)
		if writeErr != nil {
			return 0, 0, 0, fmt.Errorf("write block record: %w", writeErr)
		}
		return 0, 0, 0, fmt.Errorf("short write to block file: wrote %d of %d bytes", n, len(record))
	}

	bfm.curSize += frameSize
	return fileNum, offset, size, nil
}

// WriteUndoNoSync is identical to WriteUndo but skips the fsync call.
func (bfm *BlockFileManager) WriteUndoNoSync(fileNum uint32, undoData []byte) (offset uint32, size uint32, err error) {
	bfm.mu.Lock()
	defer bfm.mu.Unlock()

	path := bfm.revPath(fileNum)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return 0, 0, fmt.Errorf("open rev file: %w", err)
	}
	defer f.Close()

	preWritePos, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return 0, 0, fmt.Errorf("seek to end of rev file: %w", err)
	}

	checksum := crypto.DoubleSHA256(undoData)

	record := make([]byte, 8+len(undoData)+32)
	copy(record[:4], bfm.magic[:])
	binary.LittleEndian.PutUint32(record[4:8], uint32(len(undoData)))
	copy(record[8:], undoData)
	copy(record[8+len(undoData):], checksum[:])

	n, writeErr := f.Write(record)
	if writeErr != nil || n != len(record) {
		_ = f.Truncate(preWritePos)
		if writeErr != nil {
			return 0, 0, fmt.Errorf("write undo record: %w", writeErr)
		}
		return 0, 0, fmt.Errorf("short write to undo file: wrote %d of %d bytes", n, len(record))
	}

	return uint32(preWritePos), uint32(len(undoData)), nil
}

// SyncAll fsyncs the current blk*.dat and rev*.dat files.
func (bfm *BlockFileManager) SyncAll() error {
	bfm.mu.Lock()
	fileNum := bfm.curFile
	bfm.mu.Unlock()

	blkPath := bfm.blkPath(fileNum)
	if f, err := os.OpenFile(blkPath, os.O_WRONLY, 0600); err == nil {
		syncErr := f.Sync()
		f.Close()
		if syncErr != nil {
			return fmt.Errorf("sync block file %d: %w", fileNum, syncErr)
		}
	}

	revPath := bfm.revPath(fileNum)
	if f, err := os.OpenFile(revPath, os.O_WRONLY, 0600); err == nil {
		syncErr := f.Sync()
		f.Close()
		if syncErr != nil {
			return fmt.Errorf("sync rev file %d: %w", fileNum, syncErr)
		}
	}

	return nil
}

// ReadBlock reads a block from the specified file at the given byte offset.
// The offset points to the start of the frame (magic bytes).
func (bfm *BlockFileManager) ReadBlock(fileNum, offset, size uint32) (*types.Block, error) {
	f, err := os.Open(bfm.blkPath(fileNum))
	if err != nil {
		return nil, fmt.Errorf("open block file %d: %w", fileNum, err)
	}
	defer f.Close()

	if _, err := f.Seek(int64(offset), io.SeekStart); err != nil {
		return nil, fmt.Errorf("seek to offset %d: %w", offset, err)
	}

	// Read and validate frame header.
	var frame [8]byte
	if _, err := io.ReadFull(f, frame[:]); err != nil {
		return nil, fmt.Errorf("read block frame: %w", err)
	}

	if !bytes.Equal(frame[:4], bfm.magic[:]) {
		return nil, fmt.Errorf("invalid magic at file %d offset %d", fileNum, offset)
	}

	dataSize := binary.LittleEndian.Uint32(frame[4:])
	if size > 0 && dataSize != size {
		return nil, fmt.Errorf("size mismatch: frame says %d, index says %d", dataSize, size)
	}
	if dataSize > maxBlockFileSize {
		return nil, fmt.Errorf("block data size %d exceeds maximum %d — possible corruption", dataSize, maxBlockFileSize)
	}

	data := make([]byte, dataSize)
	if _, err := io.ReadFull(f, data); err != nil {
		return nil, fmt.Errorf("read block data: %w", err)
	}

	var block types.Block
	if err := block.Deserialize(bytes.NewReader(data)); err != nil {
		return nil, fmt.Errorf("deserialize block: %w", err)
	}

	return &block, nil
}

// WriteUndo writes undo data for a block to the corresponding rev*.dat file.
// Format: [magic(4)][size(4 LE)][undo data][checksum(32)].
func (bfm *BlockFileManager) WriteUndo(fileNum uint32, undoData []byte) (offset uint32, size uint32, err error) {
	bfm.mu.Lock()
	defer bfm.mu.Unlock()

	path := bfm.revPath(fileNum)

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return 0, 0, fmt.Errorf("open rev file: %w", err)
	}
	defer f.Close()

	preWritePos, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return 0, 0, fmt.Errorf("seek to end of rev file: %w", err)
	}

	checksum := crypto.DoubleSHA256(undoData)

	record := make([]byte, 8+len(undoData)+32)
	copy(record[:4], bfm.magic[:])
	binary.LittleEndian.PutUint32(record[4:8], uint32(len(undoData)))
	copy(record[8:], undoData)
	copy(record[8+len(undoData):], checksum[:])

	n, writeErr := f.Write(record)
	if writeErr != nil || n != len(record) {
		_ = f.Truncate(preWritePos)
		_ = f.Sync()
		if writeErr != nil {
			return 0, 0, fmt.Errorf("write undo record: %w", writeErr)
		}
		return 0, 0, fmt.Errorf("short write to undo file: wrote %d of %d bytes", n, len(record))
	}
	if err := f.Sync(); err != nil {
		_ = f.Truncate(preWritePos)
		_ = f.Sync()
		return 0, 0, fmt.Errorf("sync undo file: %w", err)
	}

	return uint32(preWritePos), uint32(len(undoData)), nil
}

// ReadUndo reads undo data from the specified rev*.dat file at the given offset.
func (bfm *BlockFileManager) ReadUndo(fileNum, offset, size uint32) ([]byte, error) {
	f, err := os.Open(bfm.revPath(fileNum))
	if err != nil {
		return nil, fmt.Errorf("open rev file %d: %w", fileNum, err)
	}
	defer f.Close()

	if _, err := f.Seek(int64(offset), io.SeekStart); err != nil {
		return nil, fmt.Errorf("seek to offset %d: %w", offset, err)
	}

	var frame [8]byte
	if _, err := io.ReadFull(f, frame[:]); err != nil {
		return nil, fmt.Errorf("read undo frame: %w", err)
	}

	if !bytes.Equal(frame[:4], bfm.magic[:]) {
		return nil, fmt.Errorf("invalid magic in rev file %d at offset %d", fileNum, offset)
	}

	dataSize := binary.LittleEndian.Uint32(frame[4:])
	if size > 0 && dataSize != size {
		return nil, fmt.Errorf("undo size mismatch: frame says %d, index says %d", dataSize, size)
	}
	if dataSize > maxBlockFileSize {
		return nil, fmt.Errorf("undo data size %d exceeds maximum %d — possible corruption", dataSize, maxBlockFileSize)
	}

	data := make([]byte, dataSize)
	if _, err := io.ReadFull(f, data); err != nil {
		return nil, fmt.Errorf("read undo data: %w", err)
	}

	// Read and verify checksum.
	var storedChecksum types.Hash
	if _, err := io.ReadFull(f, storedChecksum[:]); err != nil {
		return nil, fmt.Errorf("read undo checksum: %w", err)
	}
	computed := crypto.DoubleSHA256(data)
	if storedChecksum != computed {
		return nil, fmt.Errorf("undo data checksum mismatch in file %d at offset %d", fileNum, offset)
	}

	return data, nil
}

// CurrentFile returns the current block file number.
func (bfm *BlockFileManager) CurrentFile() uint32 {
	bfm.mu.Lock()
	defer bfm.mu.Unlock()
	return bfm.curFile
}
