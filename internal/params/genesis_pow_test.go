// Copyright (c) 2024-2026 The Fairchain Contributors
// Distributed under the MIT software license.

package params

import (
	"testing"

	"github.com/bams-repo/fairchain/internal/algorithms/sha256mem"
	"github.com/bams-repo/fairchain/internal/coinparams"
	"github.com/bams-repo/fairchain/internal/crypto"
)

func TestMainnetGenesisPoW(t *testing.T) {
	if coinparams.Algorithm != "sha256mem" {
		t.Skip("coinparams.Algorithm is not sha256mem")
	}
	h := sha256mem.New()
	if crypto.HashBlockHeader(&Mainnet.GenesisBlock.Header) != Mainnet.GenesisHash {
		t.Fatal("mainnet genesis identity hash mismatch")
	}
	pow := h.PoWHash(Mainnet.GenesisBlock.Header.SerializeToBytes())
	if err := crypto.ValidateProofOfWork(pow, Mainnet.GenesisBlock.Header.Bits); err != nil {
		t.Fatalf("mainnet genesis PoW: %v", err)
	}
}

func TestTestnetGenesisPoW(t *testing.T) {
	if coinparams.Algorithm != "sha256mem" {
		t.Skip("coinparams.Algorithm is not sha256mem")
	}
	h := sha256mem.New()
	if crypto.HashBlockHeader(&Testnet.GenesisBlock.Header) != Testnet.GenesisHash {
		t.Fatal("testnet genesis identity hash mismatch")
	}
	pow := h.PoWHash(Testnet.GenesisBlock.Header.SerializeToBytes())
	if err := crypto.ValidateProofOfWork(pow, Testnet.GenesisBlock.Header.Bits); err != nil {
		t.Fatalf("testnet genesis PoW: %v", err)
	}
}
