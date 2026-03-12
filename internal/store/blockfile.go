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

	f, err := os.OpenFile(bfm.blkPath(fileNum), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("open block file: %w", err)
	}
	defer f.Close()

	// Write framing: [magic(4)][size(4 LE)][data].
	var frame [8]byte
	copy(frame[:4], bfm.magic[:])
	binary.LittleEndian.PutUint32(frame[4:], uint32(len(data)))
	if _, err := f.Write(frame[:]); err != nil {
		return 0, 0, 0, fmt.Errorf("write block frame: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		return 0, 0, 0, fmt.Errorf("write block data: %w", err)
	}

	bfm.curSize += frameSize
	return fileNum, offset, size, nil
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

	// Get current file size for offset.
	var curSize int64
	if info, err := os.Stat(path); err == nil {
		curSize = info.Size()
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return 0, 0, fmt.Errorf("open rev file: %w", err)
	}
	defer f.Close()

	checksum := crypto.DoubleSHA256(undoData)

	var frame [8]byte
	copy(frame[:4], bfm.magic[:])
	binary.LittleEndian.PutUint32(frame[4:], uint32(len(undoData)))

	if _, err := f.Write(frame[:]); err != nil {
		return 0, 0, fmt.Errorf("write undo frame: %w", err)
	}
	if _, err := f.Write(undoData); err != nil {
		return 0, 0, fmt.Errorf("write undo data: %w", err)
	}
	if _, err := f.Write(checksum[:]); err != nil {
		return 0, 0, fmt.Errorf("write undo checksum: %w", err)
	}

	return uint32(curSize), uint32(len(undoData)), nil
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
