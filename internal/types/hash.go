// Copyright (c) 2024-2026 The Fairchain Contributors
// Fairchain is an experiment in modularity, designed to improve on the work
// of Satoshi Nakamoto and to inspire more creative genius in the space.
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

package types

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
)

// HashSize is the fixed size of a consensus hash (SHA-256d → 32 bytes).
const HashSize = 32

// Hash is a 32-byte double-SHA-256 digest used throughout the consensus layer.
// It is stored in internal byte order (little-endian, same as Bitcoin).
type Hash [HashSize]byte

// ZeroHash is the all-zero hash, used as the previous-block reference in genesis.
var ZeroHash Hash

// HashFromBytes creates a Hash from a byte slice. Panics if len != HashSize.
func HashFromBytes(b []byte) Hash {
	if len(b) != HashSize {
		panic(fmt.Sprintf("types: HashFromBytes requires exactly %d bytes, got %d", HashSize, len(b)))
	}
	var h Hash
	copy(h[:], b)
	return h
}

// HashFromHex parses a hex-encoded hash string (64 chars, internal byte order).
func HashFromHex(s string) (Hash, error) {
	b, err := hex.DecodeString(s)
	if err != nil {
		return ZeroHash, fmt.Errorf("types: invalid hex hash: %w", err)
	}
	if len(b) != HashSize {
		return ZeroHash, fmt.Errorf("types: hex hash must be %d bytes, got %d", HashSize, len(b))
	}
	return HashFromBytes(b), nil
}

// HashFromReverseHex parses a hex-encoded hash in display byte order (reversed,
// as shown by block explorers) and converts to internal byte order.
func HashFromReverseHex(s string) (Hash, error) {
	b, err := hex.DecodeString(s)
	if err != nil {
		return ZeroHash, fmt.Errorf("types: invalid hex hash: %w", err)
	}
	if len(b) != HashSize {
		return ZeroHash, fmt.Errorf("types: hex hash must be %d bytes, got %d", HashSize, len(b))
	}
	// Reverse to internal byte order.
	for i, j := 0, len(b)-1; i < j; i, j = i+1, j-1 {
		b[i], b[j] = b[j], b[i]
	}
	return HashFromBytes(b), nil
}

// String returns the hash as a hex string in internal byte order.
func (h Hash) String() string {
	return hex.EncodeToString(h[:])
}

// ReverseString returns the hash as a hex string in display byte order
// (reversed, matching the convention used in block explorers).
func (h Hash) ReverseString() string {
	var rev [HashSize]byte
	for i := 0; i < HashSize; i++ {
		rev[i] = h[HashSize-1-i]
	}
	return hex.EncodeToString(rev[:])
}

// IsZero returns true if the hash is all zeros.
func (h Hash) IsZero() bool {
	return h == ZeroHash
}

// Less returns true if h < other when compared as 256-bit little-endian integers.
// Used for target comparison in PoW validation.
func (h Hash) Less(other Hash) bool {
	// Compare from the most significant byte (index 31) down to 0.
	for i := HashSize - 1; i >= 0; i-- {
		if h[i] < other[i] {
			return true
		}
		if h[i] > other[i] {
			return false
		}
	}
	return false
}

// LessOrEqual returns true if h <= other.
func (h Hash) LessOrEqual(other Hash) bool {
	return h == other || h.Less(other)
}

// Reversed returns a copy of the hash with bytes in reverse order.
// Use this to convert between big-endian algorithm output and the
// little-endian internal byte order expected by Less / LessOrEqual.
func (h Hash) Reversed() Hash {
	var r Hash
	for i := 0; i < HashSize; i++ {
		r[i] = h[HashSize-1-i]
	}
	return r
}

// Encode writes the hash to a byte slice in canonical order.
func (h Hash) Encode(buf []byte) {
	copy(buf[:HashSize], h[:])
}

// PutUint32LE is a helper to write a uint32 in little-endian to a buffer.
func PutUint32LE(buf []byte, v uint32) {
	binary.LittleEndian.PutUint32(buf, v)
}

// PutUint64LE is a helper to write a uint64 in little-endian to a buffer.
func PutUint64LE(buf []byte, v uint64) {
	binary.LittleEndian.PutUint64(buf, v)
}

// ReadUint32LE reads a uint32 from a buffer in little-endian.
func ReadUint32LE(buf []byte) uint32 {
	return binary.LittleEndian.Uint32(buf)
}

// ReadUint64LE reads a uint64 from a buffer in little-endian.
func ReadUint64LE(buf []byte) uint64 {
	return binary.LittleEndian.Uint64(buf)
}
