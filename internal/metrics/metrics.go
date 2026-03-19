package metrics

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
)

// NodeMetrics holds lightweight atomic counters for node activity.
// Designed for low-overhead inline instrumentation; no external dependencies.
type NodeMetrics struct {
	BlocksAccepted   atomic.Uint64
	BlocksRejected   atomic.Uint64
	BlocksMined      atomic.Uint64
	OrphansReceived  atomic.Uint64
	Reorgs           atomic.Uint64
	ReorgDepthTotal  atomic.Uint64
	PeersConnected   atomic.Int64
	PeersDisconnects atomic.Uint64
	TxsAccepted      atomic.Uint64
	TxsRejected      atomic.Uint64
	TxsExpired       atomic.Uint64
	BytesReceived    atomic.Uint64
	BytesSent        atomic.Uint64
}

// Global is the process-wide metrics instance.
var Global = &NodeMetrics{}

// Snapshot returns a JSON-serializable snapshot of all counters.
func (m *NodeMetrics) Snapshot() map[string]uint64 {
	return map[string]uint64{
		"blocks_accepted":   m.BlocksAccepted.Load(),
		"blocks_rejected":   m.BlocksRejected.Load(),
		"blocks_mined":      m.BlocksMined.Load(),
		"orphans_received":  m.OrphansReceived.Load(),
		"reorgs":            m.Reorgs.Load(),
		"reorg_depth_total": m.ReorgDepthTotal.Load(),
		"peers_connected":   uint64(m.PeersConnected.Load()),
		"peers_disconnects": m.PeersDisconnects.Load(),
		"txs_accepted":      m.TxsAccepted.Load(),
		"txs_rejected":      m.TxsRejected.Load(),
		"txs_expired":       m.TxsExpired.Load(),
		"bytes_received":    m.BytesReceived.Load(),
		"bytes_sent":        m.BytesSent.Load(),
	}
}

// JSON returns the snapshot as a JSON byte slice.
func (m *NodeMetrics) JSON() ([]byte, error) {
	return json.Marshal(m.Snapshot())
}

// promMetric describes a single Prometheus metric line.
type promMetric struct {
	name  string
	help  string
	mtype string // "counter" or "gauge"
	value uint64
}

// Prometheus returns all metrics in Prometheus exposition text format
// (text/plain; version=0.0.4). No external dependencies required.
func (m *NodeMetrics) Prometheus() string {
	const ns = "fairchain"
	metrics := []promMetric{
		{ns + "_blocks_accepted_total", "Total blocks accepted into the chain.", "counter", m.BlocksAccepted.Load()},
		{ns + "_blocks_rejected_total", "Total blocks rejected by validation.", "counter", m.BlocksRejected.Load()},
		{ns + "_blocks_mined_total", "Total blocks mined by this node.", "counter", m.BlocksMined.Load()},
		{ns + "_orphans_received_total", "Total orphan blocks received.", "counter", m.OrphansReceived.Load()},
		{ns + "_reorgs_total", "Total chain reorganizations.", "counter", m.Reorgs.Load()},
		{ns + "_reorg_depth_total", "Cumulative depth of all reorganizations.", "counter", m.ReorgDepthTotal.Load()},
		{ns + "_peers_connected", "Number of currently connected peers.", "gauge", uint64(m.PeersConnected.Load())},
		{ns + "_peers_disconnects_total", "Total peer disconnections.", "counter", m.PeersDisconnects.Load()},
		{ns + "_txs_accepted_total", "Total transactions accepted into the mempool.", "counter", m.TxsAccepted.Load()},
		{ns + "_txs_rejected_total", "Total transactions rejected.", "counter", m.TxsRejected.Load()},
		{ns + "_txs_expired_total", "Total transactions expired from the mempool.", "counter", m.TxsExpired.Load()},
		{ns + "_bytes_received_total", "Total bytes received from peers.", "counter", m.BytesReceived.Load()},
		{ns + "_bytes_sent_total", "Total bytes sent to peers.", "counter", m.BytesSent.Load()},
	}

	var b strings.Builder
	for _, pm := range metrics {
		fmt.Fprintf(&b, "# HELP %s %s\n", pm.name, pm.help)
		fmt.Fprintf(&b, "# TYPE %s %s\n", pm.name, pm.mtype)
		fmt.Fprintf(&b, "%s %d\n", pm.name, pm.value)
	}
	return b.String()
}
