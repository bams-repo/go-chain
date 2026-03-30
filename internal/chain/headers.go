// Copyright (c) 2024-2026 The Fairchain Contributors
// Fairchain is an experiment in modularity, designed to improve on the work
// of Satoshi Nakamoto and to inspire more creative genius in the space.
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

package chain

import (
	"errors"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/bams-repo/fairchain/internal/consensus"
	"github.com/bams-repo/fairchain/internal/crypto"
	"github.com/bams-repo/fairchain/internal/logging"
	"github.com/bams-repo/fairchain/internal/params"
	"github.com/bams-repo/fairchain/internal/types"
)

// HeaderNode represents a validated block header in the header tree.
type HeaderNode struct {
	Hash   types.Hash
	Height uint32
	Header types.BlockHeader
	Work   *big.Int    // Cumulative chain work up to and including this header.
	Parent *HeaderNode // nil for genesis.
}

const (
	// maxRejectedHeaders caps the rejected header cache to bound memory.
	maxRejectedHeaders = 50000

	// maxHeaderIndexSize is the upper bound on headers stored in the index.
	// New headers that extend the best chain are always accepted; headers on
	// forks are rejected once this limit is reached.
	maxHeaderIndexSize = 200000

	// maxForkDepth is how far behind the best chain tip a fork can fall
	// before its headers are pruned from the index to reclaim memory.
	// Bitcoin Core uses nMinimumChainWork; for a small network, depth-based
	// pruning is simpler and sufficient.
	maxForkDepth uint32 = 2016
)

// Throttle HeadersToFetch debug — the block scheduler calls this every tick.
var headersToFetchLogMu sync.Mutex
var lastHeadersToFetchLog time.Time

// HeaderIndex maintains the in-memory header tree for header-first sync.
// It tracks all validated headers, supports forks, and identifies the
// best-work header chain tip.
type HeaderIndex struct {
	mu sync.RWMutex

	params *params.ChainParams
	engine consensus.Engine

	byHash     map[types.Hash]*HeaderNode
	bestHeader *HeaderNode
	genesis    *HeaderNode

	// rejected tracks hashes of headers that failed validation (and their
	// descendants). Prevents repeated validation of known-bad headers and
	// the CPU cost of walking the parent chain for difficulty retarget.
	// Bitcoin Core uses BLOCK_FAILED_VALID / BLOCK_FAILED_CHILD flags.
	rejected      map[types.Hash]struct{}
	rejectedOrder []types.Hash

	// bestChainByHeight provides O(1) height-to-hash lookups for the best
	// chain. Rebuilt when bestHeader changes. Index = height.
	// Bitcoin Core uses CBlockIndex::pskip for O(log n); a flat slice is
	// simpler and faster for our expected chain lengths.
	bestChainByHeight []*HeaderNode
}

var (
	ErrOrphanHeader    = errors.New("header parent not found in index")
	ErrDuplicateHeader = errors.New("header already in index")
	ErrRejectedHeader  = errors.New("header descends from a rejected chain")
)

// NewHeaderIndex creates a header index seeded with the genesis header.
func NewHeaderIndex(p *params.ChainParams, engine consensus.Engine, genesisHeader *types.BlockHeader) *HeaderIndex {
	genesisHash := crypto.HashBlockHeader(genesisHeader)
	genesisWork := crypto.CalcWork(genesisHeader.Bits)

	hdr := *genesisHeader
	node := &HeaderNode{
		Hash:   genesisHash,
		Height: 0,
		Header: hdr,
		Work:   genesisWork,
		Parent: nil,
	}

	idx := &HeaderIndex{
		params:            p,
		engine:            engine,
		byHash:            make(map[types.Hash]*HeaderNode),
		bestHeader:        node,
		genesis:           node,
		rejected:          make(map[types.Hash]struct{}),
		bestChainByHeight: []*HeaderNode{node},
	}
	idx.byHash[genesisHash] = node
	return idx
}

// AddHeader validates and adds a single header to the index.
// Validation includes PoW, difficulty retarget, and timestamp rules.
// Parent must already exist in the index.
func (idx *HeaderIndex) AddHeader(header *types.BlockHeader, nowUnix uint32) (*HeaderNode, error) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	return idx.addHeaderLocked(header, nowUnix)
}

// InsertTrustedHeader adds a header that has already been validated and
// persisted to the chain store. Skips PoW and consensus checks entirely.
// Used to populate the header index at startup from the on-disk chain,
// avoiding the expensive re-hashing of every block header.
func (idx *HeaderIndex) InsertTrustedHeader(header *types.BlockHeader) error {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	headerHash := crypto.HashBlockHeader(header)
	if _, exists := idx.byHash[headerHash]; exists {
		return nil
	}

	parentNode, ok := idx.byHash[header.PrevBlock]
	if !ok {
		return ErrOrphanHeader
	}

	childHeight := parentNode.Height + 1
	blockWork := crypto.CalcWork(header.Bits)
	cumulativeWork := new(big.Int).Add(parentNode.Work, blockWork)

	hdr := *header
	node := &HeaderNode{
		Hash:   headerHash,
		Height: childHeight,
		Header: hdr,
		Work:   cumulativeWork,
		Parent: parentNode,
	}
	idx.byHash[headerHash] = node

	if cumulativeWork.Cmp(idx.bestHeader.Work) > 0 {
		idx.bestHeader = node
		idx.rebuildHeightIndex()
	}

	return nil
}

func (idx *HeaderIndex) addHeaderLocked(header *types.BlockHeader, nowUnix uint32) (*HeaderNode, error) {
	headerHash := crypto.HashBlockHeader(header)

	if _, exists := idx.byHash[headerHash]; exists {
		return nil, ErrDuplicateHeader
	}

	// Reject immediately if this header or its parent is in the rejected set.
	// Bitcoin Core marks descendants with BLOCK_FAILED_CHILD.
	if _, bad := idx.rejected[headerHash]; bad {
		return nil, ErrRejectedHeader
	}
	if _, bad := idx.rejected[header.PrevBlock]; bad {
		idx.addRejectedLocked(headerHash)
		return nil, ErrRejectedHeader
	}

	parentNode, ok := idx.byHash[header.PrevBlock]
	if !ok {
		logging.ChainSyncDebug("addHeaderLocked: orphan (parent not in index)",
			"hash", headerHash.ReverseString()[:16],
			"prev", header.PrevBlock.ReverseString()[:16],
			"index_size", len(idx.byHash))
		return nil, ErrOrphanHeader
	}

	// Enforce index size limit: reject fork headers when at capacity.
	// Headers extending the best chain are always accepted.
	if len(idx.byHash) >= maxHeaderIndexSize {
		childWork := new(big.Int).Add(parentNode.Work, crypto.CalcWork(header.Bits))
		if childWork.Cmp(idx.bestHeader.Work) <= 0 {
			return nil, fmt.Errorf("header index at capacity (%d), rejecting non-best-chain header", maxHeaderIndexSize)
		}
	}

	childHeight := parentNode.Height + 1
	ancestorFn := idx.buildAncestorLookup(parentNode)

	if err := consensus.FullValidateHeader(
		idx.engine,
		header,
		&parentNode.Header,
		childHeight,
		ancestorFn,
		nowUnix,
		parentNode.Height,
		idx.params,
	); err != nil {
		logging.ChainSyncDebug("addHeaderLocked: validation failed",
			"hash", headerHash.ReverseString()[:16],
			"height", childHeight,
			"bits", fmt.Sprintf("0x%08x", header.Bits),
			"timestamp", header.Timestamp,
			"error", err)
		idx.addRejectedLocked(headerHash)
		return nil, fmt.Errorf("header validation: %w", err)
	}

	blockWork := crypto.CalcWork(header.Bits)
	cumulativeWork := new(big.Int).Add(parentNode.Work, blockWork)

	hdr := *header
	node := &HeaderNode{
		Hash:   headerHash,
		Height: childHeight,
		Header: hdr,
		Work:   cumulativeWork,
		Parent: parentNode,
	}
	idx.byHash[headerHash] = node

	if cumulativeWork.Cmp(idx.bestHeader.Work) > 0 {
		oldTip := idx.bestHeader
		idx.bestHeader = node
		idx.rebuildHeightIndex()
		logging.ChainSyncDebug("header index best tip updated (more work)",
			"new_height", node.Height,
			"new_hash", node.Hash.ReverseString()[:16],
			"prev_best_height", oldTip.Height,
			"prev_best_hash", oldTip.Hash.ReverseString()[:16])
		if node.Height%100 == 0 {
			idx.pruneStaleForks()
		}
	}

	return node, nil
}

// rebuildHeightIndex updates the bestChainByHeight slice. For the common case
// of a direct chain extension (linear sync), it appends in O(1). Falls back
// to a full rebuild for forks or gaps.
func (idx *HeaderIndex) rebuildHeightIndex() {
	height := idx.bestHeader.Height

	if int(height) == len(idx.bestChainByHeight) &&
		height > 0 &&
		idx.bestChainByHeight[height-1] == idx.bestHeader.Parent {
		idx.bestChainByHeight = append(idx.bestChainByHeight, idx.bestHeader)
		return
	}

	index := make([]*HeaderNode, height+1)
	node := idx.bestHeader
	for node != nil {
		index[node.Height] = node
		node = node.Parent
	}
	idx.bestChainByHeight = index
}

// pruneStaleForks removes headers on forks that have fallen more than
// maxForkDepth behind the best chain tip. Walks the best chain to build
// the active set, then removes any header not on the best chain whose
// height is too far behind.
func (idx *HeaderIndex) pruneStaleForks() {
	bestHeight := idx.bestHeader.Height
	if bestHeight < maxForkDepth {
		return
	}
	pruneBelow := bestHeight - maxForkDepth

	// Build set of hashes on the best chain at or below the prune threshold.
	bestChainHashes := make(map[types.Hash]struct{})
	node := idx.bestHeader
	for node != nil {
		bestChainHashes[node.Hash] = struct{}{}
		if node.Height <= pruneBelow {
			break
		}
		node = node.Parent
	}
	// Continue to genesis so all best-chain hashes are protected.
	for node != nil {
		bestChainHashes[node.Hash] = struct{}{}
		node = node.Parent
	}

	for hash, n := range idx.byHash {
		if n.Height > pruneBelow {
			continue
		}
		if _, onBest := bestChainHashes[hash]; onBest {
			continue
		}
		delete(idx.byHash, hash)
	}
}

// AddHeaders validates and adds a batch of headers in order.
// Each header must connect to the previous. Stops at the first invalid header.
// Returns the count of successfully added headers and the first error (if any).
func (idx *HeaderIndex) AddHeaders(headers []types.BlockHeader, nowUnix uint32) (int, error) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	added := 0
	dupes := 0
	rejected := 0
	for i := range headers {
		_, err := idx.addHeaderLocked(&headers[i], nowUnix)
		if err != nil {
			if errors.Is(err, ErrDuplicateHeader) {
				dupes++
				continue
			}
			if errors.Is(err, ErrRejectedHeader) {
				rejected++
				continue
			}
			logging.ChainSyncDebug("AddHeaders: hard error",
				"index", i,
				"error", err,
				"added_so_far", added,
				"best_height", idx.bestHeader.Height)
			return added, err
		}
		added++
	}

	logging.ChainSyncDebug("AddHeaders complete",
		"batch_size", len(headers),
		"added", added,
		"dupes", dupes,
		"rejected", rejected,
		"best_height", idx.bestHeader.Height,
		"index_size", len(idx.byHash))
	return added, nil
}

// BestHeader returns the tip of the best-work header chain.
func (idx *HeaderIndex) BestHeader() *HeaderNode {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.bestHeader
}

// BestHeaderHeight returns the height of the best header tip.
func (idx *HeaderIndex) BestHeaderHeight() uint32 {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.bestHeader.Height
}

// HasHeader checks if a header hash exists in the index.
func (idx *HeaderIndex) HasHeader(hash types.Hash) bool {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	_, ok := idx.byHash[hash]
	return ok
}

// GetHeader returns the HeaderNode for a given hash, or nil.
func (idx *HeaderIndex) GetHeader(hash types.Hash) *HeaderNode {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.byHash[hash]
}

// GetHeaderByHeight returns the header on the best chain at the given height.
// Walks backwards from the best header tip.
func (idx *HeaderIndex) GetHeaderByHeight(height uint32) *HeaderNode {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.getHeaderByHeightLocked(height)
}

func (idx *HeaderIndex) getHeaderByHeightLocked(height uint32) *HeaderNode {
	if height < uint32(len(idx.bestChainByHeight)) {
		return idx.bestChainByHeight[height]
	}
	return nil
}

// HeaderLocator builds a block locator from the best header tip using the
// same exponential spacing as chain.BlockLocator().
func (idx *HeaderIndex) HeaderLocator() []types.Hash {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	var locator []types.Hash
	step := uint32(1)
	node := idx.bestHeader

	for node != nil {
		locator = append(locator, node.Hash)

		if node.Height == 0 {
			break
		}

		targetHeight := uint32(0)
		if node.Height >= step {
			targetHeight = node.Height - step
		}

		for node.Parent != nil && node.Height > targetHeight {
			node = node.Parent
		}

		if len(locator) > 10 {
			step *= 2
		}
	}

	return locator
}

// HeadersToFetch returns an ordered list of header hashes between startHeight
// and the best header tip that do not yet have full block data.
// Used by the block download scheduler.
func (idx *HeaderIndex) HeadersToFetch(startHeight uint32, limit int) []types.Hash {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	var result []types.Hash
	bestH := idx.bestHeader.Height
	nilCount := 0

	for h := startHeight; h <= bestH && len(result) < limit; h++ {
		node := idx.getHeaderByHeightLocked(h)
		if node != nil {
			result = append(result, node.Hash)
		} else {
			nilCount++
		}
	}

	if logging.DebugMode {
		headersToFetchLogMu.Lock()
		if time.Since(lastHeadersToFetchLog) >= 2*time.Second || nilCount > 0 {
			lastHeadersToFetchLog = time.Now()
			logging.ChainSyncDebug("HeadersToFetch",
				"start", startHeight,
				"limit", limit,
				"best_header", bestH,
				"returned", len(result),
				"nil_gaps", nilCount)
		}
		headersToFetchLogMu.Unlock()
	}

	return result
}

// Count returns the total number of headers in the index.
func (idx *HeaderIndex) Count() int {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return len(idx.byHash)
}

// addRejectedLocked records a header hash as rejected with FIFO eviction.
func (idx *HeaderIndex) addRejectedLocked(hash types.Hash) {
	if _, exists := idx.rejected[hash]; exists {
		return
	}
	for len(idx.rejected) >= maxRejectedHeaders && len(idx.rejectedOrder) > 0 {
		evict := idx.rejectedOrder[0]
		idx.rejectedOrder = idx.rejectedOrder[1:]
		delete(idx.rejected, evict)
	}
	idx.rejected[hash] = struct{}{}
	idx.rejectedOrder = append(idx.rejectedOrder, hash)
}

// buildAncestorLookup returns a function that looks up a header at a given
// height by walking the parent chain from parentNode. This ensures that when
// validating a fork header, we use the fork's own ancestors (and their
// timestamps) rather than the best chain's. Bitcoin Core achieves this via
// CBlockIndex::GetAncestor which follows pskip pointers along the actual
// chain ancestry. We use the bestChainByHeight shortcut only when the parent
// is confirmed to be on the best chain.
func (idx *HeaderIndex) buildAncestorLookup(parentNode *HeaderNode) func(uint32) *types.BlockHeader {
	onBestChain := parentNode.Height < uint32(len(idx.bestChainByHeight)) &&
		idx.bestChainByHeight[parentNode.Height] == parentNode

	return func(height uint32) *types.BlockHeader {
		if onBestChain && height < uint32(len(idx.bestChainByHeight)) {
			node := idx.bestChainByHeight[height]
			if node != nil {
				h := node.Header
				return &h
			}
		}
		node := parentNode
		for node != nil && node.Height > height {
			node = node.Parent
		}
		if node != nil && node.Height == height {
			h := node.Header
			return &h
		}
		return nil
	}
}
