// Copyright (c) 2024-2026 The Fairchain Contributors
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

package stratum

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bams-repo/fairchain/internal/algorithms"
	"github.com/bams-repo/fairchain/internal/chain"
	"github.com/bams-repo/fairchain/internal/crypto"
	"github.com/bams-repo/fairchain/internal/logging"
	"github.com/bams-repo/fairchain/internal/mempool"
	"github.com/bams-repo/fairchain/internal/params"
	"github.com/bams-repo/fairchain/internal/types"
)

// Config holds stratum server configuration.
type Config struct {
	ListenAddr string // TCP listen address (e.g. "0.0.0.0:3333")
	VardiffMin float64 // minimum share difficulty
	VardiffMax float64 // maximum share difficulty (0 = network diff)
	// Target shares per minute per worker for vardiff adjustment.
	VardiffTargetSharesPerMin float64
}

// DefaultConfig returns sensible defaults for a solo/small-pool stratum server.
func DefaultConfig() Config {
	return Config{
		ListenAddr:                "0.0.0.0:3333",
		VardiffMin:                0.001,
		VardiffMax:                0, // network difficulty
		VardiffTargetSharesPerMin: 20,
	}
}

// WorkerInfo holds stats for a connected stratum worker (exported for UI).
type WorkerInfo struct {
	Name         string  `json:"name"`
	Addr         string  `json:"addr"`
	ConnectedAt  int64   `json:"connectedAt"`
	SharesValid  int64   `json:"sharesValid"`
	SharesStale  int64   `json:"sharesStale"`
	SharesInvalid int64  `json:"sharesInvalid"`
	Difficulty   float64 `json:"difficulty"`
	LastShareAt  int64   `json:"lastShareAt"`
	Hashrate     float64 `json:"hashrate"`
}

type worker struct {
	conn   net.Conn
	name   string
	addr   string
	authed bool

	mu          sync.Mutex
	difficulty  float64
	subscribedExtranonce string

	connectedAt time.Time
	lastShareAt time.Time

	sharesValid   atomic.Int64
	sharesStale   atomic.Int64
	sharesInvalid atomic.Int64

	// Vardiff tracking
	vardiffShareCount int
	vardiffWindowStart time.Time

	done chan struct{}
}

// job represents an active mining job sent to workers.
type job struct {
	id         string
	prevBlock  types.Hash
	coinbase1  []byte // coinbase prefix (before extranonce)
	coinbase2  []byte // coinbase suffix (after extranonce)
	merkleHashes []types.Hash // merkle branch hashes
	version    uint32
	bits       uint32
	timestamp  uint32
	height     uint32
	target     types.Hash
	txs        []types.Transaction
	cleanJobs  bool
}

// Server is an embedded Stratum V1 TCP server.
type Server struct {
	cfg      Config
	chain    *chain.Chain
	mempool  *mempool.Mempool
	params   *params.ChainParams
	hasher   algorithms.Hasher
	rewardScript []byte

	onBlock func(*types.Block) // callback when a block is found

	listener net.Listener

	mu       sync.RWMutex
	workers  map[*worker]struct{}

	jobMu      sync.RWMutex
	currentJob *job
	jobCounter uint64

	extranonceMu sync.Mutex
	extranonceCounter uint32

	ctx    context.Context
	cancel context.CancelFunc

	running atomic.Bool
	sharesValid   atomic.Int64
	sharesStale   atomic.Int64
	blocksFound   atomic.Int64
}

// New creates a new stratum server. onBlock is called when a valid block is mined.
func New(cfg Config, c *chain.Chain, mp *mempool.Mempool, p *params.ChainParams, hasher algorithms.Hasher, rewardScript []byte, onBlock func(*types.Block)) *Server {
	return &Server{
		cfg:          cfg,
		chain:        c,
		mempool:      mp,
		params:       p,
		hasher:       hasher,
		rewardScript: rewardScript,
		onBlock:      onBlock,
		workers:      make(map[*worker]struct{}),
	}
}

// Start begins listening for stratum connections.
func (s *Server) Start(ctx context.Context) error {
	if s.running.Load() {
		return fmt.Errorf("stratum server already running")
	}

	ln, err := net.Listen("tcp", s.cfg.ListenAddr)
	if err != nil {
		return fmt.Errorf("stratum listen: %w", err)
	}

	s.listener = ln
	s.ctx, s.cancel = context.WithCancel(ctx)
	s.running.Store(true)

	logging.L.Info("stratum server started", "component", "stratum", "addr", s.cfg.ListenAddr)

	go s.acceptLoop()
	go s.jobLoop()

	return nil
}

// Stop shuts down the stratum server and disconnects all workers.
func (s *Server) Stop() {
	if !s.running.Load() {
		return
	}
	s.running.Store(false)
	if s.cancel != nil {
		s.cancel()
	}
	if s.listener != nil {
		s.listener.Close()
	}

	s.mu.Lock()
	for w := range s.workers {
		w.conn.Close()
	}
	s.mu.Unlock()

	logging.L.Info("stratum server stopped", "component", "stratum")
}

// Running returns true if the server is accepting connections.
func (s *Server) Running() bool {
	return s.running.Load()
}

// ListenAddr returns the address the server is listening on, or the configured
// address if not yet started.
func (s *Server) ListenAddr() string {
	if s.listener != nil {
		return s.listener.Addr().String()
	}
	return s.cfg.ListenAddr
}

// Workers returns info about all connected workers.
func (s *Server) Workers() []WorkerInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	infos := make([]WorkerInfo, 0, len(s.workers))
	for w := range s.workers {
		w.mu.Lock()
		diff := w.difficulty
		w.mu.Unlock()

		valid := w.sharesValid.Load()
		elapsed := time.Since(w.connectedAt).Seconds()
		var hashrate float64
		if elapsed > 0 && valid > 0 {
			// Approximate: shares * difficulty * 2^32 / elapsed
			hashrate = float64(valid) * diff * 4294967296.0 / elapsed
		}

		infos = append(infos, WorkerInfo{
			Name:          w.name,
			Addr:          w.addr,
			ConnectedAt:   w.connectedAt.Unix(),
			SharesValid:   valid,
			SharesStale:   w.sharesStale.Load(),
			SharesInvalid: w.sharesInvalid.Load(),
			Difficulty:    diff,
			LastShareAt:   w.lastShareAt.Unix(),
			Hashrate:      hashrate,
		})
	}
	return infos
}

// Stats returns aggregate server statistics.
func (s *Server) Stats() map[string]interface{} {
	s.mu.RLock()
	workerCount := len(s.workers)
	s.mu.RUnlock()

	return map[string]interface{}{
		"running":      s.running.Load(),
		"listenAddr":   s.cfg.ListenAddr,
		"workers":      workerCount,
		"sharesValid":  s.sharesValid.Load(),
		"sharesStale":  s.sharesStale.Load(),
		"blocksFound":  s.blocksFound.Load(),
	}
}

func (s *Server) acceptLoop() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.ctx.Done():
				return
			default:
			}
			continue
		}
		go s.handleWorker(conn)
	}
}

func (s *Server) handleWorker(conn net.Conn) {
	s.extranonceMu.Lock()
	s.extranonceCounter++
	enonce := s.extranonceCounter
	s.extranonceMu.Unlock()

	w := &worker{
		conn:        conn,
		addr:        conn.RemoteAddr().String(),
		difficulty:  s.cfg.VardiffMin,
		connectedAt: time.Now(),
		subscribedExtranonce: fmt.Sprintf("%08x", enonce),
		vardiffWindowStart: time.Now(),
		done:        make(chan struct{}),
	}

	s.mu.Lock()
	s.workers[w] = struct{}{}
	s.mu.Unlock()

	logging.L.Info("stratum worker connected", "component", "stratum", "addr", w.addr)

	defer func() {
		close(w.done)
		conn.Close()
		s.mu.Lock()
		delete(s.workers, w)
		s.mu.Unlock()
		logging.L.Info("stratum worker disconnected", "component", "stratum", "addr", w.addr, "name", w.name)
	}()

	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 0, 16384), 16384)

	for scanner.Scan() {
		select {
		case <-s.ctx.Done():
			return
		default:
		}

		line := scanner.Text()
		if line == "" {
			continue
		}

		var req stratumRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			continue
		}

		s.handleRequest(w, &req)
	}
}

// --- Stratum V1 protocol types ---

type stratumRequest struct {
	ID     interface{}       `json:"id"`
	Method string            `json:"method"`
	Params json.RawMessage   `json:"params"`
}

type stratumResponse struct {
	ID     interface{} `json:"id"`
	Result interface{} `json:"result"`
	Error  interface{} `json:"error"`
}

type stratumNotify struct {
	ID     interface{} `json:"id"`
	Method string      `json:"method"`
	Params interface{} `json:"params"`
}

func (s *Server) sendJSON(w *worker, v interface{}) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	data = append(data, '\n')
	w.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	w.conn.Write(data)
}

func (s *Server) handleRequest(w *worker, req *stratumRequest) {
	switch req.Method {
	case "mining.subscribe":
		s.handleSubscribe(w, req)
	case "mining.authorize":
		s.handleAuthorize(w, req)
	case "mining.submit":
		s.handleSubmit(w, req)
	case "mining.extranonce.subscribe":
		s.sendJSON(w, stratumResponse{ID: req.ID, Result: true})
	default:
		s.sendJSON(w, stratumResponse{ID: req.ID, Error: []interface{}{20, "unknown method", nil}})
	}
}

func (s *Server) handleSubscribe(w *worker, req *stratumRequest) {
	// Response: [[["mining.set_difficulty", subID], ["mining.notify", subID]], extranonce1, extranonce2_size]
	result := []interface{}{
		[][]string{
			{"mining.set_difficulty", "1"},
			{"mining.notify", "1"},
		},
		w.subscribedExtranonce,
		4, // extranonce2 size in bytes
	}
	s.sendJSON(w, stratumResponse{ID: req.ID, Result: result})

	// Clamp initial difficulty to network difficulty so shares are never
	// harder than the block target (important on testnets with very low diff).
	s.jobMu.RLock()
	j := s.currentJob
	s.jobMu.RUnlock()
	if j != nil {
		netDiff := targetToDifficulty(j.target)
		w.mu.Lock()
		if netDiff > 0 && w.difficulty > netDiff {
			w.difficulty = netDiff
		}
		w.mu.Unlock()
	}

	// Send initial difficulty
	s.sendSetDifficulty(w)

	// Send current job if available
	if j != nil {
		s.sendJob(w, j)
	}
}

func (s *Server) handleAuthorize(w *worker, req *stratumRequest) {
	var params []string
	if err := json.Unmarshal(req.Params, &params); err != nil || len(params) < 1 {
		s.sendJSON(w, stratumResponse{ID: req.ID, Result: false, Error: []interface{}{20, "invalid params", nil}})
		return
	}

	w.mu.Lock()
	w.name = params[0]
	w.authed = true
	w.mu.Unlock()

	s.sendJSON(w, stratumResponse{ID: req.ID, Result: true})
	logging.L.Info("stratum worker authorized", "component", "stratum", "worker", params[0], "addr", w.addr)
}

func (s *Server) handleSubmit(w *worker, req *stratumRequest) {
	// params: [worker_name, job_id, extranonce2, ntime, nonce]
	var params []string
	if err := json.Unmarshal(req.Params, &params); err != nil || len(params) < 5 {
		s.sendJSON(w, stratumResponse{ID: req.ID, Result: false, Error: []interface{}{20, "invalid params", nil}})
		w.sharesInvalid.Add(1)
		return
	}

	jobID := params[1]
	extranonce2Hex := params[2]
	ntimeHex := params[3]
	nonceHex := params[4]

	s.jobMu.RLock()
	j := s.currentJob
	s.jobMu.RUnlock()

	if j == nil || j.id != jobID {
		s.sendJSON(w, stratumResponse{ID: req.ID, Result: false, Error: []interface{}{21, "job not found", nil}})
		w.sharesStale.Add(1)
		s.sharesStale.Add(1)
		return
	}

	// Decode submitted values
	extranonce2, err := hex.DecodeString(extranonce2Hex)
	if err != nil || len(extranonce2) != 4 {
		s.sendJSON(w, stratumResponse{ID: req.ID, Result: false, Error: []interface{}{20, "invalid extranonce2", nil}})
		w.sharesInvalid.Add(1)
		return
	}

	ntime, err := decodeUint32Hex(ntimeHex)
	if err != nil {
		s.sendJSON(w, stratumResponse{ID: req.ID, Result: false, Error: []interface{}{20, "invalid ntime", nil}})
		w.sharesInvalid.Add(1)
		return
	}

	nonce, err := decodeUint32Hex(nonceHex)
	if err != nil {
		s.sendJSON(w, stratumResponse{ID: req.ID, Result: false, Error: []interface{}{20, "invalid nonce", nil}})
		w.sharesInvalid.Add(1)
		return
	}

	// Rebuild coinbase with extranonce1 + extranonce2
	extranonce1, _ := hex.DecodeString(w.subscribedExtranonce)
	coinbase := make([]byte, 0, len(j.coinbase1)+len(extranonce1)+len(extranonce2)+len(j.coinbase2))
	coinbase = append(coinbase, j.coinbase1...)
	coinbase = append(coinbase, extranonce1...)
	coinbase = append(coinbase, extranonce2...)
	coinbase = append(coinbase, j.coinbase2...)

	// Deserialize coinbase tx
	var coinbaseTx types.Transaction
	if err := coinbaseTx.Deserialize(bytes.NewReader(coinbase)); err != nil {
		s.sendJSON(w, stratumResponse{ID: req.ID, Result: false, Error: []interface{}{20, "coinbase deserialize failed", nil}})
		w.sharesInvalid.Add(1)
		return
	}

	// Compute merkle root from coinbase + branch
	coinbaseHash, _ := crypto.HashTransaction(&coinbaseTx)
	merkleRoot := computeMerkleRootFromBranch(coinbaseHash, j.merkleHashes)

	// Build header
	header := types.BlockHeader{
		Version:    j.version,
		PrevBlock:  j.prevBlock,
		MerkleRoot: merkleRoot,
		Timestamp:  ntime,
		Bits:       j.bits,
		Nonce:      nonce,
	}

	// Compute PoW hash
	var hdrBuf [types.BlockHeaderSize]byte
	header.SerializeInto(hdrBuf[:])
	powHash := s.hasher.PoWHash(hdrBuf[:])

	// Check against worker's share difficulty
	w.mu.Lock()
	shareDiff := w.difficulty
	w.mu.Unlock()
	shareTarget := difficultyToTarget(shareDiff)

	// DEBUG: log header, hash, and target for share validation diagnosis
	logging.L.Info("stratum share debug",
		"component", "stratum",
		"nonce_hex", nonceHex,
		"ntime_hex", ntimeHex,
		"nonce_dec", nonce,
		"ntime_dec", ntime,
		"version", j.version,
		"bits", fmt.Sprintf("%08x", j.bits),
		"header_hex", hex.EncodeToString(hdrBuf[:]),
		"pow_hash", hex.EncodeToString(powHash[:]),
		"share_diff", shareDiff,
		"share_target", hex.EncodeToString(shareTarget[:]),
		"net_target", hex.EncodeToString(j.target[:]),
		"hash_le_target", powHash.LessOrEqual(shareTarget),
	)

	if !powHash.LessOrEqual(shareTarget) {
		s.sendJSON(w, stratumResponse{ID: req.ID, Result: false, Error: []interface{}{23, "low difficulty share", nil}})
		w.sharesInvalid.Add(1)
		return
	}

	// Valid share
	w.sharesValid.Add(1)
	w.lastShareAt = time.Now()
	s.sharesValid.Add(1)

	// Vardiff adjustment
	s.adjustVardiff(w)

	// Check if it also meets the network target
	if powHash.LessOrEqual(j.target) {
		// Found a block!
		txs := make([]types.Transaction, 0, 1+len(j.txs))
		txs = append(txs, coinbaseTx)
		txs = append(txs, j.txs...)

		block := &types.Block{
			Header:       header,
			Transactions: txs,
		}

		s.blocksFound.Add(1)
		blockHash := crypto.HashBlockHeader(&header)
		logging.L.Info("stratum: block found!",
			"component", "stratum",
			"hash", blockHash.ReverseString(),
			"height", j.height,
			"worker", w.name)

		if s.onBlock != nil {
			s.onBlock(block)
		}
	}

	s.sendJSON(w, stratumResponse{ID: req.ID, Result: true})
}

// --- Job generation ---

func (s *Server) jobLoop() {
	// Generate initial job
	s.generateJob()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	var lastTipHash types.Hash
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			tipHash, _ := s.chain.Tip()
			if tipHash != lastTipHash {
				lastTipHash = tipHash
				s.generateJob()
				s.broadcastJob(true)
			} else {
				// Update timestamp on existing job
				s.updateJobTimestamp()
			}
		}
	}
}

func (s *Server) generateJob() {
	tipHash, tipHeight := s.chain.Tip()
	tipHeader, err := s.chain.TipHeader()
	if err != nil {
		return
	}

	newHeight := tipHeight + 1
	subsidy := s.params.CalcSubsidy(newHeight)

	tmpl := s.mempool.BlockTemplate()
	totalFees := tmpl.TotalFees

	// Build coinbase transaction, split into coinbase1 and coinbase2 around
	// the extranonce injection point.
	coinbase1, coinbase2 := s.buildSplitCoinbase(newHeight, subsidy+totalFees)

	// Collect non-coinbase transactions
	var txs []types.Transaction
	for _, tx := range tmpl.Transactions {
		txs = append(txs, *tx)
	}

	// Compute merkle branch (hashes of non-coinbase txs for the worker to merge)
	var merkleBranch []types.Hash
	for _, tx := range txs {
		h, _ := crypto.HashTransaction(&tx)
		merkleBranch = append(merkleBranch, h)
	}

	ts := uint32(time.Now().Unix())
	if ts <= tipHeader.Timestamp {
		ts = tipHeader.Timestamp + 1
	}

	// Get difficulty bits for the next block
	bits := tipHeader.Bits
	target := crypto.CompactToHash(bits)

	s.jobCounter++
	j := &job{
		id:           fmt.Sprintf("%x", s.jobCounter),
		prevBlock:    tipHash,
		coinbase1:    coinbase1,
		coinbase2:    coinbase2,
		merkleHashes: merkleBranch,
		version:      1,
		bits:         bits,
		timestamp:    ts,
		height:       newHeight,
		target:       target,
		txs:          txs,
		cleanJobs:    true,
	}

	s.jobMu.Lock()
	s.currentJob = j
	s.jobMu.Unlock()
}

func (s *Server) updateJobTimestamp() {
	s.jobMu.Lock()
	defer s.jobMu.Unlock()
	if s.currentJob == nil {
		return
	}
	s.currentJob.timestamp = uint32(time.Now().Unix())
}

func (s *Server) broadcastJob(clean bool) {
	s.jobMu.RLock()
	j := s.currentJob
	s.jobMu.RUnlock()
	if j == nil {
		return
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	for w := range s.workers {
		if w.authed {
			s.sendJob(w, j)
		}
	}
}

func (s *Server) sendJob(w *worker, j *job) {
	// mining.notify params:
	// [job_id, prevhash, coinb1, coinb2, merkle_branch[], version, nbits, ntime, clean_jobs]
	prevhashHex := stratumPrevhashHex(j.prevBlock)
	branchHexes := make([]string, 0, len(j.merkleHashes))
	for _, h := range j.merkleHashes {
		branchHexes = append(branchHexes, hex.EncodeToString(h[:]))
	}

	notify := stratumNotify{
		ID:     nil,
		Method: "mining.notify",
		Params: []interface{}{
			j.id,
			prevhashHex,
			hex.EncodeToString(j.coinbase1),
			hex.EncodeToString(j.coinbase2),
			branchHexes,
			fmt.Sprintf("%08x", j.version),
			fmt.Sprintf("%08x", j.bits),
			fmt.Sprintf("%08x", j.timestamp),
			j.cleanJobs,
		},
	}
	s.sendJSON(w, notify)
}

func (s *Server) sendSetDifficulty(w *worker) {
	w.mu.Lock()
	diff := w.difficulty
	w.mu.Unlock()

	notify := stratumNotify{
		ID:     nil,
		Method: "mining.set_difficulty",
		Params: []interface{}{diff},
	}
	s.sendJSON(w, notify)
}

// --- Vardiff ---

func (s *Server) adjustVardiff(w *worker) {
	w.mu.Lock()
	w.vardiffShareCount++
	count := w.vardiffShareCount
	elapsed := time.Since(w.vardiffWindowStart).Seconds()
	w.mu.Unlock()

	// Evaluate every 30 seconds or 10 shares, whichever comes first
	if elapsed < 30 && count < 10 {
		return
	}

	if elapsed <= 0 {
		return
	}

	sharesPerMin := float64(count) / (elapsed / 60.0)
	target := s.cfg.VardiffTargetSharesPerMin
	if target <= 0 {
		target = 20
	}

	ratio := sharesPerMin / target

	w.mu.Lock()
	defer w.mu.Unlock()

	newDiff := w.difficulty
	if ratio > 1.5 {
		newDiff = w.difficulty * ratio * 0.8 // increase, but not fully
	} else if ratio < 0.5 && ratio > 0 {
		newDiff = w.difficulty * ratio * 1.2 // decrease, but not fully
	} else {
		// Reset window, no change needed
		w.vardiffShareCount = 0
		w.vardiffWindowStart = time.Now()
		return
	}

	// Clamp
	if newDiff < s.cfg.VardiffMin {
		newDiff = s.cfg.VardiffMin
	}
	if s.cfg.VardiffMax > 0 && newDiff > s.cfg.VardiffMax {
		newDiff = s.cfg.VardiffMax
	}

	if newDiff != w.difficulty {
		w.difficulty = newDiff
		w.vardiffShareCount = 0
		w.vardiffWindowStart = time.Now()

		// Send new difficulty (must unlock before sending)
		diff := w.difficulty
		go func() {
			notify := stratumNotify{
				ID:     nil,
				Method: "mining.set_difficulty",
				Params: []interface{}{diff},
			}
			s.sendJSON(w, notify)
		}()
	}
}

// --- Coinbase construction ---

func (s *Server) buildSplitCoinbase(height uint32, totalReward uint64) (coinbase1, coinbase2 []byte) {
	// coinbase1 = tx version + vin count + prevout + scriptSig length + height push + tag
	// (extranonce injection point)
	// coinbase2 = sequence + vout count + output value + pkscript + locktime

	pushLen := minimalHeightPushLen(height)
	heightBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(heightBytes, height)

	// scriptSig: [pushLen][height LE bytes][coinbase tag][8 bytes extranonce space]
	tag := []byte("/stratum/")
	scriptSigPre := make([]byte, 0, 1+pushLen+len(tag))
	scriptSigPre = append(scriptSigPre, byte(pushLen))
	scriptSigPre = append(scriptSigPre, heightBytes[:pushLen]...)
	scriptSigPre = append(scriptSigPre, tag...)

	// extranonce is 8 bytes (extranonce1=4 + extranonce2=4)
	extranonceSize := 8
	scriptSigTotalLen := len(scriptSigPre) + extranonceSize

	// Build coinbase1: version(4) + txin_count(1=varint) + prevout(36) + scriptSig_len(varint) + scriptSigPre
	cb1 := make([]byte, 0, 128)
	// version
	var verBuf [4]byte
	binary.LittleEndian.PutUint32(verBuf[:], 1)
	cb1 = append(cb1, verBuf[:]...)
	// txin count = 1
	cb1 = append(cb1, 0x01)
	// prevout: 32 zero bytes (coinbase) + index 0xFFFFFFFF
	cb1 = append(cb1, types.CoinbaseOutPoint.Hash[:]...)
	var idxBuf [4]byte
	binary.LittleEndian.PutUint32(idxBuf[:], types.CoinbaseOutPoint.Index)
	cb1 = append(cb1, idxBuf[:]...)
	// scriptSig length as varint
	cb1 = append(cb1, byte(scriptSigTotalLen))
	// scriptSig prefix (up to extranonce injection point)
	cb1 = append(cb1, scriptSigPre...)

	// Build coinbase2: sequence(4) + vout_count(1) + value(8) + pkscript_len(varint) + pkscript + locktime(4)
	cb2 := make([]byte, 0, 64+len(s.rewardScript))
	var seqBuf [4]byte
	binary.LittleEndian.PutUint32(seqBuf[:], 0xFFFFFFFF)
	cb2 = append(cb2, seqBuf[:]...)
	// vout count = 1
	cb2 = append(cb2, 0x01)
	// value
	var valBuf [8]byte
	binary.LittleEndian.PutUint64(valBuf[:], totalReward)
	cb2 = append(cb2, valBuf[:]...)
	// pkscript len as varint
	cb2 = append(cb2, byte(len(s.rewardScript)))
	cb2 = append(cb2, s.rewardScript...)
	// locktime
	cb2 = append(cb2, 0, 0, 0, 0)

	return cb1, cb2
}

// --- Helpers ---

func minimalHeightPushLen(height uint32) int {
	switch {
	case height <= 0xFF:
		return 1
	case height <= 0xFFFF:
		return 2
	case height <= 0xFFFFFF:
		return 3
	default:
		return 4
	}
}

func computeMerkleRootFromBranch(coinbaseHash types.Hash, branch []types.Hash) types.Hash {
	current := coinbaseHash
	for _, h := range branch {
		combined := make([]byte, 64)
		copy(combined[:32], current[:])
		copy(combined[32:], h[:])
		current = crypto.DoubleSHA256(combined)
	}
	return current
}

// stratumPrevhashHex encodes a hash for the stratum prevhash field.
// Bitcoin stratum convention: each 4-byte group is byte-swapped from the
// internal LE representation so that cpuminer's le32dec + bswap32 pipeline
// reconstructs the original LE bytes for hashing.
func stratumPrevhashHex(h types.Hash) string {
	var out [32]byte
	for i := 0; i < 32; i += 4 {
		out[i+0] = h[i+3]
		out[i+1] = h[i+2]
		out[i+2] = h[i+1]
		out[i+3] = h[i+0]
	}
	return hex.EncodeToString(out[:])
}

func decodeUint32Hex(s string) (uint32, error) {
	s = strings.TrimPrefix(s, "0x")
	if len(s) != 8 {
		return 0, fmt.Errorf("expected 8 hex chars, got %d", len(s))
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint32(b), nil
}

// targetToDifficulty converts a 256-bit target hash to a stratum difficulty value.
func targetToDifficulty(target types.Hash) float64 {
	// Convert LE hash to big-endian big.Int
	var be [32]byte
	for i := 0; i < 32; i++ {
		be[i] = target[31-i]
	}
	targetInt := new(big.Int).SetBytes(be[:])
	if targetInt.Sign() <= 0 {
		return 0
	}
	diff1, _ := new(big.Int).SetString("00000000FFFF0000000000000000000000000000000000000000000000000000", 16)
	diff := new(big.Float).SetInt(diff1)
	diff.Quo(diff, new(big.Float).SetInt(targetInt))
	f, _ := diff.Float64()
	return f
}

// difficultyToTarget converts a stratum difficulty value to a 256-bit target hash.
// difficulty 1 = target 0x00000000FFFF0000000000000000000000000000000000000000000000000000
func difficultyToTarget(diff float64) types.Hash {
	if diff <= 0 {
		diff = 1
	}

	// diff1 target (Bitcoin standard)
	diff1, _ := new(big.Int).SetString("00000000FFFF0000000000000000000000000000000000000000000000000000", 16)

	// target = diff1 / difficulty
	target := new(big.Float).SetInt(diff1)
	target.Quo(target, new(big.Float).SetFloat64(diff))

	targetInt, _ := target.Int(nil)
	b := targetInt.Bytes()

	var h types.Hash
	// Write in little-endian (internal hash order)
	for i := 0; i < len(b) && i < 32; i++ {
		h[31-i] = b[i]
	}
	return h
}
