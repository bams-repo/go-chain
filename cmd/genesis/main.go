package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/bams-repo/fairchain/internal/algorithms"
	"github.com/bams-repo/fairchain/internal/coinparams"
	"github.com/bams-repo/fairchain/internal/consensus/pow"
	"github.com/bams-repo/fairchain/internal/difficulty"
	"github.com/bams-repo/fairchain/internal/crypto"
	fcparams "github.com/bams-repo/fairchain/internal/params"
	"github.com/bams-repo/fairchain/internal/types"
)

func main() {
	network := flag.String("network", "regtest", "Network name: mainnet, testnet, regtest")
	message := flag.String("message", coinparams.NameLower+" genesis", "Coinbase message for genesis block")
	timestamp := flag.Int64("timestamp", 0, "Unix timestamp (0 = now)")
	flag.Parse()

	p := fcparams.NetworkByName(*network)
	if p == nil {
		fmt.Fprintf(os.Stderr, "unknown network: %s\n", *network)
		os.Exit(1)
	}

	ts := uint32(time.Now().Unix())
	if *timestamp > 0 {
		ts = uint32(*timestamp)
	}

	cfg := fcparams.GenesisConfig{
		NetworkName:     p.Name,
		CoinbaseMessage: []byte(*message),
		Timestamp:       ts,
		Bits:            p.InitialBits,
		Version:         1,
		Reward:          p.InitialSubsidy,
		RewardScript:    []byte{0x00},
	}

	if p.Name == "testnet" {
		cfg.ExtraOutputs = []types.TxOutput{
			{
				Value:    fcparams.TestnetPremineAmount,
				PkScript: fcparams.TestnetBurnScript,
			},
		}
	}

	log.Printf("Building genesis block for %s...", p.Name)
	log.Printf("  Bits:      0x%08x", cfg.Bits)
	log.Printf("  Timestamp: %d", cfg.Timestamp)
	log.Printf("  Message:   %q", string(cfg.CoinbaseMessage))
	if len(cfg.ExtraOutputs) > 0 {
		log.Printf("  Extra outputs: %d", len(cfg.ExtraOutputs))
	}

	block := fcparams.BuildGenesisBlock(cfg)

	hasher, err := algorithms.GetHasher(coinparams.Algorithm)
	if err != nil {
		log.Fatalf("Unsupported PoW algorithm %q: %v", coinparams.Algorithm, err)
	}
	retargeter, err := difficulty.GetRetargeter(coinparams.DifficultyAlgorithm)
	if err != nil {
		log.Fatalf("Unsupported difficulty algorithm %q: %v", coinparams.DifficultyAlgorithm, err)
	}
	engine := pow.New(hasher, retargeter)

	log.Println("Mining genesis block (this may take a moment)...")
	start := time.Now()
	if err := engine.MineGenesis(&block); err != nil {
		log.Fatalf("Failed to mine genesis: %v", err)
	}
	elapsed := time.Since(start)

	hash := crypto.HashBlockHeader(&block.Header)

	log.Printf("Genesis block mined in %v", elapsed)
	log.Printf("  Hash:       %s", hash.ReverseString())
	log.Printf("  Nonce:      %d", block.Header.Nonce)
	log.Printf("  MerkleRoot: %s", block.Header.MerkleRoot.ReverseString())
	log.Printf("  Timestamp:  %d", block.Header.Timestamp)

	fmt.Println("\n// --- Genesis block Go definition ---")
	fmt.Printf("// Network: %s\n", p.Name)
	fmt.Printf("// Hash:    %s\n", hash.ReverseString())
	fmt.Printf("// Nonce:   %d\n", block.Header.Nonce)
	fmt.Printf("// Mined:   %s\n", time.Unix(int64(block.Header.Timestamp), 0).UTC())
	fmt.Printf("// Elapsed: %v\n", elapsed)
	fmt.Println("//")
	fmt.Printf("// Bits:       0x%08x\n", block.Header.Bits)
	fmt.Printf("// MerkleRoot: %s\n", block.Header.MerkleRoot)
	fmt.Printf("// Timestamp:  %d\n", block.Header.Timestamp)

	fmt.Println("\n// --- Paste into internal/params/networks.go ---")
	fmt.Println("")
	fmt.Println("GenesisBlock: types.Block{")
	fmt.Println("\tHeader: types.BlockHeader{")
	fmt.Printf("\t\tVersion:   %d,\n", block.Header.Version)
	fmt.Println("\t\tPrevBlock: types.ZeroHash,")
	fmt.Printf("\t\tMerkleRoot: %s,\n", formatHash(block.Header.MerkleRoot, 2))
	fmt.Printf("\t\tTimestamp: %d,\n", block.Header.Timestamp)
	fmt.Printf("\t\tBits:      0x%08x,\n", block.Header.Bits)
	fmt.Printf("\t\tNonce:     %d,\n", block.Header.Nonce)
	fmt.Println("\t},")
	fmt.Println("\tTransactions: []types.Transaction{{")
	tx := block.Transactions[0]
	fmt.Printf("\t\tVersion: %d,\n", tx.Version)
	fmt.Println("\t\tInputs: []types.TxInput{{")
	fmt.Println("\t\t\tPreviousOutPoint: types.CoinbaseOutPoint,")
	fmt.Printf("\t\t\tSignatureScript:  %s,\n", formatByteSlice(tx.Inputs[0].SignatureScript))
	fmt.Printf("\t\t\tSequence:         0x%08X,\n", tx.Inputs[0].Sequence)
	fmt.Println("\t\t}},")
	fmt.Println("\t\tOutputs: []types.TxOutput{")
	for _, out := range tx.Outputs {
		fmt.Println("\t\t\t{")
		fmt.Printf("\t\t\t\tValue:    %d,\n", out.Value)
		fmt.Printf("\t\t\t\tPkScript: %s,\n", formatByteSlice(out.PkScript))
		fmt.Println("\t\t\t},")
	}
	fmt.Println("\t\t},")
	fmt.Printf("\t\tLockTime: %d,\n", tx.LockTime)
	fmt.Println("\t}},")
	fmt.Println("},")
	fmt.Printf("GenesisHash: %s,\n", formatHash(hash, 0))
}

func formatHash(h types.Hash, indentLevel int) string {
	indent := ""
	for i := 0; i < indentLevel; i++ {
		indent += "\t"
	}
	innerIndent := indent + "\t"
	s := "types.Hash{\n"
	for row := 0; row < 4; row++ {
		s += innerIndent
		for col := 0; col < 8; col++ {
			i := row*8 + col
			if col < 7 {
				s += fmt.Sprintf("0x%02x, ", h[i])
			} else {
				s += fmt.Sprintf("0x%02x,", h[i])
			}
		}
		s += "\n"
	}
	s += indent + "}"
	return s
}

func formatByteSlice(b []byte) string {
	if len(b) <= 8 {
		s := "[]byte{"
		for i, v := range b {
			if i > 0 {
				s += ", "
			}
			s += fmt.Sprintf("0x%02x", v)
		}
		s += "}"
		return s
	}
	if isPrintable(b) {
		return fmt.Sprintf("[]byte(%q)", string(b))
	}
	s := "[]byte{\n"
	for i := 0; i < len(b); i += 8 {
		s += "\t\t\t\t"
		end := i + 8
		if end > len(b) {
			end = len(b)
		}
		for j := i; j < end; j++ {
			if j < end-1 {
				s += fmt.Sprintf("0x%02x, ", b[j])
			} else {
				s += fmt.Sprintf("0x%02x,", b[j])
			}
		}
		s += "\n"
	}
	s += "\t\t\t}"
	return s
}

func isPrintable(b []byte) bool {
	for _, c := range b {
		if c < 0x20 || c > 0x7e {
			return false
		}
	}
	return len(b) > 0
}
