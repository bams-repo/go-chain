package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math/big"
	"os"

	"github.com/bams-repo/fairchain/internal/algorithms/sha256mem"
	"github.com/bams-repo/fairchain/internal/crypto"
	"github.com/bams-repo/fairchain/internal/types"
)

func main() {
	if len(os.Args) < 9 {
		fmt.Fprintf(os.Stderr, "Usage: %s <prevhash_hex> <cb1_hex> <cb2_hex> <en1_hex> <en2_hex> <version_hex> <nbits_hex> <ntime_hex> [nonce_hex]\n", os.Args[0])
		os.Exit(1)
	}

	prevhashHex := os.Args[1]
	cb1Hex := os.Args[2]
	cb2Hex := os.Args[3]
	en1Hex := os.Args[4]
	en2Hex := os.Args[5]
	versionHex := os.Args[6]
	nbitsHex := os.Args[7]
	ntimeHex := os.Args[8]

	nonceHex := "00000000"
	if len(os.Args) > 9 {
		nonceHex = os.Args[9]
	}

	// Decode prevhash: default undoes Fairchain's 4-byte-group stratum swap.
	// Set PREVHASH_RAW=1 to test pools that use the notify bytes directly.
	prevhashRaw, _ := hex.DecodeString(prevhashHex)
	var prevBlock types.Hash
	if os.Getenv("PREVHASH_RAW") == "1" {
		copy(prevBlock[:], prevhashRaw)
	} else {
		for i := 0; i < 32; i += 4 {
			prevBlock[i+0] = prevhashRaw[i+3]
			prevBlock[i+1] = prevhashRaw[i+2]
			prevBlock[i+2] = prevhashRaw[i+1]
			prevBlock[i+3] = prevhashRaw[i+0]
		}
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

	fmt.Printf("Coinbase (%d bytes): %s\n", len(coinbase), hex.EncodeToString(coinbase))

	// Compute coinbase hash using Fairchain's exact method
	var coinbaseTx types.Transaction
	if err := coinbaseTx.Deserialize(bytes.NewReader(coinbase)); err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: coinbase deserialize failed: %v\n", err)
		fmt.Fprintf(os.Stderr, "Falling back to direct double-SHA256 of raw bytes\n")
		cbHash := crypto.DoubleSHA256(coinbase)
		fmt.Printf("coinbaseHash (LE): %s\n", hex.EncodeToString(cbHash[:]))
		merkleRootBE := cbHash.Reversed()
		fmt.Printf("merkleRoot (BE):   %s\n", hex.EncodeToString(merkleRootBE[:]))
		buildAndHash(prevBlock, merkleRootBE, versionHex, nbitsHex, ntimeHex, nonceHex)
		return
	}

	coinbaseHash, _ := crypto.HashTransaction(&coinbaseTx)
	fmt.Printf("coinbaseHash (LE): %s\n", hex.EncodeToString(coinbaseHash[:]))

	// No merkle branches for this test
	merkleRoot := coinbaseHash
	merkleRootBE := merkleRoot.Reversed()
	if os.Getenv("MERKLE_RAW") == "1" {
		merkleRootBE = merkleRoot
	}
	fmt.Printf("merkleRoot (BE):   %s\n", hex.EncodeToString(merkleRootBE[:]))

	buildAndHash(prevBlock, merkleRootBE, versionHex, nbitsHex, ntimeHex, nonceHex)
}

func buildAndHash(prevBlock types.Hash, merkleRootBE types.Hash, versionHex, nbitsHex, ntimeHex, nonceHex string) {
	versionBytes, _ := hex.DecodeString(versionHex)
	version := binary.BigEndian.Uint32(versionBytes)
	bitsBytes, _ := hex.DecodeString(nbitsHex)
	bits := binary.BigEndian.Uint32(bitsBytes)
	ntimeBytes, _ := hex.DecodeString(ntimeHex)
	ntime := binary.BigEndian.Uint32(ntimeBytes)
	nonceBytes, _ := hex.DecodeString(nonceHex)
	nonce := binary.LittleEndian.Uint32(nonceBytes)

	header := types.BlockHeader{
		Version:    version,
		PrevBlock:  prevBlock,
		MerkleRoot: merkleRootBE,
		Timestamp:  ntime,
		Bits:       bits,
		Nonce:      nonce,
	}

	var hdrBuf [types.BlockHeaderSize]byte
	header.SerializeInto(hdrBuf[:])

	fmt.Printf("\nHeader (%d bytes): %s\n", len(hdrBuf), hex.EncodeToString(hdrBuf[:]))
	fmt.Printf("  version=%d ntime=%d bits=0x%08x nonce=%d\n", version, ntime, bits, nonce)
	fmt.Printf("  prevBlock(LE): %s\n", hex.EncodeToString(prevBlock[:]))

	// SHA256 seed
	seed := sha256.Sum256(hdrBuf[:])
	fmt.Printf("  SHA256(header):  %s\n", hex.EncodeToString(seed[:]))

	// sha256d (double SHA256)
	sha256d := sha256.Sum256(seed[:])
	fmt.Printf("  sha256d(header): %s\n", hex.EncodeToString(sha256d[:]))

	// Full sha256mem PoW hash
	hasher := &sha256mem.Hasher{}
	powHash := hasher.PoWHash(hdrBuf[:])
	fmt.Printf("  PoWHash:         %s\n", hex.EncodeToString(powHash[:]))

	// Show as raw hash for difficulty comparison
	rawHash := powHash.Reversed()
	fmt.Printf("  rawHash:         %s\n", hex.EncodeToString(rawHash[:]))

	// Compute difficulty with standard Bitcoin formula for each hash
	diff1BE, _ := hex.DecodeString("00000000FFFF0000000000000000000000000000000000000000000000000000")
	diff1Num := new(big.Int).SetBytes(diff1BE)

	fmt.Printf("\n  Difficulty calculations (standard Bitcoin diff1):\n")

	// SHA256 difficulty (treating as BE number)
	seedNum := new(big.Int).SetBytes(seed[:])
	if seedNum.Sign() > 0 {
		seedDiff := new(big.Float).Quo(new(big.Float).SetInt(diff1Num), new(big.Float).SetInt(seedNum))
		f, _ := seedDiff.Float64()
		fmt.Printf("    SHA256 diff:  %e\n", f)
	}

	// sha256d difficulty
	sha256dNum := new(big.Int).SetBytes(sha256d[:])
	if sha256dNum.Sign() > 0 {
		sha256dDiff := new(big.Float).Quo(new(big.Float).SetInt(diff1Num), new(big.Float).SetInt(sha256dNum))
		f, _ := sha256dDiff.Float64()
		fmt.Printf("    sha256d diff: %e\n", f)
	}

	// sha256mem difficulty (using rawHash as BE number)
	rawHashNum := new(big.Int).SetBytes(rawHash[:])
	if rawHashNum.Sign() > 0 {
		memDiff := new(big.Float).Quo(new(big.Float).SetInt(diff1Num), new(big.Float).SetInt(rawHashNum))
		f, _ := memDiff.Float64()
		fmt.Printf("    sha256mem diff: %e\n", f)
	}

	// sha256mem displayed hash difficulty (PoWHash bytes treated as BE)
	powHashNum := new(big.Int).SetBytes(powHash[:])
	if powHashNum.Sign() > 0 {
		powDiff := new(big.Float).Quo(new(big.Float).SetInt(diff1Num), new(big.Float).SetInt(powHashNum))
		f, _ := powDiff.Float64()
		fmt.Printf("    PoWHash(display) diff: %e\n", f)
	}
}
