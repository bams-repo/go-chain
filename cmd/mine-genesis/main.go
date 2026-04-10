package main

import (
	"fmt"
	"os"
	"time"

	"github.com/bams-repo/fairchain/internal/algorithms/sha256mem"
	"github.com/bams-repo/fairchain/internal/coinparams"
	"github.com/bams-repo/fairchain/internal/consensus/pow"
	"github.com/bams-repo/fairchain/internal/crypto"
	"github.com/bams-repo/fairchain/internal/difficulty"
	"github.com/bams-repo/fairchain/internal/params"
	"github.com/bams-repo/fairchain/internal/types"
)

func mineNetwork(name string, p *params.ChainParams) {
	hasher := sha256mem.New()
	retargeter, err := difficulty.GetRetargeter(coinparams.DifficultyAlgorithm)
	if err != nil {
		fmt.Fprintf(os.Stderr, "get retargeter: %v\n", err)
		os.Exit(1)
	}
	engine := pow.New(hasher, retargeter)

	genesis := p.GenesisBlock
	genesis.Header.Nonce = 0

	fmt.Printf("=== %s (bits=0x%08x) ===\n", name, genesis.Header.Bits)
	start := time.Now()

	if err := engine.MineGenesis(&genesis); err != nil {
		fmt.Fprintf(os.Stderr, "%s mine genesis: %v\n", name, err)
		os.Exit(1)
	}

	elapsed := time.Since(start)
	blockHash := crypto.HashBlockHeader(&genesis.Header)
	powHash := hasher.PoWHash(genesis.Header.SerializeToBytes())

	fmt.Printf("Done in %v\n", elapsed)
	fmt.Printf("Nonce:     %d\n", genesis.Header.Nonce)
	fmt.Printf("BlockHash: %s\n", blockHash.ReverseString())
	fmt.Printf("PoWHash:   %s\n", powHash.ReverseString())

	if err := crypto.ValidateProofOfWork(powHash, genesis.Header.Bits); err != nil {
		fmt.Fprintf(os.Stderr, "%s PoW validation FAILED: %v\n", name, err)
		os.Exit(1)
	}
	fmt.Printf("PoW:       PASS\n\n")

	fmt.Println("MerkleRoot:")
	printHash(genesis.Header.MerkleRoot)
	fmt.Printf("Nonce: %d\n\n", genesis.Header.Nonce)
	fmt.Println("GenesisHash:")
	printHash(blockHash)
	fmt.Println()
}

func main() {
	mineNetwork("mainnet", params.Mainnet)
	mineNetwork("testnet", params.Testnet)
}

func printHash(h types.Hash) {
	fmt.Println("types.Hash{")
	for row := 0; row < 4; row++ {
		fmt.Printf("\t0x%02x", h[row*8])
		for col := 1; col < 8; col++ {
			fmt.Printf(", 0x%02x", h[row*8+col])
		}
		fmt.Println(",")
	}
	fmt.Println("}")
}
