// Copyright (c) 2024-2026 The Fairchain Contributors
// Fairchain is an experiment in modularity, designed to improve on the work
// of Satoshi Nakamoto and to inspire more creative genius in the space.
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bams-repo/fairchain/internal/coinparams"
	"github.com/bams-repo/fairchain/internal/config"
	"github.com/bams-repo/fairchain/internal/logging"
	"github.com/bams-repo/fairchain/internal/miner"
	"github.com/bams-repo/fairchain/internal/node"
	"github.com/bams-repo/fairchain/internal/params"
	"github.com/bams-repo/fairchain/internal/types"
	"github.com/bams-repo/fairchain/internal/utxo"
	"github.com/bams-repo/fairchain/internal/version"
	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// App is the Go struct bound to the Wails frontend. All public methods are
// callable from JavaScript via the generated bindings.
type App struct {
	ctx         context.Context
	node        *node.Node
	irc         *ircClient
	trayEnd     func()
	startupTime time.Time

	// Cached P2P port probe result, refreshed periodically in the background.
	probeMu     sync.RWMutex
	probeOpen   bool
	probeIP     string
	probePort   string
	probeError  string
	probeReady  bool // true once the first probe has completed
}

func NewApp() *App {
	return &App{}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.startupTime = time.Now()

	logLevel := "info"
	if v := os.Getenv("FAIRCHAIN_DEBUG"); v == "1" || v == "true" || strings.EqualFold(v, "yes") {
		logLevel = "debug"
	}
	logging.Init(logLevel, "text")
	if logLevel == "debug" {
		logging.EnableDebug()
	}

	cfg := config.DefaultConfig()
	cfg.Network = networkForBuild()

	if d := strings.TrimSpace(os.Getenv("FAIRCHAIN_DATADIR")); d != "" {
		cfg.DataDir = d
	}

	if netParams := params.NetworkByName(cfg.Network); netParams != nil {
		cfg.ListenAddr = fmt.Sprintf("0.0.0.0:%d", netParams.DefaultPort)
		cfg.RPCAddr = fmt.Sprintf("127.0.0.1:%d", netParams.DefaultPort+1)
	}

	opts := node.Options{
		NoRPCAuth: true,
	}

	n, err := node.New(cfg, opts)
	if err != nil {
		logging.L.Error("failed to initialize node", "error", err)
		return
	}

	if err := n.Start(ctx); err != nil {
		logging.L.Error("failed to start node", "error", err)
		n.Stop()
		return
	}

	if v := strings.TrimSpace(os.Getenv("FAIRCHAIN_MINING")); v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes") {
		n.SetMining(true)
		logging.L.Info("mining enabled via FAIRCHAIN_MINING", "network", cfg.Network)
	}

	a.node = n

	go a.probeLoop(ctx)

	nickPath := ircNickPath(n.Config())
	savedNick := loadIRCNick(nickPath)

	a.irc = newIRCClient(ircConfig{
		ServerAddr: "irc.libera.chat:6697",
		Channel:    "#test112221",
		NickPrefix: coinparams.NameLower,
		SavedNick:  savedNick,
		OnNickChange: func(nick string) {
			saveIRCNick(nickPath, nick)
		},
	})

	go func() {
		connectCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		if err := a.irc.Connect(connectCtx); err != nil {
			logging.L.Warn("wallet social chat failed to connect at startup", "error", err)
		}
	}()

	logging.L.Info(coinparams.Name+" Wallet started", "version", version.String())

	trayStart, trayEnd := initTray(trayIconPNG, coinparams.Name+" Wallet", trayCallbacks{
		OnShow: func() { wailsRuntime.WindowShow(a.ctx) },
		OnQuit: func() { wailsRuntime.Quit(a.ctx) },
	})
	a.trayEnd = trayEnd
	trayStart()
}

func (a *App) shutdown(ctx context.Context) {
	if a.trayEnd != nil {
		a.trayEnd()
	}
	if a.irc != nil {
		a.irc.Close()
	}
	if a.node != nil {
		a.node.Stop()
	}
}

// CoinInfo returns branding constants for the frontend. This is the ONLY
// source of truth for names, ticker, and units in the UI.
func (a *App) CoinInfo() map[string]interface{} {
	return map[string]interface{}{
		"name":            coinparams.Name,
		"nameLower":       coinparams.NameLower,
		"ticker":          coinparams.Ticker,
		"decimals":        coinparams.Decimals,
		"baseUnitName":    coinparams.BaseUnitName,
		"displayUnitName": coinparams.DisplayUnitName,
		"version":         version.String(),
		"copyright":       coinparams.CopyrightHolder,
		"network":         networkForBuild(),
	}
}

// GetBlockchainInfo returns basic chain state for the overview page.
func (a *App) GetBlockchainInfo() (map[string]interface{}, error) {
	if a.node == nil {
		return nil, fmt.Errorf("node not initialized")
	}
	bc := a.node.Chain()
	tipHash, tipHeight := bc.Tip()
	return map[string]interface{}{
		"height":   tipHeight,
		"bestHash": tipHash.ReverseString(),
		"network":  a.node.Config().Network,
	}, nil
}

// GetBalance returns the wallet balance (confirmed and unconfirmed).
func (a *App) GetBalance() (map[string]interface{}, error) {
	if a.node == nil {
		return nil, fmt.Errorf("node not initialized")
	}
	w := a.node.Wallet()
	bc := a.node.Chain()
	_, tipHeight := bc.Tip()

	iter := func(fn func(txHash [32]byte, index uint32, value uint64, pkScript []byte, height uint32, isCoinbase bool)) {
		bc.UtxoSet().ForEach(func(txHash types.Hash, index uint32, entry *utxo.UtxoEntry) {
			fn(txHash, index, entry.Value, entry.PkScript, entry.Height, entry.IsCoinbase)
		})
	}

	confirmed := w.GetBalance(iter, tipHeight, 1, a.node.Params().CoinbaseMaturity)
	total := w.GetBalance(iter, tipHeight, 0, a.node.Params().CoinbaseMaturity)
	unconfirmed := total - confirmed

	return map[string]interface{}{
		"confirmed":   float64(confirmed) / float64(coinparams.CoinsPerBaseUnit),
		"unconfirmed": float64(unconfirmed) / float64(coinparams.CoinsPerBaseUnit),
	}, nil
}

// GetPeerCount returns the number of connected peers.
func (a *App) GetPeerCount() (int, error) {
	if a.node == nil {
		return 0, fmt.Errorf("node not initialized")
	}
	return a.node.P2PMgr().PeerCount(), nil
}

// GetWalletAddress returns the default receive address.
func (a *App) GetWalletAddress() (string, error) {
	if a.node == nil {
		return "", fmt.Errorf("node not initialized")
	}
	return a.node.Wallet().GetDefaultAddress(), nil
}

// GetSyncProgress returns a value between 0.0 and 1.0 indicating sync progress.
func (a *App) GetSyncProgress() (float64, error) {
	if a.node == nil {
		return 0, fmt.Errorf("node not initialized")
	}
	return a.computeSyncProgress(), nil
}

// computeSyncProgress calculates a combined two-phase progress value.
// Header sync occupies 0.0–0.5, block sync occupies 0.5–1.0.
// The target height is derived from the best v2+ peer height advertised
// during handshake. If no peer height is known yet, headerHeight is used
// as a lower-bound estimate so the bar still advances.
func (a *App) computeSyncProgress() float64 {
	bc := a.node.Chain()
	p2p := a.node.P2PMgr()

	syncState := p2p.GetSyncState()
	_, blockHeight := bc.Tip()
	headerHeight := p2p.HeaderSyncHeight()
	bestPeerHeight := p2p.BestPeerHeight()

	switch syncState {
	case "SYNCED":
		return 1.0
	case "INITIAL":
		if bestPeerHeight > 0 && blockHeight > 0 {
			pct := float64(blockHeight) / float64(bestPeerHeight)
			if pct > 1.0 {
				pct = 1.0
			}
			return pct
		}
		return 0.0
	case "HEADER_SYNC":
		target := bestPeerHeight
		if target == 0 {
			target = headerHeight
		}
		if target == 0 {
			return 0.01
		}
		headerPct := float64(headerHeight) / float64(target)
		if headerPct > 1.0 {
			headerPct = 1.0
		}
		return headerPct * 0.5
	case "BLOCK_SYNC":
		target := headerHeight
		if target == 0 {
			target = bestPeerHeight
		}
		if target == 0 || blockHeight >= target {
			return 1.0
		}
		blockPct := float64(blockHeight) / float64(target)
		return 0.5 + blockPct*0.5
	default:
		return 0.0
	}
}

// GetSyncStatus returns detailed sync state for the sync overlay modal.
// Mirrors the information shown by Bitcoin Core's modal sync dialog.
func (a *App) GetSyncStatus() (map[string]interface{}, error) {
	if a.node == nil {
		return nil, fmt.Errorf("node not initialized")
	}

	bc := a.node.Chain()
	p2p := a.node.P2PMgr()

	_, blockHeight := bc.Tip()
	bestPeerHeight := p2p.BestPeerHeight()
	headerHeight := p2p.HeaderSyncHeight()
	syncState := p2p.GetSyncState()
	peers := p2p.PeerCount()
	progress := a.computeSyncProgress()

	var lastBlockTime int64
	if tipHeader, err := bc.TipHeader(); err == nil {
		lastBlockTime = int64(tipHeader.Timestamp)
	}

	var stageLabel string
	switch syncState {
	case "INITIAL":
		if peers == 0 {
			stageLabel = "Connecting to network..."
		} else {
			stageLabel = "Preparing sync..."
		}
	case "HEADER_SYNC":
		if bestPeerHeight > 0 {
			stageLabel = fmt.Sprintf("Downloading headers... (%d / %d)", headerHeight, bestPeerHeight)
		} else {
			stageLabel = fmt.Sprintf("Downloading headers... (%d)", headerHeight)
		}
	case "BLOCK_SYNC":
		target := headerHeight
		if target == 0 {
			target = bestPeerHeight
		}
		remaining := uint32(0)
		if target > blockHeight {
			remaining = target - blockHeight
		}
		stageLabel = fmt.Sprintf("Downloading blocks... (%d / %d, %d remaining)", blockHeight, target, remaining)
	case "SYNCED":
		stageLabel = "Synchronized"
	default:
		stageLabel = "Unknown"
	}

	return map[string]interface{}{
		"syncState":      syncState,
		"stageLabel":     stageLabel,
		"headerHeight":   headerHeight,
		"blockHeight":    blockHeight,
		"bestPeerHeight": bestPeerHeight,
		"peers":          peers,
		"progress":       progress,
		"lastBlockTime":  lastBlockTime,
	}, nil
}

// GetUpdateStatus returns whether a newer version has been observed on the
// network. The frontend uses this to display an update banner.
// It also reports when the wallet's wire protocol is outdated relative to
// peers, which means the wallet is incompatible with the current network.
func (a *App) GetUpdateStatus() map[string]interface{} {
	if a.node == nil {
		return map[string]interface{}{
			"available":        false,
			"protocolOutdated": false,
			"releasesURL":      version.ReleasesURL,
		}
	}
	p2p := a.node.P2PMgr()
	available := p2p.IsUpdateAvailable()
	result := map[string]interface{}{
		"available":        available,
		"protocolOutdated": p2p.IsProtocolOutdated(),
		"localVersion":     version.String(),
		"releasesURL":      version.ReleasesURL,
	}
	if pv := p2p.HighestPeerVersion(); pv != nil {
		result["networkVersion"] = pv.String()
	}
	return result
}

// GetDebugInfo returns comprehensive node information for the debug window.
func (a *App) GetDebugInfo() (map[string]interface{}, error) {
	if a.node == nil {
		return nil, fmt.Errorf("node not initialized")
	}
	bc := a.node.Chain()
	p2p := a.node.P2PMgr()
	mp := a.node.Mempool()
	cfg := a.node.Config()
	tipHash, tipHeight := bc.Tip()
	inbound, outbound := p2p.ConnectionCounts()

	var lastBlockTime string
	if h, err := bc.TipHeader(); err == nil {
		lastBlockTime = time.Unix(int64(h.Timestamp), 0).Format("Mon Jan 02 15:04:05 2006")
	}

	return map[string]interface{}{
		"clientVersion": version.String(),
		"userAgent":     version.UserAgent(),
		"datadir":       cfg.NetworkDataDir(),
		"startupTime":   a.startupTime.Format("Mon Jan 02 15:04:05 2006"),
		"network":       cfg.Network,
		"connections":   p2p.PeerCount(),
		"inbound":       inbound,
		"outbound":      outbound,
		"blocks":        tipHeight,
		"bestHash":      tipHash.ReverseString(),
		"lastBlockTime": lastBlockTime,
		"mempoolTx":     mp.Count(),
		"mempoolBytes":  mp.TotalSize(),
	}, nil
}

// GetPeerList returns detailed info about all connected peers.
func (a *App) GetPeerList() ([]map[string]interface{}, error) {
	if a.node == nil {
		return nil, fmt.Errorf("node not initialized")
	}
	infos := a.node.P2PMgr().PeerInfos()
	// Map iteration order is random; stable sort so the UI row order and selection stay aligned.
	sort.Slice(infos, func(i, j int) bool { return infos[i].Addr < infos[j].Addr })
	result := make([]map[string]interface{}, len(infos))
	for i, p := range infos {
		result[i] = map[string]interface{}{
			"addr":      p.Addr,
			"addrLocal": p.AddrLocal,
			"subver":    p.SubVer,
			"version":   p.Version,
			"inbound":   p.Inbound,
			"connTime":  p.ConnTime,
			"lastSend":  p.LastSend,
			"lastRecv":  p.LastRecv,
			"bytesSent": p.BytesSent,
			"bytesRecv": p.BytesRecv,
			"pingTime":  p.PingTime,
			"startingHeight": p.StartingHeight,
			"banScore":  p.BanScore,
		}
	}
	return result, nil
}

// ResolveGeo resolves geolocation for the given IP addresses using public
// APIs. This runs server-side to avoid WebView fetch restrictions. An empty
// ip string resolves the node's own public IP.
func (a *App) ResolveGeo(ips []string) []map[string]interface{} {
	client := &http.Client{Timeout: 6 * time.Second}
	results := make([]map[string]interface{}, 0, len(ips))

	for _, ip := range ips {
		geo := resolveOneGeo(client, ip)
		if geo != nil {
			results = append(results, geo)
		}
	}
	return results
}

func resolveOneGeo(client *http.Client, ip string) map[string]interface{} {
	type provider struct {
		url   string
		parse func(map[string]interface{}) map[string]interface{}
	}

	isSelf := ip == ""
	encoded := ip

	providers := []provider{
		{
			url: func() string {
				if isSelf {
					return "https://ipwho.is/"
				}
				return "https://ipwho.is/" + encoded
			}(),
			parse: func(payload map[string]interface{}) map[string]interface{} {
				if success, _ := payload["success"].(bool); !success {
					return nil
				}
				lat, latOk := toFloat(payload["latitude"])
				lon, lonOk := toFloat(payload["longitude"])
				if !latOk || !lonOk {
					return nil
				}
				resolvedIP, _ := payload["ip"].(string)
				var org string
				if conn, ok := payload["connection"].(map[string]interface{}); ok {
					org, _ = conn["org"].(string)
				}
				return map[string]interface{}{
					"ip":      resolvedIP,
					"lat":     lat,
					"lon":     lon,
					"city":    strOrEmpty(payload["city"]),
					"region":  strOrEmpty(payload["region"]),
					"country": strOrEmpty(payload["country"]),
					"org":     org,
				}
			},
		},
		{
			url: func() string {
				if isSelf {
					return "https://ipapi.co/json/"
				}
				return "https://ipapi.co/" + encoded + "/json/"
			}(),
			parse: func(payload map[string]interface{}) map[string]interface{} {
				if _, hasErr := payload["error"]; hasErr {
					return nil
				}
				lat, latOk := toFloat(payload["latitude"])
				lon, lonOk := toFloat(payload["longitude"])
				if !latOk || !lonOk {
					return nil
				}
				resolvedIP, _ := payload["ip"].(string)
				country := strOrEmpty(payload["country_name"])
				if country == "" {
					country = strOrEmpty(payload["country"])
				}
				return map[string]interface{}{
					"ip":      resolvedIP,
					"lat":     lat,
					"lon":     lon,
					"city":    strOrEmpty(payload["city"]),
					"region":  strOrEmpty(payload["region"]),
					"country": country,
					"org":     strOrEmpty(payload["org"]),
				}
			},
		},
		{
			url: func() string {
				if isSelf {
					return "https://ipinfo.io/json"
				}
				return "https://ipinfo.io/" + encoded + "/json"
			}(),
			parse: func(payload map[string]interface{}) map[string]interface{} {
				loc, _ := payload["loc"].(string)
				parts := strings.SplitN(loc, ",", 2)
				if len(parts) != 2 {
					return nil
				}
				lat, latOk := toFloat(parts[0])
				lon, lonOk := toFloat(parts[1])
				if !latOk || !lonOk {
					return nil
				}
				resolvedIP, _ := payload["ip"].(string)
				return map[string]interface{}{
					"ip":      resolvedIP,
					"lat":     lat,
					"lon":     lon,
					"city":    strOrEmpty(payload["city"]),
					"region":  strOrEmpty(payload["region"]),
					"country": strOrEmpty(payload["country"]),
					"org":     strOrEmpty(payload["org"]),
				}
			},
		},
	}

	var results []map[string]interface{}
	for _, p := range providers {
		resp, err := client.Get(p.url)
		if err != nil {
			continue
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 32*1024))
		resp.Body.Close()
		if err != nil || resp.StatusCode != 200 {
			continue
		}
		var payload map[string]interface{}
		if err := json.Unmarshal(body, &payload); err != nil {
			continue
		}
		if result := p.parse(payload); result != nil {
			results = append(results, result)
		}
	}
	if len(results) == 0 {
		return nil
	}
	if len(results) == 1 {
		return results[0]
	}

	// Multiple providers succeeded — pick the result whose region has
	// the most agreement. IP geolocation providers sometimes disagree at
	// the regional level; majority-vote reduces the chance of a single
	// inaccurate provider placing the dot in the wrong state/country.
	regionCount := make(map[string]int)
	for _, r := range results {
		region := strings.ToLower(strOrEmpty(r["region"]))
		if region != "" {
			regionCount[region]++
		}
	}
	bestRegion := ""
	bestCount := 0
	for region, count := range regionCount {
		if count > bestCount {
			bestCount = count
			bestRegion = region
		}
	}
	if bestRegion != "" && bestCount > 1 {
		for _, r := range results {
			if strings.ToLower(strOrEmpty(r["region"])) == bestRegion {
				return r
			}
		}
	}
	return results[0]
}

func toFloat(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case string:
		var f float64
		if _, err := fmt.Sscanf(n, "%f", &f); err == nil {
			return f, true
		}
	}
	return 0, false
}

func strOrEmpty(v interface{}) string {
	s, _ := v.(string)
	return s
}

// IsPublicRoutableIP checks whether an IP is publicly routable (not private,
// loopback, link-local, or unspecified).
func IsPublicRoutableIP(ip string) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	return !parsed.IsLoopback() && !parsed.IsPrivate() && !parsed.IsLinkLocalUnicast() &&
		!parsed.IsLinkLocalMulticast() && !parsed.IsUnspecified()
}

// ExecuteRPC runs a JSON-RPC command from the debug console.
// Accepts a method name and a JSON-encoded array of params.
func (a *App) ExecuteRPC(method string, paramsJSON string) (map[string]interface{}, error) {
	if a.node == nil {
		return nil, fmt.Errorf("node not initialized")
	}
	rpcSrv := a.node.RPCServer()
	if rpcSrv == nil {
		return nil, fmt.Errorf("RPC server not running")
	}

	var params []json.RawMessage
	if paramsJSON != "" && paramsJSON != "[]" {
		if err := json.Unmarshal([]byte(paramsJSON), &params); err != nil {
			return map[string]interface{}{
				"error": fmt.Sprintf("invalid params: %v", err),
			}, nil
		}
	}

	result, rpcErr := rpcSrv.DispatchRPC(method, params)
	if rpcErr != nil {
		return map[string]interface{}{
			"error": rpcErr.Message,
		}, nil
	}

	jsonBytes, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return map[string]interface{}{
			"error": fmt.Sprintf("marshal result: %v", err),
		}, nil
	}
	return map[string]interface{}{
		"result": string(jsonBytes),
	}, nil
}

// ListRPCMethods returns all available JSON-RPC method names.
func (a *App) ListRPCMethods() ([]string, error) {
	if a.node == nil {
		return nil, fmt.Errorf("node not initialized")
	}
	rpcSrv := a.node.RPCServer()
	if rpcSrv == nil {
		return nil, fmt.Errorf("RPC server not running")
	}
	return rpcSrv.ListMethods(), nil
}

// GetNetworkTotals returns cumulative bytes sent/received across all peers.
func (a *App) GetNetworkTotals() (map[string]interface{}, error) {
	if a.node == nil {
		return nil, fmt.Errorf("node not initialized")
	}
	infos := a.node.P2PMgr().PeerInfos()
	var totalSent, totalRecv int64
	for _, p := range infos {
		totalSent += p.BytesSent
		totalRecv += p.BytesRecv
	}
	return map[string]interface{}{
		"totalBytesSent": totalSent,
		"totalBytesRecv": totalRecv,
		"peers":          len(infos),
	}, nil
}

// RescanBlockchain triggers a UTXO set rebuild from the stored blocks.
func (a *App) RescanBlockchain() (string, error) {
	if a.node == nil {
		return "", fmt.Errorf("node not initialized")
	}
	if err := a.node.Chain().RescanUTXOSet(); err != nil {
		return "", fmt.Errorf("rescan failed: %w", err)
	}
	return "Rescan complete", nil
}

// SetMining toggles the built-in miner at runtime.
func (a *App) SetMining(enabled bool) error {
	if a.node == nil {
		return fmt.Errorf("node not initialized")
	}
	a.node.SetMining(enabled)
	return nil
}

// GetMiningStatus returns the current mining state and hashrate.
func (a *App) GetMiningStatus() map[string]interface{} {
	if a.node == nil {
		return map[string]interface{}{
			"mining":        false,
			"hashrate":      0,
			"hashrateReady": false,
		}
	}
	return map[string]interface{}{
		"mining":        a.node.IsMining(),
		"hashrate":      a.node.GetHashrate(),
		"hashrateReady": a.node.HashrateReady(),
	}
}

// ToggleMining flips mining on/off and returns the new state.
func (a *App) ToggleMining() (map[string]interface{}, error) {
	if a.node == nil {
		return nil, fmt.Errorf("node not initialized")
	}
	newState := !a.node.IsMining()
	a.node.SetMining(newState)
	return map[string]interface{}{
		"mining":   a.node.IsMining(),
		"hashrate": a.node.GetHashrate(),
	}, nil
}

// GetMiningConfig returns the full mining configuration for the Mining tab.
func (a *App) GetMiningConfig() map[string]interface{} {
	maxThreads := miner.MaxWorkers()
	result := map[string]interface{}{
		"mining":        false,
		"hashrate":      uint64(0),
		"hashrateReady": false,
		"threads":       maxThreads,
		"maxThreads":    maxThreads,
		"powerLimit":    100,
	}
	if a.node == nil {
		return result
	}
	result["mining"] = a.node.IsMining()
	result["hashrate"] = a.node.GetHashrate()
	result["hashrateReady"] = a.node.HashrateReady()
	result["threads"] = a.node.MiningWorkers()
	result["powerLimit"] = a.node.MiningPowerLimit()
	if !a.node.IsMining() {
		result["threads"] = maxThreads
	}
	return result
}

// SetMiningConfig updates mining parameters. If threads change, the miner is
// restarted to pick up the new count.
func (a *App) SetMiningConfig(threads int, powerLimit int) error {
	if a.node == nil {
		return fmt.Errorf("node not initialized")
	}
	a.node.SetMiningPowerLimit(powerLimit)

	wasMining := a.node.IsMining()
	currentThreads := a.node.MiningWorkers()

	if threads != currentThreads && wasMining {
		a.node.SetMiningThreads(threads)
		a.node.SetMining(false)
		a.node.SetMining(true)
	} else {
		a.node.SetMiningThreads(threads)
	}
	return nil
}

// StartStratum starts the embedded stratum server.
func (a *App) StartStratum(port int) error {
	if a.node == nil {
		return fmt.Errorf("node not initialized")
	}
	addr := fmt.Sprintf("0.0.0.0:%d", port)
	return a.node.StartStratum(a.ctx, addr)
}

// StopStratum stops the embedded stratum server.
func (a *App) StopStratum() {
	if a.node != nil {
		a.node.StopStratum()
	}
}

// GetStratumStatus returns the stratum server state and worker list.
func (a *App) GetStratumStatus() map[string]interface{} {
	result := map[string]interface{}{
		"running":     false,
		"listenAddr":  "",
		"workers":     []interface{}{},
		"sharesValid": int64(0),
		"sharesStale": int64(0),
		"blocksFound": int64(0),
	}
	if a.node == nil {
		return result
	}
	srv := a.node.Stratum()
	if srv == nil {
		return result
	}
	stats := srv.Stats()
	for k, v := range stats {
		result[k] = v
	}
	workers := srv.Workers()
	workerList := make([]map[string]interface{}, len(workers))
	for i, w := range workers {
		workerList[i] = map[string]interface{}{
			"name":          w.Name,
			"addr":          w.Addr,
			"connectedAt":   w.ConnectedAt,
			"sharesValid":   w.SharesValid,
			"sharesStale":   w.SharesStale,
			"sharesInvalid": w.SharesInvalid,
			"difficulty":    w.Difficulty,
			"lastShareAt":   w.LastShareAt,
			"hashrate":      w.Hashrate,
		}
	}
	result["workerList"] = workerList
	return result
}

// ConnectIRC manually attempts to connect to the configured IRC network.
func (a *App) ConnectIRC() error {
	if a.irc == nil {
		return fmt.Errorf("social chat not initialized")
	}
	connectCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return a.irc.Connect(connectCtx)
}

// GetIRCStatus returns connection metadata used by the social tab.
func (a *App) GetIRCStatus() map[string]interface{} {
	if a.irc == nil {
		return map[string]interface{}{
			"connected": false,
			"error":     "social chat not initialized",
		}
	}
	return a.irc.Status()
}

// GetIRCMessages returns a bounded in-memory history of chat messages.
func (a *App) GetIRCMessages() []map[string]interface{} {
	if a.irc == nil {
		return nil
	}
	return a.irc.Messages()
}

// GetIRCUsers returns the current channel user list.
func (a *App) GetIRCUsers() []string {
	if a.irc == nil {
		return nil
	}
	return a.irc.Users()
}

// SendIRCMessage sends a channel message to the connected IRC server.
func (a *App) SendIRCMessage(message string) error {
	if a.irc == nil {
		return fmt.Errorf("social chat not initialized")
	}
	return a.irc.SendMessage(message)
}

// ChangeIRCNick requests a nickname change on the connected IRC server.
func (a *App) ChangeIRCNick(nick string) error {
	if a.irc == nil {
		return fmt.Errorf("social chat not initialized")
	}
	return a.irc.ChangeNick(nick)
}

// probeLoop runs the P2P reachability probe on startup and then every 60s.
// It waits a short period after startup for peers to connect before the first
// probe, since the probe requires at least one outbound peer.
func (a *App) probeLoop(ctx context.Context) {
	// Wait for peers to connect before first probe.
	select {
	case <-time.After(15 * time.Second):
	case <-ctx.Done():
		return
	}

	a.runProbe(ctx)

	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			a.runProbe(ctx)
		case <-ctx.Done():
			return
		}
	}
}

// runProbe resolves the public IP and asks a connected peer to dial back.
func (a *App) runProbe(ctx context.Context) {
	cfg := a.node.Config()
	_, portStr, _ := net.SplitHostPort(cfg.ListenAddr)

	client := &http.Client{Timeout: 8 * time.Second}
	publicIP := resolvePublicIP(client)

	if publicIP == "" {
		a.probeMu.Lock()
		a.probeOpen = false
		a.probeIP = ""
		a.probePort = portStr
		a.probeError = "could not determine public IP"
		a.probeReady = true
		a.probeMu.Unlock()
		return
	}

	probeAddr := net.JoinHostPort(publicIP, portStr)
	probeCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()

	reachable, err := a.node.P2PMgr().ProbeReachability(probeCtx, probeAddr)

	a.probeMu.Lock()
	a.probeIP = publicIP
	a.probePort = portStr
	a.probeOpen = reachable
	a.probeReady = true
	if err != nil {
		a.probeError = err.Error()
	} else {
		a.probeError = ""
	}
	a.probeMu.Unlock()

	if reachable {
		logging.L.Info("P2P port probe: reachable", "addr", probeAddr)
	} else {
		errMsg := ""
		if err != nil {
			errMsg = err.Error()
		}
		logging.L.Info("P2P port probe: not reachable", "addr", probeAddr, "error", errMsg)
	}
}

// GetNodeConfig returns node configuration and status information for the
// overview page's Node Configuration card.
func (a *App) GetNodeConfig() map[string]interface{} {
	if a.node == nil {
		return map[string]interface{}{
			"listenPort":    0,
			"dataDir":       "",
			"diskUsageMB":   0,
			"maxInbound":    0,
			"maxOutbound":   0,
			"miningEnabled": false,
			"hashrate":      uint64(0),
			"hashrateReady": false,
			"bannedCount":   0,
			"reachable":     false,
			"uptime":        0,
		}
	}

	cfg := a.node.Config()
	p2p := a.node.P2PMgr()

	_, portStr, _ := net.SplitHostPort(cfg.ListenAddr)

	var diskMB float64
	dataDir := cfg.NetworkDataDir()
	filepath.Walk(dataDir, func(_ string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			diskMB += float64(info.Size())
		}
		return nil
	})
	diskMB = diskMB / (1024 * 1024)

	uptime := int64(time.Since(a.startupTime).Seconds())

	a.probeMu.RLock()
	reachable := a.probeOpen
	a.probeMu.RUnlock()

	return map[string]interface{}{
		"listenPort":    portStr,
		"dataDir":       dataDir,
		"diskUsageMB":   int64(diskMB),
		"maxInbound":    cfg.MaxInbound,
		"maxOutbound":   cfg.MaxOutbound,
		"miningEnabled": a.node.IsMining(),
		"hashrate":      a.node.GetHashrate(),
		"hashrateReady": a.node.HashrateReady(),
		"bannedCount":   p2p.BannedCount(),
		"reachable":     reachable,
		"uptime":        uptime,
	}
}

// OpenDataDir opens the node's data directory in the system file manager.
func (a *App) OpenDataDir() error {
	if a.node == nil {
		return fmt.Errorf("node not initialized")
	}
	dir := a.node.Config().NetworkDataDir()
	return openFileManager(dir)
}

// InstallService registers the wallet as an OS-level service that starts
// automatically on boot. Returns a human-readable status message.
func (a *App) InstallService() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("cannot determine executable path: %w", err)
	}
	return installService(exe, coinparams.NameLower)
}

// UninstallService removes the previously installed OS-level service.
func (a *App) UninstallService() (string, error) {
	return uninstallService(coinparams.NameLower)
}

// IsServiceInstalled checks whether the OS-level auto-start service exists.
func (a *App) IsServiceInstalled() bool {
	return isServiceInstalled(coinparams.NameLower)
}

// TestPort checks whether the node's P2P port is reachable from the internet
// by asking a connected peer to dial back via the P2P probe protocol. This
// avoids reliance on external HTTP port-check services. The result also
// updates the cached probe state used by GetNodeConfig.
func (a *App) TestPort() map[string]interface{} {
	if a.node == nil {
		return map[string]interface{}{
			"open":     false,
			"publicIP": "",
			"port":     "",
			"error":    "node not initialized",
		}
	}

	cfg := a.node.Config()
	_, portStr, _ := net.SplitHostPort(cfg.ListenAddr)

	client := &http.Client{Timeout: 8 * time.Second}
	publicIP := resolvePublicIP(client)
	if publicIP == "" {
		return map[string]interface{}{
			"open":     false,
			"publicIP": "",
			"port":     portStr,
			"error":    "could not determine public IP",
		}
	}

	probeAddr := net.JoinHostPort(publicIP, portStr)
	probeCtx, cancel := context.WithTimeout(a.ctx, 12*time.Second)
	defer cancel()

	reachable, err := a.node.P2PMgr().ProbeReachability(probeCtx, probeAddr)

	// Update the cached state so GetNodeConfig reflects the latest result.
	a.probeMu.Lock()
	a.probeOpen = reachable
	a.probeIP = publicIP
	a.probePort = portStr
	a.probeReady = true
	if err != nil {
		a.probeError = err.Error()
	} else {
		a.probeError = ""
	}
	a.probeMu.Unlock()

	errStr := ""
	if err != nil {
		errStr = err.Error()
	}
	return map[string]interface{}{
		"open":     reachable,
		"publicIP": publicIP,
		"port":     portStr,
		"error":    errStr,
	}
}

func resolvePublicIP(client *http.Client) string {
	urls := []string{
		"https://api.ipify.org",
		"https://ifconfig.me/ip",
		"https://icanhazip.com",
	}
	for _, u := range urls {
		resp, err := client.Get(u)
		if err != nil {
			continue
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 256))
		resp.Body.Close()
		if err != nil || resp.StatusCode != 200 {
			continue
		}
		ip := strings.TrimSpace(string(body))
		if net.ParseIP(ip) != nil {
			return ip
		}
	}
	return ""
}

// GetMainnetLaunchInfo returns the mainnet mining start epoch and the current
// network name so the UI can display a countdown.
func (a *App) GetMainnetLaunchInfo() map[string]interface{} {
	return map[string]interface{}{
		"miningStartTime": params.Mainnet.MiningStartTime,
		"network":         networkForBuild(),
	}
}

func ircNickPath(cfg *config.Config) string {
	return filepath.Join(cfg.NetworkDataDir(), "irc_nick")
}

func loadIRCNick(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func saveIRCNick(path, nick string) {
	_ = os.WriteFile(path, []byte(nick+"\n"), 0600)
}
