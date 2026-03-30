// Copyright (c) 2024-2026 The Fairchain Contributors
// Fairchain is an experiment in modularity, designed to improve on the work
// of Satoshi Nakamoto and to inspire more creative genius in the space.
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

package discovery

import (
	"log"

	"github.com/bams-repo/fairchain/internal/logging"
	"github.com/bams-repo/fairchain/internal/store"
)

// Discovery manages peer address discovery from multiple sources.
type Discovery struct {
	peerStore store.PeerStore
	seeds     []string
}

// New creates a new Discovery instance.
func New(ps store.PeerStore, seeds []string) *Discovery {
	return &Discovery{
		peerStore: ps,
		seeds:     seeds,
	}
}

// Bootstrap returns the initial set of peer addresses to connect to.
// Combines static seeds with persisted peers.
func (d *Discovery) Bootstrap() []string {
	var addrs []string
	addrs = append(addrs, d.seeds...)

	stored, err := d.peerStore.GetPeers()
	if err != nil {
		log.Printf("[discovery] failed to load stored peers: %v", err)
	} else {
		addrs = append(addrs, stored...)
	}

	out := deduplicate(addrs)
	logging.P2PSyncDebug("discovery.Bootstrap",
		"seed_count", len(d.seeds),
		"total_addrs", len(out))
	return out
}

// AddPeer persists a newly discovered peer address.
func (d *Discovery) AddPeer(addr string) {
	logging.P2PSyncDebug("discovery.AddPeer", "addr", addr)
	d.peerStore.PutPeer(addr)
}

// RemovePeer removes a peer address from the persistent store.
func (d *Discovery) RemovePeer(addr string) {
	logging.P2PSyncDebug("discovery.RemovePeer", "addr", addr)
	d.peerStore.RemovePeer(addr)
}

func deduplicate(addrs []string) []string {
	seen := make(map[string]struct{}, len(addrs))
	result := make([]string, 0, len(addrs))
	for _, a := range addrs {
		if _, ok := seen[a]; !ok {
			seen[a] = struct{}{}
			result = append(result, a)
		}
	}
	return result
}
