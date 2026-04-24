// Copyright (c) 2024-2026 The Fairchain Contributors
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

package stratum

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
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

const (
	// maxJobHistory is how many recent jobs are retained for stale share acceptance.
	maxJobHistory = 16

	// maxNtimeRoll is the maximum forward ntime rolling allowed (ckpool uses 7000).
	maxNtimeRoll = 7000

	// updateInterval is the periodic work refresh interval (matches ckpool default).
	updateInterval = 30 * time.Second

	// tipPollInterval is how frequently we check for a new chain tip.
	tipPollInterval = 5 * time.Second

	// maxDupeTracked is the max number of tracked share hashes per job.
	maxDupeTracked = 65536
)

// Config holds stratum server configuration.
type Config struct {
	ListenAddr string  // TCP listen address (e.g. "0.0.0.0:3333")
	VardiffMin float64 // minimum share difficulty
	VardiffMax float64 // maximum share difficulty (0 = network diff)
	StartDiff  float64 // starting difficulty for new workers (0 = VardiffMin)
	// Target shares per minute per worker for vardiff adjustment.
	VardiffTargetSharesPerMin float64
}

// DefaultConfig returns sensible defaults for a solo/small-pool stratum server.
func DefaultConfig() Config {
	return Config{
		ListenAddr:                "0.0.0.0:3333",
		VardiffMin:                0.001,
		VardiffMax:                0, // network difficulty
		StartDiff:                 0, // use VardiffMin
		VardiffTargetSharesPerMin: 20,
	}
}

// WorkerInfo holds stats for a connected stratum worker (exported for UI).
type WorkerInfo struct {
	Name          string  `json:"name"`
	Addr          string  `json:"addr"`
	ConnectedAt   int64   `json:"connectedAt"`
	SharesValid   int64   `json:"sharesValid"`
	SharesStale   int64   `json:"sharesStale"`
	SharesInvalid int64   `json:"sharesInvalid"`
	Difficulty    float64 `json:"difficulty"`
	LastShareAt   int64   `json:"lastShareAt"`
	Hashrate      float64 `json:"hashrate"`
}

type worker struct {
	conn   net.Conn
	name   string
	addr   string
	authed bool

	mu          sync.Mutex
	difficulty  float64
	suggestDiff float64 // miner-suggested difficulty (via mining.suggest_difficulty)
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
	merkleHashes []types.Hash // merkle branch (proper binary tree path)
	version    uint32
	bits       uint32
	timestamp  uint32
	height     uint32
	target     types.Hash
	txs        []types.Transaction
	cleanJobs  bool
	createdAt  time.Time

	// Duplicate share tracking: set of PoW hash hex strings
	dupeMu sync.Mutex
	dupes  map[string]struct{}
}

func (j *job) isDuplicateShare(powHash types.Hash) bool {
	key := hex.EncodeToString(powHash[:])
	j.dupeMu.Lock()
	defer j.dupeMu.Unlock()
	if j.dupes == nil {
		j.dupes = make(map[string]struct{})
	}
	if _, exists := j.dupes[key]; exists {
		return true
	}
	if len(j.dupes) < maxDupeTracked {
		j.dupes[key] = struct{}{}
	}
	return false
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

	jobMu    sync.RWMutex
	jobs     map[string]*job // all active jobs by ID
	currentJob *job          // most recent job

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
		jobs:         make(map[string]*job),
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

	startDiff := s.cfg.StartDiff
	if startDiff <= 0 {
		startDiff = s.cfg.VardiffMin
	}

	w := &worker{
		conn:        conn,
		addr:        conn.RemoteAddr().String(),
		difficulty:  startDiff,
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

		logging.StratumDebug("<<< recv from worker",
			"addr", w.addr,
			"raw", line,
		)

		var req stratumRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			logging.StratumDebug("bad JSON from worker", "addr", w.addr, "error", err)
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
	logging.StratumDebug(">>> send to worker",
		"addr", w.addr,
		"json", string(data),
	)
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
	case "mining.suggest_difficulty":
		s.handleSuggestDifficulty(w, req)
	case "mining.extranonce.subscribe":
		s.sendJSON(w, stratumResponse{ID: req.ID, Result: true})
	default:
		// Handle mining.suggest* prefix match (ckpool-compatible)
		if strings.HasPrefix(req.Method, "mining.suggest") {
			s.handleSuggestDifficulty(w, req)
			return
		}
		s.sendJSON(w, stratumResponse{ID: req.ID, Error: []interface{}{20, "unknown method", nil}})
	}
}

func (s *Server) handleSubscribe(w *worker, req *stratumRequest) {
	result := []interface{}{
		[][]string{
			{"mining.set_difficulty", "1"},
			{"mining.notify", "1"},
		},
		w.subscribedExtranonce,
		4, // extranonce2 size in bytes
	}
	logging.StratumDebug("subscribe",
		"addr", w.addr,
		"extranonce1", w.subscribedExtranonce,
		"extranonce2_size", 4,
	)
	s.sendJSON(w, stratumResponse{ID: req.ID, Result: result})

	// Clamp initial difficulty to network difficulty
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

	s.sendSetDifficulty(w)

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

func (s *Server) handleSuggestDifficulty(w *worker, req *stratumRequest) {
	var params []interface{}
	if err := json.Unmarshal(req.Params, &params); err != nil || len(params) < 1 {
		s.sendJSON(w, stratumResponse{ID: req.ID, Result: true})
		return
	}

	var sugDiff float64
	switch v := params[0].(type) {
	case float64:
		sugDiff = v
	case json.Number:
		sugDiff, _ = v.Float64()
	}

	if sugDiff <= 0 {
		s.sendJSON(w, stratumResponse{ID: req.ID, Result: true})
		return
	}

	// Clamp to pool minimum
	if sugDiff < s.cfg.VardiffMin {
		sugDiff = s.cfg.VardiffMin
	}

	w.mu.Lock()
	w.suggestDiff = sugDiff
	if sugDiff != w.difficulty {
		w.difficulty = sugDiff
	}
	w.mu.Unlock()

	s.sendSetDifficulty(w)
	s.sendJSON(w, stratumResponse{ID: req.ID, Result: true})
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

	// Look up job from history (supports stale share acceptance)
	s.jobMu.RLock()
	j := s.jobs[jobID]
	currentJobID := ""
	if s.currentJob != nil {
		currentJobID = s.currentJob.id
	}
	s.jobMu.RUnlock()

	if j == nil {
		s.sendJSON(w, stratumResponse{ID: req.ID, Result: false, Error: []interface{}{21, "job not found", nil}})
		w.sharesStale.Add(1)
		s.sharesStale.Add(1)
		return
	}

	isStale := j.id != currentJobID

	// Decode submitted values
	extranonce2, err := hex.DecodeString(extranonce2Hex)
	if err != nil || len(extranonce2) != 4 {
		s.sendJSON(w, stratumResponse{ID: req.ID, Result: false, Error: []interface{}{20, "invalid extranonce2", nil}})
		w.sharesInvalid.Add(1)
		return
	}

	ntime, err := decodeUint32BE(ntimeHex)
	if err != nil {
		s.sendJSON(w, stratumResponse{ID: req.ID, Result: false, Error: []interface{}{20, "invalid ntime", nil}})
		w.sharesInvalid.Add(1)
		return
	}

	nonce, err := decodeUint32LE(nonceHex)
	if err != nil {
		s.sendJSON(w, stratumResponse{ID: req.ID, Result: false, Error: []interface{}{20, "invalid nonce", nil}})
		w.sharesInvalid.Add(1)
		return
	}

	// Ntime validation (ckpool-compatible: must be >= job ntime and <= job ntime + maxNtimeRoll)
	if ntime < j.timestamp || ntime > j.timestamp+maxNtimeRoll {
		s.sendJSON(w, stratumResponse{ID: req.ID, Result: false, Error: []interface{}{20, "invalid ntime", nil}})
		w.sharesInvalid.Add(1)
		return
	}

	logging.StratumDebug("submit: decoded params",
		"worker", w.name,
		"job_id", jobID,
		"stale", isStale,
		"extranonce1", w.subscribedExtranonce,
		"extranonce2_hex", extranonce2Hex,
		"ntime_hex", ntimeHex, "ntime_dec", ntime,
		"nonce_hex", nonceHex, "nonce_dec", nonce,
	)

	// Rebuild coinbase with extranonce1 + extranonce2
	extranonce1, _ := hex.DecodeString(w.subscribedExtranonce)
	coinbase := make([]byte, 0, len(j.coinbase1)+len(extranonce1)+len(extranonce2)+len(j.coinbase2))
	coinbase = append(coinbase, j.coinbase1...)
	coinbase = append(coinbase, extranonce1...)
	coinbase = append(coinbase, extranonce2...)
	coinbase = append(coinbase, j.coinbase2...)

	logging.StratumDebug("submit: coinbase assembled",
		"coinbase1_hex", hex.EncodeToString(j.coinbase1),
		"extranonce1_hex", hex.EncodeToString(extranonce1),
		"extranonce2_hex", hex.EncodeToString(extranonce2),
		"coinbase2_hex", hex.EncodeToString(j.coinbase2),
		"full_coinbase_hex", hex.EncodeToString(coinbase),
	)

	var coinbaseTx types.Transaction
	if err := coinbaseTx.Deserialize(bytes.NewReader(coinbase)); err != nil {
		logging.StratumDebug("submit: coinbase deserialize FAILED", "error", err)
		s.sendJSON(w, stratumResponse{ID: req.ID, Result: false, Error: []interface{}{20, "coinbase deserialize failed", nil}})
		w.sharesInvalid.Add(1)
		return
	}

	coinbaseHash, _ := crypto.HashTransaction(&coinbaseTx)
	merkleRoot := computeMerkleRootFromBranch(coinbaseHash, j.merkleHashes)
	merkleRootBE := merkleRoot.Reversed()

	logging.StratumDebug("submit: merkle computed",
		"coinbase_hash_LE", hex.EncodeToString(coinbaseHash[:]),
		"merkle_root_LE", hex.EncodeToString(merkleRoot[:]),
		"merkle_root_BE", hex.EncodeToString(merkleRootBE[:]),
		"merkle_branch_count", len(j.merkleHashes),
	)

	header := types.BlockHeader{
		Version:    j.version,
		PrevBlock:  j.prevBlock,
		MerkleRoot: merkleRootBE,
		Timestamp:  ntime,
		Bits:       j.bits,
		Nonce:      nonce,
	}

	var hdrBuf [types.BlockHeaderSize]byte
	header.SerializeInto(hdrBuf[:])
	powHash := s.hasher.PoWHash(hdrBuf[:])

	if logging.StratumDebugMode {
		seedHash := sha256.Sum256(hdrBuf[:])
		logging.StratumDebug("submit: seed SHA256(header)",
			"seed_hex", hex.EncodeToString(seedHash[:]),
		)
	}

	// Duplicate share detection
	rawHash := powHash.Reversed()
	if j.isDuplicateShare(rawHash) {
		s.sendJSON(w, stratumResponse{ID: req.ID, Result: false, Error: []interface{}{22, "duplicate share", nil}})
		w.sharesInvalid.Add(1)
		return
	}

	// Share validation — compare in cpuminer's raw memory layout
	w.mu.Lock()
	shareDiff := w.difficulty
	w.mu.Unlock()
	shareTargetRaw := difficultyToTarget(shareDiff)

	netTargetRaw := j.target

	hashMeetsShareTarget := validHashRaw(rawHash, shareTargetRaw)
	hashMeetsNetTarget := validHashRaw(rawHash, netTargetRaw)

	if logging.StratumDebugMode {
		logging.StratumDebug("submit: share validation",
			"header_hex", hex.EncodeToString(hdrBuf[:]),
			"header_nonce", fmt.Sprintf("%08x (%d)", header.Nonce, header.Nonce),
		)
		hashU32 := fmt.Sprintf("%08x %08x %08x %08x %08x %08x %08x %08x",
			binary.LittleEndian.Uint32(rawHash[0:4]),
			binary.LittleEndian.Uint32(rawHash[4:8]),
			binary.LittleEndian.Uint32(rawHash[8:12]),
			binary.LittleEndian.Uint32(rawHash[12:16]),
			binary.LittleEndian.Uint32(rawHash[16:20]),
			binary.LittleEndian.Uint32(rawHash[20:24]),
			binary.LittleEndian.Uint32(rawHash[24:28]),
			binary.LittleEndian.Uint32(rawHash[28:32]))
		targetU32 := fmt.Sprintf("%08x %08x %08x %08x %08x %08x %08x %08x",
			binary.LittleEndian.Uint32(shareTargetRaw[0:4]),
			binary.LittleEndian.Uint32(shareTargetRaw[4:8]),
			binary.LittleEndian.Uint32(shareTargetRaw[8:12]),
			binary.LittleEndian.Uint32(shareTargetRaw[12:16]),
			binary.LittleEndian.Uint32(shareTargetRaw[16:20]),
			binary.LittleEndian.Uint32(shareTargetRaw[20:24]),
			binary.LittleEndian.Uint32(shareTargetRaw[24:28]),
			binary.LittleEndian.Uint32(shareTargetRaw[28:32]))
		logging.StratumDebug("submit: raw hash u32",
			"hash_raw_bytes", hex.EncodeToString(rawHash[:]),
			"hash_u32", hashU32,
		)
		logging.StratumDebug("submit: share target u32",
			"target_raw_bytes", hex.EncodeToString(shareTargetRaw[:]),
			"target_u32", targetU32,
			"share_diff", shareDiff,
		)
		logging.StratumDebug("submit: comparison result",
			"hash_le_share_target", hashMeetsShareTarget,
			"hash_le_net_target", hashMeetsNetTarget,
		)
	}

	if !hashMeetsShareTarget {
		logging.StratumDebug("submit: REJECTED — hash > share target")
		s.sendJSON(w, stratumResponse{ID: req.ID, Result: false, Error: []interface{}{23, "low difficulty share", nil}})
		w.sharesInvalid.Add(1)
		return
	}

	// Stale shares that meet difficulty are still counted (ckpool behavior)
	if isStale {
		logging.StratumDebug("submit: stale share accepted (difficulty met)",
			"worker", w.name, "job_id", jobID)
	}

	logging.StratumDebug("submit: ACCEPTED — valid share",
		"worker", w.name,
		"nonce", fmt.Sprintf("0x%08x", nonce),
		"stale", isStale,
	)
	w.sharesValid.Add(1)
	w.lastShareAt = time.Now()
	s.sharesValid.Add(1)

	s.adjustVardiff(w)

	// Check if it also meets the network target (only non-stale blocks)
	if hashMeetsNetTarget && !isStale {
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
	s.generateJob()

	tipTicker := time.NewTicker(tipPollInterval)
	defer tipTicker.Stop()

	updateTicker := time.NewTicker(updateInterval)
	defer updateTicker.Stop()

	var lastTipHash types.Hash
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-tipTicker.C:
			tipHash, _ := s.chain.Tip()
			if tipHash != lastTipHash {
				lastTipHash = tipHash
				s.generateJob()
				s.broadcastJob(true)
			}
		case <-updateTicker.C:
			// Periodic work refresh with updated timestamp and transactions.
			// clean_jobs=false so miners don't discard in-progress work.
			s.generatePeriodicJob()
			s.broadcastJob(false)
		}
	}
}

func (s *Server) generateJob() {
	s.generateJobInternal(true)
}

func (s *Server) generatePeriodicJob() {
	s.generateJobInternal(false)
}

func (s *Server) generateJobInternal(clean bool) {
	tipHash, tipHeight := s.chain.Tip()
	tipHeader, err := s.chain.TipHeader()
	if err != nil {
		return
	}

	newHeight := tipHeight + 1
	subsidy := s.params.CalcSubsidy(newHeight)

	tmpl := s.mempool.BlockTemplate()
	totalFees := tmpl.TotalFees

	coinbase1, coinbase2 := s.buildSplitCoinbase(newHeight, subsidy+totalFees)

	var txs []types.Transaction
	for _, tx := range tmpl.Transactions {
		txs = append(txs, *tx)
	}

	// Compute proper Stratum merkle branch (binary tree path)
	merkleBranch := computeMerkleBranch(txs)

	ts := uint32(time.Now().Unix())
	if ts <= tipHeader.Timestamp {
		ts = tipHeader.Timestamp + 1
	}

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
		cleanJobs:    clean,
		createdAt:    time.Now(),
	}

	logging.StratumDebug("job generated",
		"job_id", j.id,
		"height", newHeight,
		"bits", fmt.Sprintf("0x%08x", bits),
		"target_LE", hex.EncodeToString(target[:]),
		"target_diff", targetToDifficulty(target),
		"prevblock_LE", hex.EncodeToString(tipHash[:]),
		"prevhash_stratum", stratumPrevhashHex(tipHash),
		"timestamp", ts,
		"version", fmt.Sprintf("0x%08x", j.version),
		"coinbase1_hex", hex.EncodeToString(coinbase1),
		"coinbase2_hex", hex.EncodeToString(coinbase2),
		"tx_count", len(txs),
		"clean_jobs", clean,
		"merkle_branch_len", len(merkleBranch),
	)

	s.jobMu.Lock()
	s.jobs[j.id] = j
	s.currentJob = j

	// Prune old jobs beyond maxJobHistory
	if len(s.jobs) > maxJobHistory {
		var oldest *job
		for _, old := range s.jobs {
			if old.id == j.id {
				continue
			}
			if oldest == nil || old.createdAt.Before(oldest.createdAt) {
				oldest = old
			}
		}
		if oldest != nil {
			delete(s.jobs, oldest.id)
		}
	}
	s.jobMu.Unlock()
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
			s.sendJobWithClean(w, j, clean)
		}
	}
}

func (s *Server) sendJob(w *worker, j *job) {
	s.sendJobWithClean(w, j, j.cleanJobs)
}

func (s *Server) sendJobWithClean(w *worker, j *job, clean bool) {
	prevhashHex := stratumPrevhashHex(j.prevBlock)
	branchHexes := make([]string, 0, len(j.merkleHashes))
	for _, h := range j.merkleHashes {
		branchHexes = append(branchHexes, hex.EncodeToString(h[:]))
	}

	versionHex := fmt.Sprintf("%08x", j.version)
	bitsHex := fmt.Sprintf("%08x", j.bits)
	ntimeHex := fmt.Sprintf("%08x", j.timestamp)

	logging.StratumDebug("sendJob: mining.notify",
		"addr", w.addr,
		"job_id", j.id,
		"height", j.height,
		"prevhash_stratum", prevhashHex,
		"version_hex", versionHex,
		"bits_hex", bitsHex,
		"ntime_hex", ntimeHex,
		"ntime_dec", j.timestamp,
		"clean", clean,
	)

	notify := stratumNotify{
		ID:     nil,
		Method: "mining.notify",
		Params: []interface{}{
			j.id,
			prevhashHex,
			hex.EncodeToString(j.coinbase1),
			hex.EncodeToString(j.coinbase2),
			branchHexes,
			versionHex,
			bitsHex,
			ntimeHex,
			clean,
		},
	}
	s.sendJSON(w, notify)
}

func (s *Server) sendSetDifficulty(w *worker) {
	w.mu.Lock()
	diff := w.difficulty
	w.mu.Unlock()

	diffTarget := difficultyToTarget(diff)
	logging.StratumDebug("sendSetDifficulty",
		"addr", w.addr,
		"difficulty", diff,
		"target_LE", hex.EncodeToString(diffTarget[:]),
	)

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
		newDiff = w.difficulty * ratio * 0.8
	} else if ratio < 0.5 && ratio > 0 {
		newDiff = w.difficulty * ratio * 1.2
	} else {
		w.vardiffShareCount = 0
		w.vardiffWindowStart = time.Now()
		return
	}

	// Clamp to configured min
	if newDiff < s.cfg.VardiffMin {
		newDiff = s.cfg.VardiffMin
	}

	// Honour miner-suggested difficulty as floor
	if w.suggestDiff > 0 && newDiff < w.suggestDiff {
		newDiff = w.suggestDiff
	}

	// Clamp to configured max
	if s.cfg.VardiffMax > 0 && newDiff > s.cfg.VardiffMax {
		newDiff = s.cfg.VardiffMax
	}

	// Always clamp to network difficulty (ckpool behavior)
	s.jobMu.RLock()
	cj := s.currentJob
	s.jobMu.RUnlock()
	if cj != nil {
		netDiff := targetToDifficulty(cj.target)
		if netDiff > 0 && newDiff > netDiff {
			newDiff = netDiff
		}
	}

	if newDiff != w.difficulty {
		w.difficulty = newDiff
		w.vardiffShareCount = 0
		w.vardiffWindowStart = time.Now()

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
	pushLen := minimalHeightPushLen(height)
	heightBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(heightBytes, height)

	tag := []byte("/stratum/")
	scriptSigPre := make([]byte, 0, 1+pushLen+len(tag))
	scriptSigPre = append(scriptSigPre, byte(pushLen))
	scriptSigPre = append(scriptSigPre, heightBytes[:pushLen]...)
	scriptSigPre = append(scriptSigPre, tag...)

	extranonceSize := 8 // extranonce1=4 + extranonce2=4
	scriptSigTotalLen := len(scriptSigPre) + extranonceSize

	cb1 := make([]byte, 0, 128)
	var verBuf [4]byte
	binary.LittleEndian.PutUint32(verBuf[:], 1)
	cb1 = append(cb1, verBuf[:]...)
	cb1 = append(cb1, 0x01)
	cb1 = append(cb1, types.CoinbaseOutPoint.Hash[:]...)
	var idxBuf [4]byte
	binary.LittleEndian.PutUint32(idxBuf[:], types.CoinbaseOutPoint.Index)
	cb1 = append(cb1, idxBuf[:]...)
	cb1 = append(cb1, byte(scriptSigTotalLen))
	cb1 = append(cb1, scriptSigPre...)

	cb2 := make([]byte, 0, 64+len(s.rewardScript))
	var seqBuf [4]byte
	binary.LittleEndian.PutUint32(seqBuf[:], 0xFFFFFFFF)
	cb2 = append(cb2, seqBuf[:]...)
	cb2 = append(cb2, 0x01)
	var valBuf [8]byte
	binary.LittleEndian.PutUint64(valBuf[:], totalReward)
	cb2 = append(cb2, valBuf[:]...)
	cb2 = append(cb2, byte(len(s.rewardScript)))
	cb2 = append(cb2, s.rewardScript...)
	cb2 = append(cb2, 0, 0, 0, 0)

	return cb1, cb2
}

// --- Merkle branch computation (proper binary tree path for Stratum) ---
// Stratum's merkle branch is the sibling hashes needed to reconstruct the
// merkle root from the coinbase hash. For a block with txs [cb, tx1, tx2, ...],
// the merkle tree is built as a binary tree; the branch contains one sibling
// per tree level, always on the coinbase path's other side.

func computeMerkleBranch(txs []types.Transaction) []types.Hash {
	if len(txs) == 0 {
		return nil
	}

	// Start with tx hashes (non-coinbase transactions).
	// The coinbase will be hashed by the miner, so the branch needs sibling
	// hashes at each level starting from the bottom.
	hashes := make([]types.Hash, len(txs))
	for i, tx := range txs {
		h, _ := crypto.HashTransaction(&tx)
		hashes[i] = h
	}

	// The branch is built by taking the right sibling at index 1 (the first tx
	// in the layer), then combining pairs to form the next layer, and repeating.
	// This follows ckpool's wb_merkle_bin_txns approach.
	var branch []types.Hash
	for len(hashes) > 0 {
		// The first hash in the current layer is the sibling for the coinbase path
		branch = append(branch, hashes[0])

		if len(hashes) == 1 {
			break
		}

		// Combine adjacent pairs to form the next layer
		var next []types.Hash
		for i := 0; i < len(hashes); i += 2 {
			if i+1 < len(hashes) {
				combined := make([]byte, 64)
				copy(combined[:32], hashes[i][:])
				copy(combined[32:], hashes[i+1][:])
				next = append(next, crypto.DoubleSHA256(combined))
			} else {
				// Odd element — duplicate the last hash (Bitcoin merkle tree rule)
				combined := make([]byte, 64)
				copy(combined[:32], hashes[i][:])
				copy(combined[32:], hashes[i][:])
				next = append(next, crypto.DoubleSHA256(combined))
			}
		}
		hashes = next
	}

	return branch
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

// validHashRaw replicates cpuminer-opt's valid_hash() exactly.
// Both hash and target are in raw memory layout (as cpuminer stores them):
// 8 consecutive uint32 LE words, compared from word 7 (most significant,
// bytes 28-31) down to word 0 (least significant, bytes 0-3).
func validHashRaw(hash, target types.Hash) bool {
	for i := 7; i >= 0; i-- {
		h := binary.LittleEndian.Uint32(hash[i*4 : i*4+4])
		t := binary.LittleEndian.Uint32(target[i*4 : i*4+4])
		if h > t {
			return false
		}
		if h < t {
			return true
		}
	}
	return true
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

func decodeUint32BE(s string) (uint32, error) {
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

func decodeUint32LE(s string) (uint32, error) {
	s = strings.TrimPrefix(s, "0x")
	if len(s) != 8 {
		return 0, fmt.Errorf("expected 8 hex chars, got %d", len(s))
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(b), nil
}

// targetToDifficulty converts a 256-bit LE target hash to a stratum difficulty value.
// Uses the Bitcoin diff1 constant: target = diff1 / difficulty.
func targetToDifficulty(target types.Hash) float64 {
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

// difficultyToTarget replicates cpuminer-opt's diff_to_hash exactly.
// cpuminer (128-bit path) computes:
//
//	targ[0] = 0xFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF  (low 128 bits, all 1s)
//	targ[1] = (uint128)((1.0 / diff) * 2^96)      (high 128 bits)
//
// The result is stored in memory as two LE uint128s: targ[0] at bytes 0-15,
// targ[1] at bytes 16-31.
func difficultyToTarget(diff float64) types.Hash {
	if diff <= 0 {
		diff = 1
	}

	invDiff := new(big.Float).SetPrec(256).Quo(
		new(big.Float).SetPrec(256).SetFloat64(1.0),
		new(big.Float).SetPrec(256).SetFloat64(diff),
	)
	exp96 := new(big.Float).SetPrec(256).SetInt(new(big.Int).Lsh(big.NewInt(1), 96))
	high128f := new(big.Float).SetPrec(256).Mul(invDiff, exp96)
	high128, _ := high128f.Int(nil)

	max128 := new(big.Int).Lsh(big.NewInt(1), 128)
	max128.Sub(max128, big.NewInt(1))
	if high128.Cmp(max128) > 0 {
		high128.Set(max128)
	}

	var h types.Hash
	for i := 0; i < 16; i++ {
		h[i] = 0xFF
	}
	high128Bytes := high128.Bytes()
	for i := 0; i < len(high128Bytes) && i < 16; i++ {
		h[16+(15-i)] = high128Bytes[i]
	}
	return h
}
