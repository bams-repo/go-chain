package rpc

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/bams-repo/fairchain/internal/chain"
	"github.com/bams-repo/fairchain/internal/crypto"
	"github.com/bams-repo/fairchain/internal/logging"
	"github.com/bams-repo/fairchain/internal/mempool"
	"github.com/bams-repo/fairchain/internal/metrics"
	"github.com/bams-repo/fairchain/internal/p2p"
	"github.com/bams-repo/fairchain/internal/params"
	"github.com/bams-repo/fairchain/internal/types"
)

// Server provides a minimal local HTTP JSON API for node status and control.
type Server struct {
	chain   *chain.Chain
	mempool *mempool.Mempool
	p2p     *p2p.Manager
	params  *params.ChainParams
	server  *http.Server
}

// New creates a new RPC server.
func New(addr string, c *chain.Chain, mp *mempool.Mempool, pm *p2p.Manager, p *params.ChainParams) *Server {
	s := &Server{
		chain:   c,
		mempool: mp,
		p2p:     pm,
		params:  p,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/getinfo", s.handleGetInfo)
	mux.HandleFunc("/getblockcount", s.handleGetBlockCount)
	mux.HandleFunc("/getbestblockhash", s.handleGetBestBlockHash)
	mux.HandleFunc("/getpeerinfo", s.handleGetPeerInfo)
	mux.HandleFunc("/getblock", s.handleGetBlock)
	mux.HandleFunc("/getblockbyheight", s.handleGetBlockByHeight)
	mux.HandleFunc("/submitblock", s.handleSubmitBlock)
	mux.HandleFunc("/getmempoolinfo", s.handleGetMempoolInfo)
	mux.HandleFunc("/getblockchaininfo", s.handleGetBlockchainInfo)
	mux.HandleFunc("/gettxout", s.handleGetTxOut)
	mux.HandleFunc("/gettxoutsetinfo", s.handleGetTxOutSetInfo)
	mux.HandleFunc("/getrawmempool", s.handleGetRawMempool)
	mux.HandleFunc("/getmempoolentry", s.handleGetMempoolEntry)
	mux.HandleFunc("/metrics", s.handleMetrics)

	s.server = &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	return s
}

// Start begins serving RPC requests.
func (s *Server) Start() error {
	logging.L.Info("RPC listening", "component", "rpc", "addr", s.server.Addr)
	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logging.L.Error("RPC server error", "component", "rpc", "error", err)
		}
	}()
	return nil
}

// Stop gracefully shuts down the RPC server.
func (s *Server) Stop(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

func (s *Server) handleGetInfo(w http.ResponseWriter, r *http.Request) {
	tipHash, tipHeight := s.chain.Tip()
	resp := map[string]interface{}{
		"network":     s.params.Name,
		"height":      tipHeight,
		"best_hash":   tipHash.ReverseString(),
		"peers":       s.p2p.PeerCount(),
		"mempool_size": s.mempool.Count(),
	}
	writeJSON(w, resp)
}

func (s *Server) handleGetBlockCount(w http.ResponseWriter, r *http.Request) {
	_, height := s.chain.Tip()
	writeJSON(w, map[string]interface{}{"height": height})
}

func (s *Server) handleGetBestBlockHash(w http.ResponseWriter, r *http.Request) {
	hash, _ := s.chain.Tip()
	writeJSON(w, map[string]interface{}{"hash": hash.ReverseString()})
}

func (s *Server) handleGetPeerInfo(w http.ResponseWriter, r *http.Request) {
	addrs := s.p2p.PeerAddrs()
	writeJSON(w, map[string]interface{}{"peers": addrs, "count": len(addrs)})
}

func (s *Server) handleGetBlock(w http.ResponseWriter, r *http.Request) {
	hashStr := r.URL.Query().Get("hash")
	if hashStr == "" {
		writeError(w, http.StatusBadRequest, "missing hash parameter")
		return
	}
	hash, err := types.HashFromReverseHex(hashStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid hash: %v", err))
		return
	}
	block, err := s.chain.GetBlock(hash)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("block not found: %v", err))
		return
	}
	blockHash := crypto.HashBlockHeader(&block.Header)
	resp := map[string]interface{}{
		"hash":        blockHash.ReverseString(),
		"version":     block.Header.Version,
		"prev_block":  block.Header.PrevBlock.ReverseString(),
		"merkle_root": block.Header.MerkleRoot.ReverseString(),
		"timestamp":   block.Header.Timestamp,
		"bits":        fmt.Sprintf("%08x", block.Header.Bits),
		"nonce":       block.Header.Nonce,
		"tx_count":    len(block.Transactions),
	}
	writeJSON(w, resp)
}

func (s *Server) handleGetBlockByHeight(w http.ResponseWriter, r *http.Request) {
	heightStr := r.URL.Query().Get("height")
	if heightStr == "" {
		writeError(w, http.StatusBadRequest, "missing height parameter")
		return
	}
	var height uint32
	if _, err := fmt.Sscanf(heightStr, "%d", &height); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid height: %v", err))
		return
	}
	block, blockHash, err := s.chain.GetBlockByHeight(height)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("block not found: %v", err))
		return
	}
	resp := map[string]interface{}{
		"hash":        blockHash.ReverseString(),
		"height":      height,
		"version":     block.Header.Version,
		"prev_block":  block.Header.PrevBlock.ReverseString(),
		"merkle_root": block.Header.MerkleRoot.ReverseString(),
		"timestamp":   block.Header.Timestamp,
		"bits":        fmt.Sprintf("%08x", block.Header.Bits),
		"nonce":       block.Header.Nonce,
		"tx_count":    len(block.Transactions),
	}
	writeJSON(w, resp)
}

func (s *Server) handleSubmitBlock(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}
	var block types.Block
	if err := block.Deserialize(r.Body); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid block: %v", err))
		return
	}
	height, err := s.chain.ProcessBlock(&block)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("rejected: %v", err))
		return
	}
	blockHash := crypto.HashBlockHeader(&block.Header)
	writeJSON(w, map[string]interface{}{
		"accepted": true,
		"hash":     blockHash.ReverseString(),
		"height":   height,
	})
}

func (s *Server) handleGetMempoolInfo(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]interface{}{
		"size": s.mempool.Count(),
	})
}

func (s *Server) handleGetBlockchainInfo(w http.ResponseWriter, r *http.Request) {
	info := s.chain.GetChainInfo()
	resp := map[string]interface{}{
		"chain":                  info.Network,
		"blocks":                 info.Height,
		"best_block_hash":        info.BestHash.ReverseString(),
		"genesis_block_hash":     info.GenesisHash.ReverseString(),
		"bits":                   fmt.Sprintf("%08x", info.Bits),
		"difficulty":             info.Difficulty,
		"chainwork":              fmt.Sprintf("%064x", info.Chainwork),
		"median_time_past":       info.MedianTimePast,
		"retarget_interval":      info.RetargetInterval,
		"retarget_epoch":         info.RetargetEpoch,
		"epoch_progress":         info.EpochProgress,
		"epoch_blocks_remaining": info.EpochBlocksLeft,
		"verification_progress":  info.VerificationProg,
		"peers":                  s.p2p.PeerCount(),
		"mempool_size":           s.mempool.Count(),
	}
	writeJSON(w, resp)
}

// handleGetTxOut implements Bitcoin Core's gettxout RPC.
// Returns details about an unspent transaction output.
// Parameters: txid (hex, display order), n (output index), include_mempool (optional, default true).
func (s *Server) handleGetTxOut(w http.ResponseWriter, r *http.Request) {
	txidStr := r.URL.Query().Get("txid")
	if txidStr == "" {
		writeError(w, http.StatusBadRequest, "missing txid parameter")
		return
	}
	txHash, err := types.HashFromReverseHex(txidStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid txid: %v", err))
		return
	}

	nStr := r.URL.Query().Get("n")
	if nStr == "" {
		writeError(w, http.StatusBadRequest, "missing n parameter")
		return
	}
	var n uint32
	if _, err := fmt.Sscanf(nStr, "%d", &n); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid n: %v", err))
		return
	}

	utxoSet := s.chain.UtxoSet()
	entry := utxoSet.Get(txHash, n)
	if entry == nil {
		writeJSON(w, nil)
		return
	}

	tipHash, tipHeight := s.chain.Tip()
	confirmations := uint32(0)
	if tipHeight >= entry.Height {
		confirmations = tipHeight - entry.Height + 1
	}

	resp := map[string]interface{}{
		"bestblock":     tipHash.ReverseString(),
		"confirmations": confirmations,
		"value":         entry.Value,
		"scriptPubKey": map[string]interface{}{
			"hex": hex.EncodeToString(entry.PkScript),
		},
		"coinbase": entry.IsCoinbase,
	}
	writeJSON(w, resp)
}

// handleGetTxOutSetInfo implements Bitcoin Core's gettxoutsetinfo RPC.
// Returns statistics about the unspent transaction output set.
func (s *Server) handleGetTxOutSetInfo(w http.ResponseWriter, r *http.Request) {
	info := s.chain.TxOutSetInfo()

	resp := map[string]interface{}{
		"height":       info.Height,
		"bestblock":    info.BestHash.ReverseString(),
		"txouts":       info.TxOuts,
		"total_amount": info.TotalValue,
	}
	writeJSON(w, resp)
}

// handleGetRawMempool implements Bitcoin Core's getrawmempool RPC.
// Returns all transaction IDs in the memory pool.
// Parameter: verbose (optional, default false). If true, returns detailed entries.
func (s *Server) handleGetRawMempool(w http.ResponseWriter, r *http.Request) {
	verbose := r.URL.Query().Get("verbose") == "true"

	if !verbose {
		hashes := s.mempool.GetTxHashes()
		txids := make([]string, len(hashes))
		for i, h := range hashes {
			txids[i] = h.ReverseString()
		}
		writeJSON(w, txids)
		return
	}

	entries := s.mempool.GetAllEntries()
	result := make(map[string]interface{}, len(entries))
	for _, e := range entries {
		result[e.Hash.ReverseString()] = map[string]interface{}{
			"size": e.Size,
			"fee":  e.Fee,
			"fees": map[string]interface{}{
				"base": e.Fee,
			},
			"feerate": e.FeeRate,
		}
	}
	writeJSON(w, result)
}

// handleGetMempoolEntry implements Bitcoin Core's getmempoolentry RPC.
// Returns mempool data for a given transaction.
func (s *Server) handleGetMempoolEntry(w http.ResponseWriter, r *http.Request) {
	txidStr := r.URL.Query().Get("txid")
	if txidStr == "" {
		writeError(w, http.StatusBadRequest, "missing txid parameter")
		return
	}
	txHash, err := types.HashFromReverseHex(txidStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid txid: %v", err))
		return
	}

	entry, ok := s.mempool.GetTxEntry(txHash)
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Sprintf("transaction %s not in mempool", txidStr))
		return
	}

	resp := map[string]interface{}{
		"size": entry.Size,
		"fee":  entry.Fee,
		"fees": map[string]interface{}{
			"base": entry.Fee,
		},
		"feerate": entry.FeeRate,
	}
	writeJSON(w, resp)
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, metrics.Global.Snapshot())
}

func writeJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
