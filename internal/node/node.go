// Copyright (c) 2024-2026 The Fairchain Contributors
// Fairchain is an experiment in modularity, designed to improve on the work
// of Satoshi Nakamoto and to inspire more creative genius in the space.
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

package node

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/bams-repo/fairchain/internal/algorithms"
	"github.com/bams-repo/fairchain/internal/chain"
	"github.com/bams-repo/fairchain/internal/coinparams"
	"github.com/bams-repo/fairchain/internal/config"
	"github.com/bams-repo/fairchain/internal/consensus"
	"github.com/bams-repo/fairchain/internal/consensus/pow"
	"github.com/bams-repo/fairchain/internal/crypto"
	"github.com/bams-repo/fairchain/internal/difficulty"
	"github.com/bams-repo/fairchain/internal/logging"
	"github.com/bams-repo/fairchain/internal/mempool"
	"github.com/bams-repo/fairchain/internal/metrics"
	"github.com/bams-repo/fairchain/internal/miner"
	"github.com/bams-repo/fairchain/internal/p2p"
	"github.com/bams-repo/fairchain/internal/params"
	"github.com/bams-repo/fairchain/internal/rpc"
	"github.com/bams-repo/fairchain/internal/store"
	"github.com/bams-repo/fairchain/internal/timeadjust"
	"github.com/bams-repo/fairchain/internal/types"
	"github.com/bams-repo/fairchain/internal/wallet"
)

// Options controls optional node behaviour. Callers (daemon, GUI) set these
// based on their own CLI flags or UI state.
type Options struct {
	MiningEnabled bool
	NoRPCAuth     bool
	NoSeedNodes   bool
	ConnectOnly   []string
	RPCTLSCert    string
	RPCTLSKey     string
}

// Node encapsulates the full node lifecycle: stores, chain, mempool, P2P,
// wallet, RPC, and optional miner. Both fairchaind and fairchain-qt embed
// this to share the identical startup/shutdown sequence.
type Node struct {
	cfg    *config.Config
	params *params.ChainParams
	opts   Options

	engine     consensus.Engine
	adjClock   *timeadjust.AdjustedClock
	blockStore *store.FileStore
	peerStore  *store.BoltStore
	lockFile   *os.File

	chain   *chain.Chain
	mempool *mempool.Mempool
	p2p     *p2p.Manager
	wallet  *wallet.HDWallet
	rpc     *rpc.Server
	miner       *miner.Miner
	minerCancel context.CancelFunc

	cancel context.CancelFunc
}

// New creates a Node, opening stores and initialising the chain, mempool, and
// wallet. It does NOT start networking or mining — call Start for that.
func New(cfg *config.Config, opts Options) (*Node, error) {
	log := logging.L

	// Resolve chain params.
	p := params.NetworkByName(cfg.Network)
	if p == nil {
		return nil, fmt.Errorf("unknown network %q", cfg.Network)
	}
	cfg.DataDirName = p.DataDirName

	// Resolve PoW hasher.
	hasher, err := algorithms.GetHasher(coinparams.Algorithm)
	if err != nil {
		return nil, fmt.Errorf("unsupported PoW algorithm %q: %w", coinparams.Algorithm, err)
	}

	// Resolve difficulty retargeter.
	retargeter, err := difficulty.GetRetargeter(coinparams.DifficultyAlgorithm)
	if err != nil {
		return nil, fmt.Errorf("unsupported difficulty algorithm %q: %w", coinparams.DifficultyAlgorithm, err)
	}

	// Mine/verify genesis.
	if err := initNetworkGenesis(p, hasher, retargeter); err != nil {
		return nil, fmt.Errorf("genesis init: %w", err)
	}

	// Ensure data directory tree exists.
	if err := cfg.EnsureDataDir(); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	log.Info("starting "+coinparams.NameLower+" node",
		"network", cfg.Network,
		"datadir", cfg.NetworkDataDir(),
		"blocks", cfg.BlocksDir(),
		"chainstate", cfg.ChainstateDir())

	// Acquire lock file.
	lockFile, err := store.AcquireLock(cfg.LockFilePath())
	if err != nil {
		return nil, fmt.Errorf("acquire lock file (is another instance running?): %w", err)
	}

	// Open block store (flat files + LevelDB).
	blockStore, err := store.NewFileStore(
		cfg.BlocksDir(),
		cfg.BlockIndexDir(),
		cfg.ChainstateDir(),
		p.NetworkMagic,
	)
	if err != nil {
		store.ReleaseLock(lockFile)
		return nil, fmt.Errorf("open block store: %w", err)
	}

	// Open peer store (bbolt).
	peerStore, err := store.NewBoltStore(cfg.PeerDBPath())
	if err != nil {
		blockStore.Close()
		store.ReleaseLock(lockFile)
		return nil, fmt.Errorf("open peer store: %w", err)
	}

	engine := pow.New(hasher, retargeter)
	adjClock := timeadjust.New()

	// Create blockchain.
	bc := chain.New(p, engine, blockStore, adjClock)
	if err := bc.Init(); err != nil {
		peerStore.Close()
		blockStore.Close()
		store.ReleaseLock(lockFile)
		return nil, fmt.Errorf("initialize chain: %w", err)
	}

	tipHash, tipHeight := bc.Tip()
	log.Info("chain initialized", "tip", tipHash.ReverseString(), "height", tipHeight)

	// Create mempool.
	mp := mempool.New(p, bc.UtxoSet(), func() uint32 { _, h := bc.Tip(); return h })

	// Load persisted mempool.
	if data, loadErr := os.ReadFile(cfg.MempoolPath()); loadErr == nil && len(data) > 0 {
		loaded := mp.LoadFromBytes(data)
		log.Info("loaded mempool from disk", "transactions", loaded)
	}

	// Initialize HD wallet.
	hdWallet, err := wallet.NewHDWallet(cfg.WalletDir(), p.AddressPrefix)
	if err != nil {
		peerStore.Close()
		blockStore.Close()
		store.ReleaseLock(lockFile)
		return nil, fmt.Errorf("initialize wallet: %w", err)
	}
	log.Info("wallet loaded",
		"address", hdWallet.GetDefaultAddress(),
		"keys", hdWallet.KeyCount())

	return &Node{
		cfg:        cfg,
		params:     p,
		opts:       opts,
		engine:     engine,
		adjClock:   adjClock,
		blockStore: blockStore,
		peerStore:  peerStore,
		lockFile:   lockFile,
		chain:      bc,
		mempool:    mp,
		wallet:     hdWallet,
	}, nil
}

// Start begins P2P networking, the RPC server, and (optionally) mining.
func (n *Node) Start(ctx context.Context) error {
	ctx, n.cancel = context.WithCancel(ctx)

	// P2P options.
	p2pOpts := &p2p.ManagerOptions{
		NoSeedNodes: n.opts.NoSeedNodes,
	}
	if len(n.opts.ConnectOnly) > 0 {
		p2pOpts.ConnectOnly = n.opts.ConnectOnly
	}

	n.p2p = p2p.NewManager(n.params, n.chain, n.mempool, n.peerStore,
		n.cfg.ListenAddr, n.cfg.MaxInbound, n.cfg.MaxOutbound,
		n.cfg.SeedPeers, n.adjClock, p2pOpts)

	store.LoadPeersDat(n.cfg.PeersDatPath(), n.peerStore)

	if err := n.p2p.Start(ctx); err != nil {
		n.cancel()
		return fmt.Errorf("start P2P: %w", err)
	}

	// RPC server.
	var rpcAuth *rpc.AuthConfig
	if !n.opts.NoRPCAuth {
		rpcAuth = &rpc.AuthConfig{
			User:       n.cfg.RPCUser,
			Password:   n.cfg.RPCPassword,
			CookiePath: n.cfg.RPCCookiePath(),
		}
	} else {
		host, _, _ := SplitHostPort(n.cfg.RPCAddr)
		if host != "127.0.0.1" && host != "::1" && host != "localhost" && host != "" {
			n.p2p.Stop()
			n.cancel()
			return fmt.Errorf("--norpcauth is only allowed when RPC is bound to loopback, got %q", host)
		}
	}

	var tlsCfg *rpc.TLSConfig
	if n.opts.RPCTLSCert != "" && n.opts.RPCTLSKey != "" {
		tlsCfg = &rpc.TLSConfig{
			CertFile: n.opts.RPCTLSCert,
			KeyFile:  n.opts.RPCTLSKey,
		}
	}

	rpcServer, err := rpc.New(n.cfg.RPCAddr, n.chain, n.engine, n.mempool, n.p2p, n.params, rpcAuth, tlsCfg)
	if err != nil {
		n.p2p.Stop()
		n.cancel()
		return fmt.Errorf("create RPC server: %w", err)
	}
	rpcServer.SetWallet(n.wallet)
	rpcServer.SetDataDir(n.cfg.NetworkDataDir())
	rpcServer.SetBroadcastTx(n.p2p.BroadcastTx)
	rpcServer.SetBroadcastBlock(n.p2p.BroadcastBlock)

	if err := rpcServer.Start(); err != nil {
		n.p2p.Stop()
		n.cancel()
		return fmt.Errorf("start RPC: %w", err)
	}
	n.rpc = rpcServer

	// Start miner if enabled.
	if n.opts.MiningEnabled {
		if err := n.startMiner(ctx); err != nil {
			return err
		}
	}

	return nil
}

func (n *Node) startMiner(ctx context.Context) error {
	rewardScript := n.wallet.GetDefaultP2PKHScript()
	if rewardScript == nil {
		return fmt.Errorf("wallet has no keys for mining reward")
	}

	_, pkh := n.wallet.MiningKeyCompat()
	logging.L.Info("mining key loaded",
		"pubkey_hash", fmt.Sprintf("%x", pkh[:]),
		"script_len", len(rewardScript))

	bc := n.chain
	mp := n.mempool
	p2pMgr := n.p2p

	m := miner.New(bc, n.engine, mp, n.params, rewardScript, n.adjClock, func() bool {
		return p2pMgr.ShouldMine()
	}, func(block *types.Block) {
		height, err := bc.ProcessBlock(block)
		if err != nil {
			logging.L.Warn("mined block rejected", "error", err)
			return
		}
		p2pMgr.NotifyBlockAccepted(&block.Header)
		var confirmedHashes []types.Hash
		for _, tx := range block.Transactions {
			txHash, hashErr := crypto.HashTransaction(&tx)
			if hashErr == nil {
				confirmedHashes = append(confirmedHashes, txHash)
			}
		}
		mp.RemoveTxs(confirmedHashes)
		blockHash := crypto.HashBlockHeader(&block.Header)
		metrics.Global.BlocksMined.Add(1)
		logging.L.Info("mined block accepted", "hash", blockHash.ReverseString(), "height", height)
		p2pMgr.BroadcastBlock(blockHash, block)
	})

	n.miner = m
	go m.Run(ctx)
	return nil
}

// SetMining starts or stops the built-in miner at runtime.
func (n *Node) SetMining(enabled bool) {
	if enabled {
		if n.miner != nil {
			return // already mining
		}
		ctx, cancel := context.WithCancel(context.Background())
		n.minerCancel = cancel
		if err := n.startMiner(ctx); err != nil {
			logging.L.Error("failed to start miner", "error", err)
			cancel()
			n.minerCancel = nil
			return
		}
		logging.L.Info("mining enabled at runtime", "component", "node")
	} else {
		if n.minerCancel != nil {
			n.minerCancel()
			n.minerCancel = nil
		}
		n.miner = nil
		logging.L.Info("mining disabled at runtime", "component", "node")
	}
}

// IsMining returns true if the miner is currently running.
func (n *Node) IsMining() bool {
	return n.miner != nil && n.minerCancel != nil
}

// GetHashrate returns the current mining hashrate in hashes/sec.
func (n *Node) GetHashrate() uint64 {
	if n.miner == nil {
		return 0
	}
	return n.miner.Hashrate()
}

// HashrateReady returns true once the miner has enough samples for a
// meaningful hashrate average.
func (n *Node) HashrateReady() bool {
	if n.miner == nil {
		return false
	}
	return n.miner.HashrateReady()
}

// Stop performs a graceful shutdown: persist mempool, dump peers, close stores.
func (n *Node) Stop() error {
	log := logging.L

	if n.minerCancel != nil {
		n.minerCancel()
		n.minerCancel = nil
	}

	if n.cancel != nil {
		n.cancel()
	}

	if n.rpc != nil {
		log.Info("stopping RPC server...")
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		if err := n.rpc.Stop(shutdownCtx); err != nil {
			log.Warn("RPC shutdown error", "error", err)
		}
	}

	if n.p2p != nil {
		log.Info("stopping P2P manager...")
		n.p2p.Stop()
	}

	// Persist mempool.
	if n.mempool != nil {
		if data := n.mempool.DumpToBytes(); len(data) > 0 {
			if err := os.WriteFile(n.cfg.MempoolPath(), data, 0600); err != nil {
				log.Warn("failed to persist mempool", "error", err)
			} else {
				log.Info("mempool persisted", "transactions", n.mempool.Count())
			}
		}
	}

	// Dump peers.dat.
	if n.peerStore != nil {
		store.DumpPeersDat(n.cfg.PeersDatPath(), n.peerStore)
		log.Info("closing peer store...")
		if err := n.peerStore.Close(); err != nil {
			log.Warn("peer store close error", "error", err)
		}
	}

	if n.blockStore != nil {
		log.Info("closing block store...")
		if err := n.blockStore.Close(); err != nil {
			log.Warn("block store close error", "error", err)
		}
	}

	if n.lockFile != nil {
		store.ReleaseLock(n.lockFile)
	}

	log.Info("shutdown complete")
	return nil
}

// --- Accessors for the GUI binding layer and RPC wiring ---

func (n *Node) Chain() *chain.Chain          { return n.chain }
func (n *Node) Wallet() *wallet.HDWallet     { return n.wallet }
func (n *Node) Mempool() *mempool.Mempool    { return n.mempool }
func (n *Node) P2PMgr() *p2p.Manager         { return n.p2p }
func (n *Node) Params() *params.ChainParams  { return n.params }
func (n *Node) Config() *config.Config       { return n.cfg }
func (n *Node) RPCServer() *rpc.Server       { return n.rpc }
func (n *Node) Engine() consensus.Engine      { return n.engine }

// SetShutdownFunc wires the RPC "stop" command to trigger a graceful shutdown.
func (n *Node) SetShutdownFunc(fn func()) {
	if n.rpc != nil {
		n.rpc.SetShutdownFunc(fn)
	}
}

// SplitHostPort is a utility shared by the daemon and GUI entry points.
func SplitHostPort(addr string) (string, string, error) {
	idx := strings.LastIndex(addr, ":")
	if idx < 0 {
		return addr, "", fmt.Errorf("no port in %q", addr)
	}
	return addr[:idx], addr[idx+1:], nil
}
