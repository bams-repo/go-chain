package rpc

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/bams-repo/fairchain/internal/coinparams"
	"github.com/bams-repo/fairchain/internal/crypto"
	"github.com/bams-repo/fairchain/internal/types"
	"github.com/bams-repo/fairchain/internal/utxo"
	"github.com/bams-repo/fairchain/internal/version"
	"github.com/bams-repo/fairchain/internal/wallet"
)

// JSON-RPC 1.0 request/response types matching Bitcoin Core's RPC interface.
// Stratum pool software (ckpool, Braiins, etc.) sends requests in this format.

type jsonRPCRequest struct {
	Jsonrpc string            `json:"jsonrpc"`
	ID      json.RawMessage   `json:"id"`
	Method  string            `json:"method"`
	Params  []json.RawMessage `json:"params"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type jsonRPCResponse struct {
	Result interface{}     `json:"result"`
	Error  *jsonRPCError   `json:"error"`
	ID     json.RawMessage `json:"id"`
}

const (
	rpcErrParse          = -32700
	rpcErrInvalidRequest = -32600
	rpcErrMethodNotFound = -32601
	rpcErrInvalidParams  = -32602
	rpcErrInternal       = -32603
	rpcErrMisc           = -1
	rpcErrWalletNotFound = -18
)

type rpcHandler func(params []json.RawMessage) (interface{}, *jsonRPCError)

func (s *Server) buildMethodMap() map[string]rpcHandler {
	return map[string]rpcHandler{
		// Mining (BIP 22/23 — required for stratum pool backends)
		"getblocktemplate": s.rpcGetBlockTemplate,
		"submitblock":      s.rpcSubmitBlock,
		"getmininginfo":    s.rpcGetMiningInfo,
		"getnetworkhashps": s.rpcGetNetworkHashPS,
		"preciousblock":    s.rpcPreciousBlock,

		// Blockchain
		"getblockchaininfo": s.rpcGetBlockchainInfo,
		"getblockcount":     s.rpcGetBlockCount,
		"getbestblockhash":  s.rpcGetBestBlockHash,
		"getblockhash":      s.rpcGetBlockHash,
		"getblock":          s.rpcGetBlock,
		"getdifficulty":     s.rpcGetDifficulty,

		// Raw transaction (required by ckpool and stratum servers)
		"getrawtransaction":  s.rpcGetRawTransaction,
		"sendrawtransaction": s.rpcSendRawTransaction,

		// Network
		"getnetworkinfo":     s.rpcGetNetworkInfo,
		"getpeerinfo":        s.rpcGetPeerInfo,
		"getconnectioncount": s.rpcGetConnectionCount,

		// Mempool
		"getmempoolinfo": s.rpcGetMempoolInfo,
		"getrawmempool":  s.rpcGetRawMempool,

		// UTXO
		"gettxout":        s.rpcGetTxOut,
		"gettxoutsetinfo": s.rpcGetTxOutSetInfo,

		// Wallet
		"validateaddress":  s.rpcValidateAddress,
		"getnewaddress":    s.rpcGetNewAddress,
		"getbalance":       s.rpcGetBalance,
		"getwalletinfo":    s.rpcGetWalletInfo,
		"listunspent":      s.rpcListUnspent,
		"dumpprivkey":      s.rpcDumpPrivKey,
		"importprivkey":    s.rpcImportPrivKey,
		"settxfee":         s.rpcSetTxFee,
		"sendtoaddress":    s.rpcSendToAddress,
		"getrawchangeaddress": s.rpcGetRawChangeAddress,

		// Control
		"getinfo": s.rpcGetInfo,
		"stop":    s.rpcStop,
	}
}

func (s *Server) handleJSONRPC(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONRPCError(w, nil, rpcErrInvalidRequest, "POST required")
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 8*1024*1024))
	if err != nil {
		writeJSONRPCError(w, nil, rpcErrParse, "read body: "+err.Error())
		return
	}

	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		writeJSONRPCError(w, nil, rpcErrParse, "empty body")
		return
	}

	// Bitcoin Core supports batch JSON-RPC (array of requests). Stratum
	// proxies sometimes batch multiple calls in a single HTTP request.
	if trimmed[0] == '[' {
		s.handleBatchJSONRPC(w, trimmed)
		return
	}

	var req jsonRPCRequest
	if err := json.Unmarshal(trimmed, &req); err != nil {
		writeJSONRPCError(w, nil, rpcErrParse, "parse JSON: "+err.Error())
		return
	}

	resp := s.dispatchJSONRPC(req)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleBatchJSONRPC processes an array of JSON-RPC requests and returns
// an array of responses, matching Bitcoin Core's batch RPC behavior.
const maxBatchSize = 100

func (s *Server) handleBatchJSONRPC(w http.ResponseWriter, body []byte) {
	var reqs []jsonRPCRequest
	if err := json.Unmarshal(body, &reqs); err != nil {
		writeJSONRPCError(w, nil, rpcErrParse, "parse batch JSON: "+err.Error())
		return
	}

	if len(reqs) == 0 {
		writeJSONRPCError(w, nil, rpcErrInvalidRequest, "empty batch")
		return
	}

	if len(reqs) > maxBatchSize {
		writeJSONRPCError(w, nil, rpcErrInvalidRequest, fmt.Sprintf("batch too large: %d requests, max %d", len(reqs), maxBatchSize))
		return
	}

	responses := make([]jsonRPCResponse, len(reqs))
	for i, req := range reqs {
		responses[i] = s.dispatchJSONRPC(req)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(responses)
}

// dispatchJSONRPC routes a single JSON-RPC request to the appropriate handler.
func (s *Server) dispatchJSONRPC(req jsonRPCRequest) jsonRPCResponse {
	if req.Method == "" {
		return jsonRPCResponse{
			ID:    req.ID,
			Error: newRPCError(rpcErrInvalidRequest, "missing method"),
		}
	}

	methods := s.methodMap
	handler, ok := methods[req.Method]
	if !ok {
		return jsonRPCResponse{
			ID:    req.ID,
			Error: newRPCError(rpcErrMethodNotFound, fmt.Sprintf("method %q not found", req.Method)),
		}
	}

	result, rpcErr := handler(req.Params)

	resp := jsonRPCResponse{ID: req.ID}
	if rpcErr != nil {
		resp.Error = rpcErr
	} else {
		resp.Result = result
	}
	return resp
}

func writeJSONRPCError(w http.ResponseWriter, id json.RawMessage, code int, msg string) {
	resp := jsonRPCResponse{
		ID:    id,
		Error: &jsonRPCError{Code: code, Message: msg},
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func newRPCError(code int, msg string) *jsonRPCError {
	return &jsonRPCError{Code: code, Message: msg}
}

// --- JSON-RPC method implementations ---
// These adapt existing handler logic to the JSON-RPC dispatch signature.

func (s *Server) rpcGetBlockchainInfo(_ []json.RawMessage) (interface{}, *jsonRPCError) {
	info := s.chain.GetChainInfo()
	return map[string]interface{}{
		"chain":                info.Network,
		"blocks":               info.Height,
		"headers":              info.Height,
		"bestblockhash":        info.BestHash.ReverseString(),
		"bits":                 fmt.Sprintf("%08x", info.Bits),
		"difficulty":           info.Difficulty,
		"mediantime":           info.MedianTimePast,
		"verificationprogress": info.VerificationProg,
		"initialblockdownload": s.p2p.IsSyncing(),
		"chainwork":            fmt.Sprintf("%064x", info.Chainwork),
		"pruned":               false,
		"warnings":             "",
	}, nil
}

func (s *Server) rpcGetBlockCount(_ []json.RawMessage) (interface{}, *jsonRPCError) {
	_, height := s.chain.Tip()
	return height, nil
}

func (s *Server) rpcGetBestBlockHash(_ []json.RawMessage) (interface{}, *jsonRPCError) {
	hash, _ := s.chain.Tip()
	return hash.ReverseString(), nil
}

func (s *Server) rpcGetBlockHash(params []json.RawMessage) (interface{}, *jsonRPCError) {
	if len(params) < 1 {
		return nil, newRPCError(rpcErrInvalidParams, "missing height parameter")
	}
	var height uint32
	if err := json.Unmarshal(params[0], &height); err != nil {
		return nil, newRPCError(rpcErrInvalidParams, "invalid height: "+err.Error())
	}
	header, err := s.chain.GetHeaderByHeight(height)
	if err != nil {
		return nil, newRPCError(rpcErrMisc, fmt.Sprintf("block not found at height %d", height))
	}
	hash := crypto.HashBlockHeader(header)
	return hash.ReverseString(), nil
}

func (s *Server) rpcGetBlock(params []json.RawMessage) (interface{}, *jsonRPCError) {
	if len(params) < 1 {
		return nil, newRPCError(rpcErrInvalidParams, "missing blockhash parameter")
	}
	var hashStr string
	if err := json.Unmarshal(params[0], &hashStr); err != nil {
		return nil, newRPCError(rpcErrInvalidParams, "invalid blockhash: "+err.Error())
	}
	hash, err := types.HashFromReverseHex(hashStr)
	if err != nil {
		return nil, newRPCError(rpcErrInvalidParams, "invalid hash: "+err.Error())
	}
	block, err := s.chain.GetBlock(hash)
	if err != nil {
		return nil, newRPCError(rpcErrMisc, "block not found")
	}
	blockHash := crypto.HashBlockHeader(&block.Header)
	txids := make([]string, len(block.Transactions))
	for i, tx := range block.Transactions {
		txHash, _ := crypto.HashTransaction(&tx)
		txids[i] = txHash.ReverseString()
	}
	_, tipHeight := s.chain.Tip()
	confirmations := int64(-1)
	blockHeight, heightErr := s.chain.GetBlockHeight(blockHash)
	if heightErr == nil {
		confirmations = int64(tipHeight) - int64(blockHeight) + 1
	}
	return map[string]interface{}{
		"hash":              blockHash.ReverseString(),
		"confirmations":     confirmations,
		"height":            blockHeight,
		"version":           block.Header.Version,
		"merkleroot":        block.Header.MerkleRoot.ReverseString(),
		"tx":                txids,
		"time":              block.Header.Timestamp,
		"nonce":             block.Header.Nonce,
		"bits":              fmt.Sprintf("%08x", block.Header.Bits),
		"previousblockhash": block.Header.PrevBlock.ReverseString(),
		"nTx":               len(block.Transactions),
	}, nil
}

func (s *Server) rpcGetDifficulty(_ []json.RawMessage) (interface{}, *jsonRPCError) {
	info := s.chain.GetChainInfo()
	return info.Difficulty, nil
}

func (s *Server) rpcGetNetworkInfo(_ []json.RawMessage) (interface{}, *jsonRPCError) {
	inbound, outbound := s.p2p.ConnectionCounts()
	return map[string]interface{}{
		"version":         version.ProtocolVersion,
		"subversion":      version.UserAgent(),
		"protocolversion": version.ProtocolVersion,
		"connections":     s.p2p.PeerCount(),
		"connections_in":  inbound,
		"connections_out": outbound,
		"networkactive":   true,
		"warnings":        "",
	}, nil
}

func (s *Server) rpcGetPeerInfo(_ []json.RawMessage) (interface{}, *jsonRPCError) {
	return s.p2p.PeerInfos(), nil
}

func (s *Server) rpcGetConnectionCount(_ []json.RawMessage) (interface{}, *jsonRPCError) {
	return s.p2p.PeerCount(), nil
}

func (s *Server) rpcGetMempoolInfo(_ []json.RawMessage) (interface{}, *jsonRPCError) {
	return map[string]interface{}{
		"loaded":           true,
		"size":             s.mempool.Count(),
		"bytes":            s.mempool.TotalSize(),
		"maxmempool":       300 * 1024 * 1024,
		"mempoolminfee":    s.params.MinRelayTxFeeRate,
		"mempoolexpiry":    int64(s.params.MempoolExpiry.Hours()),
	}, nil
}

func (s *Server) rpcGetRawMempool(params []json.RawMessage) (interface{}, *jsonRPCError) {
	verbose := false
	if len(params) > 0 {
		json.Unmarshal(params[0], &verbose)
	}
	if !verbose {
		hashes := s.mempool.GetTxHashes()
		txids := make([]string, len(hashes))
		for i, h := range hashes {
			txids[i] = h.ReverseString()
		}
		return txids, nil
	}
	entries := s.mempool.GetAllEntries()
	result := make(map[string]interface{}, len(entries))
	for _, e := range entries {
		result[e.Hash.ReverseString()] = map[string]interface{}{
			"size":    e.Size,
			"fee":     e.Fee,
			"fees":    map[string]interface{}{"base": e.Fee},
			"feerate": e.FeeRate,
			"time":    e.AddedAt.Unix(),
		}
	}
	return result, nil
}

func (s *Server) rpcGetTxOut(params []json.RawMessage) (interface{}, *jsonRPCError) {
	if len(params) < 2 {
		return nil, newRPCError(rpcErrInvalidParams, "missing txid and n parameters")
	}
	var txidStr string
	if err := json.Unmarshal(params[0], &txidStr); err != nil {
		return nil, newRPCError(rpcErrInvalidParams, "invalid txid: "+err.Error())
	}
	var n uint32
	if err := json.Unmarshal(params[1], &n); err != nil {
		return nil, newRPCError(rpcErrInvalidParams, "invalid n: "+err.Error())
	}
	txHash, err := types.HashFromReverseHex(txidStr)
	if err != nil {
		return nil, newRPCError(rpcErrInvalidParams, "invalid txid hex: "+err.Error())
	}
	utxoSet := s.chain.UtxoSet()
	entry := utxoSet.Get(txHash, n)
	if entry == nil {
		return nil, nil
	}
	tipHash, tipHeight := s.chain.Tip()
	confirmations := uint32(0)
	if tipHeight >= entry.Height {
		confirmations = tipHeight - entry.Height + 1
	}
	return map[string]interface{}{
		"bestblock":     tipHash.ReverseString(),
		"confirmations": confirmations,
		"value":         entry.Value,
		"scriptPubKey":  map[string]interface{}{"hex": hex.EncodeToString(entry.PkScript)},
		"coinbase":      entry.IsCoinbase,
	}, nil
}

func (s *Server) rpcGetTxOutSetInfo(_ []json.RawMessage) (interface{}, *jsonRPCError) {
	info := s.chain.TxOutSetInfo()
	return map[string]interface{}{
		"height":       info.Height,
		"bestblock":    info.BestHash.ReverseString(),
		"txouts":       info.TxOuts,
		"total_amount": info.TotalValue,
	}, nil
}

func (s *Server) rpcGetInfo(_ []json.RawMessage) (interface{}, *jsonRPCError) {
	tipHash, tipHeight := s.chain.Tip()
	info := s.chain.GetChainInfo()
	return map[string]interface{}{
		"version":         version.ProtocolVersion,
		"protocolversion": version.ProtocolVersion,
		"blocks":          tipHeight,
		"bestblockhash":   tipHash.ReverseString(),
		"difficulty":      info.Difficulty,
		"connections":     s.p2p.PeerCount(),
		"network":         s.params.Name,
		"mempool_size":    s.mempool.Count(),
	}, nil
}

func (s *Server) rpcStop(_ []json.RawMessage) (interface{}, *jsonRPCError) {
	if s.shutdownFn != nil {
		go s.shutdownFn()
	}
	return coinparams.Name + " server stopping", nil
}

func (s *Server) rpcValidateAddress(params []json.RawMessage) (interface{}, *jsonRPCError) {
	if len(params) < 1 {
		return nil, newRPCError(rpcErrInvalidParams, "missing address parameter")
	}
	var addr string
	if err := json.Unmarshal(params[0], &addr); err != nil {
		return nil, newRPCError(rpcErrInvalidParams, "invalid address: "+err.Error())
	}
	ver, pkh, err := crypto.AddressToPubKeyHash(addr)
	if err != nil {
		return map[string]interface{}{
			"isvalid": false,
			"address": addr,
		}, nil
	}
	isMine := false
	if s.wallet != nil {
		dk := s.wallet.GetKeyForAddress(addr)
		isMine = dk != nil
	}
	return map[string]interface{}{
		"isvalid":      true,
		"address":      addr,
		"scriptPubKey": hex.EncodeToString(crypto.MakeP2PKHScript(pkh)),
		"ismine":       isMine,
		"iswatchonly":  false,
		"isscript":     false,
		"iswitness":    false,
		"version":      ver,
	}, nil
}

// rpcGetNewAddress generates a new address via JSON-RPC (used by some pool
// software to generate payout addresses).
func (s *Server) rpcGetNewAddress(_ []json.RawMessage) (interface{}, *jsonRPCError) {
	if s.wallet == nil {
		return nil, newRPCError(rpcErrWalletNotFound, "wallet not loaded")
	}
	addr, err := s.wallet.GetNewAddress()
	if err != nil {
		return nil, newRPCError(rpcErrInternal, err.Error())
	}
	return addr, nil
}

// rpcGetBalance returns the wallet balance via JSON-RPC.
func (s *Server) rpcGetBalance(params []json.RawMessage) (interface{}, *jsonRPCError) {
	if s.wallet == nil {
		return nil, newRPCError(rpcErrWalletNotFound, "wallet not loaded")
	}
	minConf := uint32(1)
	if len(params) > 0 {
		var mc uint32
		if err := json.Unmarshal(params[0], &mc); err == nil {
			minConf = mc
		}
	}
	_, tipHeight := s.chain.Tip()
	balance := s.wallet.GetBalance(
		s.makeUtxoIterator(),
		tipHeight,
		minConf,
		s.params.CoinbaseMaturity,
	)
	return balance, nil
}

// rpcGetWalletInfo returns wallet status via JSON-RPC.
func (s *Server) rpcGetWalletInfo(_ []json.RawMessage) (interface{}, *jsonRPCError) {
	if s.wallet == nil {
		return nil, newRPCError(rpcErrWalletNotFound, "wallet not loaded")
	}
	_, tipHeight := s.chain.Tip()
	balance := s.wallet.GetBalance(
		s.makeUtxoIterator(),
		tipHeight,
		1,
		s.params.CoinbaseMaturity,
	)
	resp := map[string]interface{}{
		"walletname":           "default",
		"walletversion":        1,
		"balance":              balance,
		"unconfirmed_balance":  0,
		"txcount":              0,
		"keypoolsize":          s.wallet.KeyCount(),
		"paytxfee":             s.feePerByte.Load(),
		"hdseedid":             s.wallet.GetDefaultAddress(),
		"private_keys_enabled": true,
	}
	if s.wallet.IsEncrypted() {
		if s.wallet.IsLocked() {
			resp["unlocked_until"] = 0
		} else {
			resp["unlocked_until"] = -1
		}
	}
	return resp, nil
}

// rpcSendRawTransaction submits a raw transaction via JSON-RPC.
func (s *Server) rpcSendRawTransaction(params []json.RawMessage) (interface{}, *jsonRPCError) {
	if len(params) < 1 {
		return nil, newRPCError(rpcErrInvalidParams, "missing hex transaction data")
	}
	var hexStr string
	if err := json.Unmarshal(params[0], &hexStr); err != nil {
		return nil, newRPCError(rpcErrInvalidParams, "invalid hex parameter: "+err.Error())
	}
	txBytes, err := hex.DecodeString(hexStr)
	if err != nil {
		return nil, newRPCError(rpcErrInvalidParams, "invalid hex encoding: "+err.Error())
	}
	var tx types.Transaction
	if err := tx.Deserialize(bytes.NewReader(txBytes)); err != nil {
		return nil, newRPCError(rpcErrInvalidParams, "invalid transaction: "+err.Error())
	}
	txHash, err := s.mempool.AddTx(&tx)
	if err != nil {
		return nil, newRPCError(rpcErrMisc, err.Error())
	}
	if s.broadcastTx != nil {
		s.broadcastTx(txHash)
	}
	return txHash.ReverseString(), nil
}

// rpcGetRawTransaction returns a hex-encoded raw transaction. Checks the
// mempool first, then scans the UTXO set to locate the block containing the
// transaction. This matches Bitcoin Core's getrawtransaction behavior that
// ckpool and other stratum servers depend on.
func (s *Server) rpcGetRawTransaction(params []json.RawMessage) (interface{}, *jsonRPCError) {
	if len(params) < 1 {
		return nil, newRPCError(rpcErrInvalidParams, "missing txid parameter")
	}
	var txidStr string
	if err := json.Unmarshal(params[0], &txidStr); err != nil {
		return nil, newRPCError(rpcErrInvalidParams, "invalid txid: "+err.Error())
	}
	txHash, err := types.HashFromReverseHex(txidStr)
	if err != nil {
		return nil, newRPCError(rpcErrInvalidParams, "invalid txid hex: "+err.Error())
	}

	verbose := false
	if len(params) > 1 {
		json.Unmarshal(params[1], &verbose)
	}

	// Check mempool first.
	if entry, ok := s.mempool.GetTxEntry(txHash); ok {
		txBytes, serErr := entry.Tx.SerializeToBytes()
		if serErr != nil {
			return nil, newRPCError(rpcErrInternal, "serialize tx: "+serErr.Error())
		}
		hexData := hex.EncodeToString(txBytes)
		if !verbose {
			return hexData, nil
		}
		return s.buildVerboseTx(entry.Tx, txHash, hexData, 0, types.ZeroHash), nil
	}

	// Scan UTXO set for the block height containing this transaction.
	utxoSet := s.chain.UtxoSet()
	blockHeight := uint32(0)
	found := false
	utxoSet.ForEach(func(hash types.Hash, index uint32, entry *utxo.UtxoEntry) {
		if !found && hash == txHash {
			blockHeight = entry.Height
			found = true
		}
	})

	if !found {
		return nil, newRPCError(rpcErrMisc, "No such mempool or blockchain transaction")
	}

	block, _, err := s.chain.GetBlockByHeight(blockHeight)
	if err != nil {
		return nil, newRPCError(rpcErrInternal, "get block: "+err.Error())
	}

	for i := range block.Transactions {
		h, _ := crypto.HashTransaction(&block.Transactions[i])
		if h == txHash {
			txBytes, serErr := block.Transactions[i].SerializeToBytes()
			if serErr != nil {
				return nil, newRPCError(rpcErrInternal, "serialize tx: "+serErr.Error())
			}
			hexData := hex.EncodeToString(txBytes)
			if !verbose {
				return hexData, nil
			}
			blockHash := crypto.HashBlockHeader(&block.Header)
			return s.buildVerboseTx(&block.Transactions[i], txHash, hexData, blockHeight, blockHash), nil
		}
	}

	return nil, newRPCError(rpcErrMisc, "No such mempool or blockchain transaction")
}

func (s *Server) buildVerboseTx(tx *types.Transaction, txHash types.Hash, hexData string, blockHeight uint32, blockHash types.Hash) map[string]interface{} {
	_, tipHeight := s.chain.Tip()
	confirmations := 0
	if blockHash != types.ZeroHash && tipHeight >= blockHeight {
		confirmations = int(tipHeight) - int(blockHeight) + 1
	}

	vins := make([]map[string]interface{}, len(tx.Inputs))
	for i, in := range tx.Inputs {
		vin := map[string]interface{}{
			"sequence": in.Sequence,
		}
		if tx.IsCoinbase() {
			vin["coinbase"] = hex.EncodeToString(in.SignatureScript)
		} else {
			opHash := types.Hash(in.PreviousOutPoint.Hash)
			vin["txid"] = opHash.ReverseString()
			vin["vout"] = in.PreviousOutPoint.Index
			vin["scriptSig"] = map[string]interface{}{
				"hex": hex.EncodeToString(in.SignatureScript),
			}
		}
		vins[i] = vin
	}

	vouts := make([]map[string]interface{}, len(tx.Outputs))
	for i, out := range tx.Outputs {
		vouts[i] = map[string]interface{}{
			"value": out.Value,
			"n":     i,
			"scriptPubKey": map[string]interface{}{
				"hex": hex.EncodeToString(out.PkScript),
			},
		}
	}

	result := map[string]interface{}{
		"txid":          txHash.ReverseString(),
		"hash":          txHash.ReverseString(),
		"version":       tx.Version,
		"size":          tx.SerializeSize(),
		"locktime":      tx.LockTime,
		"vin":           vins,
		"vout":          vouts,
		"hex":           hexData,
		"confirmations": confirmations,
	}
	if blockHash != types.ZeroHash {
		result["blockhash"] = blockHash.ReverseString()
		result["blockheight"] = blockHeight
	}
	return result
}

// rpcPreciousBlock marks a block as precious (hint to prefer it as tip).
// ckpool calls this after successfully submitting a block. Currently,
// this is a no-op since we don't implement tip preference hints, but the
// method must exist and return null to avoid ckpool errors.
func (s *Server) rpcPreciousBlock(params []json.RawMessage) (interface{}, *jsonRPCError) {
	return nil, nil
}

// rpcListUnspent returns unspent outputs for the wallet via JSON-RPC.
func (s *Server) rpcListUnspent(params []json.RawMessage) (interface{}, *jsonRPCError) {
	if s.wallet == nil {
		return nil, newRPCError(rpcErrWalletNotFound, "wallet not loaded")
	}
	minConf := uint32(1)
	maxConf := uint32(9999999)
	if len(params) > 0 {
		var mc uint32
		if err := json.Unmarshal(params[0], &mc); err == nil {
			minConf = mc
		}
	}
	if len(params) > 1 {
		var mc uint32
		if err := json.Unmarshal(params[1], &mc); err == nil {
			maxConf = mc
		}
	}

	_, tipHeight := s.chain.Tip()
	utxos := s.wallet.FindUnspent(s.makeUtxoIterator(), tipHeight)

	var results []map[string]interface{}
	for _, u := range utxos {
		if u.Confirmations < minConf || u.Confirmations > maxConf {
			continue
		}
		if u.IsCoinbase && u.Confirmations < s.params.CoinbaseMaturity {
			continue
		}
		txHashType := types.Hash(u.TxHash)
		results = append(results, map[string]interface{}{
			"txid":          txHashType.ReverseString(),
			"vout":          u.Index,
			"address":       u.Address,
			"scriptPubKey":  hex.EncodeToString(u.PkScript),
			"amount":        u.Value,
			"confirmations": u.Confirmations,
			"spendable":     true,
		})
	}
	if results == nil {
		results = make([]map[string]interface{}, 0)
	}
	return results, nil
}

// rpcDumpPrivKey returns the private key for an address via JSON-RPC.
func (s *Server) rpcDumpPrivKey(params []json.RawMessage) (interface{}, *jsonRPCError) {
	if s.wallet == nil {
		return nil, newRPCError(rpcErrWalletNotFound, "wallet not loaded")
	}
	if len(params) < 1 {
		return nil, newRPCError(rpcErrInvalidParams, "missing address parameter")
	}
	var addr string
	if err := json.Unmarshal(params[0], &addr); err != nil {
		return nil, newRPCError(rpcErrInvalidParams, "invalid address: "+err.Error())
	}
	privKey, err := s.wallet.DumpPrivKey(addr)
	if err != nil {
		return nil, newRPCError(rpcErrMisc, err.Error())
	}
	return privKey, nil
}

// rpcImportPrivKey imports a private key via JSON-RPC.
func (s *Server) rpcImportPrivKey(params []json.RawMessage) (interface{}, *jsonRPCError) {
	if s.wallet == nil {
		return nil, newRPCError(rpcErrWalletNotFound, "wallet not loaded")
	}
	if err := s.wallet.RequireUnlocked(); err != nil {
		return nil, newRPCError(rpcErrMisc, err.Error())
	}
	if len(params) < 1 {
		return nil, newRPCError(rpcErrInvalidParams, "missing privkey parameter")
	}
	var privKeyStr string
	if err := json.Unmarshal(params[0], &privKeyStr); err != nil {
		return nil, newRPCError(rpcErrInvalidParams, "invalid privkey: "+err.Error())
	}
	addr, err := s.wallet.ImportPrivKey(privKeyStr)
	if err != nil {
		return nil, newRPCError(rpcErrMisc, err.Error())
	}
	return map[string]interface{}{"address": addr}, nil
}

// rpcSetTxFee sets the transaction fee rate via JSON-RPC.
func (s *Server) rpcSetTxFee(params []json.RawMessage) (interface{}, *jsonRPCError) {
	if len(params) < 1 {
		return nil, newRPCError(rpcErrInvalidParams, "missing amount parameter")
	}
	var fee uint64
	if err := json.Unmarshal(params[0], &fee); err != nil {
		return nil, newRPCError(rpcErrInvalidParams, "invalid fee: "+err.Error())
	}
	if fee > maxFeePerByte {
		return nil, newRPCError(rpcErrInvalidParams, fmt.Sprintf("fee rate %d exceeds maximum %d sat/byte", fee, maxFeePerByte))
	}
	s.feePerByte.Store(fee)
	return true, nil
}

// rpcSendToAddress sends coins to an address via JSON-RPC.
func (s *Server) rpcSendToAddress(params []json.RawMessage) (interface{}, *jsonRPCError) {
	if s.wallet == nil {
		return nil, newRPCError(rpcErrWalletNotFound, "wallet not loaded")
	}
	if err := s.wallet.RequireUnlocked(); err != nil {
		return nil, newRPCError(rpcErrMisc, err.Error())
	}
	if len(params) < 2 {
		return nil, newRPCError(rpcErrInvalidParams, "missing address and amount parameters")
	}
	var address string
	if err := json.Unmarshal(params[0], &address); err != nil {
		return nil, newRPCError(rpcErrInvalidParams, "invalid address: "+err.Error())
	}
	var amount uint64
	if err := json.Unmarshal(params[1], &amount); err != nil {
		return nil, newRPCError(rpcErrInvalidParams, "invalid amount: "+err.Error())
	}

	_, tipHeight := s.chain.Tip()
	utxos := s.wallet.FindUnspent(s.makeUtxoIterator(), tipHeight)

	tx, err := s.wallet.BuildTransaction(
		wallet.SendRequest{ToAddress: address, Amount: amount},
		s.feePerByte.Load(),
		utxos,
		s.params.CoinbaseMaturity,
		tipHeight,
	)
	if err != nil {
		return nil, newRPCError(rpcErrMisc, err.Error())
	}

	txHash, err := s.submitTxToMempool(tx)
	if err != nil {
		return nil, newRPCError(rpcErrMisc, "mempool rejection: "+err.Error())
	}
	return txHash.ReverseString(), nil
}

// rpcGetRawChangeAddress returns a new change address via JSON-RPC.
func (s *Server) rpcGetRawChangeAddress(_ []json.RawMessage) (interface{}, *jsonRPCError) {
	if s.wallet == nil {
		return nil, newRPCError(rpcErrWalletNotFound, "wallet not loaded")
	}
	addr, err := s.wallet.GetChangeAddress()
	if err != nil {
		return nil, newRPCError(rpcErrInternal, err.Error())
	}
	return addr, nil
}
