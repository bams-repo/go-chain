// Copyright (c) 2024-2026 The Fairchain Contributors
// Fairchain is an experiment in modularity, designed to improve on the work
// of Satoshi Nakamoto and to inspire more creative genius in the space.
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/bams-repo/fairchain/internal/coinparams"
	"github.com/bams-repo/fairchain/internal/config"
	"github.com/bams-repo/fairchain/internal/logging"
	"github.com/bams-repo/fairchain/internal/node"
	"github.com/bams-repo/fairchain/internal/params"
	"github.com/bams-repo/fairchain/internal/version"
)

func main() {
	configPath := flag.String("config", "", "Path to config file (JSON)")
	confPath := flag.String("conf", "", "Path to "+coinparams.ConfFileName+" (INI-style)")
	network := flag.String("network", "", "Override network (mainnet/testnet/regtest)")
	dataDir := flag.String("datadir", "", "Override data directory")
	listen := flag.String("listen", "", "Override P2P listen address (host:port)")
	rpcBind := flag.String("rpcbind", "", "RPC bind address (default: 127.0.0.1)")
	rpcPort := flag.String("rpcport", "", "RPC port (default: network-dependent)")
	rpcAddr := flag.String("rpc", "", "Override full RPC address (host:port) — legacy flag")
	mine := flag.Bool("mine", false, "Enable mining")
	addNode := flag.String("addnode", "", "Add a peer to connect to (ip:port)")
	seedPeers := flag.String("seed-peers", "", "Comma-separated seed peer addresses (ip:port,ip:port)")
	connectPeers := flag.String("connect", "", "Connect ONLY to these peers (ip:port,ip:port) — disables all discovery")
	maxInbound := flag.Int("maxinbound", 0, "Maximum inbound peer connections (0 = use config/default)")
	maxOutbound := flag.Int("maxoutbound", 0, "Maximum outbound peer connections (0 = use config/default)")
	noSeedNodes := flag.Bool("noseednode", false, "Suppress hardcoded seed nodes from chain params")
	logLevel := flag.String("log-level", "info", "Log level: debug, info, warn, error")
	logFormat := flag.String("log-format", "text", "Log format: text or json")
	debugFlag := flag.Bool("debug", false, "Enable hyper-verbose debug output (p2p_sync, chain_sync, sync_audit: relay, topology, IBD decisions, reorg, headers)")
	rpctlsCert := flag.String("rpctlscert", "", "Path to TLS certificate for RPC server (required for non-loopback binds)")
	rpctlsKey := flag.String("rpctlskey", "", "Path to TLS key for RPC server (required for non-loopback binds)")
	noRPCAuth := flag.Bool("norpcauth", false, "Disable RPC authentication (testing/regtest only)")
	migrateFlag := flag.Bool("migrate", false, "Migrate legacy blocks.db to new format")
	printVersion := flag.Bool("version", false, "Print version and exit")
	flag.Parse()

	if *printVersion {
		fmt.Printf("%s Daemon version v%s\n", coinparams.Name, version.String())
		os.Exit(0)
	}

	logging.Init(*logLevel, *logFormat)
	if *debugFlag {
		logging.EnableDebug()
	}
	log := logging.L

	// --- Load config: try INI conf first, then JSON, then defaults ---
	var cfg *config.Config
	var err error

	earlyNetwork := *network
	if earlyNetwork == "" {
		earlyNetwork = "regtest"
	}

	if *confPath != "" {
		cfg, err = config.LoadConf(*confPath, earlyNetwork)
		if err != nil {
			log.Error("failed to load conf", "error", err)
			os.Exit(1)
		}
	} else if *configPath != "" {
		cfg, err = config.LoadConfig(*configPath)
		if err != nil {
			log.Error("failed to load config", "error", err)
			os.Exit(1)
		}
	} else {
		cfg = config.DefaultConfig()
		defaultConf := cfg.ConfFilePath()
		if _, statErr := os.Stat(defaultConf); statErr == nil {
			cfg, err = config.LoadConf(defaultConf, earlyNetwork)
			if err != nil {
				log.Warn("failed to load default "+coinparams.ConfFileName+", using defaults", "error", err)
				cfg = config.DefaultConfig()
			}
		}
	}

	// --- Apply CLI overrides ---
	if *network != "" {
		cfg.Network = *network
	}
	if *dataDir != "" {
		cfg.DataDir = *dataDir
	}
	if *listen != "" {
		cfg.ListenAddr = *listen
	}
	if *rpcAddr != "" {
		cfg.RPCAddr = *rpcAddr
	} else if *rpcBind != "" || *rpcPort != "" {
		host, port := "127.0.0.1", "19445"
		if existing := cfg.RPCAddr; existing != "" {
			if h, p, err := node.SplitHostPort(existing); err == nil {
				host, port = h, p
			}
		}
		if *rpcBind != "" {
			host = *rpcBind
		}
		if *rpcPort != "" {
			port = *rpcPort
		}
		cfg.RPCAddr = host + ":" + port
	}
	if *mine {
		cfg.MiningEnabled = true
	}
	if *addNode != "" {
		cfg.SeedPeers = append(cfg.SeedPeers, *addNode)
	}
	if *seedPeers != "" {
		cfg.SeedPeers = append(cfg.SeedPeers, strings.Split(*seedPeers, ",")...)
	}
	if *maxInbound > 0 {
		cfg.MaxInbound = *maxInbound
	}
	if *maxOutbound > 0 {
		cfg.MaxOutbound = *maxOutbound
	}

	// --- Handle migration (exits after completion) ---
	if *migrateFlag {
		p := params.NetworkByName(cfg.Network)
		if p == nil {
			log.Error("unknown network", "network", cfg.Network)
			os.Exit(1)
		}
		cfg.DataDirName = p.DataDirName
		if err := cfg.EnsureDataDir(); err != nil {
			log.Error("failed to create data dir", "error", err)
			os.Exit(1)
		}
		if err := node.MigrateFromLegacy(cfg, p); err != nil {
			log.Error("migration failed", "error", err)
			os.Exit(1)
		}
		log.Info("migration complete")
		os.Exit(0)
	}

	// --- Build node options from CLI flags ---
	opts := node.Options{
		MiningEnabled: cfg.MiningEnabled,
		NoRPCAuth:     *noRPCAuth,
		NoSeedNodes:   *noSeedNodes,
		RPCTLSCert:    *rpctlsCert,
		RPCTLSKey:     *rpctlsKey,
	}
	if *connectPeers != "" {
		opts.ConnectOnly = strings.Split(*connectPeers, ",")
	}

	// --- Create and start node ---
	n, err := node.New(cfg, opts)
	if err != nil {
		log.Error("failed to initialize node", "error", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	if err := n.Start(ctx); err != nil {
		cancel()
		n.Stop()
		log.Error("failed to start node", "error", err)
		os.Exit(1)
	}

	// Wire the RPC "stop" command to trigger graceful shutdown.
	sigCh := make(chan os.Signal, 1)
	n.SetShutdownFunc(func() {
		sigCh <- syscall.SIGTERM
	})

	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh
	log.Info("received shutdown signal", "signal", sig)

	cancel()
	n.Stop()
}
