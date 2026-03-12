package store

import (
	"bufio"
	"os"
	"strings"

	"github.com/bams-repo/fairchain/internal/logging"
)

// DumpPeersDat writes all known peers from the peer store to a flat text file.
// One address per line. This is a convenience export for external tooling.
func DumpPeersDat(path string, ps PeerStore) {
	peers, err := ps.GetPeers()
	if err != nil {
		logging.L.Warn("failed to get peers for dump", "error", err)
		return
	}
	if len(peers) == 0 {
		return
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		logging.L.Warn("failed to create peers.dat", "error", err)
		return
	}
	defer f.Close()

	w := bufio.NewWriter(f)
	for _, addr := range peers {
		w.WriteString(addr + "\n")
	}
	w.Flush()

	logging.L.Info("peers.dat written", "count", len(peers), "path", path)
}

// LoadPeersDat reads peers from a flat text file and merges them into the peer store.
// Addresses already in the store are skipped.
func LoadPeersDat(path string, ps PeerStore) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	existing, _ := ps.GetPeers()
	known := make(map[string]bool, len(existing))
	for _, addr := range existing {
		known[addr] = true
	}

	merged := 0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		addr := strings.TrimSpace(scanner.Text())
		if addr == "" || known[addr] {
			continue
		}
		if err := ps.PutPeer(addr); err == nil {
			merged++
			known[addr] = true
		}
	}

	if merged > 0 {
		logging.L.Info("merged peers from peers.dat", "new", merged, "path", path)
	}
}
