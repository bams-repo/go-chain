package rpc

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bams-repo/fairchain/internal/chain"
	"github.com/bams-repo/fairchain/internal/coinparams"
	"github.com/bams-repo/fairchain/internal/consensus"
	"github.com/bams-repo/fairchain/internal/crypto"
	"github.com/bams-repo/fairchain/internal/logging"
	"github.com/bams-repo/fairchain/internal/mempool"
	"github.com/bams-repo/fairchain/internal/metrics"
	"github.com/bams-repo/fairchain/internal/p2p"
	"github.com/bams-repo/fairchain/internal/params"
	"github.com/bams-repo/fairchain/internal/types"
	"github.com/bams-repo/fairchain/internal/version"
)

// AuthConfig holds RPC authentication settings.
type AuthConfig struct {
	User       string
	Password   string
	CookiePath string
}

// TLSConfig holds optional TLS certificate paths for the RPC server.
type TLSConfig struct {
	CertFile string
	KeyFile  string
}

// ShutdownFunc is called when the stop RPC is invoked.
type ShutdownFunc func()

// TxBroadcaster is called after a transaction is accepted into the mempool
// to announce it to the P2P network.
type TxBroadcaster func(hash types.Hash)

// BlockBroadcaster is called after a block is accepted via submitblock RPC
// to relay it to the P2P network. Without this, pool-submitted blocks would
// be accepted locally but never propagated.
type BlockBroadcaster func(hash types.Hash, block *types.Block)

// Server provides a local HTTP JSON API matching Bitcoin Core's RPC interface.
type Server struct {
	chain        *chain.Chain
	engine       consensus.Engine
	mempool      *mempool.Mempool
	p2p          *p2p.Manager
	params       *params.ChainParams
	server       *http.Server
	shutdownFn   ShutdownFunc
	authUser     string
	authPassword string
	cookiePath   string
	wallet         WalletInterface
	broadcastTx    TxBroadcaster
	broadcastBlock BlockBroadcaster
	feePerByte     atomic.Uint64
	tlsCert        string
	tlsKey         string
	dataDir        string // Network-specific data directory for backup path restriction
	longPoll       longPollState
	rateLimiter    *ipRateLimiter
	methodMap      map[string]rpcHandler
}

// --- Per-IP RPC rate limiting ---

const (
	rpcRateLimit  = 60            // max requests per window per IP
	rpcRateWindow = 10 * time.Second
	rpcRateGC     = 60 * time.Second // stale entry cleanup interval
)

type ipBucket struct {
	count       int
	windowStart time.Time
}

type ipRateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*ipBucket
}

func newIPRateLimiter() *ipRateLimiter {
	rl := &ipRateLimiter{buckets: make(map[string]*ipBucket)}
	go rl.gcLoop()
	return rl
}

func (rl *ipRateLimiter) Allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	b, ok := rl.buckets[ip]
	if !ok || now.Sub(b.windowStart) > rpcRateWindow {
		rl.buckets[ip] = &ipBucket{count: 1, windowStart: now}
		return true
	}
	b.count++
	return b.count <= rpcRateLimit
}

func (rl *ipRateLimiter) gcLoop() {
	ticker := time.NewTicker(rpcRateGC)
	defer ticker.Stop()
	for range ticker.C {
		rl.mu.Lock()
		now := time.Now()
		for ip, b := range rl.buckets {
			if now.Sub(b.windowStart) > rpcRateWindow*2 {
				delete(rl.buckets, ip)
			}
		}
		rl.mu.Unlock()
	}
}

// New creates a new RPC server. auth may be nil only when --norpcauth is
// explicitly set. Without auth, all endpoints (including dumpprivkey, stop)
// are unprotected — only appropriate for local regtest.
func New(addr string, c *chain.Chain, e consensus.Engine, mp *mempool.Mempool, pm *p2p.Manager, p *params.ChainParams, auth *AuthConfig, tlsCfg *TLSConfig) (*Server, error) {
	s := &Server{
		chain:       c,
		engine:      e,
		mempool:     mp,
		p2p:         pm,
		params:      p,
		rateLimiter: newIPRateLimiter(),
	}
	s.feePerByte.Store(defaultFeePerByte)
	s.methodMap = s.buildMethodMap()

	if tlsCfg != nil {
		s.tlsCert = tlsCfg.CertFile
		s.tlsKey = tlsCfg.KeyFile
	}

	if auth != nil {
		if err := s.initAuth(auth); err != nil {
			return nil, fmt.Errorf("RPC auth init: %w", err)
		}
	} else {
		logging.L.Warn("RPC authentication DISABLED — all endpoints are unprotected. Use --norpcauth only for local testing.", "component", "rpc")
	}

	mux := http.NewServeMux()

	// Bitcoin Core parity: blockchain RPCs
	mux.HandleFunc("/getblockchaininfo", s.handleGetBlockchainInfo)
	mux.HandleFunc("/getblockcount", s.handleGetBlockCount)
	mux.HandleFunc("/getbestblockhash", s.handleGetBestBlockHash)
	mux.HandleFunc("/getblockhash", s.handleGetBlockHash)
	mux.HandleFunc("/getblock", s.handleGetBlock)
	mux.HandleFunc("/getblockbyheight", s.handleGetBlockByHeight)
	mux.HandleFunc("/getdifficulty", s.handleGetDifficulty)

	// Bitcoin Core parity: network RPCs
	mux.HandleFunc("/getnetworkinfo", s.handleGetNetworkInfo)
	mux.HandleFunc("/getpeerinfo", s.handleGetPeerInfo)
	mux.HandleFunc("/getconnectioncount", s.handleGetConnectionCount)
	mux.HandleFunc("/addnode", s.handleAddNode)
	mux.HandleFunc("/disconnectnode", s.handleDisconnectNode)

	// Bitcoin Core parity: mempool RPCs
	mux.HandleFunc("/getmempoolinfo", s.handleGetMempoolInfo)
	mux.HandleFunc("/getrawmempool", s.handleGetRawMempool)
	mux.HandleFunc("/getmempoolentry", s.handleGetMempoolEntry)

	// Bitcoin Core parity: UTXO RPCs
	mux.HandleFunc("/gettxout", s.handleGetTxOut)
	mux.HandleFunc("/gettxoutsetinfo", s.handleGetTxOutSetInfo)

	// Bitcoin Core parity: mining RPCs
	mux.HandleFunc("/getblocktemplate", s.handleGetBlockTemplate)
	mux.HandleFunc("/submitblock", s.handleSubmitBlock)
	mux.HandleFunc("/getmininginfo", s.handleGetMiningInfo)
	mux.HandleFunc("/getnetworkhashps", s.handleGetNetworkHashPS)
	mux.HandleFunc("/preciousblock", s.handlePreciousBlock)

	// Bitcoin Core parity: raw transaction RPCs
	mux.HandleFunc("/getrawtransaction", s.handleGetRawTransaction)

	// Bitcoin Core JSON-RPC 1.0 dispatch (stratum pool compatibility)
	mux.HandleFunc("/", s.handleJSONRPC)

	// Bitcoin Core parity: control RPCs
	mux.HandleFunc("/getinfo", s.handleGetInfo)
	mux.HandleFunc("/stop", s.handleStop)

	// Bitcoin Core parity: wallet RPCs
	mux.HandleFunc("/getnewaddress", s.handleGetNewAddress)
	mux.HandleFunc("/getbalance", s.handleGetBalance)
	mux.HandleFunc("/listunspent", s.handleListUnspent)
	mux.HandleFunc("/sendtoaddress", s.handleSendToAddress)
	mux.HandleFunc("/getwalletinfo", s.handleGetWalletInfo)
	mux.HandleFunc("/dumpprivkey", s.handleDumpPrivKey)
	mux.HandleFunc("/importprivkey", s.handleImportPrivKey)
	mux.HandleFunc("/listtransactions", s.handleListTransactions)
	mux.HandleFunc("/validateaddress", s.handleValidateAddress)
	mux.HandleFunc("/getrawchangeaddress", s.handleGetRawChangeAddress)
	mux.HandleFunc("/settxfee", s.handleSetTxFee)
	mux.HandleFunc("/sendrawtransaction", s.handleSendRawTransaction)
	mux.HandleFunc("/signrawtransactionwithwallet", s.handleSignRawTransactionWithWallet)
	mux.HandleFunc("/getreceivedbyaddress", s.handleGetReceivedByAddress)
	mux.HandleFunc("/listaddressgroupings", s.handleListAddressGroupings)
	mux.HandleFunc("/backupwallet", s.handleBackupWallet)
	mux.HandleFunc("/getaddressesbylabel", s.handleGetAddressesByLabel)
	mux.HandleFunc("/gettransaction", s.handleGetTransaction)

	// Bitcoin Core parity: wallet encryption RPCs
	mux.HandleFunc("/encryptwallet", s.handleEncryptWallet)
	mux.HandleFunc("/walletpassphrase", s.handleWalletPassphrase)
	mux.HandleFunc("/walletlock", s.handleWalletLock)

	// Chain-specific
	mux.HandleFunc("/getchainstatus", s.handleGetChainStatus)
	mux.HandleFunc("/metrics", s.handleMetrics)
	mux.HandleFunc("/prom", s.handlePrometheus)
	mux.HandleFunc("/dumpwallet", s.handleDumpWallet)

	var handler http.Handler = mux
	if s.authUser != "" {
		handler = s.authMiddleware(mux)
	}
	handler = s.rateLimitMiddleware(handler)
	handler = s.recoveryMiddleware(handler)

	s.server = &http.Server{
		Addr:           addr,
		Handler:        handler,
		ReadTimeout:    30 * time.Second,
		WriteTimeout:   60 * time.Second,
		IdleTimeout:    120 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}

	return s, nil
}

// initAuth configures RPC authentication. If explicit credentials are provided
// they are used directly. Otherwise a random cookie is generated and written to
// disk, matching Bitcoin Core's -rpcauth cookie-file behavior.
// Returns an error if authentication cannot be established — the RPC server
// must not start without authentication.
func (s *Server) initAuth(auth *AuthConfig) error {
	if auth.User != "" && auth.Password != "" {
		s.authUser = auth.User
		s.authPassword = auth.Password
		logging.L.Info("RPC authentication enabled (config credentials)", "component", "rpc")
		return nil
	}

	user := "__cookie__"
	password, err := generateCookiePassword()
	if err != nil {
		return fmt.Errorf("generate cookie password: %w", err)
	}

	s.authUser = user
	s.authPassword = password
	s.cookiePath = auth.CookiePath

	cookie := user + ":" + password
	if err := os.WriteFile(auth.CookiePath, []byte(cookie), 0600); err != nil {
		return fmt.Errorf("write cookie file %s: %w", auth.CookiePath, err)
	}
	logging.L.Info("RPC authentication enabled (cookie file)", "component", "rpc", "path", auth.CookiePath)
	return nil
}

func generateCookiePassword() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok ||
			subtle.ConstantTimeCompare([]byte(user), []byte(s.authUser)) != 1 ||
			subtle.ConstantTimeCompare([]byte(pass), []byte(s.authPassword)) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="`+coinparams.RPCRealm+`"`)
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) rateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip, _, _ := net.SplitHostPort(r.RemoteAddr)
		if ip == "" {
			ip = r.RemoteAddr
		}
		if !s.rateLimiter.Allow(ip) {
			w.Header().Set("Retry-After", "10")
			writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) recoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				buf := make([]byte, 4096)
				n := runtime.Stack(buf, false)
				logging.L.Error("RPC handler panic", "component", "rpc", "panic", rec, "stack", string(buf[:n]))
				writeError(w, http.StatusInternalServerError, "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// SetShutdownFunc registers the callback invoked by the "stop" RPC.
func (s *Server) SetShutdownFunc(fn ShutdownFunc) {
	s.shutdownFn = fn
}

// SetWallet registers the HD wallet for wallet RPC endpoints.
func (s *Server) SetWallet(w WalletInterface) {
	s.wallet = w
}

// SetBroadcastTx registers the callback for broadcasting transactions to the P2P network.
func (s *Server) SetBroadcastTx(fn TxBroadcaster) {
	s.broadcastTx = fn
}

// SetDataDir sets the network-specific data directory for restricting backup paths.
func (s *Server) SetDataDir(dir string) {
	s.dataDir = dir
}

// SetBroadcastBlock registers the callback for relaying pool-submitted blocks
// to the P2P network. This is critical for stratum pool operation — without it,
// blocks accepted via submitblock would never reach other nodes.
func (s *Server) SetBroadcastBlock(fn BlockBroadcaster) {
	s.broadcastBlock = fn
}

// Start begins serving RPC requests. Non-loopback binds require TLS to
// prevent credentials from being transmitted in cleartext.
func (s *Server) Start() error {
	useTLS := s.tlsCert != "" && s.tlsKey != ""

	if !isLoopback(s.server.Addr) && !useTLS {
		return fmt.Errorf("RPC bound to non-loopback address %s without TLS — refusing to start. "+
			"Provide --rpctlscert and --rpctlskey, or bind to 127.0.0.1", s.server.Addr)
	}

	if useTLS {
		logging.L.Info("RPC listening (TLS)", "component", "rpc", "addr", s.server.Addr)
	} else {
		logging.L.Info("RPC listening", "component", "rpc", "addr", s.server.Addr)
	}

	go func() {
		var err error
		if useTLS {
			err = s.server.ListenAndServeTLS(s.tlsCert, s.tlsKey)
		} else {
			err = s.server.ListenAndServe()
		}
		if err != nil && err != http.ErrServerClosed {
			logging.L.Error("RPC server error", "component", "rpc", "error", err)
		}
	}()
	return nil
}

func isLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	if host == "" || host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// Stop gracefully shuts down the RPC server and removes the cookie file if one was generated.
func (s *Server) Stop(ctx context.Context) error {
	if s.cookiePath != "" {
		os.Remove(s.cookiePath)
	}
	return s.server.Shutdown(ctx)
}

// --- Control RPCs ---

func (s *Server) handleGetInfo(w http.ResponseWriter, r *http.Request) {
	tipHash, tipHeight := s.chain.Tip()
	info := s.chain.GetChainInfo()
	resp := map[string]interface{}{
		"version":         version.ProtocolVersion,
		"protocolversion": version.ProtocolVersion,
		"blocks":          tipHeight,
		"bestblockhash":   tipHash.ReverseString(),
		"difficulty":      info.Difficulty,
		"connections":     s.p2p.PeerCount(),
		"network":         s.params.Name,
		"mempool_size":    s.mempool.Count(),
	}
	writeJSON(w, resp)
}

func (s *Server) handleStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}
	writeJSON(w, coinparams.Name+" server stopping")
	if s.shutdownFn != nil {
		go s.shutdownFn()
	}
}

// --- Blockchain RPCs ---

func (s *Server) handleGetBlockchainInfo(w http.ResponseWriter, r *http.Request) {
	info := s.chain.GetChainInfo()
	resp := map[string]interface{}{
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
	}
	writeJSON(w, resp)
}

func (s *Server) handleGetChainStatus(w http.ResponseWriter, r *http.Request) {
	info := s.chain.GetChainInfo()
	resp := map[string]interface{}{
		"blocks":            info.Height,
		"bestblockhash":     info.BestHash.ReverseString(),
		"bits":              fmt.Sprintf("%08x", info.Bits),
		"difficulty":        info.Difficulty,
		"peers":             s.p2p.PeerCount(),
		"retarget_epoch":    info.RetargetEpoch,
		"epoch_progress":    info.EpochProgress,
		"retarget_interval": info.RetargetInterval,
	}
	writeJSON(w, resp)
}

func (s *Server) handleGetBlockCount(w http.ResponseWriter, r *http.Request) {
	_, height := s.chain.Tip()
	writeJSON(w, height)
}

func (s *Server) handleGetBestBlockHash(w http.ResponseWriter, r *http.Request) {
	hash, _ := s.chain.Tip()
	writeJSON(w, hash.ReverseString())
}

func (s *Server) handleGetBlockHash(w http.ResponseWriter, r *http.Request) {
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
	header, err := s.chain.GetHeaderByHeight(height)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("block not found at height %d", height))
		return
	}
	hash := crypto.HashBlockHeader(header)
	writeJSON(w, hash.ReverseString())
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

	resp := map[string]interface{}{
		"hash":          blockHash.ReverseString(),
		"confirmations": confirmations,
		"size":          0,
		"height":        blockHeight,
		"version":       block.Header.Version,
		"merkleroot":    block.Header.MerkleRoot.ReverseString(),
		"tx":            txids,
		"time":          block.Header.Timestamp,
		"nonce":         block.Header.Nonce,
		"bits":          fmt.Sprintf("%08x", block.Header.Bits),
		"previousblockhash": block.Header.PrevBlock.ReverseString(),
		"nTx":           len(block.Transactions),
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

	txids := make([]string, len(block.Transactions))
	for i, tx := range block.Transactions {
		txHash, _ := crypto.HashTransaction(&tx)
		txids[i] = txHash.ReverseString()
	}

	_, tipHeight := s.chain.Tip()
	confirmations := int64(tipHeight) - int64(height) + 1

	resp := map[string]interface{}{
		"hash":          blockHash.ReverseString(),
		"confirmations": confirmations,
		"height":        height,
		"version":       block.Header.Version,
		"merkleroot":    block.Header.MerkleRoot.ReverseString(),
		"tx":            txids,
		"time":          block.Header.Timestamp,
		"nonce":         block.Header.Nonce,
		"bits":          fmt.Sprintf("%08x", block.Header.Bits),
		"previousblockhash": block.Header.PrevBlock.ReverseString(),
		"nTx":           len(block.Transactions),
	}
	writeJSON(w, resp)
}

func (s *Server) handleGetDifficulty(w http.ResponseWriter, r *http.Request) {
	info := s.chain.GetChainInfo()
	writeJSON(w, info.Difficulty)
}

// --- Network RPCs ---

func (s *Server) handleGetNetworkInfo(w http.ResponseWriter, r *http.Request) {
	inbound, outbound := s.p2p.ConnectionCounts()
	resp := map[string]interface{}{
		"version":         version.ProtocolVersion,
		"subversion":      version.UserAgent(),
		"protocolversion": version.ProtocolVersion,
		"connections":     s.p2p.PeerCount(),
		"connections_in":  inbound,
		"connections_out": outbound,
		"networkactive":   true,
		"networks": []map[string]interface{}{
			{
				"name":      "ipv4",
				"reachable": true,
			},
		},
		"warnings": "",
	}
	writeJSON(w, resp)
}

func (s *Server) handleGetPeerInfo(w http.ResponseWriter, r *http.Request) {
	infos := s.p2p.PeerInfos()
	writeJSON(w, infos)
}

func (s *Server) handleGetConnectionCount(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.p2p.PeerCount())
}

func (s *Server) handleAddNode(w http.ResponseWriter, r *http.Request) {
	addr := r.URL.Query().Get("node")
	if addr == "" {
		writeError(w, http.StatusBadRequest, "missing node parameter")
		return
	}
	if err := s.p2p.AddNode(addr); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, map[string]interface{}{"added": addr})
}

func (s *Server) handleDisconnectNode(w http.ResponseWriter, r *http.Request) {
	addr := r.URL.Query().Get("address")
	if addr == "" {
		writeError(w, http.StatusBadRequest, "missing address parameter")
		return
	}
	if err := s.p2p.DisconnectNode(addr); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, map[string]interface{}{"disconnected": addr})
}

// --- Mining RPCs ---

func (s *Server) handleSubmitBlock(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}

	body, err := readLimitedBody(r, 8*1024*1024)
	if err != nil {
		writeError(w, http.StatusBadRequest, "read body: "+err.Error())
		return
	}

	var block types.Block

	// Try hex-encoded block first (Bitcoin Core parity for stratum pools).
	hexStr := string(bytes.TrimSpace(body))
	if isHexString(hexStr) {
		blockBytes, decErr := hex.DecodeString(hexStr)
		if decErr != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid hex: %v", decErr))
			return
		}
		if err := block.Deserialize(bytes.NewReader(blockBytes)); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid block: %v", err))
			return
		}
	} else {
		// Fall back to raw binary deserialization (legacy format).
		if err := block.Deserialize(bytes.NewReader(body)); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid block: %v", err))
			return
		}
	}

	height, err := s.chain.ProcessBlock(&block)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("rejected: %v", err))
		return
	}

	s.postBlockAccept(&block, height)

	blockHash := crypto.HashBlockHeader(&block.Header)
	writeJSON(w, map[string]interface{}{
		"accepted": true,
		"hash":     blockHash.ReverseString(),
		"height":   height,
	})
}

// handlePreciousBlock is a no-op that matches Bitcoin Core's preciousblock RPC.
// ckpool calls this after submitting a block to hint that the node should
// prefer it as the chain tip.
func (s *Server) handlePreciousBlock(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, nil)
}

// handleGetRawTransaction returns a hex-encoded raw transaction by txid.
func (s *Server) handleGetRawTransaction(w http.ResponseWriter, r *http.Request) {
	txidStr := r.URL.Query().Get("txid")
	if txidStr == "" {
		writeError(w, http.StatusBadRequest, "missing txid parameter")
		return
	}
	verbose := r.URL.Query().Get("verbose") == "true" || r.URL.Query().Get("verbose") == "1"

	var params []json.RawMessage
	txidJSON, _ := json.Marshal(txidStr)
	params = append(params, json.RawMessage(txidJSON))
	if verbose {
		params = append(params, json.RawMessage("true"))
	}

	result, rpcErr := s.rpcGetRawTransaction(params)
	if rpcErr != nil {
		writeError(w, http.StatusNotFound, rpcErr.Message)
		return
	}
	writeJSON(w, result)
}

// --- Mempool RPCs ---

func (s *Server) handleGetMempoolInfo(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]interface{}{
		"loaded":           true,
		"size":             s.mempool.Count(),
		"bytes":            s.mempool.TotalSize(),
		"maxmempool":       300 * 1024 * 1024,
		"mempoolminfee":    s.params.MinRelayTxFeeRate,
		"mempoolexpiry":    int64(s.params.MempoolExpiry.Hours()),
	})
}

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
			"time":    e.AddedAt.Unix(),
		}
	}
	writeJSON(w, result)
}

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
		"time":    entry.AddedAt.Unix(),
	}
	writeJSON(w, resp)
}

// --- UTXO RPCs ---

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

// --- Metrics ---

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, metrics.Global.Snapshot())
}

func (s *Server) handlePrometheus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.Write([]byte(metrics.Global.Prometheus()))
}

// --- Helpers ---

func writeJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
