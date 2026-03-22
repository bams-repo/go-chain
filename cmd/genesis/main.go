// Copyright (c) 2024-2026 The Fairchain Contributors
// Fairchain is an experiment in modularity, designed to improve on the work
// of Satoshi Nakamoto and to inspire more creative genius in the space.
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bams-repo/fairchain/internal/algorithms"
	"github.com/bams-repo/fairchain/internal/coinparams"
	"github.com/bams-repo/fairchain/internal/crypto"
	fcparams "github.com/bams-repo/fairchain/internal/params"
	"github.com/bams-repo/fairchain/internal/types"
)

func main() {
	network := flag.String("network", "regtest", "Network name: mainnet, testnet, regtest")
	message := flag.String("message", coinparams.NameLower+" genesis", "Coinbase message for genesis block")
	timestamp := flag.Int64("timestamp", 0, "Unix timestamp (0 = now)")
	threads := flag.Int("threads", runtime.NumCPU(), "Number of mining threads")
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
	log.Printf("  Threads:   %d", *threads)
	if len(cfg.ExtraOutputs) > 0 {
		log.Printf("  Extra outputs: %d", len(cfg.ExtraOutputs))
	}

	block := fcparams.BuildGenesisBlock(cfg)

	merkle, err := crypto.ComputeMerkleRoot(block.Transactions)
	if err != nil {
		log.Fatalf("compute merkle root: %v", err)
	}
	block.Header.MerkleRoot = merkle

	target := crypto.CompactToHash(block.Header.Bits)

	log.Println("Mining genesis block...")

	nThreads := *threads
	if nThreads < 1 {
		nThreads = 1
	}

	var totalHashes atomic.Uint64
	var found atomic.Bool
	var winnerMu sync.Mutex
	var winnerHeader types.BlockHeader

	start := time.Now()

	// Hashrate reporter
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		var lastCount uint64
		var lastTime time.Time = start
		for range ticker.C {
			if found.Load() {
				return
			}
			now := time.Now()
			count := totalHashes.Load()
			dt := now.Sub(lastTime).Seconds()
			dh := count - lastCount
			rate := float64(dh) / dt
			log.Printf("  [mining] %d hashes | %.1f H/s | elapsed %v",
				count, rate, now.Sub(start).Truncate(time.Second))
			lastCount = count
			lastTime = now
		}
	}()

	// Split the 32-bit nonce space across threads.
	// Each thread gets a contiguous range.
	rangeSize := uint64(math.MaxUint32+1) / uint64(nThreads)

	var wg sync.WaitGroup
	for t := 0; t < nThreads; t++ {
		wg.Add(1)
		startNonce := uint32(uint64(t) * rangeSize)
		endNonce := uint32(uint64(t+1)*rangeSize - 1)
		if t == nThreads-1 {
			endNonce = math.MaxUint32
		}

		go func(threadID int, nonceStart, nonceEnd uint32) {
			defer wg.Done()

			hasher, err := algorithms.GetHasher(coinparams.Algorithm)
			if err != nil {
				log.Printf("thread %d: failed to get hasher: %v", threadID, err)
				return
			}

			hdr := block.Header
			hdr.Nonce = nonceStart
			var localCount uint64

			for nonce := nonceStart; ; nonce++ {
				if found.Load() {
					return
				}

				hdr.Nonce = nonce
				hash := hasher.PoWHash(hdr.SerializeToBytes())
				localCount++

				if localCount%500 == 0 {
					totalHashes.Add(500)
				}

				if hash.LessOrEqual(target) {
					if found.CompareAndSwap(false, true) {
						totalHashes.Add(localCount % 500)
						winnerMu.Lock()
						winnerHeader = hdr
						winnerMu.Unlock()
						log.Printf("  [thread %d] FOUND at nonce %d", threadID, nonce)
					}
					return
				}

				if nonce == nonceEnd {
					totalHashes.Add(localCount % 500)
					return
				}
			}
		}(t, startNonce, endNonce)
	}

	wg.Wait()
	elapsed := time.Since(start)

	if !found.Load() {
		log.Fatal("Nonce space exhausted without finding valid genesis")
	}

	block.Header = winnerHeader
	hash := crypto.HashBlockHeader(&block.Header)
	total := totalHashes.Load()

	// Compute final hash to verify
	powHash := func() types.Hash {
		hasher, _ := algorithms.GetHasher(coinparams.Algorithm)
		return hasher.PoWHash(block.Header.SerializeToBytes())
	}()
	powLE := types.Hash{}
	for i := 0; i < 32; i++ {
		powLE[i] = powHash[31-i]
	}
	_ = powLE

	log.Printf("Genesis block mined in %v", elapsed)
	log.Printf("  Hash:       %s", hash.ReverseString())
	log.Printf("  Nonce:      %d", block.Header.Nonce)
	log.Printf("  MerkleRoot: %s", block.Header.MerkleRoot.ReverseString())
	log.Printf("  Timestamp:  %d", block.Header.Timestamp)
	log.Printf("  Total hashes: %d", total)
	log.Printf("  Avg hashrate: %.1f H/s (%d threads)", float64(total)/elapsed.Seconds(), nThreads)

	// Verify
	if err := crypto.ValidateProofOfWork(powHash, block.Header.Bits); err != nil {
		log.Fatalf("VERIFICATION FAILED: %v", err)
	}
	log.Println("  Verification: OK")

	fmt.Println("\n// --- Genesis block Go definition ---")
	fmt.Printf("// Network: %s\n", p.Name)
	fmt.Printf("// Hash:    %s\n", hash.ReverseString())
	fmt.Printf("// PoW:     %s\n", types.Hash(powHash).ReverseString())
	fmt.Printf("// Nonce:   %d\n", block.Header.Nonce)
	fmt.Printf("// Mined:   %s\n", time.Unix(int64(block.Header.Timestamp), 0).UTC())
	fmt.Printf("// Elapsed: %v (%d threads, %.1f H/s)\n", elapsed, nThreads, float64(total)/elapsed.Seconds())
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

	// Also print the raw LE bytes for easy pasting
	fmt.Println("")
	fmt.Println("// GenesisHash bytes (internal byte order):")
	fmt.Printf("//   %x\n", hash)
	fmt.Println("// GenesisHash display (RPC/explorer order):")
	fmt.Printf("//   %s\n", hash.ReverseString())

	// Print nonce in hex too for reference
	var nonceBuf [4]byte
	binary.LittleEndian.PutUint32(nonceBuf[:], block.Header.Nonce)
	fmt.Printf("// Nonce: %d (0x%08x)\n", block.Header.Nonce, block.Header.Nonce)
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
