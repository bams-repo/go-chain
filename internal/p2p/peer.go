// Copyright (c) 2024-2026 The Fairchain Contributors
// Fairchain is an experiment in modularity, designed to improve on the work
// of Satoshi Nakamoto and to inspire more creative genius in the space.
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

package p2p

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bams-repo/fairchain/internal/protocol"
	"github.com/bams-repo/fairchain/internal/types"
)

// Peer represents a connected remote node.
// Fields are grouped by function: identity, protocol state, liveness,
// misbehavior scoring, rate limiting, and inventory deduplication.
type Peer struct {
	conn    net.Conn
	addr    string
	inbound bool
	magic   [4]byte
	reader  *bufio.Reader
	id      int32  // unique peer ID assigned by the Manager
	connType string // "inbound", "outbound-full-relay", or "manual"

	mu      sync.Mutex
	version *protocol.VersionMsg

	// Liveness tracking (Bitcoin Core parity: BIP 31 ping/pong).
	connectedAt    time.Time
	lastRecv       atomic.Int64 // unix timestamp of last message received
	lastSend       atomic.Int64 // unix timestamp of last message sent
	pingNonce      uint64       // nonce of the outstanding ping (0 = none pending)
	pingSent       time.Time    // when the outstanding ping was sent
	lastPong       time.Time    // when the last valid pong was received
	pingLatency    time.Duration
	minPingLatency time.Duration // best observed ping

	// Block/tx relay timestamps (Bitcoin Core parity).
	lastBlockTime atomic.Int64 // unix timestamp of last valid block received from this peer
	lastTxTime    atomic.Int64 // unix timestamp of last valid tx received from this peer

	// Sync state: last common header/block heights with this peer.
	syncedHeaders atomic.Int32
	syncedBlocks  atomic.Int32

	// BIP 130: peer prefers block announcements via headers instead of inv.
	prefersHeaders int32 // atomic: 1 = peer sent sendheaders

	// Headers-per-window DoS tracking: sliding window counter that resets
	// every headerWindowDuration. Bitcoin Core only limits per-message count
	// (MaxHeadersPerMsg); this provides additional protection against peers
	// that send an unreasonable volume of unsolicited header messages.
	headersReceived int32     // headers received in current window
	headerWindowStart time.Time // start of current sliding window

	// Bitcoin Core parity (ConsiderEviction): highest header height this peer
	// has actually proven by delivering valid headers. Distinct from BestHeight
	// which is the self-reported version message height.
	provenHeaderHeight atomic.Uint32

	// Chain-sync eviction state: deadline by which the peer must prove its
	// claimed chain with headers, and whether a final getheaders was sent.
	chainSyncDeadline time.Time
	chainSyncWarned   bool

	// Misbehavior scoring (Bitcoin Core parity: ban at 100).
	banScore int32 // atomic-style but guarded by mu for compound ops

	// Rate limiting: sliding window message counter.
	msgCount   int32     // messages received in current window
	windowStart time.Time // start of current rate-limit window

	// Address gossip: Bitcoin Core only responds to one getaddr per connection.
	getaddrResponded bool

	// Per-peer getheaders throttle: max 1 response per 2 seconds.
	lastGetHeadersResp time.Time

	// Per-peer getblocks throttle during IBD.
	lastGetBlocksResp time.Time

	// Inventory deduplication — bounded FIFO to prevent unbounded growth.
	knownInv  *boundedHashSet
	sendQueue chan outMsg

	// Bytes transferred.
	bytesRecv atomic.Int64
	bytesSent atomic.Int64

	done chan struct{}
}

type outMsg struct {
	cmd     string
	payload []byte
}

const (
	peerSendQueueSize = 1024
	readTimeout       = 5 * time.Minute
	writeTimeout      = 30 * time.Second

	// Bitcoin Core parity: ping every 2 minutes.
	PingInterval = 2 * time.Minute
	// Bitcoin Core parity: disconnect if no pong within 20 minutes.
	PongTimeout = 20 * time.Minute

	// Misbehavior threshold — peer is banned when score reaches this.
	BanThreshold int32 = 100
	// Duration of an IP ban.
	BanDuration = 24 * time.Hour

	// Rate limiting: max messages per window.
	RateLimitWindow   = 10 * time.Second
	RateLimitMaxMsgs  = 500

	// Per-peer header DoS: max headers received in a sliding window.
	// Generous enough for full IBD (2000 headers/batch × many batches)
	// but catches runaway request loops. Window resets every 20 minutes.
	headerWindowDuration = 20 * time.Minute
	headerWindowMax      = 500_000

	// MinPeerProtoVersion is the lowest wire protocol version we accept.
	MinPeerProtoVersion uint32 = 6

	// MaxPeerStartHeight rejects peers advertising an implausibly high
	// chain height, preventing an attacker from forcing permanent IBD mode.
	MaxPeerStartHeight uint32 = 100_000_000
)

// NewPeer wraps a connection into a Peer.
func NewPeer(conn net.Conn, inbound bool, magic [4]byte) *Peer {
	now := time.Now()
	connType := "outbound-full-relay"
	if inbound {
		connType = "inbound"
	}
	p := &Peer{
		conn:        conn,
		addr:        conn.RemoteAddr().String(),
		inbound:     inbound,
		magic:       magic,
		reader:      bufio.NewReader(conn),
		connType:    connType,
		connectedAt: now,
		lastPong:    now,
		windowStart: now,
		knownInv:    newBoundedHashSet(10000),
		sendQueue:   make(chan outMsg, peerSendQueueSize),
		done:        make(chan struct{}),
	}
	p.lastRecv.Store(now.Unix())
	p.lastSend.Store(now.Unix())
	p.syncedHeaders.Store(-1)
	p.syncedBlocks.Store(-1)
	return p
}

// --- Identity accessors ---

func (p *Peer) Addr() string    { return p.addr }
func (p *Peer) IsInbound() bool { return p.inbound }

func (p *Peer) Version() *protocol.VersionMsg {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.version == nil {
		return nil
	}
	v := *p.version
	return &v
}

func (p *Peer) SetVersion(v *protocol.VersionMsg) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if v == nil {
		p.version = nil
		return
	}
	cp := *v
	p.version = &cp
}

func (p *Peer) SetStartHeightIfGreater(height uint32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.version == nil {
		return
	}
	if height > p.version.StartHeight {
		p.version.StartHeight = height
	}
}

// BestHeight returns the highest block height known for this peer,
// incorporating both the initial handshake height and any updates
// received via block relay.
func (p *Peer) BestHeight() uint32 {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.version == nil {
		return 0
	}
	return p.version.StartHeight
}

// ProvenHeaderHeight returns the highest header height this peer has proven
// by delivering valid headers (as opposed to self-reported BestHeight).
func (p *Peer) ProvenHeaderHeight() uint32 { return p.provenHeaderHeight.Load() }

// SetProvenHeaderHeightIfGreater atomically updates the proven header height
// if the new value exceeds the current one.
func (p *Peer) SetProvenHeaderHeightIfGreater(h uint32) {
	for {
		old := p.provenHeaderHeight.Load()
		if h <= old {
			return
		}
		if p.provenHeaderHeight.CompareAndSwap(old, h) {
			return
		}
	}
}

// --- Liveness ---

func (p *Peer) ConnectedAt() time.Time     { return p.connectedAt }
func (p *Peer) LastRecv() time.Time         { return time.Unix(p.lastRecv.Load(), 0) }
func (p *Peer) LastSend() time.Time         { return time.Unix(p.lastSend.Load(), 0) }
func (p *Peer) PingLatency() time.Duration  { return p.pingLatency }

func (p *Peer) stampRecv() { p.lastRecv.Store(time.Now().Unix()) }
func (p *Peer) stampSend() { p.lastSend.Store(time.Now().Unix()) }

// SetPingNonce records that a ping with the given nonce was sent.
func (p *Peer) SetPingNonce(nonce uint64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pingNonce = nonce
	p.pingSent = time.Now()
}

// HandlePong processes an incoming pong. Returns true if the nonce matched.
func (p *Peer) HandlePong(nonce uint64) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.pingNonce == 0 || nonce != p.pingNonce {
		return false
	}
	p.pingLatency = time.Since(p.pingSent)
	if p.minPingLatency == 0 || p.pingLatency < p.minPingLatency {
		p.minPingLatency = p.pingLatency
	}
	p.lastPong = time.Now()
	p.pingNonce = 0
	return true
}

// PongOverdue returns true if a ping is outstanding and the pong timeout
// has elapsed — matching Bitcoin Core's 20-minute pong deadline.
func (p *Peer) PongOverdue() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.pingNonce == 0 {
		return false
	}
	return time.Since(p.pingSent) > PongTimeout
}

// LastPong returns when the last valid pong was received.
func (p *Peer) LastPong() time.Time {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastPong
}

// --- Peer identity ---

func (p *Peer) ID() int32        { return p.id }
func (p *Peer) SetID(id int32)   { p.id = id }
func (p *Peer) ConnType() string { return p.connType }
func (p *Peer) SetConnType(t string) { p.connType = t }

// StampLastBlock records that a valid block was received from this peer.
func (p *Peer) StampLastBlock() { p.lastBlockTime.Store(time.Now().Unix()) }

// StampLastTx records that a valid transaction was received from this peer.
func (p *Peer) StampLastTx() { p.lastTxTime.Store(time.Now().Unix()) }

// SetSyncedHeaders records the last common header height with this peer.
func (p *Peer) SetSyncedHeaders(h int32) { p.syncedHeaders.Store(h) }

// SetSyncedBlocks records the last common block height with this peer.
func (p *Peer) SetSyncedBlocks(h int32) { p.syncedBlocks.Store(h) }

// SetPrefersHeaders marks whether this peer prefers header announcements (BIP 130).
func (p *Peer) SetPrefersHeaders(v bool) {
	if v {
		atomic.StoreInt32(&p.prefersHeaders, 1)
	} else {
		atomic.StoreInt32(&p.prefersHeaders, 0)
	}
}

// PrefersHeaders returns true if the peer sent sendheaders (BIP 130).
func (p *Peer) PrefersHeaders() bool {
	return atomic.LoadInt32(&p.prefersHeaders) == 1
}

// AddHeadersReceived adds n to the sliding-window header counter and returns
// the current window total. The window resets every headerWindowDuration.
func (p *Peer) AddHeadersReceived(n int32) int32 {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	if now.Sub(p.headerWindowStart) > headerWindowDuration {
		p.headersReceived = 0
		p.headerWindowStart = now
	}
	p.headersReceived += n
	return p.headersReceived
}

// HeadersReceived returns the headers received in the current window.
func (p *Peer) HeadersReceived() int32 {
	p.mu.Lock()
	defer p.mu.Unlock()
	if time.Since(p.headerWindowStart) > headerWindowDuration {
		return 0
	}
	return p.headersReceived
}

// --- Misbehavior scoring ---

// AddBanScore increases the peer's misbehavior score. Returns the new score.
func (p *Peer) AddBanScore(delta int32) int32 {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.banScore += delta
	return p.banScore
}

// BanScore returns the current misbehavior score.
func (p *Peer) BanScore() int32 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.banScore
}

// --- Rate limiting ---

// CheckRateLimit returns true if the peer is within acceptable message rates.
// Returns false if the peer is flooding (should be penalized/disconnected).
func (p *Peer) CheckRateLimit() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	if now.Sub(p.windowStart) > RateLimitWindow {
		p.msgCount = 0
		p.windowStart = now
	}
	p.msgCount++
	return p.msgCount <= int32(RateLimitMaxMsgs)
}

// --- Addr gossip ---

// MarkGetAddrResponded marks that we've responded to a getaddr from this peer.
// Returns true if this is the first getaddr (should respond), false if already responded.
func (p *Peer) MarkGetAddrResponded() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.getaddrResponded {
		return false
	}
	p.getaddrResponded = true
	return true
}

// CheckGetHeadersThrottle returns true if enough time has passed since the
// last getheaders response to this peer. Updates the timestamp on success.
func (p *Peer) CheckGetHeadersThrottle() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if time.Since(p.lastGetHeadersResp) < 2*time.Second {
		return false
	}
	p.lastGetHeadersResp = time.Now()
	return true
}

// CheckGetBlocksThrottle returns true if enough time has passed since the
// last getblocks response to this peer. Used during IBD to prevent spam.
func (p *Peer) CheckGetBlocksThrottle() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if time.Since(p.lastGetBlocksResp) < 5*time.Second {
		return false
	}
	p.lastGetBlocksResp = time.Now()
	return true
}

// --- Inventory ---

func (p *Peer) AddKnownInventory(hash types.Hash) {
	p.knownInv.Add(hash)
}

func (p *Peer) HasKnownInventory(hash types.Hash) bool {
	return p.knownInv.Has(hash)
}

// --- I/O ---

func (p *Peer) SendMessage(cmd string, payload []byte) {
	select {
	case p.sendQueue <- outMsg{cmd: cmd, payload: payload}:
	case <-p.done:
	default:
		log.Printf("[p2p] send queue full for peer %s, dropping message %s", p.addr, cmd)
	}
}

// SendCritical enqueues a consensus-critical message (block, inv) with a
// brief blocking timeout instead of silently dropping. This prevents block
// relay messages from being lost under transient load.
func (p *Peer) SendCritical(cmd string, payload []byte) {
	select {
	case p.sendQueue <- outMsg{cmd: cmd, payload: payload}:
	case <-p.done:
	case <-time.After(2 * time.Second):
		log.Printf("[p2p] send queue full for peer %s, dropping critical message %s after timeout", p.addr, cmd)
	}
}

// TrySendMessage attempts a non-blocking send and returns true if the message
// was enqueued. Used by sync logic to detect full queues and try other peers.
func (p *Peer) TrySendMessage(cmd string, payload []byte) bool {
	select {
	case p.sendQueue <- outMsg{cmd: cmd, payload: payload}:
		return true
	case <-p.done:
		return false
	default:
		return false
	}
}

// SendQueue returns the underlying send channel for diagnostic inspection.
func (p *Peer) SendQueue() chan outMsg { return p.sendQueue }

// SendLowPriority sends a message only if the queue has plenty of headroom.
// Used for addr gossip and other non-consensus messages so they don't starve
// block relay and sync traffic when the queue is under pressure.
func (p *Peer) SendLowPriority(cmd string, payload []byte) {
	if len(p.sendQueue) > cap(p.sendQueue)/2 {
		return
	}
	select {
	case p.sendQueue <- outMsg{cmd: cmd, payload: payload}:
	case <-p.done:
	default:
	}
}

func (p *Peer) WriteLoop() {
	for {
		select {
		case msg := <-p.sendQueue:
			p.conn.SetWriteDeadline(time.Now().Add(writeTimeout))
			if err := protocol.EncodeMessageHeader(p.conn, p.magic, msg.cmd, msg.payload); err != nil {
				log.Printf("[p2p] write header error to %s: %v", p.addr, err)
				p.Close()
				return
			}
			if _, err := p.conn.Write(msg.payload); err != nil {
				log.Printf("[p2p] write payload error to %s: %v", p.addr, err)
				p.Close()
				return
			}
			p.bytesSent.Add(int64(protocol.MessageHeaderSize + len(msg.payload)))
			p.stampSend()
		case <-p.done:
			return
		}
	}
}

// payloadReadTimeout is the deadline for reading a declared payload once the
// header has been received. Much shorter than readTimeout because a peer that
// declares a large payload but trickles data is either broken or malicious.
const payloadReadTimeout = 30 * time.Second

func (p *Peer) ReadMessage() (*protocol.MessageHeader, []byte, error) {
	p.conn.SetReadDeadline(time.Now().Add(readTimeout))
	hdr, err := protocol.DecodeMessageHeader(p.reader)
	if err != nil {
		return nil, nil, fmt.Errorf("read header from %s: %w", p.addr, err)
	}

	if hdr.Magic != p.magic {
		return nil, nil, fmt.Errorf("bad magic from %s: got %x want %x", p.addr, hdr.Magic, p.magic)
	}

	// Tighten the deadline for the payload read: the peer declared a payload
	// size in the header, so the data should arrive promptly. This prevents
	// a peer from holding a large allocation hostage by trickling bytes.
	p.conn.SetReadDeadline(time.Now().Add(payloadReadTimeout))

	// Stream the payload through a limited reader instead of pre-allocating
	// the full declared size. This bounds memory to what is actually received.
	payload, err := io.ReadAll(io.LimitReader(p.reader, int64(hdr.Length)))
	if err != nil {
		return nil, nil, fmt.Errorf("read payload from %s: %w", p.addr, err)
	}
	if uint32(len(payload)) != hdr.Length {
		return nil, nil, fmt.Errorf("short payload from %s: got %d want %d", p.addr, len(payload), hdr.Length)
	}

	expectedChecksum := doubleSHA256First4(payload)
	if !bytes.Equal(hdr.Checksum[:], expectedChecksum[:]) {
		return nil, nil, fmt.Errorf("checksum mismatch from %s", p.addr)
	}

	p.bytesRecv.Add(int64(protocol.MessageHeaderSize + len(payload)))
	p.stampRecv()
	return hdr, payload, nil
}

// --- Lifecycle ---

func (p *Peer) Close() {
	select {
	case <-p.done:
	default:
		close(p.done)
	}
	p.conn.Close()
}

func (p *Peer) Done() <-chan struct{} { return p.done }

// --- Info snapshot for RPC (Bitcoin Core parity) ---

// PeerInfo mirrors Bitcoin Core's getpeerinfo response fields.
// JSON keys match Bitcoin Core exactly for tooling compatibility.
type PeerInfo struct {
	ID              int32   `json:"id"`
	Addr            string  `json:"addr"`
	AddrLocal       string  `json:"addrlocal,omitempty"`
	Network         string  `json:"network"`
	Services        string  `json:"services"`
	RelayTxes       bool    `json:"relaytxes"`
	LastSend        int64   `json:"lastsend"`
	LastRecv        int64   `json:"lastrecv"`
	LastTransaction int64   `json:"last_transaction"`
	LastBlock       int64   `json:"last_block"`
	BytesSent       int64   `json:"bytessent"`
	BytesRecv       int64   `json:"bytesrecv"`
	ConnTime        int64   `json:"conntime"`
	TimeOffset      int64   `json:"timeoffset"`
	PingTime        float64 `json:"pingtime"`
	MinPing         float64 `json:"minping"`
	Version         uint32  `json:"version"`
	SubVer          string  `json:"subver"`
	Inbound         bool    `json:"inbound"`
	BIP152HBCompact bool    `json:"bip152_hb_to"`
	StartingHeight  int32   `json:"startingheight"`
	SyncedHeaders   int32   `json:"synced_headers"`
	SyncedBlocks    int32   `json:"synced_blocks"`
	BanScore        int32   `json:"banscore"`
	PrefersHeaders  bool    `json:"prefers_headers"`
	ConnectionType  string  `json:"connection_type"`
}

func (p *Peer) Info() PeerInfo {
	info := PeerInfo{
		ID:              p.id,
		Addr:            p.addr,
		Network:         classifyNetwork(p.addr),
		RelayTxes:       true,
		LastSend:        p.lastSend.Load(),
		LastRecv:        p.lastRecv.Load(),
		LastTransaction: p.lastTxTime.Load(),
		LastBlock:       p.lastBlockTime.Load(),
		BytesSent:       p.bytesSent.Load(),
		BytesRecv:       p.bytesRecv.Load(),
		ConnTime:        p.connectedAt.Unix(),
		SyncedHeaders:   p.syncedHeaders.Load(),
		SyncedBlocks:    p.syncedBlocks.Load(),
		PrefersHeaders:  p.PrefersHeaders(),
		ConnectionType:  p.connType,
		Inbound:         p.inbound,
	}

	p.mu.Lock()
	info.BanScore = p.banScore
	if p.pingLatency > 0 {
		info.PingTime = p.pingLatency.Seconds()
	}
	if p.minPingLatency > 0 {
		info.MinPing = p.minPingLatency.Seconds()
	}
	if p.version != nil {
		info.Version = p.version.Version
		info.SubVer = p.version.UserAgent
		info.StartingHeight = int32(p.version.StartHeight)
		info.Services = fmt.Sprintf("%016x", p.version.Services)
	}
	p.mu.Unlock()

	if localAddr := p.conn.LocalAddr(); localAddr != nil {
		info.AddrLocal = localAddr.String()
	}

	return info
}

// classifyNetwork returns "ipv4", "ipv6", or "unknown" based on the peer address.
func classifyNetwork(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return "unknown"
	}
	if ip.To4() != nil {
		return "ipv4"
	}
	return "ipv6"
}

// --- Crypto helpers ---

func doubleSHA256First4(data []byte) [4]byte {
	first := sha256sum(data)
	second := sha256sum(first[:])
	var out [4]byte
	copy(out[:], second[:4])
	return out
}

func sha256sum(data []byte) [32]byte {
	return sha256.Sum256(data)
}
