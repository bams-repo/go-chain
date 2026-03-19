package rpc

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/bams-repo/fairchain/internal/coinparams"
	"github.com/bams-repo/fairchain/internal/crypto"
	"github.com/bams-repo/fairchain/internal/logging"
	"github.com/bams-repo/fairchain/internal/types"
)

// longPollState tracks the current template identity for BIP 22 long polling.
// When the tip changes or the mempool is significantly modified, waiting
// long-poll clients are woken up to fetch a fresh template.
type longPollState struct {
	mu      sync.Mutex
	current string
	waiters []chan struct{}
}

const maxLongPollWaiters = 100

func (lp *longPollState) currentID() string {
	lp.mu.Lock()
	defer lp.mu.Unlock()
	return lp.current
}

func (lp *longPollState) update(id string) {
	lp.mu.Lock()
	defer lp.mu.Unlock()
	if lp.current == id {
		return
	}
	lp.current = id
	for _, ch := range lp.waiters {
		close(ch)
	}
	lp.waiters = lp.waiters[:0]
}

func (lp *longPollState) wait() <-chan struct{} {
	lp.mu.Lock()
	defer lp.mu.Unlock()
	if len(lp.waiters) >= maxLongPollWaiters {
		ch := make(chan struct{})
		close(ch)
		return ch
	}
	ch := make(chan struct{})
	lp.waiters = append(lp.waiters, ch)
	return ch
}

// --- REST handlers (path-based routing) ---

func (s *Server) handleGetBlockTemplate(w http.ResponseWriter, r *http.Request) {
	result, rpcErr := s.rpcGetBlockTemplate(nil)
	if rpcErr != nil {
		writeError(w, http.StatusInternalServerError, rpcErr.Message)
		return
	}
	writeJSON(w, result)
}

func (s *Server) handleGetMiningInfo(w http.ResponseWriter, r *http.Request) {
	result, rpcErr := s.rpcGetMiningInfo(nil)
	if rpcErr != nil {
		writeError(w, http.StatusInternalServerError, rpcErr.Message)
		return
	}
	writeJSON(w, result)
}

func (s *Server) handleGetNetworkHashPS(w http.ResponseWriter, r *http.Request) {
	var params []json.RawMessage

	if nStr := r.URL.Query().Get("nblocks"); nStr != "" {
		params = append(params, json.RawMessage(nStr))
	}
	if hStr := r.URL.Query().Get("height"); hStr != "" {
		if len(params) == 0 {
			params = append(params, json.RawMessage("120"))
		}
		params = append(params, json.RawMessage(hStr))
	}

	result, rpcErr := s.rpcGetNetworkHashPS(params)
	if rpcErr != nil {
		writeError(w, http.StatusBadRequest, rpcErr.Message)
		return
	}
	writeJSON(w, result)
}

// --- JSON-RPC handlers (used by both JSON-RPC dispatcher and REST wrappers) ---

// templateRequest mirrors the BIP 22/23 template_request object that pool
// software sends as the first parameter to getblocktemplate.
type templateRequest struct {
	Mode         string   `json:"mode"`
	Capabilities []string `json:"capabilities"`
	Rules        []string `json:"rules"`
	LongPollID   string   `json:"longpollid"`
	Data         string   `json:"data"`
	WorkID       string   `json:"workid"`
}

func (tr *templateRequest) hasCapability(cap string) bool {
	for _, c := range tr.Capabilities {
		if c == cap {
			return true
		}
	}
	return false
}

// rpcGetBlockTemplate implements BIP 22/23 getblocktemplate with full pool
// compatibility. Supports template mode (default), proposal mode, long
// polling, coinbasetxn/coinbasevalue capabilities, and all required fields
// for ckpool, Braiins Pool, and other standard stratum server backends.
func (s *Server) rpcGetBlockTemplate(params []json.RawMessage) (interface{}, *jsonRPCError) {
	var tmplReq templateRequest
	if len(params) > 0 && len(params[0]) > 0 {
		if err := json.Unmarshal(params[0], &tmplReq); err != nil {
			return nil, newRPCError(rpcErrInvalidParams, "invalid template_request: "+err.Error())
		}
	}

	// BIP 23: block proposal mode.
	if tmplReq.Mode == "proposal" {
		return s.handleBlockProposal(tmplReq)
	}

	// BIP 22: long polling — block until the template changes.
	if tmplReq.LongPollID != "" {
		s.handleLongPoll(tmplReq.LongPollID)
	}

	tipHash, tipHeight := s.chain.Tip()
	tipHeader, err := s.chain.TipHeader()
	if err != nil {
		return nil, newRPCError(rpcErrInternal, "get tip header: "+err.Error())
	}

	newHeight := tipHeight + 1
	subsidy := s.params.CalcSubsidy(newHeight)

	mempoolTmpl := s.mempool.BlockTemplate()

	coinbaseValue := subsidy + mempoolTmpl.TotalFees

	nextBits := s.engine.CalcNextBits(tipHeader, tipHeight, s.chain.GetAncestor, s.params)

	target := crypto.CompactToBig(nextBits)
	var targetHex string
	if target.Sign() > 0 {
		targetHex = fmt.Sprintf("%064x", target)
	}

	curTime := uint32(time.Now().Unix())
	if curTime <= tipHeader.Timestamp {
		curTime = tipHeader.Timestamp + 1
	}

	info := s.chain.GetChainInfo()
	minTime := info.MedianTimePast + 1
	maxTime := curTime + uint32(s.params.MaxTimeFutureDrift.Seconds())

	txEntries := make([]interface{}, len(mempoolTmpl.Entries))
	for i, e := range mempoolTmpl.Entries {
		txBytes, serErr := e.Tx.SerializeToBytes()
		if serErr != nil {
			return nil, newRPCError(rpcErrInternal, fmt.Sprintf("serialize tx %d: %v", i, serErr))
		}

		txEntry := map[string]interface{}{
			"data":    hex.EncodeToString(txBytes),
			"txid":    e.TxID.ReverseString(),
			"hash":    e.TxID.ReverseString(),
			"fee":     e.Fee,
			"sigops":  0,
			"weight":  e.Size * 4,
			"depends": []int{},
		}
		txEntries[i] = txEntry
	}

	longpollID := fmt.Sprintf("%s%d", tipHash.ReverseString(), s.mempool.Count())
	s.longPoll.update(longpollID)

	const defaultSigOpLimit = 80000

	mutableFields := []string{
		"time", "time/increment",
		"transactions/add",
		"prevblock",
		"coinbase/append",
	}

	capabilities := []string{"proposal"}

	resp := map[string]interface{}{
		"version":           uint32(1),
		"rules":             []string{},
		"vbavailable":       map[string]interface{}{},
		"vbrequired":        0,
		"previousblockhash": tipHash.ReverseString(),
		"transactions":      txEntries,
		"coinbaseaux":       map[string]interface{}{"flags": ""},
		"coinbasevalue":     coinbaseValue,
		"longpollid":        longpollID,
		"target":            targetHex,
		"mintime":           minTime,
		"maxtime":           maxTime,
		"mutable":           mutableFields,
		"noncerange":        "00000000ffffffff",
		"sigoplimit":        defaultSigOpLimit,
		"sizelimit":         s.params.MaxBlockSize,
		"weightlimit":       s.params.MaxBlockSize * 4,
		"curtime":           curTime,
		"bits":              fmt.Sprintf("%08x", nextBits),
		"height":            newHeight,
		"capabilities":      capabilities,
		"expires":           120,
	}

	// BIP 22: if the client advertises "coinbasetxn" capability, include a
	// default coinbase transaction the pool can use or modify. This is what
	// ckpool and most stratum servers expect.
	if tmplReq.hasCapability("coinbasetxn") {
		cbTx := s.buildDefaultCoinbase(newHeight, coinbaseValue)
		cbBytes, serErr := cbTx.SerializeToBytes()
		if serErr != nil {
			return nil, newRPCError(rpcErrInternal, "serialize coinbase: "+serErr.Error())
		}
		cbHash, _ := crypto.HashTransaction(&cbTx)
		resp["coinbasetxn"] = map[string]interface{}{
			"data":    hex.EncodeToString(cbBytes),
			"txid":    cbHash.ReverseString(),
			"hash":    cbHash.ReverseString(),
			"fee":     -int64(mempoolTmpl.TotalFees),
			"sigops":  0,
			"weight":  cbTx.SerializeSize() * 4,
			"depends": []int{},
			"required": true,
		}
	}

	return resp, nil
}

// buildDefaultCoinbase constructs a BIP 34 coinbase transaction suitable for
// inclusion in a getblocktemplate response. The pool may modify the scriptSig
// (appending extra nonce data) and replace the output script with its own
// payout address.
func (s *Server) buildDefaultCoinbase(height uint32, value uint64) types.Transaction {
	pushLen := minimalHeightPushLen(height)
	heightBytes := make([]byte, 4)
	types.PutUint32LE(heightBytes, height)
	msg := make([]byte, 0, 1+pushLen+len(coinparams.CoinbaseTag))
	msg = append(msg, byte(pushLen))
	msg = append(msg, heightBytes[:pushLen]...)
	msg = append(msg, []byte(coinparams.CoinbaseTag)...)

	var rewardScript []byte
	if s.wallet != nil {
		addr := s.wallet.GetDefaultAddress()
		_, pkh, err := crypto.AddressToPubKeyHash(addr)
		if err == nil {
			rewardScript = crypto.MakeP2PKHScript(pkh)
		}
	}
	if rewardScript == nil {
		rewardScript = []byte{0x00}
	}

	return types.Transaction{
		Version: 1,
		Inputs: []types.TxInput{
			{
				PreviousOutPoint: types.CoinbaseOutPoint,
				SignatureScript:  msg,
				Sequence:         0xFFFFFFFF,
			},
		},
		Outputs: []types.TxOutput{
			{
				Value:    value,
				PkScript: rewardScript,
			},
		},
		LockTime: 0,
	}
}

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

// handleBlockProposal implements BIP 23 block proposal validation.
// The client sends a fully-assembled block for validation (excluding PoW check).
// Returns null if acceptable, or a rejection reason string.
func (s *Server) handleBlockProposal(req templateRequest) (interface{}, *jsonRPCError) {
	if req.Data == "" {
		return nil, newRPCError(rpcErrInvalidParams, "missing block data for proposal")
	}

	blockBytes, err := hex.DecodeString(req.Data)
	if err != nil {
		return "rejected", nil
	}

	var block types.Block
	if err := block.Deserialize(bytes.NewReader(blockBytes)); err != nil {
		return "rejected", nil
	}

	tipHash, _ := s.chain.Tip()
	if block.Header.PrevBlock != tipHash {
		return "bad-prevblk", nil
	}

	if len(block.Transactions) == 0 {
		return "bad-txns", nil
	}
	if !block.Transactions[0].IsCoinbase() {
		return "bad-txns", nil
	}

	merkle, err := crypto.ComputeMerkleRoot(block.Transactions)
	if err != nil {
		return "bad-txnmrklroot", nil
	}
	if merkle != block.Header.MerkleRoot {
		return "bad-txnmrklroot", nil
	}

	tipHeader, err := s.chain.TipHeader()
	if err != nil {
		return nil, newRPCError(rpcErrInternal, "get tip header: "+err.Error())
	}
	_, tipHeight := s.chain.Tip()
	expectedBits := s.engine.CalcNextBits(tipHeader, tipHeight, s.chain.GetAncestor, s.params)
	if block.Header.Bits != expectedBits {
		return "bad-diffbits", nil
	}

	return nil, nil
}

// handleLongPoll blocks until the template identified by longpollID has been
// superseded, or a timeout of 30 seconds elapses. This matches Bitcoin Core's
// long-poll behavior for BIP 22.
func (s *Server) handleLongPoll(longpollID string) {
	current := s.longPoll.currentID()
	if current != longpollID {
		return
	}

	ch := s.longPoll.wait()
	select {
	case <-ch:
	case <-time.After(30 * time.Second):
	}
}

// NotifyBlockChange should be called when a new block is accepted or the
// mempool changes significantly, to wake up long-polling clients.
func (s *Server) NotifyBlockChange() {
	tipHash, _ := s.chain.Tip()
	id := fmt.Sprintf("%s%d", tipHash.ReverseString(), s.mempool.Count())
	s.longPoll.update(id)
}

// rpcSubmitBlock accepts a hex-encoded serialized block (BIP 22 format).
// Returns null on success, a BIP 22 rejection reason string on failure.
func (s *Server) rpcSubmitBlock(params []json.RawMessage) (interface{}, *jsonRPCError) {
	if len(params) < 1 {
		return nil, newRPCError(rpcErrInvalidParams, "missing hex block data")
	}

	var hexData string
	if err := json.Unmarshal(params[0], &hexData); err != nil {
		return nil, newRPCError(rpcErrInvalidParams, "invalid hex parameter: "+err.Error())
	}

	blockBytes, err := hex.DecodeString(hexData)
	if err != nil {
		return nil, newRPCError(rpcErrInvalidParams, "invalid hex encoding: "+err.Error())
	}

	var block types.Block
	if err := block.Deserialize(bytes.NewReader(blockBytes)); err != nil {
		return "inconclusive", nil
	}

	height, err := s.chain.ProcessBlock(&block)
	if err != nil {
		reason := mapBlockErrorToRejection(err.Error())
		logging.L.Warn("submitblock rejected", "component", "rpc", "reason", reason, "error", err)
		return reason, nil
	}

	s.postBlockAccept(&block, height)
	return nil, nil
}

// postBlockAccept handles mempool cleanup and block relay after a block is
// accepted via submitblock (JSON-RPC or REST). This mirrors the logic in the
// internal miner callback and is essential for stratum pool operation.
func (s *Server) postBlockAccept(block *types.Block, height uint32) {
	var confirmedHashes []types.Hash
	for i := range block.Transactions {
		txHash, err := crypto.HashTransaction(&block.Transactions[i])
		if err == nil {
			confirmedHashes = append(confirmedHashes, txHash)
		}
	}
	s.mempool.RemoveTxs(confirmedHashes)

	blockHash := crypto.HashBlockHeader(&block.Header)
	if s.broadcastBlock != nil {
		s.broadcastBlock(blockHash, block)
	}

	logging.L.Info("submitblock accepted",
		"component", "rpc",
		"hash", blockHash.ReverseString(),
		"height", height)

	s.NotifyBlockChange()
}

// mapBlockErrorToRejection converts internal block validation error messages
// to BIP 22 standard rejection reason strings that stratum pool software
// understands.
func mapBlockErrorToRejection(errMsg string) string {
	lower := strings.ToLower(errMsg)

	switch {
	case strings.Contains(lower, "hash") && strings.Contains(lower, "target"):
		return "high-hash"
	case strings.Contains(lower, "pow"):
		return "high-hash"
	case strings.Contains(lower, "prevblock") || strings.Contains(lower, "prev_block") || strings.Contains(lower, "parent"):
		return "bad-prevblk"
	case strings.Contains(lower, "merkle"):
		return "bad-txnmrklroot"
	case strings.Contains(lower, "timestamp") && strings.Contains(lower, "future"):
		return "time-too-new"
	case strings.Contains(lower, "timestamp") && (strings.Contains(lower, "past") || strings.Contains(lower, "median")):
		return "time-too-old"
	case strings.Contains(lower, "timestamp"):
		return "time-invalid"
	case strings.Contains(lower, "bits") || strings.Contains(lower, "difficulty"):
		return "bad-diffbits"
	case strings.Contains(lower, "coinbase"):
		return "bad-cb-length"
	case strings.Contains(lower, "version"):
		return "bad-version"
	case strings.Contains(lower, "duplicate"):
		return "duplicate"
	case strings.Contains(lower, "transaction"):
		return "bad-txns"
	case strings.Contains(lower, "size"):
		return "bad-blk-length"
	default:
		return "rejected"
	}
}

// rpcGetMiningInfo returns mining-related state matching Bitcoin Core's format.
func (s *Server) rpcGetMiningInfo(_ []json.RawMessage) (interface{}, *jsonRPCError) {
	_, tipHeight := s.chain.Tip()
	info := s.chain.GetChainInfo()

	hashps, _ := s.rpcGetNetworkHashPS(nil)

	chainName := s.params.Name
	switch chainName {
	case "mainnet":
		chainName = "main"
	case "testnet":
		chainName = "test"
	}

	return map[string]interface{}{
		"blocks":        tipHeight,
		"difficulty":    info.Difficulty,
		"networkhashps": hashps,
		"pooledtx":      s.mempool.Count(),
		"chain":         chainName,
		"warnings":      "",
	}, nil
}

// rpcGetNetworkHashPS estimates network hash rate over a window of blocks.
// params[0]: nblocks (default 120), params[1]: height (default -1 = tip)
func (s *Server) rpcGetNetworkHashPS(params []json.RawMessage) (interface{}, *jsonRPCError) {
	nblocks := 120
	height := int64(-1)

	if len(params) > 0 {
		var n int
		if err := json.Unmarshal(params[0], &n); err == nil {
			nblocks = n
		}
	}
	if len(params) > 1 {
		var h int64
		if err := json.Unmarshal(params[1], &h); err == nil {
			height = h
		}
	}

	_, tipHeight := s.chain.Tip()

	endHeight := tipHeight
	if height >= 0 && uint32(height) <= tipHeight {
		endHeight = uint32(height)
	}

	if nblocks <= 0 || nblocks > int(endHeight) {
		nblocks = int(endHeight)
	}
	if nblocks == 0 {
		return float64(0), nil
	}

	startHeight := endHeight - uint32(nblocks)

	endHeader, err := s.chain.GetHeaderByHeight(endHeight)
	if err != nil {
		return float64(0), nil
	}
	startHeader, err := s.chain.GetHeaderByHeight(startHeight)
	if err != nil {
		return float64(0), nil
	}

	timeDiff := int64(endHeader.Timestamp) - int64(startHeader.Timestamp)
	if timeDiff <= 0 {
		return float64(0), nil
	}

	totalWork := new(big.Float)
	maxTarget := new(big.Int).Lsh(big.NewInt(1), 256)

	for h := startHeight + 1; h <= endHeight; h++ {
		hdr, hErr := s.chain.GetHeaderByHeight(h)
		if hErr != nil {
			continue
		}
		blockTarget := crypto.CompactToBig(hdr.Bits)
		if blockTarget.Sign() <= 0 {
			continue
		}
		blockWork := new(big.Int).Div(maxTarget, blockTarget)
		totalWork.Add(totalWork, new(big.Float).SetInt(blockWork))
	}

	hashps := new(big.Float).Quo(totalWork, new(big.Float).SetFloat64(float64(timeDiff)))
	result, _ := hashps.Float64()

	if math.IsInf(result, 0) || math.IsNaN(result) {
		return float64(0), nil
	}

	return result, nil
}

func readLimitedBody(r *http.Request, limit int64) ([]byte, error) {
	return readAllLimited(r.Body, limit)
}

func readAllLimited(r interface{ Read([]byte) (int, error) }, limit int64) ([]byte, error) {
	var buf bytes.Buffer
	_, err := buf.ReadFrom(&limitedReader{r: r, n: limit})
	return buf.Bytes(), err
}

type limitedReader struct {
	r interface{ Read([]byte) (int, error) }
	n int64
}

func (lr *limitedReader) Read(p []byte) (int, error) {
	if lr.n <= 0 {
		return 0, fmt.Errorf("body too large")
	}
	if int64(len(p)) > lr.n {
		p = p[:lr.n]
	}
	n, err := lr.r.Read(p)
	lr.n -= int64(n)
	return n, err
}

func isHexString(s string) bool {
	if len(s) == 0 || len(s)%2 != 0 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

