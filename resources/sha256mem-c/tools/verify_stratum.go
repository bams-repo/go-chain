// Quick verification: build header from stratum params using Fairchain's exact logic
package main

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
)

func doubleSHA256(data []byte) [32]byte {
	h1 := sha256.Sum256(data)
	return sha256.Sum256(h1[:])
}

func main() {
	if len(os.Args) < 9 {
		fmt.Fprintf(os.Stderr, "Usage: %s <prevhash_hex> <cb1_hex> <cb2_hex> <en1_hex> <en2_hex> <version_hex> <bits_hex> <ntime_hex>\n", os.Args[0])
		os.Exit(1)
	}

	prevhashHex := os.Args[1]
	cb1Hex := os.Args[2]
	cb2Hex := os.Args[3]
	en1Hex := os.Args[4]
	en2Hex := os.Args[5]
	versionHex := os.Args[6]
	bitsHex := os.Args[7]
	ntimeHex := os.Args[8]

	// Decode prevhash: undo 4-byte-group swap (stratumPrevhashHex encoding)
	prevhashRaw, _ := hex.DecodeString(prevhashHex)
	var prevhash [32]byte
	for i := 0; i < 32; i += 4 {
		prevhash[i+0] = prevhashRaw[i+3]
		prevhash[i+1] = prevhashRaw[i+2]
		prevhash[i+2] = prevhashRaw[i+1]
		prevhash[i+3] = prevhashRaw[i+0]
	}

	// Assemble coinbase
	cb1, _ := hex.DecodeString(cb1Hex)
	cb2, _ := hex.DecodeString(cb2Hex)
	en1, _ := hex.DecodeString(en1Hex)
	en2, _ := hex.DecodeString(en2Hex)
	coinbase := make([]byte, 0, len(cb1)+len(en1)+len(en2)+len(cb2))
	coinbase = append(coinbase, cb1...)
	coinbase = append(coinbase, en1...)
	coinbase = append(coinbase, en2...)
	coinbase = append(coinbase, cb2...)

	// Coinbase hash
	cbHash := doubleSHA256(coinbase)

	// Merkle root (no branches → cbHash is the root)
	// In Fairchain: types.Hash stores LE (reversed). Reversed() → header.
	// But cbHash from doubleSHA256 is raw SHA256 output (same as types.Hash.Reversed()).
	// For header: merkleRootBE = merkleRoot.Reversed()
	// If merkleRoot = types.Hash(cbHash reversed), then merkleRootBE = cbHash.
	// Wait — crypto.HashTransaction returns types.Hash which is cbHash bytes REVERSED.
	// Then computeMerkleRootFromBranch (no branches) = coinbaseHash = reversed.
	// Then .Reversed() = original cbHash bytes.
	// So header merkle field = cbHash (raw double-SHA256 output).
	merkleRoot := cbHash

	// Decode scalars
	versionBytes, _ := hex.DecodeString(versionHex)
	version := binary.BigEndian.Uint32(versionBytes)
	bitsBytes, _ := hex.DecodeString(bitsHex)
	bits := binary.BigEndian.Uint32(bitsBytes)
	ntimeBytes, _ := hex.DecodeString(ntimeHex)
	ntime := binary.BigEndian.Uint32(ntimeBytes)

	// Build 80-byte header
	var header [80]byte
	binary.LittleEndian.PutUint32(header[0:4], version)
	copy(header[4:36], prevhash[:])
	copy(header[36:68], merkleRoot[:])
	binary.LittleEndian.PutUint32(header[68:72], ntime)
	binary.LittleEndian.PutUint32(header[72:76], bits)
	binary.LittleEndian.PutUint32(header[76:80], 0) // nonce=0

	fmt.Printf("Header (nonce=0): %s\n", hex.EncodeToString(header[:]))
	fmt.Printf("Coinbase hash:    %s\n", hex.EncodeToString(cbHash[:]))
	fmt.Printf("Prevhash (LE):    %s\n", hex.EncodeToString(prevhash[:]))
	fmt.Printf("Merkle root:      %s\n", hex.EncodeToString(merkleRoot[:]))

	// Also try: merkle root = cbHash REVERSED (if types.Hash stores it differently)
	var merkleRootAlt [32]byte
	for i := 0; i < 32; i++ {
		merkleRootAlt[i] = cbHash[31-i]
	}
	var headerAlt [80]byte
	copy(headerAlt[:], header[:])
	copy(headerAlt[36:68], merkleRootAlt[:])
	fmt.Printf("\nAlt header (merkle reversed): %s\n", hex.EncodeToString(headerAlt[:]))

	// SHA256 seed of both headers
	seed1 := sha256.Sum256(header[:])
	seed2 := sha256.Sum256(headerAlt[:])
	fmt.Printf("\nSHA256(header):     %s\n", hex.EncodeToString(seed1[:]))
	fmt.Printf("SHA256(header_alt): %s\n", hex.EncodeToString(seed2[:]))
}
