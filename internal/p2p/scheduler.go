// Copyright (c) 2024-2026 The Fairchain Contributors
// Fairchain is an experiment in modularity, designed to improve on the work
// of Satoshi Nakamoto and to inspire more creative genius in the space.
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

package p2p

import (
	"sort"
	"sync"
	"time"

	"github.com/bams-repo/fairchain/internal/chain"
	"github.com/bams-repo/fairchain/internal/crypto"
	"github.com/bams-repo/fairchain/internal/types"
)

const (
	DefaultMaxInFlightPerPeer = 16
	DefaultMaxGlobalInFlight  = 128
	DefaultMaxStagingSize     = 512
	DefaultRequestTimeout     = 30 * time.Second
)

// BlockScheduler coordinates parallel block body downloads from multiple peers
// based on a validated header chain. It assigns block requests to peers,
// tracks in-flight requests, handles timeouts, and ensures blocks are
// delivered to the chain layer in order.
type BlockScheduler struct {
	mu sync.Mutex

	headerIndex *chain.HeaderIndex
	chain       *chain.Chain

	needed    []schedulerEntry
	inFlight  map[types.Hash]*inFlightEntry
	peerStats map[string]*peerDownloadStats
	staging   map[types.Hash]*stagedBlock

	maxInFlightPerPeer int
	maxGlobalInFlight  int
	maxStagingSize     int
	requestTimeout     time.Duration

	nextConnectHeight uint32
}

type schedulerEntry struct {
	Hash   types.Hash
	Height uint32
}

// InFlightEntry tracks a single in-flight block request.
type inFlightEntry struct {
	Hash        types.Hash
	Height      uint32
	PeerAddr    string
	RequestedAt time.Time
}

// stagedBlock pairs a downloaded block with the peer that provided it,
// enabling misbehavior attribution when ProcessBlock fails.
type stagedBlock struct {
	Block    *types.Block
	PeerAddr string
}

type peerDownloadStats struct {
	InFlight    int
	Completed   int
	Failed      int
	AvgLatency  time.Duration
	LastRequest time.Time
	BytesRecv   uint64
}

// SchedulerStats provides a snapshot of scheduler state for logging/RPC.
type SchedulerStats struct {
	Needed    int
	InFlight  int
	Staging   int
	NextHeight uint32
}

// NewBlockScheduler creates a scheduler with default bounds.
func NewBlockScheduler(headerIdx *chain.HeaderIndex, c *chain.Chain) *BlockScheduler {
	_, tipHeight := c.Tip()
	return &BlockScheduler{
		headerIndex:        headerIdx,
		chain:              c,
		needed:             nil,
		inFlight:           make(map[types.Hash]*inFlightEntry),
		peerStats:          make(map[string]*peerDownloadStats),
		staging:            make(map[types.Hash]*stagedBlock),
		maxInFlightPerPeer: DefaultMaxInFlightPerPeer,
		maxGlobalInFlight:  DefaultMaxGlobalInFlight,
		maxStagingSize:     DefaultMaxStagingSize,
		requestTimeout:     DefaultRequestTimeout,
		nextConnectHeight:  tipHeight + 1,
	}
}

// Populate scans the header chain from the current chain tip to the header tip
// and queues all blocks that need downloading.
func (s *BlockScheduler) Populate() {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, tipHeight := s.chain.Tip()
	s.nextConnectHeight = tipHeight + 1
	bestHeaderHeight := s.headerIndex.BestHeaderHeight()

	alreadyQueued := make(map[types.Hash]struct{}, len(s.needed))
	for _, e := range s.needed {
		alreadyQueued[e.Hash] = struct{}{}
	}

	hashes := s.headerIndex.HeadersToFetch(s.nextConnectHeight, int(bestHeaderHeight-tipHeight))
	for i, h := range hashes {
		height := s.nextConnectHeight + uint32(i)
		if _, ok := alreadyQueued[h]; ok {
			continue
		}
		if _, ok := s.inFlight[h]; ok {
			continue
		}
		if _, ok := s.staging[h]; ok {
			continue
		}
		s.needed = append(s.needed, schedulerEntry{Hash: h, Height: height})
	}
}

// AssignWork selects the next batch of blocks to request from a given peer.
// Returns up to `limit` block hashes, respecting per-peer and global limits.
func (s *BlockScheduler) AssignWork(peerAddr string, limit int) []types.Hash {
	s.mu.Lock()
	defer s.mu.Unlock()

	stats := s.getOrCreateStats(peerAddr)

	if stats.InFlight >= s.maxInFlightPerPeer {
		return nil
	}
	if len(s.inFlight) >= s.maxGlobalInFlight {
		return nil
	}
	if len(s.staging) >= s.maxStagingSize {
		return nil
	}

	available := s.maxInFlightPerPeer - stats.InFlight
	if available > limit {
		available = limit
	}
	globalAvailable := s.maxGlobalInFlight - len(s.inFlight)
	if available > globalAvailable {
		available = globalAvailable
	}

	var assigned []types.Hash
	var remaining []schedulerEntry

	for _, entry := range s.needed {
		if len(assigned) >= available {
			remaining = append(remaining, entry)
			continue
		}
		if _, ok := s.inFlight[entry.Hash]; ok {
			remaining = append(remaining, entry)
			continue
		}

		s.inFlight[entry.Hash] = &inFlightEntry{
			Hash:        entry.Hash,
			Height:      entry.Height,
			PeerAddr:    peerAddr,
			RequestedAt: time.Now(),
		}
		stats.InFlight++
		stats.LastRequest = time.Now()
		assigned = append(assigned, entry.Hash)
	}

	s.needed = remaining
	return assigned
}

// BlockReceived is called when a block body arrives from a peer.
// Returns true if the block was expected (was in-flight).
// Defense-in-depth: verifies the block header hashes to the expected hash
// before accepting it into staging (Bitcoin Core re-verifies in net_processing).
func (s *BlockScheduler) BlockReceived(hash types.Hash, block *types.Block, peerAddr string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.inFlight[hash]
	if !ok {
		return false
	}

	if crypto.HashBlockHeader(&block.Header) != hash {
		stats := s.getOrCreateStats(peerAddr)
		stats.InFlight--
		stats.Failed++
		delete(s.inFlight, hash)
		s.needed = append(s.needed, schedulerEntry{Hash: hash, Height: entry.Height})
		return false
	}

	stats := s.getOrCreateStats(peerAddr)
	latency := time.Since(entry.RequestedAt)

	if stats.Completed == 0 {
		stats.AvgLatency = latency
	} else {
		stats.AvgLatency = (stats.AvgLatency*time.Duration(stats.Completed) + latency) / time.Duration(stats.Completed+1)
	}
	stats.Completed++
	stats.InFlight--

	delete(s.inFlight, hash)
	s.staging[hash] = &stagedBlock{Block: block, PeerAddr: peerAddr}

	return true
}

// DrainReady returns blocks from staging that can be connected to the chain
// in order (starting from nextConnectHeight). Returns them in height order
// with peer attribution for misbehavior tracking.
func (s *BlockScheduler) DrainReady() []stagedBlock {
	s.mu.Lock()
	defer s.mu.Unlock()

	var ready []stagedBlock

	for {
		node := s.headerIndex.GetHeaderByHeight(s.nextConnectHeight)
		if node == nil {
			break
		}

		staged, ok := s.staging[node.Hash]
		if !ok {
			break
		}

		delete(s.staging, node.Hash)
		ready = append(ready, *staged)
		s.nextConnectHeight++
	}

	return ready
}

// RequeueBlock moves a block hash back to the needed queue after a validation
// failure. This allows the block to be re-requested from a different peer.
func (s *BlockScheduler) RequeueBlock(hash types.Hash, height uint32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.staging, hash)
	s.needed = append(s.needed, schedulerEntry{Hash: hash, Height: height})
}

// HandleTimeout checks for in-flight requests that have exceeded the timeout.
// Timed-out blocks are moved back to the needed queue.
func (s *BlockScheduler) HandleTimeout() []inFlightEntry {
	s.mu.Lock()
	defer s.mu.Unlock()

	var timedOut []inFlightEntry
	now := time.Now()

	for hash, entry := range s.inFlight {
		if now.Sub(entry.RequestedAt) > s.requestTimeout {
			timedOut = append(timedOut, *entry)

			stats := s.getOrCreateStats(entry.PeerAddr)
			stats.InFlight--
			stats.Failed++

			s.needed = append(s.needed, schedulerEntry{
				Hash:   hash,
				Height: entry.Height,
			})
			delete(s.inFlight, hash)
		}
	}

	return timedOut
}

// RemovePeer cleans up all in-flight requests for a disconnected peer,
// moving their blocks back to the needed queue.
func (s *BlockScheduler) RemovePeer(peerAddr string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for hash, entry := range s.inFlight {
		if entry.PeerAddr == peerAddr {
			s.needed = append(s.needed, schedulerEntry{
				Hash:   hash,
				Height: entry.Height,
			})
			delete(s.inFlight, hash)
		}
	}

	delete(s.peerStats, peerAddr)
}

// IsComplete returns true when all needed blocks have been downloaded
// and connected (needed queue empty, in-flight empty, staging empty).
func (s *BlockScheduler) IsComplete() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.needed) == 0 && len(s.inFlight) == 0 && len(s.staging) == 0
}

// Stats returns scheduler statistics for logging/RPC.
func (s *BlockScheduler) Stats() SchedulerStats {
	s.mu.Lock()
	defer s.mu.Unlock()
	return SchedulerStats{
		Needed:     len(s.needed),
		InFlight:   len(s.inFlight),
		Staging:    len(s.staging),
		NextHeight: s.nextConnectHeight,
	}
}

// NeedCount returns the number of blocks still needed.
func (s *BlockScheduler) NeedCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.needed)
}

// InFlightCount returns the total number of in-flight requests.
func (s *BlockScheduler) InFlightCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.inFlight)
}

// StagingCount returns the number of blocks in the staging pool.
func (s *BlockScheduler) StagingCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.staging)
}

// UpdateNextConnectHeight advances the scheduler's notion of the chain tip
// after blocks have been successfully connected.
func (s *BlockScheduler) UpdateNextConnectHeight(height uint32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if height >= s.nextConnectHeight {
		s.nextConnectHeight = height + 1
	}
}

// scorePeer returns a priority score for assigning work to a peer.
// Higher score = more likely to receive work.
func (s *BlockScheduler) scorePeer(stats *peerDownloadStats) float64 {
	completed := float64(stats.Completed)
	failed := float64(stats.Failed)
	reliability := completed / (completed + failed + 1)
	latencyMs := float64(stats.AvgLatency.Milliseconds())
	speed := 1.0 / (latencyMs + 100)
	headroom := float64(s.maxInFlightPerPeer - stats.InFlight)
	return reliability * speed * headroom
}

// SortPeersByScore sorts a peer slice by download score (highest first).
// Peers without stats are placed last.
func (s *BlockScheduler) SortPeersByScore(peers []*Peer) {
	s.mu.Lock()
	scores := make(map[string]float64, len(peers))
	for _, p := range peers {
		if stats, ok := s.peerStats[p.Addr()]; ok {
			scores[p.Addr()] = s.scorePeer(stats)
		}
	}
	s.mu.Unlock()

	sort.Slice(peers, func(i, j int) bool {
		return scores[peers[i].Addr()] > scores[peers[j].Addr()]
	})
}

func (s *BlockScheduler) getOrCreateStats(peerAddr string) *peerDownloadStats {
	stats, ok := s.peerStats[peerAddr]
	if !ok {
		stats = &peerDownloadStats{}
		s.peerStats[peerAddr] = stats
	}
	return stats
}

// hashBlock is a helper that computes the identity hash of a block.
func hashBlock(block *types.Block) types.Hash {
	return crypto.HashBlockHeader(&block.Header)
}
