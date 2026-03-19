package mempool

import (
	"bytes"
	"container/heap"
	"encoding/binary"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/bams-repo/fairchain/internal/consensus"
	"github.com/bams-repo/fairchain/internal/crypto"
	"github.com/bams-repo/fairchain/internal/params"
	"github.com/bams-repo/fairchain/internal/types"
	"github.com/bams-repo/fairchain/internal/utxo"
)

// TxEntry wraps a transaction with its computed metadata.
type TxEntry struct {
	Tx      *types.Transaction
	Hash    types.Hash
	Fee     uint64
	FeeRate uint64    // Fee per byte of serialized transaction size.
	Size    int
	AddedAt time.Time // When this entry was accepted into the mempool.
	heapIdx int       // Index in the fee heap; -1 if not in heap.
}

// Mempool holds unconfirmed transactions awaiting inclusion in a block.
// Thread-safe for concurrent access from P2P and RPC handlers.
type Mempool struct {
	mu   sync.RWMutex
	txs  map[types.Hash]*TxEntry
	p    *params.ChainParams

	// Track which outpoints are spent by mempool transactions for double-spend detection.
	spentOutpoints map[[36]byte]types.Hash // outpoint key -> spending tx hash

	totalBytes int       // total serialized byte size of all transactions
	feeOrder   feeHeap   // min-heap for O(log n) eviction of lowest fee rate tx

	utxoSet     *utxo.Set
	tipHeightFn func() uint32
}

const maxMempoolBytes = 300 * 1024 * 1024 // 300 MB cap on total mempool byte size

// New creates a new empty mempool. tipHeightFn is called when needed for maturity
// checks; it must return the current chain tip height. This eliminates the race
// between chain tip advancement and mempool validation.
func New(p *params.ChainParams, utxoSet *utxo.Set, tipHeightFn func() uint32) *Mempool {
	m := &Mempool{
		txs:            make(map[types.Hash]*TxEntry),
		p:              p,
		spentOutpoints: make(map[[36]byte]types.Hash),
		utxoSet:        utxoSet,
		tipHeightFn:   tipHeightFn,
	}
	heap.Init(&m.feeOrder)
	return m
}

// AddTx validates and adds a transaction to the mempool.
// Returns the transaction hash and fee if accepted.
func (m *Mempool) AddTx(tx *types.Transaction) (types.Hash, error) {
	if tx.IsCoinbase() {
		return types.ZeroHash, fmt.Errorf("coinbase transactions cannot enter mempool")
	}

	if len(tx.Inputs) == 0 {
		return types.ZeroHash, fmt.Errorf("transaction has no inputs")
	}
	if len(tx.Outputs) == 0 {
		return types.ZeroHash, fmt.Errorf("transaction has no outputs")
	}

	txHash, err := crypto.HashTransaction(tx)
	if err != nil {
		return types.ZeroHash, fmt.Errorf("hash transaction: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.txs[txHash]; exists {
		return txHash, fmt.Errorf("transaction %s already in mempool", txHash)
	}

	txSize := tx.SerializeSize()
	if txSize == 0 {
		txSize = 1
	}

	// Check for double-spends against other mempool transactions.
	for _, in := range tx.Inputs {
		key := utxo.OutpointKey(in.PreviousOutPoint.Hash, in.PreviousOutPoint.Index)
		if conflictTx, exists := m.spentOutpoints[key]; exists {
			return types.ZeroHash, fmt.Errorf("tx %s double-spends outpoint %s:%d (conflicts with %s)",
				txHash, in.PreviousOutPoint.Hash, in.PreviousOutPoint.Index, conflictTx)
		}
	}

	// Validate against the UTXO set (input existence, maturity, value).
	fee, err := consensus.ValidateSingleTransaction(tx, m.utxoSet, m.tipHeightFn(), m.p)
	if err != nil {
		return types.ZeroHash, fmt.Errorf("validation failed: %w", err)
	}

	// Enforce minimum absolute relay fee.
	if m.p.MinRelayTxFee > 0 && fee < m.p.MinRelayTxFee {
		return types.ZeroHash, fmt.Errorf("fee %d below minimum relay fee %d", fee, m.p.MinRelayTxFee)
	}

	// Enforce minimum fee rate (sat/byte) to prevent cheap mempool flooding.
	if m.p.MinRelayTxFeeRate > 0 {
		feeRate := fee / uint64(txSize)
		if feeRate < m.p.MinRelayTxFeeRate {
			return types.ZeroHash, fmt.Errorf("fee rate %d sat/byte below minimum %d sat/byte (fee=%d, size=%d)",
				feeRate, m.p.MinRelayTxFeeRate, fee, txSize)
		}
	}

	// Evict lowest-fee transactions only after the incoming tx passes full
	// validation. This prevents an attacker from draining the mempool with
	// structurally valid but UTXO-invalid transactions.
	for uint32(len(m.txs)) >= m.p.MaxMempoolSize && len(m.txs) > 0 {
		m.evictLowestFeeRateUnsafe()
	}
	for m.totalBytes+txSize > maxMempoolBytes && len(m.txs) > 0 {
		m.evictLowestFeeRateUnsafe()
	}

	entry := &TxEntry{
		Tx:      tx,
		Hash:    txHash,
		Fee:     fee,
		FeeRate: fee / uint64(txSize),
		Size:    txSize,
		AddedAt: time.Now(),
	}

	m.txs[txHash] = entry
	m.totalBytes += txSize
	heap.Push(&m.feeOrder, entry)

	for _, in := range tx.Inputs {
		key := utxo.OutpointKey(in.PreviousOutPoint.Hash, in.PreviousOutPoint.Index)
		m.spentOutpoints[key] = txHash
	}

	return txHash, nil
}

// GetTx retrieves a transaction by hash.
func (m *Mempool) GetTx(hash types.Hash) (*types.Transaction, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	entry, ok := m.txs[hash]
	if !ok {
		return nil, false
	}
	return entry.Tx, true
}

// GetTxEntry retrieves a mempool entry with metadata by hash.
func (m *Mempool) GetTxEntry(hash types.Hash) (*TxEntry, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	entry, ok := m.txs[hash]
	return entry, ok
}

// HasTx checks if a transaction is in the mempool.
func (m *Mempool) HasTx(hash types.Hash) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.txs[hash]
	return ok
}

// RemoveTx removes a transaction from the mempool (e.g., after block inclusion).
func (m *Mempool) RemoveTx(hash types.Hash) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.removeTxUnsafe(hash)
}

// RemoveTxs removes multiple transactions (e.g., all txs in a newly accepted block).
func (m *Mempool) RemoveTxs(hashes []types.Hash) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, h := range hashes {
		m.removeTxUnsafe(h)
	}
}

func (m *Mempool) removeTxUnsafe(hash types.Hash) {
	entry, ok := m.txs[hash]
	if !ok {
		return
	}
	m.totalBytes -= entry.Size
	if entry.heapIdx >= 0 && entry.heapIdx < m.feeOrder.Len() && m.feeOrder[entry.heapIdx] == entry {
		heap.Remove(&m.feeOrder, entry.heapIdx)
	}
	for _, in := range entry.Tx.Inputs {
		key := utxo.OutpointKey(in.PreviousOutPoint.Hash, in.PreviousOutPoint.Index)
		delete(m.spentOutpoints, key)
	}
	delete(m.txs, hash)
}

func (m *Mempool) evictLowestFeeRateUnsafe() {
	for m.feeOrder.Len() > 0 {
		entry := heap.Pop(&m.feeOrder).(*TxEntry)
		if _, exists := m.txs[entry.Hash]; exists {
			m.removeTxUnsafe(entry.Hash)
			return
		}
	}
}

// BlockTemplateTx holds a single transaction with its computed metadata,
// matching the per-tx data that BIP 22 getblocktemplate returns.
type BlockTemplateTx struct {
	Tx   *types.Transaction
	TxID types.Hash
	Fee  uint64
	Size int
}

// BlockTemplateResult holds an atomic snapshot of mempool transactions and their
// aggregate fees, suitable for block template construction. This mirrors Bitcoin
// Core's BlockAssembler which snapshots the mempool under a single lock.
type BlockTemplateResult struct {
	Transactions []*types.Transaction
	Entries      []BlockTemplateTx
	TotalFees    uint64
}

// BlockTemplate returns an atomic snapshot of all mempool transactions and their
// total fees under a single lock acquisition. This prevents the TOCTOU race
// where transactions could be added between separate GetAll/TotalFees calls,
// which would cause the coinbase to claim fees for transactions not in the block.
func (m *Mempool) BlockTemplate() *BlockTemplateResult {
	m.mu.RLock()
	defer m.mu.RUnlock()

	entries := make([]*TxEntry, 0, len(m.txs))
	for _, e := range m.txs {
		entries = append(entries, e)
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].FeeRate != entries[j].FeeRate {
			return entries[i].FeeRate > entries[j].FeeRate
		}
		return hashLess(entries[i].Hash, entries[j].Hash)
	})

	var totalFees uint64
	txs := make([]*types.Transaction, len(entries))
	tmplEntries := make([]BlockTemplateTx, len(entries))
	for i, e := range entries {
		txs[i] = e.Tx
		tmplEntries[i] = BlockTemplateTx{
			Tx:   e.Tx,
			TxID: e.Hash,
			Fee:  e.Fee,
			Size: e.Size,
		}
		prev := totalFees
		totalFees += e.Fee
		if totalFees < prev {
			totalFees = prev
			txs = txs[:i]
			tmplEntries = tmplEntries[:i]
			break
		}
	}
	return &BlockTemplateResult{Transactions: txs, Entries: tmplEntries, TotalFees: totalFees}
}

// GetAll returns all transactions ordered by fee rate (highest first) for block template building.
func (m *Mempool) GetAll() []*types.Transaction {
	return m.BlockTemplate().Transactions
}

// GetAllEntries returns all mempool entries with metadata, ordered by fee rate.
func (m *Mempool) GetAllEntries() []*TxEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()

	entries := make([]*TxEntry, 0, len(m.txs))
	for _, e := range m.txs {
		entries = append(entries, e)
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].FeeRate != entries[j].FeeRate {
			return entries[i].FeeRate > entries[j].FeeRate
		}
		return hashLess(entries[i].Hash, entries[j].Hash)
	})

	return entries
}

// GetTxHashes returns all transaction hashes in the mempool.
func (m *Mempool) GetTxHashes() []types.Hash {
	m.mu.RLock()
	defer m.mu.RUnlock()
	hashes := make([]types.Hash, 0, len(m.txs))
	for h := range m.txs {
		hashes = append(hashes, h)
	}
	sort.Slice(hashes, func(i, j int) bool {
		return hashLess(hashes[i], hashes[j])
	})
	return hashes
}

// TotalFees returns the sum of all fees in the mempool.
func (m *Mempool) TotalFees() uint64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var total uint64
	for _, e := range m.txs {
		total += e.Fee
	}
	return total
}

// TotalSize returns the total serialized size of all mempool transactions.
func (m *Mempool) TotalSize() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var total int
	for _, e := range m.txs {
		total += e.Size
	}
	return total
}

// Count returns the number of transactions in the mempool.
func (m *Mempool) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.txs)
}

// EvictLowestFeeRate removes the transaction with the lowest fee rate.
// Returns true if a transaction was evicted.
func (m *Mempool) EvictLowestFeeRate() bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.txs) == 0 {
		return false
	}

	m.evictLowestFeeRateUnsafe()
	return true
}

// ExpireOldTxs removes transactions that have been in the mempool longer than
// the configured MempoolExpiry duration. Matches Bitcoin Core's CTxMemPool::Expire().
// Returns the number of transactions expired.
func (m *Mempool) ExpireOldTxs() int {
	expiry := m.p.MempoolExpiry
	if expiry <= 0 {
		return 0
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	cutoff := time.Now().Add(-expiry)
	var expired []types.Hash
	for hash, entry := range m.txs {
		if entry.AddedAt.Before(cutoff) {
			expired = append(expired, hash)
		}
	}
	for _, hash := range expired {
		m.removeTxUnsafe(hash)
	}
	return len(expired)
}

// IsOutpointSpent checks if an outpoint is already spent by a mempool transaction.
func (m *Mempool) IsOutpointSpent(txHash types.Hash, index uint32) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	key := utxo.OutpointKey(txHash, index)
	_, exists := m.spentOutpoints[key]
	return exists
}

// Persistence format marker. The legacy v0 format starts with a varint count
// (first byte 0x00–0xFE for counts < 2^32). We use 0xFF as the v1 sentinel
// because in Bitcoin's varint encoding 0xFF signals a 64-bit count, which
// would imply > 4 billion transactions — impossible given MaxMempoolSize.
// This makes version detection unambiguous.
const mempoolDumpV1Marker byte = 0xFF

// DumpToBytes serializes all mempool transactions for persistence.
// Format v1: version(1 byte) + varint(count) + [int64_le(addedAt_unix) + tx_bytes]...
func (m *Mempool) DumpToBytes() []byte {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.txs) == 0 {
		return nil
	}

	var txData []byte
	successCount := uint64(0)
	for _, entry := range m.txs {
		txBytes, err := entry.Tx.SerializeToBytes()
		if err != nil {
			continue
		}
		var ts [8]byte
		binary.LittleEndian.PutUint64(ts[:], uint64(entry.AddedAt.Unix()))
		txData = append(txData, ts[:]...)
		txData = append(txData, txBytes...)
		successCount++
	}

	if successCount == 0 {
		return nil
	}

	countBuf := make([]byte, 9)
	n := types.PutVarInt(countBuf, successCount)

	buf := make([]byte, 0, 1+n+len(txData))
	buf = append(buf, mempoolDumpV1Marker)
	buf = append(buf, countBuf[:n]...)
	buf = append(buf, txData...)
	return buf
}

// LoadFromBytes deserializes transactions from a mempool.dat dump and re-validates them.
// Supports both v0 (legacy) and v1 (timestamped) formats. Transactions older than
// MempoolExpiry are skipped on load, matching Bitcoin Core's LoadMempool behavior.
// Returns the number of transactions successfully loaded.
func (m *Mempool) LoadFromBytes(data []byte) int {
	if len(data) == 0 {
		return 0
	}

	if data[0] == mempoolDumpV1Marker {
		return m.loadV1(data[1:])
	}
	return m.loadV0(data)
}

func (m *Mempool) loadV0(data []byte) int {
	count, err := types.ReadVarIntFromBytes(data)
	if err != nil {
		return 0
	}
	offset := types.VarIntSize(count)

	maxCount := uint64(m.p.MaxMempoolSize)
	if maxCount == 0 {
		maxCount = 50000
	}
	if count > maxCount {
		count = maxCount
	}

	loaded := 0
	for i := uint64(0); i < count; i++ {
		if offset >= len(data) {
			break
		}
		var tx types.Transaction
		reader := bytes.NewReader(data[offset:])
		if err := tx.Deserialize(reader); err != nil {
			break
		}
		consumed := len(data) - offset - reader.Len()
		offset += consumed

		if _, err := m.AddTx(&tx); err == nil {
			loaded++
		}
	}
	return loaded
}

func (m *Mempool) loadV1(data []byte) int {
	if len(data) == 0 {
		return 0
	}

	count, err := types.ReadVarIntFromBytes(data)
	if err != nil {
		return 0
	}
	offset := types.VarIntSize(count)

	maxCount := uint64(m.p.MaxMempoolSize)
	if maxCount == 0 {
		maxCount = 50000
	}
	if count > maxCount {
		count = maxCount
	}

	now := time.Now()
	expiry := m.p.MempoolExpiry
	loaded := 0

	for i := uint64(0); i < count; i++ {
		if offset+8 > len(data) {
			break
		}
		addedAtUnix := int64(binary.LittleEndian.Uint64(data[offset : offset+8]))
		offset += 8

		addedAt := time.Unix(addedAtUnix, 0)

		if offset >= len(data) {
			break
		}
		var tx types.Transaction
		reader := bytes.NewReader(data[offset:])
		if err := tx.Deserialize(reader); err != nil {
			break
		}
		consumed := len(data) - offset - reader.Len()
		offset += consumed

		if expiry > 0 && now.Sub(addedAt) > expiry {
			continue
		}

		if txHash, err := m.AddTx(&tx); err == nil {
			loaded++
			m.mu.Lock()
			if entry, ok := m.txs[txHash]; ok {
				entry.AddedAt = addedAt
			}
			m.mu.Unlock()
		}
	}
	return loaded
}

func hashLess(a, b types.Hash) bool {
	for i := types.HashSize - 1; i >= 0; i-- {
		if a[i] < b[i] {
			return true
		}
		if a[i] > b[i] {
			return false
		}
	}
	return false
}

// feeHeap implements heap.Interface for O(log n) eviction of lowest fee rate tx.
type feeHeap []*TxEntry

func (h feeHeap) Len() int { return len(h) }
func (h feeHeap) Less(i, j int) bool {
	if h[i].FeeRate != h[j].FeeRate {
		return h[i].FeeRate < h[j].FeeRate
	}
	return hashLess(h[i].Hash, h[j].Hash)
}
func (h feeHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].heapIdx = i
	h[j].heapIdx = j
}
func (h *feeHeap) Push(x interface{}) {
	entry := x.(*TxEntry)
	entry.heapIdx = len(*h)
	*h = append(*h, entry)
}
func (h *feeHeap) Pop() interface{} {
	old := *h
	n := len(old)
	entry := old[n-1]
	old[n-1] = nil
	entry.heapIdx = -1
	*h = old[:n-1]
	return entry
}
