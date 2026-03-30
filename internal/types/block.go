// Copyright (c) 2024-2026 The Fairchain Contributors
// Fairchain is an experiment in modularity, designed to improve on the work
// of Satoshi Nakamoto and to inspire more creative genius in the space.
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

package types

import (
	"encoding/binary"
	"fmt"
	"io"
)

// BlockHeaderSize is the fixed canonical size of a serialized block header.
// version(4) + prevBlock(32) + merkleRoot(32) + timestamp(4) + bits(4) + nonce(4) = 80 bytes.
const BlockHeaderSize = 80

// BlockHeader contains the consensus-critical fields of a block.
// Serialization order is fixed and must be identical on all nodes.
type BlockHeader struct {
	Version    uint32 // Block version; allows future soft/hard fork signaling.
	PrevBlock  Hash   // Hash of the previous block header.
	MerkleRoot Hash   // Merkle root of all transactions in the block.
	Timestamp  uint32 // Block timestamp as Unix epoch seconds.
	Bits       uint32 // Compact representation of the target difficulty.
	Nonce      uint32 // PoW nonce (iterated by miners).
}

// Serialize writes the block header in canonical 80-byte format.
func (h *BlockHeader) Serialize(w io.Writer) error {
	var buf [BlockHeaderSize]byte
	h.serializeInto(buf[:])
	_, err := w.Write(buf[:])
	return err
}

// SerializeToBytes returns the canonical 80-byte header.
func (h *BlockHeader) SerializeToBytes() []byte {
	var buf [BlockHeaderSize]byte
	h.serializeInto(buf[:])
	return buf[:]
}

// SerializeInto writes the canonical 80-byte header into buf.
// len(buf) must be at least BlockHeaderSize (no allocation; for mining hot paths).
func (h *BlockHeader) SerializeInto(buf []byte) {
	if len(buf) < BlockHeaderSize {
		panic("BlockHeader.SerializeInto: buf shorter than BlockHeaderSize")
	}
	h.serializeInto(buf)
}

func (h *BlockHeader) serializeInto(buf []byte) {
	binary.LittleEndian.PutUint32(buf[0:4], h.Version)
	copy(buf[4:36], h.PrevBlock[:])
	copy(buf[36:68], h.MerkleRoot[:])
	binary.LittleEndian.PutUint32(buf[68:72], h.Timestamp)
	binary.LittleEndian.PutUint32(buf[72:76], h.Bits)
	binary.LittleEndian.PutUint32(buf[76:80], h.Nonce)
}

// Deserialize reads a block header from canonical 80-byte format.
func (h *BlockHeader) Deserialize(r io.Reader) error {
	var buf [BlockHeaderSize]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return fmt.Errorf("read block header: %w", err)
	}
	h.Version = binary.LittleEndian.Uint32(buf[0:4])
	copy(h.PrevBlock[:], buf[4:36])
	copy(h.MerkleRoot[:], buf[36:68])
	h.Timestamp = binary.LittleEndian.Uint32(buf[68:72])
	h.Bits = binary.LittleEndian.Uint32(buf[72:76])
	h.Nonce = binary.LittleEndian.Uint32(buf[76:80])
	return nil
}

// Block is a complete block: header + transactions.
type Block struct {
	Header       BlockHeader
	Transactions []Transaction
}

// Serialize writes the block in canonical format: header + varint(txcount) + txs.
func (b *Block) Serialize(w io.Writer) error {
	if err := b.Header.Serialize(w); err != nil {
		return err
	}
	if err := WriteVarInt(w, uint64(len(b.Transactions))); err != nil {
		return err
	}
	for i := range b.Transactions {
		if err := b.Transactions[i].Serialize(w); err != nil {
			return fmt.Errorf("serialize tx %d: %w", i, err)
		}
	}
	return nil
}

// SerializeToBytes returns the canonical byte representation of the full block.
func (b *Block) SerializeToBytes() ([]byte, error) {
	sw := &sliceWriter{buf: make([]byte, 0, 256)}
	if err := b.Serialize(sw); err != nil {
		return nil, err
	}
	return sw.buf, nil
}

// Deserialize reads a block from canonical format.
func (b *Block) Deserialize(r io.Reader) error {
	if err := b.Header.Deserialize(r); err != nil {
		return err
	}
	txCount, err := ReadVarInt(r)
	if err != nil {
		return fmt.Errorf("read tx count: %w", err)
	}
	if txCount > MaxBlockTxCount {
		return fmt.Errorf("block tx count %d exceeds max %d", txCount, MaxBlockTxCount)
	}
	b.Transactions = make([]Transaction, txCount)
	for i := uint64(0); i < txCount; i++ {
		if err := b.Transactions[i].Deserialize(r); err != nil {
			return fmt.Errorf("deserialize tx %d: %w", i, err)
		}
	}
	return nil
}

const MaxBlockTxCount = 50000
