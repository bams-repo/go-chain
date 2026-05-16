// Copyright (c) 2024-2026 The Fairchain Contributors
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

package wallet

import "testing"

func TestSortListTxResults(t *testing.T) {
	results := []map[string]interface{}{
		{"blockheight": uint32(5), "vout": uint32(1), "txid": "a"},
		{"blockheight": uint32(5), "vout": uint32(0), "txid": "b"},
		{"blockheight": uint32(0), "vout": uint32(2), "txid": "c"},
		{"blockheight": uint32(0), "vout": uint32(0), "txid": "d"},
	}
	sortListTxResults(results)
	// Height 0 first, ascending vout; then higher block height, ascending vout.
	want := []string{"d", "c", "b", "a"}
	for i := range want {
		if results[i]["txid"] != want[i] {
			t.Fatalf("position %d: want txid %q, got %q (full order: %v, %v, %v, %v)",
				i, want[i], results[i]["txid"],
				results[0]["txid"], results[1]["txid"], results[2]["txid"], results[3]["txid"])
		}
	}
}
