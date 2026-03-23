// Copyright (c) 2024-2026 The Fairchain Contributors
// Fairchain is an experiment in modularity, designed to improve on the work
// of Satoshi Nakamoto and to inspire more creative genius in the space.
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

package p2p

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bams-repo/fairchain/internal/chain"
	"github.com/bams-repo/fairchain/internal/crypto"
	"github.com/bams-repo/fairchain/internal/logging"
	"github.com/bams-repo/fairchain/internal/mempool"
	"github.com/bams-repo/fairchain/internal/metrics"
	"github.com/bams-repo/fairchain/internal/params"
	"github.com/bams-repo/fairchain/internal/protocol"
	"github.com/bams-repo/fairchain/internal/store"
	"github.com/bams-repo/fairchain/internal/types"
	"github.com/bams-repo/fairchain/internal/version"
)

// TimeSampler accepts peer clock samples for network-adjusted time.
type TimeSampler interface {
	AddSample(addr string, peerTime int64)
}

// Manager handles peer connections, handshakes, and message routing.
type Manager struct {
	params      *params.ChainParams
	chain       *chain.Chain
	mempool     *mempool.Mempool
	peerStore   store.PeerStore
	listenAddr  string
	maxInbound  int
	maxOutbound int
	seedPeers   []string
	connectOnly []string // -connect peers: when non-empty, ONLY connect to these (no discovery)
	noSeedNodes bool     // -noseednode: suppress hardcoded SeedNodes from params
	timeSampler TimeSampler

	mu             sync.RWMutex
	peers          map[string]*Peer
	localNonce     uint64
	bestPeerHeight uint32

	// Ban list: IP → expiry time. Keyed by IP (no port) to match Bitcoin Core.
	banMu  sync.RWMutex
	banned map[string]time.Time

	// Reconnection backoff: addr → next allowed attempt time.
	backoffMu sync.Mutex
	backoff   map[string]time.Time
	backoffN  map[string]int // consecutive failure count per addr

	nextPeerID int32 // monotonically increasing peer ID counter
	manualPeers map[string]struct{} // addresses added via AddNode (for connection_type)

	seenBlocks *boundedHashSet
	seenTxs    *boundedHashSet

	// Per-peer sync request throttle: prevents spamming getblocks per peer when
	// already waiting for a response. Enables parallel block requests from
	// multiple peers during IBD (Bitcoin Core parity).
	lastSyncReqPerPeer   map[string]time.Time
	lastSyncReqPerPeerMu sync.Mutex

	// Per-peer addr rate limit: max 1000 addresses per connection lifetime
	// to prevent peer store flooding (Bitcoin Core parity).
	addrCountPerPeer map[string]int
	addrCountMu      sync.Mutex

	// Current sync peer — only this peer is exempt from rate limits during IBD.
	syncPeerAddr    string
	syncPeerAddrMu  sync.RWMutex
	syncPeerSince   time.Time
	lastSyncHeight  uint32

	// IBD block processing queue: decouples network I/O from block validation.
	ibdBlockQueue chan *ibdBlockItem
	ibdQueueDone  chan struct{}

	// Header-first sync state machine (Tasks 3-7).
	syncState       SyncState
	syncStateMu     sync.RWMutex

	headerSyncPeerAddr string
	headerSyncSince    time.Time
	lastHeaderHeight   uint32
	headerSyncStalls   int

	headerIndex    *chain.HeaderIndex
	blockScheduler *BlockScheduler

	blockSyncLastProgress time.Time
	blockSyncLastHeight   uint32

	// Bitcoin Core parity: once IBD finishes, it latches and never reverts
	// to true until the process restarts (m_cached_finished_ibd).
	finishedIBD bool

	ctx      context.Context
	listener net.Listener
}

type ibdBlockItem struct {
	block *types.Block
	peer  *Peer
}

// SyncState represents the current phase of the header-first sync state machine.
type SyncState int

const (
	SyncStateInitial    SyncState = iota
	SyncStateHeaderSync
	SyncStateBlockSync
	SyncStateSynced
)

func (s SyncState) String() string {
	switch s {
	case SyncStateInitial:
		return "INITIAL"
	case SyncStateHeaderSync:
		return "HEADER_SYNC"
	case SyncStateBlockSync:
		return "BLOCK_SYNC"
	case SyncStateSynced:
		return "SYNCED"
	default:
		return "UNKNOWN"
	}
}

const (
	maxSeenBlocks = 10000
	maxSeenTxs    = 50000

	// Reconnection backoff parameters.
	backoffBase = 5 * time.Second
	backoffMax  = 10 * time.Minute

	// Peer store pruning: remove addresses not seen in this duration.
	peerStorePruneAge = 7 * 24 * time.Hour

	// Header-first sync constants.
	headerSyncStallTimeout = 30 * time.Second
	blockSyncStallTimeout  = 60 * time.Second
	maxStallsBeforeRotate  = 3
	maxStallsBeforeBan     = 10
	maxHeadersPerPeer      = 100000
)

// boundedHashSet is a bounded set of hashes with FIFO eviction, modeled after
// Bitcoin Core's CRollingBloomFilter used for inventory deduplication.
type boundedHashSet struct {
	mu    sync.Mutex
	items map[types.Hash]struct{}
	order []types.Hash
	cap   int
}

func newBoundedHashSet(capacity int) *boundedHashSet {
	return &boundedHashSet{
		items: make(map[types.Hash]struct{}, capacity),
		order: make([]types.Hash, 0, capacity),
		cap:   capacity,
	}
}

func (s *boundedHashSet) Add(h types.Hash) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.items[h]; ok {
		return
	}
	s.evictUntilSpace()
	s.items[h] = struct{}{}
	s.order = append(s.order, h)
}

func (s *boundedHashSet) Has(h types.Hash) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.items[h]
	return ok
}

func (s *boundedHashSet) AddOrHas(h types.Hash) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.items[h]; ok {
		return true
	}
	s.evictUntilSpace()
	s.items[h] = struct{}{}
	s.order = append(s.order, h)
	return false
}

func (s *boundedHashSet) Remove(h types.Hash) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.items, h)
}

// evictUntilSpace removes the oldest entry from the FIFO, skipping stale
// entries that were already removed via Remove() to prevent unbounded
// growth of the order slice.
func (s *boundedHashSet) evictUntilSpace() {
	for len(s.items) >= s.cap && len(s.order) > 0 {
		evict := s.order[0]
		s.order = s.order[1:]
		delete(s.items, evict)
	}
	if cap(s.order) > s.cap*2 && len(s.order) < s.cap {
		compacted := make([]types.Hash, len(s.order), s.cap)
		copy(compacted, s.order)
		s.order = compacted
	}
}

// ManagerOptions holds optional configuration for the P2P manager.
type ManagerOptions struct {
	ConnectOnly []string // When non-empty, connect ONLY to these peers (Bitcoin Core -connect).
	NoSeedNodes bool     // Suppress hardcoded SeedNodes from ChainParams (Bitcoin Core -noseednode).
}

// NewManager creates a new P2P manager. ts may be nil if no time adjustment is needed.
func NewManager(p *params.ChainParams, c *chain.Chain, mp *mempool.Mempool, ps store.PeerStore, listenAddr string, maxIn, maxOut int, seeds []string, ts TimeSampler, opts *ManagerOptions) *Manager {
	var nonce uint64
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		logging.L.Warn("rand.Read failed for local nonce, using time-based fallback", "component", "p2p", "error", err)
		nonce = uint64(time.Now().UnixNano())
	} else {
		nonce = binary.LittleEndian.Uint64(b)
	}

	headerIndex := c.NewHeaderIndex()

	mgr := &Manager{
		params:      p,
		chain:       c,
		mempool:     mp,
		peerStore:   ps,
		listenAddr:  listenAddr,
		maxInbound:  maxIn,
		maxOutbound: maxOut,
		seedPeers:   seeds,
		timeSampler: ts,
		peers:       make(map[string]*Peer),
		localNonce:  nonce,
		manualPeers: make(map[string]struct{}),
		banned:      make(map[string]time.Time),
		backoff:     make(map[string]time.Time),
		backoffN:    make(map[string]int),
		seenBlocks:         newBoundedHashSet(maxSeenBlocks),
		seenTxs:            newBoundedHashSet(maxSeenTxs),
		lastSyncReqPerPeer: make(map[string]time.Time),
		addrCountPerPeer:   make(map[string]int),
		ibdBlockQueue:      make(chan *ibdBlockItem, 1024),
		ibdQueueDone:       make(chan struct{}),
		syncState:          SyncStateInitial,
		headerIndex:        headerIndex,
	}

	if opts != nil {
		mgr.connectOnly = opts.ConnectOnly
		mgr.noSeedNodes = opts.NoSeedNodes
	}

	return mgr
}

// Start begins listening for connections and connecting to seed peers.
func (m *Manager) Start(ctx context.Context) error {
	m.ctx = ctx
	ln, err := net.Listen("tcp", m.listenAddr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", m.listenAddr, err)
	}
	m.listener = ln
	logging.L.Info("listening", "component", "p2p", "addr", m.listenAddr)

	go m.acceptLoop(ctx)
	go m.connectSeeds(ctx)
	go m.reconnectLoop(ctx)
	go m.syncLoop(ctx)
	go m.pingLoop(ctx)
	go m.livenessLoop(ctx)
	go m.addrBroadcastLoop(ctx)
	go m.orphanEvictionLoop(ctx)
	go m.mempoolExpiryLoop(ctx)
	go m.ibdProcessLoop(ctx)
	if logging.DebugMode {
		go m.topologyLoop(ctx)
	}

	return nil
}

// Stop shuts down the P2P manager.
func (m *Manager) Stop() {
	if m.listener != nil {
		m.listener.Close()
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, p := range m.peers {
		p.Close()
	}
}

// PeerCount returns the number of connected peers.
func (m *Manager) PeerCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.peers)
}

// IsSyncing returns true if the node is in header sync or block sync phase.
func (m *Manager) IsSyncing() bool {
	m.syncStateMu.RLock()
	state := m.syncState
	m.syncStateMu.RUnlock()
	return state == SyncStateHeaderSync || state == SyncStateBlockSync
}

// SyncState returns the current sync state name for RPC/logging.
func (m *Manager) GetSyncState() string {
	m.syncStateMu.RLock()
	defer m.syncStateMu.RUnlock()
	return m.syncState.String()
}

// HeaderSyncHeight returns the best validated header height from the header
// index. During header-first sync this advances ahead of the chain tip.
func (m *Manager) HeaderSyncHeight() uint32 {
	if m.headerIndex == nil {
		return 0
	}
	return m.headerIndex.BestHeaderHeight()
}

// BestPeerHeight returns the highest block height reported by any connected peer.
func (m *Manager) BestPeerHeight() uint32 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.bestPeerHeight
}

// PeerAddrs returns the addresses of all connected peers.
func (m *Manager) PeerAddrs() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	addrs := make([]string, 0, len(m.peers))
	for addr := range m.peers {
		addrs = append(addrs, addr)
	}
	return addrs
}

// PeerInfos returns detailed info for all connected peers (for RPC).
func (m *Manager) PeerInfos() []PeerInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	infos := make([]PeerInfo, 0, len(m.peers))
	for _, p := range m.peers {
		infos = append(infos, p.Info())
	}
	return infos
}

// ConnectionCounts returns the number of inbound and outbound connections.
func (m *Manager) ConnectionCounts() (inbound, outbound int) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, p := range m.peers {
		if p.IsInbound() {
			inbound++
		} else {
			outbound++
		}
	}
	return
}

// AddNode attempts to connect to the given address. Returns an error if
// already connected, banned, or the address is malformed/private.
func (m *Manager) AddNode(addr string) error {
	if err := validatePeerAddress(addr); err != nil {
		return fmt.Errorf("invalid address %q: %w", addr, err)
	}
	if m.IsBanned(addr) {
		return fmt.Errorf("node %s is banned", addr)
	}
	m.mu.RLock()
	_, exists := m.peers[addr]
	m.mu.RUnlock()
	if exists {
		return fmt.Errorf("already connected to %s", addr)
	}
	m.mu.Lock()
	m.manualPeers[addr] = struct{}{}
	m.mu.Unlock()
	go m.connectPeer(m.ctx, addr)
	return nil
}

// validatePeerAddress checks that addr is a well-formed host:port with a
// numeric port and a non-private, non-loopback IP to prevent SSRF.
func validatePeerAddress(addr string) error {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("must be host:port: %w", err)
	}
	if host == "" {
		return fmt.Errorf("empty host")
	}
	port := 0
	if _, err := fmt.Sscanf(portStr, "%d", &port); err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("port must be a number between 1 and 65535")
	}
	ip := net.ParseIP(host)
	if ip == nil {
		ips, err := net.LookupIP(host)
		if err != nil || len(ips) == 0 {
			return fmt.Errorf("cannot resolve hostname %q", host)
		}
		ip = ips[0]
	}
	if ip.IsLoopback() || ip.IsUnspecified() {
		return fmt.Errorf("loopback and unspecified addresses not allowed")
	}
	if ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return fmt.Errorf("private/link-local addresses not allowed via addnode")
	}
	return nil
}

// validateGossipAddress performs lightweight validation on addresses received
// via addr gossip. Rejects private, loopback, and malformed addresses to
// prevent peer store poisoning and eclipse attacks.
//
// When localLoopback is true (the node itself is listening on loopback),
// loopback addresses are accepted — this enables mesh formation for testnet,
// regtest, and local multi-node setups. Bitcoin Core doesn't need this because
// it uses a different discovery mechanism for regtest; for a small network
// that relies on addr gossip for mesh formation, this is necessary.
func validateGossipAddress(addr string, localLoopback ...bool) error {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("must be host:port: %w", err)
	}
	if host == "" {
		return fmt.Errorf("empty host")
	}
	port := 0
	if _, err := fmt.Sscanf(portStr, "%d", &port); err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("invalid port")
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("non-IP address")
	}
	isLocal := len(localLoopback) > 0 && localLoopback[0]
	if ip.IsLoopback() {
		if isLocal {
			return nil
		}
		return fmt.Errorf("non-routable address")
	}
	if ip.IsUnspecified() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return fmt.Errorf("non-routable address")
	}
	return nil
}

// isLocalLoopback returns true if this manager is listening on a loopback address.
func (m *Manager) isLocalLoopback() bool {
	host, _, err := net.SplitHostPort(m.listenAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// DisconnectNode disconnects a peer by address. Returns an error if not found.
func (m *Manager) DisconnectNode(addr string) error {
	m.mu.RLock()
	var target *Peer
	for a, p := range m.peers {
		if a == addr {
			target = p
			break
		}
	}
	m.mu.RUnlock()
	if target == nil {
		return fmt.Errorf("peer %s not found", addr)
	}
	target.Close()
	return nil
}

// BroadcastBlock announces a new block to all peers that don't already know it.
func (m *Manager) BroadcastBlock(hash types.Hash, block *types.Block) {
	m.seenBlocks.Add(hash)

	blockBytes, err := block.SerializeToBytes()
	if err != nil {
		logging.L.Error("failed to serialize block for relay", "component", "p2p", "hash", hash.ReverseString(), "error", err)
		return
	}

	inv := protocol.InvMsg{
		Inventory: []protocol.InvVector{
			{Type: protocol.InvTypeBlock, Hash: hash},
		},
	}
	var invBuf bytes.Buffer
	inv.Encode(&invBuf)
	invPayload := invBuf.Bytes()

	// Bitcoin Core (BIP 152) pushes full blocks to ~3 high-bandwidth relay
	// peers and sends inv to the rest. This prevents send-queue flooding on
	// nodes with many connections while keeping relay latency low.
	const maxDirectPush = 3

	m.mu.RLock()
	eligible := make([]*Peer, 0, len(m.peers))
	for _, p := range m.peers {
		if !p.HasKnownInventory(hash) {
			eligible = append(eligible, p)
		}
	}
	m.mu.RUnlock()

	shufflePeers(eligible)

	if logging.DebugMode {
		directAddrs := make([]string, 0, maxDirectPush)
		invAddrs := make([]string, 0, len(eligible))
		for i, p := range eligible {
			if i < maxDirectPush {
				directAddrs = append(directAddrs, p.Addr())
			} else {
				invAddrs = append(invAddrs, p.Addr())
			}
		}
		skipped := len(m.peers) - len(eligible)
		logging.L.Debug("[dbg] BroadcastBlock",
			"hash", hash.ReverseString()[:16],
			"total_peers", len(m.peers),
			"eligible", len(eligible),
			"skipped_known", skipped,
			"direct_push", directAddrs,
			"inv_to", invAddrs)
	}

	var hdrsBuf bytes.Buffer
	hdrsMsg := protocol.HeadersMsg{Headers: []types.BlockHeader{block.Header}}
	hdrsMsg.Encode(&hdrsBuf)
	hdrsPayload := hdrsBuf.Bytes()

	for i, p := range eligible {
		p.AddKnownInventory(hash)
		if i < maxDirectPush {
			p.SendCritical(protocol.CmdBlock, blockBytes)
		} else if p.PrefersHeaders() {
			p.SendCritical(protocol.CmdHeaders, hdrsPayload)
		} else {
			p.SendCritical(protocol.CmdInv, invPayload)
		}
	}
}

// BroadcastTx announces a new transaction to all peers.
func (m *Manager) BroadcastTx(hash types.Hash) {
	m.seenTxs.Add(hash)

	inv := protocol.InvMsg{
		Inventory: []protocol.InvVector{
			{Type: protocol.InvTypeTx, Hash: hash},
		},
	}
	var buf bytes.Buffer
	inv.Encode(&buf)
	payload := buf.Bytes()

	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, p := range m.peers {
		if !p.HasKnownInventory(hash) {
			p.AddKnownInventory(hash)
			p.SendMessage(protocol.CmdInv, payload)
		}
	}
}

// --- Ban management ---

// extractIP strips the port from an addr string to get the bare IP,
// matching Bitcoin Core's per-IP (not per-connection) ban behavior.
func extractIP(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return host
}

// BanPeer bans a peer's IP for BanDuration.
func (m *Manager) BanPeer(addr string) {
	ip := extractIP(addr)
	m.banMu.Lock()
	m.banned[ip] = time.Now().Add(BanDuration)
	m.banMu.Unlock()
	logging.L.Warn("peer banned", "component", "p2p", "ip", ip, "duration", BanDuration)
}

// IsBanned checks if an IP is currently banned.
func (m *Manager) IsBanned(addr string) bool {
	ip := extractIP(addr)
	m.banMu.RLock()
	expiry, ok := m.banned[ip]
	m.banMu.RUnlock()
	if !ok {
		return false
	}
	if time.Now().After(expiry) {
		m.banMu.Lock()
		delete(m.banned, ip)
		m.banMu.Unlock()
		return false
	}
	return true
}

// addMisbehavior increases a peer's ban score and disconnects+bans if threshold reached.
// In -connect mode, ban suppression only applies to the explicitly configured -connect
// peers. Discovered peers (connected via mesh formation) are subject to normal banning
// to prevent an attacker from abusing the connect-only exemption.
func (m *Manager) addMisbehavior(peer *Peer, score int32, reason string) {
	if len(m.connectOnly) > 0 {
		peerAddr := peer.Addr()
		for _, addr := range m.connectOnly {
			if peerAddr == addr {
				logging.L.Debug("peer misbehavior (connect-only peer, ban suppressed)", "component", "p2p",
					"addr", peerAddr, "delta", score, "reason", reason)
				return
			}
		}
	}
	newScore := peer.AddBanScore(score)
	if newScore >= BanThreshold {
		logging.L.Warn("peer reached ban threshold", "component", "p2p",
			"addr", peer.Addr(), "score", newScore, "reason", reason)
		m.BanPeer(peer.Addr())
		peer.Close()
	} else {
		logging.L.Debug("peer misbehavior", "component", "p2p",
			"addr", peer.Addr(), "score", newScore, "delta", score, "reason", reason)
	}
}

// --- Reconnection backoff ---

func (m *Manager) canReconnect(addr string) bool {
	m.backoffMu.Lock()
	defer m.backoffMu.Unlock()
	next, ok := m.backoff[addr]
	if !ok {
		return true
	}
	return time.Now().After(next)
}

func (m *Manager) recordConnectFailure(addr string) {
	m.backoffMu.Lock()
	defer m.backoffMu.Unlock()
	m.backoffN[addr]++
	n := m.backoffN[addr]
	delay := backoffBase * time.Duration(1<<min(n, 10))
	if delay > backoffMax {
		delay = backoffMax
	}
	m.backoff[addr] = time.Now().Add(delay)
}

func (m *Manager) recordConnectSuccess(addr string) {
	m.backoffMu.Lock()
	defer m.backoffMu.Unlock()
	delete(m.backoff, addr)
	delete(m.backoffN, addr)
}

// --- Outbound counting ---

func (m *Manager) outboundCount() int {
	count := 0
	for _, p := range m.peers {
		if !p.IsInbound() {
			count++
		}
	}
	return count
}

// maxOutboundPerSubnet limits how many outbound peers may share a /16 subnet.
// Bitcoin Core uses 1 to resist eclipse attacks; we use 2 to be slightly more
// lenient for small networks where operators may share a hosting provider.
const maxOutboundPerSubnet = 2

// subnetKey returns the /16 prefix for an address string ("host:port").
// For IPv4-mapped IPv6 (::ffff:a.b.c.d) the IPv4 /16 is returned.
// Returns "" if the address cannot be parsed (e.g. hostname).
func subnetKey(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return ""
	}
	if v4 := ip.To4(); v4 != nil {
		return fmt.Sprintf("%d.%d", v4[0], v4[1])
	}
	// IPv6: use the first 4 bytes (/32) as a rough grouping.
	if len(ip) >= 4 {
		return fmt.Sprintf("%02x%02x:%02x%02x", ip[0], ip[1], ip[2], ip[3])
	}
	return ""
}

// outboundSubnetCount returns how many outbound peers share the given /16 subnet.
// Caller must hold m.mu (at least RLock).
func (m *Manager) outboundSubnetCount(sn string) int {
	if sn == "" {
		return 0
	}
	count := 0
	for _, p := range m.peers {
		if !p.IsInbound() && subnetKey(p.Addr()) == sn {
			count++
		}
	}
	return count
}

// --- Inbound eviction (Bitcoin Core parity) ---
// When inbound slots are full and a new inbound peer connects, evict the
// worst-performing inbound peer. "Worst" = highest ping latency among those
// with the shortest connection time, excluding peers that recently relayed
// useful blocks/txs. Simplified version: evict the inbound peer with the
// highest ping latency.

func (m *Manager) maybeEvictInbound() {
	m.mu.RLock()
	var inbound []*Peer
	for _, p := range m.peers {
		if p.IsInbound() {
			inbound = append(inbound, p)
		}
	}
	m.mu.RUnlock()

	if len(inbound) < m.maxInbound {
		return
	}

	// Sort by ping latency descending — evict the slowest.
	sort.Slice(inbound, func(i, j int) bool {
		return inbound[i].PingLatency() > inbound[j].PingLatency()
	})

	victim := inbound[0]
	logging.L.Info("evicting inbound peer for slot", "component", "p2p",
		"addr", victim.Addr(), "ping_ms", victim.PingLatency().Milliseconds())
	victim.Close()
}

// --- Connection loops ---

func (m *Manager) acceptLoop(ctx context.Context) {
	for {
		conn, err := m.listener.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
				logging.L.Error("accept error", "component", "p2p", "error", err)
				continue
			}
		}

		remoteAddr := conn.RemoteAddr().String()
		if m.IsBanned(remoteAddr) {
			conn.Close()
			continue
		}

		m.mu.Lock()
		inboundCount := 0
		for _, p := range m.peers {
			if p.IsInbound() {
				inboundCount++
			}
		}
		m.mu.Unlock()

		// Bitcoin Core does not enforce a per-IP inbound limit. Multiple
		// nodes behind NAT legitimately share the same public IP. Sybil
		// resistance is handled by the eviction logic when slots are full.
		if inboundCount >= m.maxInbound {
			m.maybeEvictInbound()
			time.Sleep(50 * time.Millisecond)

			m.mu.Lock()
			recount := 0
			for _, p := range m.peers {
				if p.IsInbound() {
					recount++
				}
			}
			m.mu.Unlock()
			if recount >= m.maxInbound {
				conn.Close()
				continue
			}
		}

		peer := NewPeer(conn, true, m.params.NetworkMagic)
		go m.handlePeer(ctx, peer)
	}
}

func (m *Manager) connectSeeds(ctx context.Context) {
	if len(m.connectOnly) > 0 {
		if logging.DebugMode {
			logging.L.Debug("[dbg] connectSeeds: connect-only mode", "targets", m.connectOnly)
		}
		for _, addr := range m.connectOnly {
			select {
			case <-ctx.Done():
				return
			default:
			}
			m.connectPeer(ctx, addr)
		}
		return
	}

	allSeeds := make([]string, 0, len(m.seedPeers)+len(m.params.SeedNodes))
	allSeeds = append(allSeeds, m.seedPeers...)
	if !m.noSeedNodes {
		allSeeds = append(allSeeds, m.params.SeedNodes...)
	}

	if stored, err := m.peerStore.GetPeers(); err == nil {
		allSeeds = append(allSeeds, stored...)
	}

	if logging.DebugMode {
		logging.L.Debug("[dbg] connectSeeds",
			"cli_seeds", len(m.seedPeers),
			"param_seeds", len(m.params.SeedNodes),
			"total_targets", len(allSeeds))
	}

	for _, addr := range allSeeds {
		select {
		case <-ctx.Done():
			return
		default:
		}
		m.connectPeer(ctx, addr)
	}
}

func (m *Manager) reconnectLoop(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.mu.RLock()
			outCount := m.outboundCount()
			inCount := len(m.peers) - outCount
			connected := make(map[string]bool, len(m.peers)*2)
			for addr, p := range m.peers {
				connected[addr] = true
				if v := p.Version(); v != nil && v.AddrFrom != "" {
					connected[v.AddrFrom] = true
				}
			}
			m.mu.RUnlock()

			if logging.DebugMode {
				logging.L.Debug("[dbg] reconnectLoop tick",
					"outbound", outCount,
					"inbound", inCount,
					"max_outbound", m.maxOutbound,
					"known_addrs", len(connected))
			}

			if outCount >= m.maxOutbound {
				continue
			}

			// -connect mode: always maintain connections to the explicit list,
			// then also connect to discovered peers for mesh formation.
			// Bitcoin Core's strict -connect prevents discovery, but for a small
			// network this creates an unworkable hub-and-spoke topology.
			if len(m.connectOnly) > 0 {
				for _, addr := range m.connectOnly {
					if !connected[addr] && !m.IsBanned(addr) && m.canReconnect(addr) {
						m.connectPeer(ctx, addr)
						connected[addr] = true
					}
				}
				// Also connect to discovered peers for mesh formation.
				if stored, err := m.peerStore.GetPeers(); err == nil {
					for _, addr := range stored {
						if outCount >= m.maxOutbound {
							break
						}
						if !connected[addr] && !m.IsBanned(addr) && m.canReconnect(addr) && addr != m.listenAddr {
							m.connectPeer(ctx, addr)
							connected[addr] = true
							outCount++
						}
					}
				}
				continue
			}

			allSeeds := make([]string, 0, len(m.seedPeers)+len(m.params.SeedNodes))
			allSeeds = append(allSeeds, m.seedPeers...)
			if !m.noSeedNodes {
				allSeeds = append(allSeeds, m.params.SeedNodes...)
			}
			for _, addr := range allSeeds {
				if !connected[addr] && !m.IsBanned(addr) && m.canReconnect(addr) {
					m.connectPeer(ctx, addr)
					connected[addr] = true
				}
			}

			if stored, err := m.peerStore.GetPeers(); err == nil {
				for _, addr := range stored {
					if !connected[addr] && !m.IsBanned(addr) && m.canReconnect(addr) {
						m.connectPeer(ctx, addr)
						break
					}
				}
			}
		}
	}
}

func (m *Manager) connectPeer(ctx context.Context, addr string) {
	if addr == m.listenAddr {
		return
	}

	m.mu.RLock()
	if _, exists := m.peers[addr]; exists {
		m.mu.RUnlock()
		if logging.DebugMode {
			logging.L.Debug("[dbg] connectPeer: already connected", "addr", addr)
		}
		return
	}
	for _, p := range m.peers {
		if v := p.Version(); v != nil && v.AddrFrom == addr {
			m.mu.RUnlock()
			if logging.DebugMode {
				logging.L.Debug("[dbg] connectPeer: duplicate via listen addr", "addr", addr, "existing", p.Addr())
			}
			return
		}
	}
	// Subnet diversity: reject outbound connection if the /16 is already saturated.
	// Loopback is exempt — multiple local nodes legitimately share 127.0.0.1.
	host, _, _ := net.SplitHostPort(addr)
	addrIsLoopback := net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback()
	if !addrIsLoopback {
		sn := subnetKey(addr)
		if sn != "" && m.outboundSubnetCount(sn) >= maxOutboundPerSubnet {
			m.mu.RUnlock()
			if logging.DebugMode {
				logging.L.Debug("[dbg] connectPeer: subnet limit reached", "addr", addr, "subnet", sn)
			}
			return
		}
	}
	m.mu.RUnlock()

	if m.IsBanned(addr) {
		return
	}

	if logging.DebugMode {
		logging.L.Debug("[dbg] connectPeer: dialing", "addr", addr)
	}

	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		if logging.DebugMode {
			logging.L.Debug("[dbg] connectPeer: dial failed", "addr", addr, "error", err)
		}
		m.recordConnectFailure(addr)
		return
	}

	peer := NewPeer(conn, false, m.params.NetworkMagic)
	go m.handlePeer(ctx, peer)
}

func (m *Manager) handlePeer(ctx context.Context, peer *Peer) {
	defer func() {
		if r := recover(); r != nil {
			logging.L.Error("panic in peer handler — disconnecting peer",
				"component", "p2p", "addr", peer.Addr(), "panic", fmt.Sprintf("%v", r))
			m.addMisbehavior(peer, 100, fmt.Sprintf("handler panic: %v", r))
		}
		peer.Close()
		m.mu.Lock()
		disconnectedAddr := peer.Addr()
		delete(m.peers, disconnectedAddr)
		m.mu.Unlock()
		// Clean up per-peer tracking maps when peer disconnects.
		m.addrCountMu.Lock()
		delete(m.addrCountPerPeer, disconnectedAddr)
		m.addrCountMu.Unlock()
		m.lastSyncReqPerPeerMu.Lock()
		delete(m.lastSyncReqPerPeer, disconnectedAddr)
		m.lastSyncReqPerPeerMu.Unlock()
		if m.blockScheduler != nil {
			m.blockScheduler.RemovePeer(disconnectedAddr)
		}
		m.mu.Lock()
		var best uint32
		for _, p := range m.peers {
			if v := p.Version(); v != nil && v.StartHeight > best {
				best = v.StartHeight
			}
		}
		m.bestPeerHeight = best
		m.mu.Unlock()
		if logging.DebugMode {
			m.mu.RLock()
			remaining := len(m.peers)
			m.mu.RUnlock()
			logging.L.Debug("[dbg] peer disconnected",
				"addr", peer.Addr(),
				"inbound", peer.IsInbound(),
				"remaining_peers", remaining)
		} else {
			logging.L.Debug("peer disconnected", "component", "p2p", "addr", peer.Addr())
		}
		metrics.Global.PeersDisconnects.Add(1)
		metrics.Global.PeersConnected.Add(-1)
	}()

	if err := m.handshake(peer); err != nil {
		logging.L.Warn("handshake failed", "component", "p2p", "addr", peer.Addr(), "error", err)
		if !peer.IsInbound() {
			m.recordConnectFailure(peer.Addr())
		}
		return
	}

	m.recordConnectSuccess(peer.Addr())

	m.mu.Lock()
	m.nextPeerID++
	peer.SetID(m.nextPeerID)
	if _, manual := m.manualPeers[peer.Addr()]; manual {
		peer.SetConnType("manual")
	}
	m.peers[peer.Addr()] = peer
	if peer.Version().StartHeight > m.bestPeerHeight {
		m.bestPeerHeight = peer.Version().StartHeight
	}
	m.mu.Unlock()

	// Store the peer's advertised listen address (AddrFrom) rather than the
	// ephemeral TCP address. This ensures addr gossip propagates reachable
	// addresses, matching Bitcoin Core's behavior of tracking listen addrs.
	listenAddr := peer.Version().AddrFrom
	if listenAddr != "" && listenAddr != m.listenAddr && validateGossipAddress(listenAddr, m.isLocalLoopback()) == nil {
		m.peerStore.PutPeer(listenAddr)
	} else if peer.Addr() != m.listenAddr {
		m.peerStore.PutPeer(peer.Addr())
	}

	if logging.DebugMode {
		m.mu.RLock()
		totalPeers := len(m.peers)
		m.mu.RUnlock()
		logging.L.Info("[dbg] peer connected",
			"addr", peer.Addr(),
			"listen_addr", listenAddr,
			"inbound", peer.IsInbound(),
			"version", peer.Version().Version,
			"start_height", peer.Version().StartHeight,
			"user_agent", peer.Version().UserAgent,
			"total_peers", totalPeers)
	} else {
		logging.L.Info("peer connected", "component", "p2p", "addr", peer.Addr(), "version", peer.Version().Version, "height", peer.Version().StartHeight)
	}
	metrics.Global.PeersConnected.Add(1)

	go peer.WriteLoop()

	// BIP 130: request header announcements from v2 peers.
	if peer.Version().Version >= 2 {
		peer.SendMessage(protocol.CmdSendHeaders, nil)
	}

	m.sendGetAddr(peer)

	for {
		select {
		case <-ctx.Done():
			return
		case <-peer.Done():
			return
		default:
		}

		hdr, payload, err := peer.ReadMessage()
		if err != nil {
			logging.L.Debug("read error", "component", "p2p", "addr", peer.Addr(), "error", err)
			return
		}

		// Rate-limit only unsolicited traffic. Request messages (getheaders,
		// getdata, getblocks) and their responses (headers, block) are
		// legitimate during sync on both sides of the connection. Bitcoin
		// Core does not rate-limit these.
		if !peer.CheckRateLimit() {
			cmd := hdr.CommandString()
			exempt := cmd == protocol.CmdBlock || cmd == protocol.CmdHeaders ||
				cmd == protocol.CmdGetHeaders || cmd == protocol.CmdGetData ||
				cmd == protocol.CmdGetBlocks || cmd == protocol.CmdPing ||
				cmd == protocol.CmdPong
			if !exempt {
				m.addMisbehavior(peer, 10, "message rate limit exceeded")
				if peer.BanScore() >= BanThreshold {
					return
				}
			}
		}

		m.handleMessage(ctx, peer, hdr, payload)
	}
}

// --- Ping/pong keepalive (Bitcoin Core parity: BIP 31) ---

// pingLoop sends a ping to every connected peer every PingInterval.
func (m *Manager) pingLoop(ctx context.Context) {
	ticker := time.NewTicker(PingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.mu.RLock()
			peers := make([]*Peer, 0, len(m.peers))
			for _, p := range m.peers {
				peers = append(peers, p)
			}
			m.mu.RUnlock()

			for _, p := range peers {
				nonce := m.randomNonce()
				p.SetPingNonce(nonce)
				ping := protocol.PingMsg{Nonce: nonce}
				var buf bytes.Buffer
				ping.Encode(&buf)
				p.SendMessage(protocol.CmdPing, buf.Bytes())
			}
		}
	}
}

// livenessLoop checks for peers that have not responded to pings within
// the PongTimeout and disconnects them. Runs every 30 seconds.
func (m *Manager) livenessLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.mu.RLock()
			peers := make([]*Peer, 0, len(m.peers))
			for _, p := range m.peers {
				peers = append(peers, p)
			}
			m.mu.RUnlock()

			for _, p := range peers {
				if p.PongOverdue() {
					logging.L.Warn("peer pong timeout", "component", "p2p",
						"addr", p.Addr(), "timeout", PongTimeout)
					p.Close()
				}
			}

			// Prune expired bans.
			m.banMu.Lock()
			now := time.Now()
			for ip, expiry := range m.banned {
				if now.After(expiry) {
					delete(m.banned, ip)
				}
			}
			m.banMu.Unlock()
		}
	}
}

func (m *Manager) randomNonce() uint64 {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		logging.L.Warn("rand.Read failed for random nonce, using time-based fallback", "component", "p2p", "error", err)
		return uint64(time.Now().UnixNano())
	}
	return binary.LittleEndian.Uint64(b)
}

// --- Handshake ---

func (m *Manager) handshake(peer *Peer) error {
	if peer.IsInbound() {
		return m.handshakeInbound(peer)
	}
	return m.handshakeOutbound(peer)
}

func (m *Manager) handshakeOutbound(peer *Peer) error {
	peer.conn.SetDeadline(time.Now().Add(30 * time.Second))
	defer peer.conn.SetDeadline(time.Time{})

	if err := m.sendVersion(peer); err != nil {
		return fmt.Errorf("send version: %w", err)
	}

	if err := m.readAndProcessVersion(peer); err != nil {
		return err
	}

	hdr, _, err := peer.ReadMessage()
	if err != nil {
		return fmt.Errorf("read verack: %w", err)
	}
	if hdr.CommandString() != protocol.CmdVerack {
		return fmt.Errorf("expected verack, got %s", hdr.CommandString())
	}

	if err := m.sendVerack(peer); err != nil {
		return fmt.Errorf("send verack: %w", err)
	}

	return nil
}

func (m *Manager) handshakeInbound(peer *Peer) error {
	peer.conn.SetDeadline(time.Now().Add(30 * time.Second))
	defer peer.conn.SetDeadline(time.Time{})

	if err := m.readAndProcessVersion(peer); err != nil {
		return err
	}

	if err := m.sendVersion(peer); err != nil {
		return fmt.Errorf("send version: %w", err)
	}

	if err := m.sendVerack(peer); err != nil {
		return fmt.Errorf("send verack: %w", err)
	}

	hdr, _, err := peer.ReadMessage()
	if err != nil {
		return fmt.Errorf("read verack: %w", err)
	}
	if hdr.CommandString() != protocol.CmdVerack {
		return fmt.Errorf("expected verack, got %s", hdr.CommandString())
	}

	return nil
}

func (m *Manager) sendVersion(peer *Peer) error {
	_, height := m.chain.Tip()
	msg := protocol.VersionMsg{
		Version:     protocol.ProtocolVersion,
		Services:    1,
		Timestamp:   time.Now().Unix(),
		AddrRecv:    peer.Addr(),
		AddrFrom:    m.listenAddr,
		Nonce:       m.localNonce,
		UserAgent:   version.UserAgent(),
		StartHeight: height,
	}

	var buf bytes.Buffer
	msg.Encode(&buf)
	payload := buf.Bytes()

	if err := protocol.EncodeMessageHeader(peer.conn, m.params.NetworkMagic, protocol.CmdVersion, payload); err != nil {
		return err
	}
	_, err := peer.conn.Write(payload)
	return err
}

func (m *Manager) sendVerack(peer *Peer) error {
	return protocol.EncodeMessageHeader(peer.conn, m.params.NetworkMagic, protocol.CmdVerack, nil)
}

func (m *Manager) readAndProcessVersion(peer *Peer) error {
	hdr, versionPayload, err := peer.ReadMessage()
	if err != nil {
		return fmt.Errorf("read version: %w", err)
	}
	if hdr.CommandString() != protocol.CmdVersion {
		return fmt.Errorf("expected version, got %s", hdr.CommandString())
	}

	var theirVersion protocol.VersionMsg
	if err := theirVersion.Decode(bytes.NewReader(versionPayload)); err != nil {
		return fmt.Errorf("decode version: %w", err)
	}

	if theirVersion.Nonce == m.localNonce {
		return fmt.Errorf("self-connection detected")
	}

	if theirVersion.Version < MinPeerProtoVersion {
		return fmt.Errorf("peer protocol version %d below minimum %d", theirVersion.Version, MinPeerProtoVersion)
	}
	if theirVersion.StartHeight > MaxPeerStartHeight {
		return fmt.Errorf("peer start height %d exceeds sanity limit %d", theirVersion.StartHeight, MaxPeerStartHeight)
	}

	peer.SetVersion(&theirVersion)

	if m.timeSampler != nil && theirVersion.Timestamp != 0 {
		m.timeSampler.AddSample(peer.Addr(), theirVersion.Timestamp)
	}

	return nil
}

// --- Message handling ---

func (m *Manager) handleMessage(ctx context.Context, peer *Peer, hdr *protocol.MessageHeader, payload []byte) {
	cmd := hdr.CommandString()
	r := bytes.NewReader(payload)

	if logging.DebugMode && cmd != protocol.CmdPing && cmd != protocol.CmdPong {
		logging.L.Debug("[dbg] msg recv",
			"cmd", cmd,
			"peer", peer.Addr(),
			"size", len(payload))
	}

	switch cmd {
	case protocol.CmdPing:
		var ping protocol.PingMsg
		if err := ping.Decode(r); err != nil {
			m.addMisbehavior(peer, 1, "malformed ping")
			return
		}
		pong := protocol.PongMsg{Nonce: ping.Nonce}
		var buf bytes.Buffer
		pong.Encode(&buf)
		peer.SendMessage(protocol.CmdPong, buf.Bytes())

	case protocol.CmdPong:
		var pong protocol.PongMsg
		if err := pong.Decode(r); err != nil {
			m.addMisbehavior(peer, 1, "malformed pong")
			return
		}
		if !peer.HandlePong(pong.Nonce) {
			m.addMisbehavior(peer, 1, "unexpected pong nonce")
		}

	case protocol.CmdInv:
		var inv protocol.InvMsg
		if err := inv.Decode(r); err != nil {
			m.addMisbehavior(peer, 20, "malformed inv")
			return
		}
		m.handleInv(peer, &inv)

	case protocol.CmdGetData:
		var getData protocol.GetDataMsg
		if err := getData.Decode(r); err != nil {
			m.addMisbehavior(peer, 20, "malformed getdata")
			return
		}
		m.handleGetData(peer, &getData)

	case protocol.CmdBlock:
		var block types.Block
		if err := block.Deserialize(r); err != nil {
			logging.L.Warn("bad block payload", "component", "p2p", "addr", peer.Addr(), "error", err)
			m.addMisbehavior(peer, 20, "malformed block")
			return
		}
		m.handleBlock(peer, &block)

	case protocol.CmdTx:
		var tx types.Transaction
		if err := tx.Deserialize(r); err != nil {
			logging.L.Warn("bad tx payload", "component", "p2p", "addr", peer.Addr(), "error", err)
			m.addMisbehavior(peer, 10, "malformed tx")
			return
		}
		m.handleTx(peer, &tx)

	case protocol.CmdGetBlocks:
		var getBlocks protocol.GetBlocksMsg
		if err := getBlocks.Decode(r); err != nil {
			m.addMisbehavior(peer, 10, "malformed getblocks")
			return
		}
		m.handleGetBlocks(peer, &getBlocks)

	case protocol.CmdGetHeaders:
		if peer.Version() == nil || peer.Version().Version < 2 {
			m.addMisbehavior(peer, 10, "getheaders from v1 peer")
			return
		}
		var getHeaders protocol.GetHeadersMsg
		if err := getHeaders.Decode(r); err != nil {
			m.addMisbehavior(peer, 10, "malformed getheaders")
			return
		}
		m.handleGetHeaders(peer, &getHeaders)

	case protocol.CmdHeaders:
		if peer.Version() == nil || peer.Version().Version < 2 {
			m.addMisbehavior(peer, 10, "headers from v1 peer")
			return
		}
		var hdrs protocol.HeadersMsg
		if err := hdrs.Decode(r); err != nil {
			m.addMisbehavior(peer, 20, "malformed headers")
			return
		}
		m.handleHeaders(peer, &hdrs)

	case protocol.CmdSendHeaders:
		peer.SetPrefersHeaders(true)

	case protocol.CmdAddr:
		var addr protocol.AddrMsg
		if err := addr.Decode(r); err != nil {
			m.addMisbehavior(peer, 10, "malformed addr")
			return
		}

		// Per-peer addr rate limit: max 1000 addresses per connection lifetime
		// to prevent peer store flooding (Bitcoin Core parity).
		m.addrCountMu.Lock()
		peerAddr := peer.Addr()
		received := m.addrCountPerPeer[peerAddr]
		remaining := 1000 - received
		if remaining <= 0 {
			m.addrCountMu.Unlock()
			logging.L.Debug("addr rate limit exceeded, ignoring", "component", "p2p", "peer", peerAddr, "total_received", received)
			return
		}
		addrs := addr.Addresses
		if len(addrs) > remaining {
			addrs = addrs[:remaining]
		}
		m.addrCountPerPeer[peerAddr] = received + len(addrs)
		m.addrCountMu.Unlock()

		localLB := m.isLocalLoopback()
		for _, a := range addrs {
			if !m.IsBanned(a) && a != m.listenAddr && validateGossipAddress(a, localLB) == nil {
				m.peerStore.PutPeer(a)
			}
		}

	case protocol.CmdGetAddr:
		m.handleGetAddr(peer)

	default:
		logging.L.Debug("unknown command", "component", "p2p", "cmd", cmd, "addr", peer.Addr())
	}
}

func (m *Manager) handleInv(peer *Peer, inv *protocol.InvMsg) {
	// Bitcoin Core skips ALL inventory during IBD (fBlocksOnly + IBD check).
	// Block INVs conflict with the header-first pipeline.
	// Tx INVs waste bandwidth — mempool entries will be immediately
	// superseded by blocks being connected.
	isIBD := m.IsSyncing()
	if isIBD {
		return
	}

	var needed []protocol.InvVector
	var alreadyHaveBlocks, alreadyHaveTxs int
	for _, iv := range inv.Inventory {
		peer.AddKnownInventory(iv.Hash)
		switch iv.Type {
		case protocol.InvTypeBlock:
			if !m.chain.HasBlockOnChain(iv.Hash) {
				needed = append(needed, iv)
			} else {
				alreadyHaveBlocks++
			}
		case protocol.InvTypeTx:
			if !m.mempool.HasTx(iv.Hash) {
				needed = append(needed, iv)
			} else {
				alreadyHaveTxs++
			}
		}
	}

	if logging.DebugMode {
		var neededBlocks, neededTxs int
		for _, iv := range needed {
			if iv.Type == protocol.InvTypeBlock {
				neededBlocks++
			} else {
				neededTxs++
			}
		}
		logging.L.Debug("[dbg] handleInv",
			"peer", peer.Addr(),
			"total_items", len(inv.Inventory),
			"need_blocks", neededBlocks,
			"need_txs", neededTxs,
			"already_blocks", alreadyHaveBlocks,
			"already_txs", alreadyHaveTxs,
			"ibd_skip_blocks", isIBD)
	}

	if len(needed) > 0 {
		getData := protocol.GetDataMsg{Inventory: needed}
		var buf bytes.Buffer
		getData.Encode(&buf)
		peer.SendMessage(protocol.CmdGetData, buf.Bytes())
	}
}

// maxGetDataBlockResponses caps the number of blocks served per getdata message
// to prevent a single peer from triggering a multi-GB bandwidth burst. Bitcoin
// Core processes getdata batches and limits to avoid this amplification vector.
const maxGetDataBlockResponses = 500

func (m *Manager) handleGetData(peer *Peer, getData *protocol.GetDataMsg) {
	var sentBlocks, sentTxs, missingBlocks, missingTxs int
	for _, iv := range getData.Inventory {
		switch iv.Type {
		case protocol.InvTypeBlock:
			if sentBlocks >= maxGetDataBlockResponses {
				missingBlocks++
				continue
			}
			block, err := m.chain.GetBlock(iv.Hash)
			if err != nil {
				missingBlocks++
				if logging.DebugMode {
					logging.L.Debug("[dbg] getdata: block not found",
						"peer", peer.Addr(),
						"hash", iv.Hash.ReverseString()[:16],
						"error", err)
				}
				continue
			}
			blockBytes, err := block.SerializeToBytes()
			if err != nil {
				continue
			}
			peer.SendMessage(protocol.CmdBlock, blockBytes)
			sentBlocks++

		case protocol.InvTypeTx:
			tx, ok := m.mempool.GetTx(iv.Hash)
			if !ok {
				missingTxs++
				continue
			}
			txBytes, err := tx.SerializeToBytes()
			if err != nil {
				continue
			}
			peer.SendMessage(protocol.CmdTx, txBytes)
			sentTxs++
		}
	}
	if logging.DebugMode {
		logging.L.Debug("[dbg] handleGetData",
			"peer", peer.Addr(),
			"requested", len(getData.Inventory),
			"sent_blocks", sentBlocks,
			"sent_txs", sentTxs,
			"missing_blocks", missingBlocks,
			"missing_txs", missingTxs)
	}
}

func (m *Manager) handleBlock(peer *Peer, block *types.Block) {
	blockHash := crypto.HashBlockHeader(&block.Header)
	peer.AddKnownInventory(blockHash)

	if m.seenBlocks.AddOrHas(blockHash) {
		if logging.DebugMode {
			logging.L.Debug("[dbg] handleBlock: already seen, skipping",
				"hash", blockHash.ReverseString()[:16],
				"peer", peer.Addr())
		}
		return
	}

	if logging.DebugMode {
		_, ourHeight := m.chain.Tip()
		logging.L.Debug("[dbg] handleBlock: processing",
			"hash", blockHash.ReverseString()[:16],
			"parent", block.Header.PrevBlock.ReverseString()[:16],
			"peer", peer.Addr(),
			"our_height", ourHeight)
	}

	// During BLOCK_SYNC with a scheduler, route through the scheduler.
	m.syncStateMu.RLock()
	state := m.syncState
	m.syncStateMu.RUnlock()

	if state == SyncStateBlockSync && m.blockScheduler != nil {
		if m.blockScheduler.BlockReceived(blockHash, block, peer.Addr()) {
			// Bitcoin Core parity: FindNextBlocksToDownload is called on
			// every block receipt. Immediately assign new work to the
			// delivering peer to eliminate idle gaps between requests.
			hashes := m.blockScheduler.AssignWork(peer.Addr(), DefaultMaxInFlightPerPeer)
			if len(hashes) > 0 {
				var invVecs []protocol.InvVector
				for _, h := range hashes {
					invVecs = append(invVecs, protocol.InvVector{Type: protocol.InvTypeBlock, Hash: h})
				}
				getData := protocol.GetDataMsg{Inventory: invVecs}
				var buf bytes.Buffer
				getData.Encode(&buf)
				peer.SendMessage(protocol.CmdGetData, buf.Bytes())
			}
			return
		}
	}

	// During IBD (legacy path), push to the processing queue.
	if m.IsSyncing() && m.blockScheduler == nil {
		m.ibdBlockQueue <- &ibdBlockItem{block: block, peer: peer}
		if peer.BestHeight() > 0 {
			m.requestBlocks(peer)
		}
		return
	}

	// During header sync, unsolicited blocks would create orphan cascades
	// since we don't have their parents yet. Drop them silently — the block
	// scheduler will fetch them in order once headers are complete.
	if state == SyncStateHeaderSync {
		return
	}

	height, err := m.chain.ProcessBlock(block)
	if err != nil {
		if errors.Is(err, chain.ErrSideChain) {
			logging.L.Info("block stored as side chain", "component", "p2p",
				"hash", blockHash.ReverseString(), "height", height, "addr", peer.Addr())
			if logging.DebugMode {
				_, tipH := m.chain.Tip()
				logging.L.Debug("[dbg] side chain detail",
					"hash", blockHash.ReverseString()[:16],
					"side_height", height,
					"main_tip", tipH)
			}
			return
		}
		m.seenBlocks.Remove(blockHash)
		if errors.Is(err, chain.ErrOrphanBlock) {
			logging.L.Info("orphan block, requesting parent from peer", "component", "p2p",
				"hash", blockHash.ReverseString(),
				"parent", block.Header.PrevBlock.ReverseString(),
				"addr", peer.Addr())
			m.requestOrphanParent(peer, block.Header.PrevBlock)
			m.requestBlocks(peer)
			return
		}
		errStr := err.Error()
		if strings.Contains(errStr, "already in chain") {
			if logging.DebugMode {
				logging.L.Debug("[dbg] handleBlock: already in chain",
					"hash", blockHash.ReverseString()[:16],
					"peer", peer.Addr())
			}
			return
		}
		logging.L.Warn("block rejected", "component", "p2p",
			"hash", blockHash.ReverseString(), "addr", peer.Addr(), "error", err)
		if strings.Contains(errStr, "proof of work") || strings.Contains(errStr, "merkle") || strings.Contains(errStr, "bits mismatch") {
			m.addMisbehavior(peer, 100, "invalid block: "+errStr)
		} else if !m.IsSyncing() {
			m.addMisbehavior(peer, 10, "rejected block: "+errStr)
		}
		return
	}

	var confirmedHashes []types.Hash
	for _, tx := range block.Transactions {
		txHash, hashErr := crypto.HashTransaction(&tx)
		if hashErr == nil {
			confirmedHashes = append(confirmedHashes, txHash)
		}
	}
	m.mempool.RemoveTxs(confirmedHashes)

	peer.SetStartHeightIfGreater(height)
	peer.StampLastBlock()
	peer.SetSyncedBlocks(int32(height))
	peer.SetSyncedHeaders(int32(height))
	m.mu.Lock()
	if height > m.bestPeerHeight {
		m.bestPeerHeight = height
	}
	m.mu.Unlock()

	logging.L.Info("block accepted from peer", "component", "p2p",
		"hash", blockHash.ReverseString(), "height", height, "addr", peer.Addr())
	m.BroadcastBlock(blockHash, block)

	if peer.BestHeight() > height {
		if logging.DebugMode {
			logging.L.Debug("[dbg] peer still ahead, requesting more",
				"peer", peer.Addr(),
				"peer_height", peer.BestHeight(),
				"our_height", height)
		}
		m.requestBlocks(peer)
	}
}

func (m *Manager) handleTx(peer *Peer, tx *types.Transaction) {
	txHash, err := crypto.HashTransaction(tx)
	if err != nil {
		return
	}
	peer.AddKnownInventory(txHash)

	if m.seenTxs.AddOrHas(txHash) {
		return
	}

	if _, err := m.mempool.AddTx(tx); err != nil {
		return
	}
	peer.StampLastTx()

	m.BroadcastTx(txHash)
}

func (m *Manager) handleGetBlocks(peer *Peer, msg *protocol.GetBlocksMsg) {
	_, tipHeight := m.chain.Tip()

	startHeight := uint32(0)
	for _, hash := range msg.BlockLocatorHashes {
		if height, ok := m.chain.FindMainChainHash(hash); ok {
			startHeight = height
			break
		}
	}

	if logging.DebugMode {
		logging.L.Debug("[dbg] handleGetBlocks",
			"peer", peer.Addr(),
			"locator_len", len(msg.BlockLocatorHashes),
			"resolved_start", startHeight,
			"our_tip", tipHeight,
			"blocks_to_send", int(tipHeight)-int(startHeight))
	}

	var invItems []protocol.InvVector
	for h := startHeight + 1; h <= tipHeight && len(invItems) < 500; h++ {
		header, err := m.chain.GetHeaderByHeight(h)
		if err != nil {
			break
		}
		hash := crypto.HashBlockHeader(header)
		peer.AddKnownInventory(hash)
		invItems = append(invItems, protocol.InvVector{Type: protocol.InvTypeBlock, Hash: hash})
		if hash == msg.HashStop {
			break
		}
	}
	if len(invItems) > 0 {
		inv := protocol.InvMsg{Inventory: invItems}
		var buf bytes.Buffer
		inv.Encode(&buf)
		peer.SendMessage(protocol.CmdInv, buf.Bytes())
	}
	sent := len(invItems)

	if logging.DebugMode && sent > 0 {
		logging.L.Debug("[dbg] handleGetBlocks sent inv",
			"peer", peer.Addr(),
			"inv_count", sent,
			"from_height", startHeight+1,
			"to_height", startHeight+uint32(sent))
	}
}

// --- Addr gossip (Bitcoin Core parity) ---

// handleGetAddr responds to a getaddr request with known peer addresses.
// Bitcoin Core only responds to one getaddr per connection to prevent
// topology scraping via repeated reconnection.
func (m *Manager) handleGetAddr(peer *Peer) {
	if !peer.MarkGetAddrResponded() {
		return
	}
	addrs := m.gatherAddresses(1000)
	if len(addrs) == 0 {
		return
	}
	msg := protocol.AddrMsg{Addresses: addrs}
	var buf bytes.Buffer
	msg.Encode(&buf)
	peer.SendMessage(protocol.CmdAddr, buf.Bytes())
}

// gatherAddresses collects up to limit known peer addresses for addr relay.
// Prefers advertised listen addresses (AddrFrom) over ephemeral connection
// addresses, matching Bitcoin Core's behavior of gossiping reachable addrs.
func (m *Manager) gatherAddresses(limit int) []string {
	m.mu.RLock()
	connected := make([]string, 0, len(m.peers))
	for _, p := range m.peers {
		if v := p.Version(); v != nil && v.AddrFrom != "" {
			connected = append(connected, v.AddrFrom)
		} else {
			connected = append(connected, p.Addr())
		}
	}
	m.mu.RUnlock()

	stored, _ := m.peerStore.GetPeers()

	seen := make(map[string]struct{})
	var addrs []string

	for _, addr := range connected {
		if addr == m.listenAddr {
			continue
		}
		if _, ok := seen[addr]; ok {
			continue
		}
		seen[addr] = struct{}{}
		addrs = append(addrs, addr)
		if len(addrs) >= limit {
			shuffleStrings(addrs)
			return addrs
		}
	}

	for _, addr := range stored {
		if addr == m.listenAddr {
			continue
		}
		if _, ok := seen[addr]; ok {
			continue
		}
		seen[addr] = struct{}{}
		addrs = append(addrs, addr)
		if len(addrs) >= limit {
			shuffleStrings(addrs)
			return addrs
		}
	}

	shuffleStrings(addrs)
	return addrs
}

// shufflePeers performs a Fisher-Yates shuffle on a peer slice using crypto/rand.
func shufflePeers(peers []*Peer) {
	for i := len(peers) - 1; i > 0; i-- {
		var j int
		b := make([]byte, 8)
		if _, err := rand.Read(b); err != nil {
			logging.L.Warn("rand.Read failed in shufflePeers, using time-based fallback", "component", "p2p", "error", err)
			j = int(uint64(time.Now().UnixNano()) % uint64(i+1))
		} else {
			j = int(binary.LittleEndian.Uint64(b) % uint64(i+1))
		}
		peers[i], peers[j] = peers[j], peers[i]
	}
}

// shuffleStrings performs a Fisher-Yates shuffle on a string slice using crypto/rand.
func shuffleStrings(s []string) {
	for i := len(s) - 1; i > 0; i-- {
		b := make([]byte, 8)
		var j int
		if _, err := rand.Read(b); err != nil {
			j = int(uint64(time.Now().UnixNano()) % uint64(i+1))
		} else {
			j = int(binary.LittleEndian.Uint64(b) % uint64(i+1))
		}
		s[i], s[j] = s[j], s[i]
	}
}

// sendGetAddr sends a getaddr request to a peer.
func (m *Manager) sendGetAddr(peer *Peer) {
	peer.SendMessage(protocol.CmdGetAddr, nil)
}

// addrBroadcastLoop periodically sends a small set of known peer addresses to
// a random subset of connected peers. Bitcoin Core uses Poisson-timed relay
// (~24h average for self-advertisement); we use 2 minutes and send to only
// 2 random peers per tick to avoid addr gossip flooding the message loop.
func (m *Manager) addrBroadcastLoop(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			addrs := m.gatherAddresses(10)
			if len(addrs) == 0 {
				continue
			}
			msg := protocol.AddrMsg{Addresses: addrs}
			var buf bytes.Buffer
			msg.Encode(&buf)
			payload := buf.Bytes()

			m.mu.RLock()
			peers := make([]*Peer, 0, len(m.peers))
			for _, p := range m.peers {
				peers = append(peers, p)
			}
			m.mu.RUnlock()

			shufflePeers(peers)
			sent := 0
			for _, p := range peers {
				if sent >= 2 {
					break
				}
				p.SendLowPriority(protocol.CmdAddr, payload)
				sent++
			}
		}
	}
}

// topologyLoop periodically dumps the full peer table when -debug is active.
// Logs every connected peer's address, direction, best height, queue usage,
// and version info. Runs every 10 seconds.
func (m *Manager) topologyLoop(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_, ourHeight := m.chain.Tip()
			m.mu.RLock()
			peerCount := len(m.peers)
			if peerCount == 0 {
				m.mu.RUnlock()
				logging.L.Debug("[dbg] topology: no peers connected", "our_height", ourHeight)
				continue
			}
			type peerInfo struct {
				addr       string
				listenAddr string
				direction  string
				bestHeight uint32
				queueLen   int
				queueCap   int
				version    string
			}
			infos := make([]peerInfo, 0, peerCount)
			for _, p := range m.peers {
				dir := "out"
				if p.IsInbound() {
					dir = "in"
				}
				la := ""
				if v := p.Version(); v != nil {
					la = v.AddrFrom
				}
				infos = append(infos, peerInfo{
					addr:       p.Addr(),
					listenAddr: la,
					direction:  dir,
					bestHeight: p.BestHeight(),
					queueLen:   len(p.SendQueue()),
					queueCap:   cap(p.SendQueue()),
				})
			}
			m.mu.RUnlock()

			logging.L.Debug("[dbg] ═══ PEER TOPOLOGY ═══",
				"our_height", ourHeight,
				"total_peers", peerCount)
			for _, pi := range infos {
				logging.L.Debug("[dbg]   peer",
					"addr", pi.addr,
					"listen", pi.listenAddr,
					"dir", pi.direction,
					"best_h", pi.bestHeight,
					"queue", fmt.Sprintf("%d/%d", pi.queueLen, pi.queueCap))
			}
		}
	}
}

// --- IBD block processing queue ---

// ibdProcessLoop is a dedicated goroutine that drains the IBD block queue,
// decoupling network I/O from block validation/storage. This allows the P2P
// read loop to continue receiving blocks while previous blocks are processed.
func (m *Manager) ibdProcessLoop(ctx context.Context) {
	defer close(m.ibdQueueDone)
	for {
		select {
		case <-ctx.Done():
			m.drainIBDQueue()
			return
		case item, ok := <-m.ibdBlockQueue:
			if !ok {
				return
			}
			m.processIBDBlock(item)
		}
	}
}

func (m *Manager) processIBDBlock(item *ibdBlockItem) {
	block := item.block
	peer := item.peer
	blockHash := crypto.HashBlockHeader(&block.Header)

	height, err := m.chain.ProcessBlock(block)
	if err != nil {
		if errors.Is(err, chain.ErrSideChain) {
			logging.L.Info("IBD block stored as side chain", "component", "p2p",
				"hash", blockHash.ReverseString(), "height", height, "addr", peer.Addr())
			return
		}
		if errors.Is(err, chain.ErrOrphanBlock) {
			logging.L.Info("IBD orphan block, requesting parent", "component", "p2p",
				"hash", blockHash.ReverseString(),
				"parent", block.Header.PrevBlock.ReverseString(),
				"addr", peer.Addr())
			m.requestOrphanParent(peer, block.Header.PrevBlock)
			m.requestBlocks(peer)
			return
		}
		m.seenBlocks.Remove(blockHash)
		errStr := err.Error()
		if strings.Contains(errStr, "already in chain") {
			return
		}
		logging.L.Warn("IBD block rejected", "component", "p2p",
			"hash", blockHash.ReverseString(), "addr", peer.Addr(), "error", err)
		if strings.Contains(errStr, "proof of work") || strings.Contains(errStr, "merkle") || strings.Contains(errStr, "bits mismatch") {
			m.addMisbehavior(peer, 100, "invalid block: "+errStr)
		}
		return
	}

	var confirmedHashes []types.Hash
	for _, tx := range block.Transactions {
		txHash, hashErr := crypto.HashTransaction(&tx)
		if hashErr == nil {
			confirmedHashes = append(confirmedHashes, txHash)
		}
	}
	m.mempool.RemoveTxs(confirmedHashes)

	peer.SetStartHeightIfGreater(height)
	peer.StampLastBlock()
	peer.SetSyncedBlocks(int32(height))
	peer.SetSyncedHeaders(int32(height))
	m.mu.Lock()
	if height > m.bestPeerHeight {
		m.bestPeerHeight = height
	}
	m.mu.Unlock()

	logging.L.Info("IBD block accepted", "component", "p2p",
		"hash", blockHash.ReverseString(), "height", height, "addr", peer.Addr())
}

// drainIBDQueue processes all remaining blocks in the IBD queue.
func (m *Manager) drainIBDQueue() {
	for {
		select {
		case item := <-m.ibdBlockQueue:
			m.processIBDBlock(item)
		default:
			return
		}
	}
}

// --- Sync ---

// transitionSyncState atomically changes the sync state and logs the transition.
func (m *Manager) transitionSyncState(newState SyncState) {
	m.syncStateMu.Lock()
	old := m.syncState
	m.syncState = newState
	m.syncStateMu.Unlock()
	if old != newState {
		logging.L.Info("sync state transition", "component", "p2p",
			"from", old.String(), "to", newState.String())
	}
}

func (m *Manager) syncLoop(ctx context.Context) {
	wasIBD := false
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			if wasIBD {
				m.chain.SetIBDMode(false)
			}
			return
		case <-ticker.C:
			m.syncStateMu.RLock()
			state := m.syncState
			m.syncStateMu.RUnlock()

			switch state {
			case SyncStateInitial:
				m.handleSyncInitial()
			case SyncStateHeaderSync:
				m.handleHeaderSyncTick()
			case SyncStateBlockSync:
				m.handleBlockSyncTick()
			case SyncStateSynced:
				m.handleSyncedTick()
			}

			isIBD := state == SyncStateHeaderSync || state == SyncStateBlockSync
			if isIBD && !wasIBD {
				m.chain.SetIBDMode(true)
				ticker.Reset(100 * time.Millisecond)
				logging.L.Info("entering IBD mode", "component", "p2p", "state", state.String())
			} else if !isIBD && wasIBD {
				m.drainIBDQueue()
				m.chain.SetIBDMode(false)
				ticker.Reset(2 * time.Second)
				logging.L.Info("exiting IBD mode", "component", "p2p")
			}
			wasIBD = isIBD
		}
	}
}

// isInitialBlockDownload mirrors Bitcoin Core's IsInitialBlockDownload().
// IBD is true when the chain tip timestamp is older than maxTipAge.
// Once IBD latches to false it stays false until the process restarts.
func (m *Manager) isInitialBlockDownload() bool {
	if m.finishedIBD {
		return false
	}
	if m.chain.IsTipStale() {
		return true
	}
	m.finishedIBD = true
	return false
}

// handleSyncInitial checks if the node needs to catch up with the network.
// Bitcoin Core parity: use tip timestamp age as the primary IBD signal,
// not raw peer height comparison. A node whose tip is recent is "synced"
// even if a peer is a few blocks ahead (normal propagation delay).
func (m *Manager) handleSyncInitial() {
	_, ourHeight := m.chain.Tip()

	m.mu.RLock()
	var bestHeight uint32
	peerCount := len(m.peers)
	for _, p := range m.peers {
		ph := p.BestHeight()
		if ph > bestHeight {
			bestHeight = ph
		}
	}
	m.mu.RUnlock()

	// No peers yet — stay in INITIAL (shows "Connecting...").
	if peerCount == 0 {
		return
	}

	// Tip is recent — we're not in IBD. Transition to SYNCED.
	if !m.isInitialBlockDownload() {
		m.transitionSyncState(SyncStateSynced)
		return
	}

	// Tip is stale but no peer is ahead — we're the best chain we know of.
	if bestHeight <= ourHeight {
		m.transitionSyncState(SyncStateSynced)
		return
	}

	m.mu.Lock()
	m.bestPeerHeight = bestHeight
	m.mu.Unlock()

	// Prefer header-first sync with v2 peers.
	if peer := m.selectHeaderSyncPeer(); peer != nil {
		m.headerSyncPeerAddr = peer.Addr()
		m.headerSyncSince = time.Now()
		m.lastHeaderHeight = m.headerIndex.BestHeaderHeight()
		m.headerSyncStalls = 0

		m.syncPeerAddrMu.Lock()
		m.syncPeerAddr = peer.Addr()
		m.syncPeerSince = time.Now()
		m.syncPeerAddrMu.Unlock()

		m.transitionSyncState(SyncStateHeaderSync)
		m.requestHeaders(peer)
		return
	}

	// No v2 peers — fall back to legacy getblocks/inv sync.
	if peer := m.selectLegacySyncPeer(); peer != nil {
		m.syncPeerAddrMu.Lock()
		m.syncPeerAddr = peer.Addr()
		m.syncPeerSince = time.Now()
		m.lastSyncHeight = ourHeight
		m.syncPeerAddrMu.Unlock()

		m.transitionSyncState(SyncStateBlockSync)
		m.requestBlocks(peer)
	}
}

// handleHeaderSyncTick drives the header sync phase on each tick.
func (m *Manager) handleHeaderSyncTick() {
	// Look up the header sync peer by address under the lock.
	// Storing an address instead of a *Peer pointer avoids stale pointer
	// dereference if the peer disconnects between ticks (Bitcoin Core uses
	// NodeId integers for the same reason).
	var syncPeer *Peer
	if m.headerSyncPeerAddr != "" {
		m.mu.RLock()
		syncPeer = m.peers[m.headerSyncPeerAddr]
		m.mu.RUnlock()
		if syncPeer == nil {
			m.headerSyncPeerAddr = ""
		}
	}

	if syncPeer == nil {
		peer := m.selectHeaderSyncPeer()
		if peer == nil {
			if legacyPeer := m.selectLegacySyncPeer(); legacyPeer != nil {
				m.transitionSyncState(SyncStateBlockSync)
				m.requestBlocks(legacyPeer)
			}
			return
		}
		m.headerSyncPeerAddr = peer.Addr()
		m.headerSyncSince = time.Now()
		m.lastHeaderHeight = m.headerIndex.BestHeaderHeight()
		m.headerSyncStalls = 0

		m.syncPeerAddrMu.Lock()
		m.syncPeerAddr = peer.Addr()
		m.syncPeerSince = time.Now()
		m.syncPeerAddrMu.Unlock()

		m.requestHeaders(peer)
		return
	}

	// Check for stall.
	currentHeight := m.headerIndex.BestHeaderHeight()
	if currentHeight > m.lastHeaderHeight {
		m.lastHeaderHeight = currentHeight
		m.headerSyncSince = time.Now()
		m.headerSyncStalls = 0
	} else if time.Since(m.headerSyncSince) > headerSyncStallTimeout {
		m.headerSyncStalls++
		m.headerSyncSince = time.Now()

		if m.headerSyncStalls >= maxStallsBeforeBan {
			logging.L.Warn("disconnecting header sync peer due to excessive stalls",
				"component", "p2p", "peer", syncPeer.Addr(),
				"stalls", m.headerSyncStalls)
			syncPeer.Close()
			m.headerSyncPeerAddr = ""
			return
		}
		if m.headerSyncStalls >= maxStallsBeforeRotate {
			logging.L.Warn("rotating header sync peer due to stall",
				"component", "p2p", "old_peer", syncPeer.Addr(),
				"stalls", m.headerSyncStalls)
			m.headerSyncPeerAddr = ""
			return
		}
		m.requestHeaders(syncPeer)
		return
	}

	// Check if we've caught up with the best peer.
	// Re-scan all peers for the current best height instead of using the
	// cached bestPeerHeight, which may be stale if new peers connected
	// during header sync (Finding 7).
	m.mu.RLock()
	var bestHeight uint32
	for _, p := range m.peers {
		if ph := p.BestHeight(); ph > bestHeight {
			bestHeight = ph
		}
	}
	m.mu.RUnlock()

	if currentHeight >= bestHeight {
		logging.L.Info("header sync complete, transitioning to block sync",
			"component", "p2p", "header_height", currentHeight)
		m.blockScheduler = NewBlockScheduler(m.headerIndex, m.chain)
		m.blockScheduler.Populate()
		_, tipH := m.chain.Tip()
		m.blockSyncLastProgress = time.Now()
		m.blockSyncLastHeight = tipH
		m.transitionSyncState(SyncStateBlockSync)
		return
	}
}

// handleBlockSyncTick drives the block download phase on each tick.
func (m *Manager) handleBlockSyncTick() {
	if m.blockScheduler == nil {
		// Legacy sync fallback — use the old getblocks/inv path.
		m.handleLegacyBlockSync()
		return
	}

	// 1. Check for timed-out requests.
	timedOut := m.blockScheduler.HandleTimeout()
	for _, entry := range timedOut {
		logging.L.Warn("block request timed out", "component", "p2p",
			"hash", entry.Hash.ReverseString(), "peer", entry.PeerAddr)
	}

	// 2. Drain ready blocks and connect them in order.
	ready := m.blockScheduler.DrainReady()
	for _, staged := range ready {
		block := staged.Block
		height, err := m.chain.ProcessBlock(block)
		if err != nil {
			blockHash := crypto.HashBlockHeader(&block.Header)
			if errors.Is(err, chain.ErrSideChain) {
				continue
			}
			errStr := err.Error()
			if strings.Contains(errStr, "already in chain") {
				continue
			}
			logging.L.Error("block from scheduler failed validation", "component", "p2p",
				"hash", blockHash.ReverseString(), "peer", staged.PeerAddr, "error", err)
			m.blockScheduler.RequeueBlock(blockHash, 0)
			m.seenBlocks.Remove(blockHash)
			m.mu.RLock()
			if badPeer, ok := m.peers[staged.PeerAddr]; ok {
				m.mu.RUnlock()
				if strings.Contains(errStr, "proof of work") || strings.Contains(errStr, "merkle") || strings.Contains(errStr, "bits mismatch") {
					m.addMisbehavior(badPeer, 100, "invalid block body: "+errStr)
				}
			} else {
				m.mu.RUnlock()
			}
			continue
		}
		m.blockScheduler.UpdateNextConnectHeight(height)
		var confirmedHashes []types.Hash
		for _, tx := range block.Transactions {
			txHash, hashErr := crypto.HashTransaction(&tx)
			if hashErr == nil {
				confirmedHashes = append(confirmedHashes, txHash)
			}
		}
		m.mempool.RemoveTxs(confirmedHashes)
	}

	// 2b. Track block sync progress; detect stalls.
	_, currentTipHeight := m.chain.Tip()
	if currentTipHeight > m.blockSyncLastHeight {
		m.blockSyncLastHeight = currentTipHeight
		m.blockSyncLastProgress = time.Now()
	} else if time.Since(m.blockSyncLastProgress) > blockSyncStallTimeout {
		logging.L.Warn("block sync stalled, re-populating scheduler",
			"component", "p2p",
			"stuck_height", m.blockSyncLastHeight,
			"stall_duration", time.Since(m.blockSyncLastProgress))
		m.blockScheduler.Populate()
		m.blockSyncLastProgress = time.Now()
	}

	// 3. Assign new work to peers.
	m.mu.RLock()
	peers := make([]*Peer, 0, len(m.peers))
	for _, p := range m.peers {
		peers = append(peers, p)
	}
	m.mu.RUnlock()

	// Sort peers by scheduler score so faster, more reliable peers get
	// work first (Bitcoin Core naturally achieves this because faster peers
	// come back for more work sooner; explicit scoring is more direct).
	m.blockScheduler.SortPeersByScore(peers)

	for _, peer := range peers {
		hashes := m.blockScheduler.AssignWork(peer.Addr(), DefaultMaxInFlightPerPeer)
		if len(hashes) == 0 {
			continue
		}
		var invVecs []protocol.InvVector
		for _, h := range hashes {
			invVecs = append(invVecs, protocol.InvVector{Type: protocol.InvTypeBlock, Hash: h})
		}
		getData := protocol.GetDataMsg{Inventory: invVecs}
		var buf bytes.Buffer
		getData.Encode(&buf)
		peer.SendMessage(protocol.CmdGetData, buf.Bytes())
	}

	// 4. Check completion.
	if m.blockScheduler.IsComplete() {
		m.transitionSyncState(SyncStateSynced)
	}
}

// handleLegacyBlockSync uses the old getblocks/inv path for v1 peers.
func (m *Manager) handleLegacyBlockSync() {
	_, ourHeight := m.chain.Tip()

	m.mu.RLock()
	var bestPeer *Peer
	var bestHeight uint32
	for _, p := range m.peers {
		ph := p.BestHeight()
		if ph > bestHeight {
			bestHeight = ph
			bestPeer = p
		}
	}
	m.mu.RUnlock()

	if bestPeer == nil || bestHeight <= ourHeight {
		m.transitionSyncState(SyncStateSynced)
		return
	}

	m.syncPeerAddrMu.Lock()
	currentAddr := m.syncPeerAddr
	if currentAddr == "" || currentAddr != bestPeer.Addr() {
		m.syncPeerAddr = bestPeer.Addr()
		m.syncPeerSince = time.Now()
		m.lastSyncHeight = ourHeight
	} else if time.Since(m.syncPeerSince) > 30*time.Second && ourHeight == m.lastSyncHeight {
		// Stall on legacy sync — rotate peer.
		m.syncPeerAddr = bestPeer.Addr()
		m.syncPeerSince = time.Now()
	}
	m.syncPeerAddrMu.Unlock()

	m.requestBlocks(bestPeer)
}

// handleSyncedTick monitors the chain while synced. Bitcoin Core parity:
// once IBD has latched off it does not re-enter. We only fall back to
// INITIAL if the tip becomes genuinely stale (timestamp-based) AND peers
// report a meaningfully higher chain. Normal 1-2 block propagation delay
// does not trigger re-sync.
func (m *Manager) handleSyncedTick() {
	_, ourHeight := m.chain.Tip()

	m.mu.RLock()
	var bestHeight uint32
	for _, p := range m.peers {
		ph := p.BestHeight()
		if ph > bestHeight {
			bestHeight = ph
		}
	}
	m.mu.RUnlock()

	// Only re-enter sync if tip is stale AND a peer is significantly ahead.
	// This prevents flip-flopping during normal mining/propagation.
	if m.chain.IsTipStale() && bestHeight > ourHeight+5 {
		m.mu.Lock()
		m.bestPeerHeight = bestHeight
		m.mu.Unlock()
		m.finishedIBD = false
		m.transitionSyncState(SyncStateInitial)
		return
	}

	if m.chain.IsTipStale() {
		logging.L.Debug("chain tip appears stale, requesting blocks from all peers",
			"component", "p2p", "height", ourHeight)
		m.mu.RLock()
		allPeers := make([]*Peer, 0, len(m.peers))
		for _, p := range m.peers {
			allPeers = append(allPeers, p)
		}
		m.mu.RUnlock()
		for _, p := range allPeers {
			m.requestBlocks(p)
		}
	}
}

// --- Header sync peer selection ---

func (m *Manager) peerSupportsHeaders(peer *Peer) bool {
	v := peer.Version()
	return v != nil && v.Version >= 2
}

func (m *Manager) selectHeaderSyncPeer() *Peer {
	_, ourHeight := m.chain.Tip()
	m.mu.RLock()
	defer m.mu.RUnlock()

	var best *Peer
	var bestHeight uint32
	for _, p := range m.peers {
		if !m.peerSupportsHeaders(p) {
			continue
		}
		ph := p.BestHeight()
		if ph > ourHeight && ph > bestHeight {
			bestHeight = ph
			best = p
		}
	}
	return best
}

func (m *Manager) selectLegacySyncPeer() *Peer {
	_, ourHeight := m.chain.Tip()
	m.mu.RLock()
	defer m.mu.RUnlock()

	var best *Peer
	var bestHeight uint32
	for _, p := range m.peers {
		ph := p.BestHeight()
		if ph > ourHeight && ph > bestHeight {
			bestHeight = ph
			best = p
		}
	}
	return best
}

// --- Header request/response ---

func (m *Manager) requestHeaders(peer *Peer) {
	locator := m.headerIndex.HeaderLocator()
	if len(locator) == 0 {
		return
	}

	msg := protocol.GetHeadersMsg{
		Version:            protocol.ProtocolVersion,
		BlockLocatorHashes: locator,
		HashStop:           types.ZeroHash,
	}
	var buf bytes.Buffer
	msg.Encode(&buf)
	peer.SendMessage(protocol.CmdGetHeaders, buf.Bytes())
}

func (m *Manager) handleHeaders(peer *Peer, msg *protocol.HeadersMsg) {
	if len(msg.Headers) == 0 {
		return
	}

	if len(msg.Headers) > protocol.MaxHeadersPerMsg {
		m.addMisbehavior(peer, 100, "headers message exceeds 2000 limit")
		return
	}

	// DoS: check per-peer header count.
	total := peer.AddHeadersReceived(int32(len(msg.Headers)))
	if total > int32(maxHeadersPerPeer) {
		m.addMisbehavior(peer, 100, "peer exceeded max headers limit")
		return
	}

	// If the first header doesn't connect to any known header, ignore the
	// batch. This is normal during initial sync when peers announce headers
	// we can't yet connect. Bitcoin Core silently drops these — no penalty.
	if !m.headerIndex.HasHeader(msg.Headers[0].PrevBlock) {
		if logging.DebugMode {
			logging.L.Debug("ignoring unconnectable headers batch",
				"component", "p2p", "peer", peer.Addr(),
				"prev", msg.Headers[0].PrevBlock.ReverseString()[:16],
				"count", len(msg.Headers))
		}
		return
	}

	// Validate that headers connect in sequence.
	for i := 1; i < len(msg.Headers); i++ {
		prevHash := crypto.HashBlockHeader(&msg.Headers[i-1])
		if msg.Headers[i].PrevBlock != prevHash {
			m.addMisbehavior(peer, 100, "headers not in sequence")
			return
		}
	}

	nowUnix := uint32(time.Now().Unix())
	added, err := m.headerIndex.AddHeaders(msg.Headers, nowUnix)
	if err != nil {
		logging.L.Warn("header validation failed", "component", "p2p",
			"peer", peer.Addr(), "added", added, "error", err)
		if added == 0 {
			errStr := err.Error()
			if strings.Contains(errStr, "proof of work") || strings.Contains(errStr, "bits mismatch") || strings.Contains(errStr, "difficulty") {
				// Invalid PoW is always malicious — ban immediately.
				m.addMisbehavior(peer, 100, "invalid header: "+errStr)
			}
			// "parent not found" and other non-PoW errors are normal during
			// sync (e.g. out-of-order delivery). No penalty — just ignore.
		}
	}

	if added > 0 {
		bestH := m.headerIndex.BestHeaderHeight()
		peer.SetSyncedHeaders(int32(bestH))
		logging.L.Info("headers received", "component", "p2p",
			"peer", peer.Addr(), "added", added, "batch_size", len(msg.Headers), "best_header", bestH)

		// Request more if the batch was full AND we actually got new headers.
		// Using batch size alone would cause infinite loops when most headers
		// are duplicates (Bitcoin Core checks if the last header is new).
		if len(msg.Headers) == protocol.MaxHeadersPerMsg && added > 0 {
			m.requestHeaders(peer)
		}

		// Bitcoin Core header-first relay: when synced and a new header
		// extends the chain tip, request the full block body via getdata.
		m.syncStateMu.RLock()
		state := m.syncState
		m.syncStateMu.RUnlock()
		if state == SyncStateSynced {
			_, tipHeight := m.chain.Tip()
			for i := range msg.Headers {
				hdrHash := crypto.HashBlockHeader(&msg.Headers[i])
				node := m.headerIndex.GetHeader(hdrHash)
				if node != nil && node.Height == tipHeight+1 {
					getData := protocol.GetDataMsg{
						Inventory: []protocol.InvVector{
							{Type: protocol.InvTypeBlock, Hash: hdrHash},
						},
					}
					var buf bytes.Buffer
					getData.Encode(&buf)
					peer.SendMessage(protocol.CmdGetData, buf.Bytes())
				}
			}
		}
	}
}

func (m *Manager) handleGetHeaders(peer *Peer, msg *protocol.GetHeadersMsg) {
	// Only throttle when synced. During IBD, peers legitimately need rapid
	// header responses. Bitcoin Core does not throttle getheaders responses.
	if !m.IsSyncing() && !peer.CheckGetHeadersThrottle() {
		return
	}

	if msg.Version < 2 {
		m.addMisbehavior(peer, 10, "getheaders with invalid version field")
		return
	}

	_, tipHeight := m.chain.Tip()

	startHeight := uint32(0)
	for _, hash := range msg.BlockLocatorHashes {
		if height, ok := m.chain.FindMainChainHash(hash); ok {
			startHeight = height
			break
		}
	}

	var headers []types.BlockHeader
	for h := startHeight + 1; h <= tipHeight && len(headers) < protocol.MaxHeadersPerMsg; h++ {
		header, err := m.chain.GetHeaderByHeight(h)
		if err != nil {
			break
		}
		headers = append(headers, *header)
		hash := crypto.HashBlockHeader(header)
		if hash == msg.HashStop {
			break
		}
	}

	if len(headers) > 0 {
		resp := protocol.HeadersMsg{Headers: headers}
		var buf bytes.Buffer
		resp.Encode(&buf)
		peer.SendMessage(protocol.CmdHeaders, buf.Bytes())
	}
}

func (m *Manager) requestBlocks(peer *Peer) {
	addr := peer.Addr()
	m.lastSyncReqPerPeerMu.Lock()
	if !m.IsSyncing() && time.Since(m.lastSyncReqPerPeer[addr]) < 500*time.Millisecond {
		m.lastSyncReqPerPeerMu.Unlock()
		logging.L.Debug("requestBlocks throttled", "component", "p2p", "peer", addr)
		return
	}
	m.lastSyncReqPerPeer[addr] = time.Now()
	m.lastSyncReqPerPeerMu.Unlock()

	locator := m.chain.BlockLocator()
	if len(locator) == 0 {
		return
	}

	if logging.DebugMode {
		_, ourHeight := m.chain.Tip()
		locatorTip := "empty"
		if len(locator) > 0 {
			locatorTip = locator[0].ReverseString()[:16]
		}
		logging.L.Debug("[dbg] requestBlocks",
			"preferred_peer", peer.Addr(),
			"our_height", ourHeight,
			"locator_len", len(locator),
			"locator_tip", locatorTip)
	}

	msg := protocol.GetBlocksMsg{
		Version:            protocol.ProtocolVersion,
		BlockLocatorHashes: locator,
		HashStop:           types.ZeroHash,
	}
	var buf bytes.Buffer
	msg.Encode(&buf)
	payload := buf.Bytes()

	if peer.TrySendMessage(protocol.CmdGetBlocks, payload) {
		if logging.DebugMode {
			logging.L.Debug("[dbg] requestBlocks: sent to preferred peer", "peer", peer.Addr())
		}
		return
	}
	if logging.DebugMode {
		logging.L.Debug("[dbg] requestBlocks: preferred peer queue full, trying others",
			"peer", peer.Addr(),
			"queue_len", len(peer.SendQueue()))
	}
	m.mu.RLock()
	candidates := make([]*Peer, 0, len(m.peers))
	for _, p := range m.peers {
		if p != peer {
			candidates = append(candidates, p)
		}
	}
	m.mu.RUnlock()
	shufflePeers(candidates)
	for _, p := range candidates {
		if p.TrySendMessage(protocol.CmdGetBlocks, payload) {
			if logging.DebugMode {
				logging.L.Debug("[dbg] requestBlocks: sent to fallback peer", "peer", p.Addr())
			}
			return
		}
	}
	logging.L.Warn("all peer queues full, could not send getblocks", "component", "p2p")
}

// requestOrphanParent sends a targeted getdata for a specific block hash to
// the given peer. This is used when an orphan block is received — rather than
// only doing a broad getblocks sync, we directly ask the source peer for the
// missing parent block. Matches Bitcoin Core's approach of requesting missing
// parents from the peer that provided the orphan.
func (m *Manager) requestOrphanParent(peer *Peer, parentHash types.Hash) {
	getData := protocol.GetDataMsg{
		Inventory: []protocol.InvVector{
			{Type: protocol.InvTypeBlock, Hash: parentHash},
		},
	}
	var buf bytes.Buffer
	getData.Encode(&buf)

	if peer.TrySendMessage(protocol.CmdGetData, buf.Bytes()) {
		logging.L.Info("requested orphan parent from peer", "component", "p2p",
			"parent", parentHash.ReverseString(),
			"peer", peer.Addr())
		return
	}

	if logging.DebugMode {
		logging.L.Debug("[dbg] requestOrphanParent: peer queue full, trying others",
			"parent", parentHash.ReverseString()[:16],
			"peer", peer.Addr())
	}

	m.mu.RLock()
	candidates := make([]*Peer, 0, len(m.peers))
	for _, p := range m.peers {
		if p != peer {
			candidates = append(candidates, p)
		}
	}
	m.mu.RUnlock()
	shufflePeers(candidates)
	for _, p := range candidates {
		if p.TrySendMessage(protocol.CmdGetData, buf.Bytes()) {
			logging.L.Info("requested orphan parent from fallback peer", "component", "p2p",
				"parent", parentHash.ReverseString(),
				"peer", p.Addr())
			return
		}
	}
	logging.L.Warn("all peer queues full, could not request orphan parent", "component", "p2p",
		"parent", parentHash.ReverseString())
}

// orphanEvictionLoop periodically sweeps expired orphans from the chain's
// orphan pool. Without this, orphans only get evicted when a new orphan is
// about to be added, which means stale orphans can sit in memory indefinitely
// if no new orphans arrive.
func (m *Manager) orphanEvictionLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			evicted := m.chain.EvictExpiredOrphans()
			if evicted > 0 {
				logging.L.Info("evicted expired orphans", "component", "p2p",
					"evicted", evicted,
					"remaining", m.chain.OrphanCount())
			}
		}
	}
}

// mempoolExpiryLoop periodically sweeps transactions that have been in the
// mempool longer than MempoolExpiry. Matches Bitcoin Core's periodic call to
// CTxMemPool::Expire() which removes transactions older than -mempoolexpiry
// (default 336 hours / 2 weeks).
func (m *Manager) mempoolExpiryLoop(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			expired := m.mempool.ExpireOldTxs()
			if expired > 0 {
				metrics.Global.TxsExpired.Add(uint64(expired))
				logging.L.Info("expired mempool transactions", "component", "mempool",
					"expired", expired,
					"remaining", m.mempool.Count())
			}
		}
	}
}
