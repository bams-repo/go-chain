// Copyright (c) 2024-2026 The Fairchain Contributors
// Fairchain is an experiment in modularity, designed to improve on the work
// of Satoshi Nakamoto and to inspire more creative genius in the space.
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

package crypto_test

import (
	"fmt"
	"testing"
	"github.com/bams-repo/fairchain/internal/crypto"
)

func TestCheckTarget(t *testing.T) {
	// Testnet InitialBits (see params.Testnet)
	bits := uint32(0x2000ea44)
	target := crypto.CompactToHash(bits)
	fmt.Printf("Bits 0x%08x -> target: %s\n", bits, target)
	
	// MinBits for testnet
	minBits := uint32(0x207fffff)
	minTarget := crypto.CompactToHash(minBits)
	fmt.Printf("MinBits 0x%08x -> target: %s\n", minBits, minTarget)
	
	// Check various bits values
	for _, b := range []uint32{0x1d00ffff, 0x1e00ffff, 0x1f00ffff, 0x1d0fffff, 0x1e0fffff, 0x1f0fffff, 0x1e03ffff, 0x1f03ffff, 0x2003ffff, 0x1f07ffff, 0x1e07ffff} {
		tgt := crypto.CompactToHash(b)
		fmt.Printf("Bits 0x%08x -> target: %s\n", b, tgt)
	}
	
	// Also check what the big int looks like
	big := crypto.CompactToBig(0x1f03ffff)
	fmt.Printf("\nBits 0x1f03ffff -> big: %s\n", big.Text(16))
	big2 := crypto.CompactToBig(0x1f07ffff)
	fmt.Printf("Bits 0x1f07ffff -> big: %s\n", big2.Text(16))
}
