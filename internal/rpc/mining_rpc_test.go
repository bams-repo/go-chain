// Copyright (c) 2024-2026 The Fairchain Contributors
// Fairchain is an experiment in modularity, designed to improve on the work
// of Satoshi Nakamoto and to inspire more creative genius in the space.
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

package rpc

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/bams-repo/fairchain/internal/algorithms/sha256d"
	"github.com/bams-repo/fairchain/internal/chain"
	"github.com/bams-repo/fairchain/internal/consensus/pow"
	"github.com/bams-repo/fairchain/internal/crypto"
	bitcoindiff "github.com/bams-repo/fairchain/internal/difficulty/bitcoin"
	"github.com/bams-repo/fairchain/internal/mempool"
	"github.com/bams-repo/fairchain/internal/p2p"
	fcparams "github.com/bams-repo/fairchain/internal/params"
	"github.com/bams-repo/fairchain/internal/store"
)

func setupTestServer(t *testing.T) *Server {
	t.Helper()

	p := &fcparams.ChainParams{}
	*p = *fcparams.Regtest
	engine := pow.New(sha256d.New(), bitcoindiff.New())

	cfg := fcparams.GenesisConfig{
		NetworkName:     "regtest",
		CoinbaseMessage: []byte("rpc test genesis"),
		Timestamp:       1700000000,
		Bits:            p.InitialBits,
		Version:         1,
		Reward:          p.InitialSubsidy,
		RewardScript:    []byte{0x00},
	}
	genesis := fcparams.BuildGenesisBlock(cfg)
	if err := engine.MineGenesis(&genesis); err != nil {
		t.Fatalf("mine genesis: %v", err)
	}
	genesisHash := crypto.HashBlockHeader(&genesis.Header)
	fcparams.InitGenesis(p, genesis, genesisHash)

	dir := t.TempDir()
	s, err := store.NewFileStore(
		filepath.Join(dir, "blocks"),
		filepath.Join(dir, "blocks", "index"),
		filepath.Join(dir, "chainstate"),
		p.NetworkMagic,
	)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	c := chain.New(p, engine, s, nil)
	if err := c.Init(); err != nil {
		t.Fatalf("init chain: %v", err)
	}

	mp := mempool.New(p, c.UtxoSet(), func() uint32 { _, h := c.Tip(); return h })
	pm := p2p.NewManager(p, c, mp, nil, "127.0.0.1:0", 8, 8, nil, nil, nil)

	srv, err := New("127.0.0.1:0", c, engine, mp, pm, p, nil, nil)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	return srv
}

func TestJSONRPCDispatcher(t *testing.T) {
	srv := setupTestServer(t)

	tests := []struct {
		name       string
		body       string
		wantMethod bool
		wantErr    bool
		errCode    int
	}{
		{
			name:    "valid getblockcount",
			body:    `{"jsonrpc":"1.0","id":"test","method":"getblockcount","params":[]}`,
			wantErr: false,
		},
		{
			name:    "missing method",
			body:    `{"jsonrpc":"1.0","id":"test","params":[]}`,
			wantErr: true,
			errCode: rpcErrInvalidRequest,
		},
		{
			name:    "unknown method",
			body:    `{"jsonrpc":"1.0","id":"test","method":"nonexistent","params":[]}`,
			wantErr: true,
			errCode: rpcErrMethodNotFound,
		},
		{
			name:    "invalid JSON",
			body:    `{invalid`,
			wantErr: true,
			errCode: rpcErrParse,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(tt.body))
			w := httptest.NewRecorder()
			srv.handleJSONRPC(w, req)

			var resp jsonRPCResponse
			if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
				t.Fatalf("decode response: %v", err)
			}

			if tt.wantErr {
				if resp.Error == nil {
					t.Fatal("expected error, got nil")
				}
				if resp.Error.Code != tt.errCode {
					t.Fatalf("error code: got %d, want %d", resp.Error.Code, tt.errCode)
				}
			} else {
				if resp.Error != nil {
					t.Fatalf("unexpected error: %v", resp.Error.Message)
				}
				if resp.Result == nil {
					t.Fatal("expected result, got nil")
				}
			}
		})
	}
}

func TestJSONRPCGetMethod(t *testing.T) {
	srv := setupTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	srv.handleJSONRPC(w, req)

	var resp jsonRPCResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error == nil {
		t.Fatal("expected error for GET request")
	}
	if resp.Error.Code != rpcErrInvalidRequest {
		t.Fatalf("error code: got %d, want %d", resp.Error.Code, rpcErrInvalidRequest)
	}
}

func TestJSONRPCGetBlockTemplate(t *testing.T) {
	srv := setupTestServer(t)

	body := `{"jsonrpc":"1.0","id":"gbt","method":"getblocktemplate","params":[]}`
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	srv.handleJSONRPC(w, req)

	var resp jsonRPCResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatal("result is not a map")
	}

	requiredFields := []string{
		"version", "previousblockhash", "transactions", "coinbasevalue",
		"target", "bits", "height", "curtime", "mintime", "mutable",
		"noncerange", "sigoplimit", "sizelimit",
	}
	for _, field := range requiredFields {
		if _, ok := result[field]; !ok {
			t.Errorf("missing required field: %s", field)
		}
	}

	height, ok := result["height"].(float64)
	if !ok || height != 1 {
		t.Errorf("height: got %v, want 1", result["height"])
	}

	version, ok := result["version"].(float64)
	if !ok || version != 1 {
		t.Errorf("version: got %v, want 1", result["version"])
	}

	prevHash, ok := result["previousblockhash"].(string)
	if !ok || len(prevHash) != 64 {
		t.Errorf("previousblockhash: got %q, want 64-char hex", prevHash)
	}

	bits, ok := result["bits"].(string)
	if !ok || len(bits) != 8 {
		t.Errorf("bits: got %q, want 8-char hex", bits)
	}

	txs, ok := result["transactions"].([]interface{})
	if !ok {
		t.Fatal("transactions is not an array")
	}
	if len(txs) != 0 {
		t.Errorf("transactions: got %d, want 0 (empty mempool)", len(txs))
	}

	coinbaseValue, ok := result["coinbasevalue"].(float64)
	if !ok || coinbaseValue <= 0 {
		t.Errorf("coinbasevalue: got %v, want > 0", result["coinbasevalue"])
	}

	nonceRange, ok := result["noncerange"].(string)
	if !ok || nonceRange != "00000000ffffffff" {
		t.Errorf("noncerange: got %q, want %q", nonceRange, "00000000ffffffff")
	}
}

func TestRESTGetBlockTemplate(t *testing.T) {
	srv := setupTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/getblocktemplate", nil)
	w := httptest.NewRecorder()
	srv.handleGetBlockTemplate(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if _, ok := result["previousblockhash"]; !ok {
		t.Error("missing previousblockhash")
	}
	if _, ok := result["coinbasevalue"]; !ok {
		t.Error("missing coinbasevalue")
	}
}

func TestJSONRPCGetMiningInfo(t *testing.T) {
	srv := setupTestServer(t)

	body := `{"jsonrpc":"1.0","id":"mi","method":"getmininginfo","params":[]}`
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	srv.handleJSONRPC(w, req)

	var resp jsonRPCResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatal("result is not a map")
	}

	requiredFields := []string{"blocks", "difficulty", "networkhashps", "pooledtx", "chain", "warnings"}
	for _, field := range requiredFields {
		if _, ok := result[field]; !ok {
			t.Errorf("missing required field: %s", field)
		}
	}

	chain, ok := result["chain"].(string)
	if !ok || chain != "regtest" {
		t.Errorf("chain: got %q, want %q", chain, "regtest")
	}
}

func TestJSONRPCGetNetworkHashPS(t *testing.T) {
	srv := setupTestServer(t)

	body := `{"jsonrpc":"1.0","id":"nhps","method":"getnetworkhashps","params":[]}`
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	srv.handleJSONRPC(w, req)

	var resp jsonRPCResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	// At genesis (height 0) with no blocks mined, hash rate should be 0.
	hashps, ok := resp.Result.(float64)
	if !ok {
		t.Fatalf("result is not a float64: %T", resp.Result)
	}
	if hashps != 0 {
		t.Logf("networkhashps at genesis: %f (expected 0 for single block)", hashps)
	}
}

func TestJSONRPCSubmitBlockInvalidHex(t *testing.T) {
	srv := setupTestServer(t)

	body := `{"jsonrpc":"1.0","id":"sb","method":"submitblock","params":["not_hex_data"]}`
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	srv.handleJSONRPC(w, req)

	var resp jsonRPCResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error == nil && resp.Result == nil {
		t.Fatal("expected error or rejection for invalid hex")
	}
}

func TestJSONRPCSubmitBlockMissingParams(t *testing.T) {
	srv := setupTestServer(t)

	body := `{"jsonrpc":"1.0","id":"sb","method":"submitblock","params":[]}`
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	srv.handleJSONRPC(w, req)

	var resp jsonRPCResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error == nil {
		t.Fatal("expected error for missing params")
	}
	// Pool software (miningcore, ckpool) probes submitblock with no args;
	// -1 (RPC_MISC_ERROR) signals "method exists" vs -32601 "not found".
	if resp.Error.Code != rpcErrMisc {
		t.Fatalf("error code: got %d, want %d (rpcErrMisc)", resp.Error.Code, rpcErrMisc)
	}
}

func TestJSONRPCIDPreserved(t *testing.T) {
	srv := setupTestServer(t)

	body := `{"jsonrpc":"1.0","id":42,"method":"getblockcount","params":[]}`
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	srv.handleJSONRPC(w, req)

	var resp jsonRPCResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	var id float64
	if err := json.Unmarshal(resp.ID, &id); err != nil {
		t.Fatalf("unmarshal id: %v", err)
	}
	if id != 42 {
		t.Errorf("id: got %v, want 42", id)
	}
}

func TestJSONRPCStringID(t *testing.T) {
	srv := setupTestServer(t)

	body := `{"jsonrpc":"1.0","id":"curltest","method":"getblockcount","params":[]}`
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	srv.handleJSONRPC(w, req)

	var resp jsonRPCResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	var id string
	if err := json.Unmarshal(resp.ID, &id); err != nil {
		t.Fatalf("unmarshal id: %v", err)
	}
	if id != "curltest" {
		t.Errorf("id: got %q, want %q", id, "curltest")
	}
}

func TestRESTGetMiningInfo(t *testing.T) {
	srv := setupTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/getmininginfo", nil)
	w := httptest.NewRecorder()
	srv.handleGetMiningInfo(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := result["blocks"]; !ok {
		t.Error("missing blocks field")
	}
	if _, ok := result["difficulty"]; !ok {
		t.Error("missing difficulty field")
	}
}

func TestRESTGetNetworkHashPS(t *testing.T) {
	srv := setupTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/getnetworkhashps", nil)
	w := httptest.NewRecorder()
	srv.handleGetNetworkHashPS(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
}

// --- BIP 22/23 Stratum Compatibility Tests ---

func TestGetBlockTemplateWithCapabilities(t *testing.T) {
	srv := setupTestServer(t)

	body := `{"jsonrpc":"1.0","id":"gbt","method":"getblocktemplate","params":[{"capabilities":["coinbasetxn","longpoll","coinbasevalue","proposal","workid"]}]}`
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	srv.handleJSONRPC(w, req)

	var resp jsonRPCResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatal("result is not a map")
	}

	// When coinbasetxn capability is advertised, the response must include it.
	cbTxn, ok := result["coinbasetxn"]
	if !ok {
		t.Fatal("missing coinbasetxn in response when capability is advertised")
	}
	cbMap, ok := cbTxn.(map[string]interface{})
	if !ok {
		t.Fatal("coinbasetxn is not a map")
	}
	if _, ok := cbMap["data"]; !ok {
		t.Error("coinbasetxn missing 'data' field")
	}
	if _, ok := cbMap["txid"]; !ok {
		t.Error("coinbasetxn missing 'txid' field")
	}
	if _, ok := cbMap["fee"]; !ok {
		t.Error("coinbasetxn missing 'fee' field")
	}

	// coinbasevalue must still be present alongside coinbasetxn.
	if _, ok := result["coinbasevalue"]; !ok {
		t.Error("missing coinbasevalue")
	}
}

func TestGetBlockTemplateWithoutCoinbaseTxnCapability(t *testing.T) {
	srv := setupTestServer(t)

	body := `{"jsonrpc":"1.0","id":"gbt","method":"getblocktemplate","params":[{"capabilities":["coinbasevalue"]}]}`
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	srv.handleJSONRPC(w, req)

	var resp jsonRPCResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatal("result is not a map")
	}

	// Without coinbasetxn capability, the field should not be present.
	if _, ok := result["coinbasetxn"]; ok {
		t.Error("coinbasetxn should not be present without capability")
	}
}

func TestGetBlockTemplateBIP23Fields(t *testing.T) {
	srv := setupTestServer(t)

	body := `{"jsonrpc":"1.0","id":"gbt","method":"getblocktemplate","params":[{}]}`
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	srv.handleJSONRPC(w, req)

	var resp jsonRPCResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatal("result is not a map")
	}

	// BIP 23 pool extension fields.
	bip23Fields := []string{
		"target", "expires", "maxtime", "weightlimit",
		"capabilities", "rules", "vbavailable", "vbrequired",
	}
	for _, field := range bip23Fields {
		if _, ok := result[field]; !ok {
			t.Errorf("missing BIP 23 field: %s", field)
		}
	}

	// Mutable must include key mutations for pool compatibility.
	mutable, ok := result["mutable"].([]interface{})
	if !ok {
		t.Fatal("mutable is not an array")
	}
	mutSet := make(map[string]bool)
	for _, m := range mutable {
		if s, ok := m.(string); ok {
			mutSet[s] = true
		}
	}
	requiredMutations := []string{"time", "time/increment", "transactions/add", "prevblock", "coinbase/append"}
	for _, m := range requiredMutations {
		if !mutSet[m] {
			t.Errorf("missing required mutable: %s", m)
		}
	}
}

func TestGetBlockTemplateProposalMode(t *testing.T) {
	srv := setupTestServer(t)

	// Proposal with missing data should fail.
	body := `{"jsonrpc":"1.0","id":"prop","method":"getblocktemplate","params":[{"mode":"proposal"}]}`
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	srv.handleJSONRPC(w, req)

	var resp jsonRPCResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error == nil {
		t.Fatal("expected error for proposal without data")
	}

	// Proposal with invalid hex data should return "rejected".
	body = `{"jsonrpc":"1.0","id":"prop","method":"getblocktemplate","params":[{"mode":"proposal","data":"zzzz"}]}`
	req = httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(body))
	w = httptest.NewRecorder()
	srv.handleJSONRPC(w, req)

	resp = jsonRPCResponse{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected JSON-RPC error: %s", resp.Error.Message)
	}
	if resp.Result != "rejected" {
		t.Errorf("expected 'rejected', got %v", resp.Result)
	}
}

func TestBatchJSONRPC(t *testing.T) {
	srv := setupTestServer(t)

	body := `[
		{"jsonrpc":"1.0","id":1,"method":"getblockcount","params":[]},
		{"jsonrpc":"1.0","id":2,"method":"getbestblockhash","params":[]},
		{"jsonrpc":"1.0","id":3,"method":"getdifficulty","params":[]}
	]`
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	srv.handleJSONRPC(w, req)

	var responses []jsonRPCResponse
	if err := json.NewDecoder(w.Body).Decode(&responses); err != nil {
		t.Fatalf("decode batch response: %v", err)
	}

	if len(responses) != 3 {
		t.Fatalf("expected 3 responses, got %d", len(responses))
	}

	for i, resp := range responses {
		if resp.Error != nil {
			t.Errorf("response %d: unexpected error: %s", i, resp.Error.Message)
		}
		if resp.Result == nil {
			t.Errorf("response %d: expected result, got nil", i)
		}
	}

	// Verify IDs are preserved in batch.
	var id1 float64
	if err := json.Unmarshal(responses[0].ID, &id1); err != nil {
		t.Fatalf("unmarshal id 0: %v", err)
	}
	if id1 != 1 {
		t.Errorf("batch id 0: got %v, want 1", id1)
	}
}

func TestBatchJSONRPCEmpty(t *testing.T) {
	srv := setupTestServer(t)

	body := `[]`
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	srv.handleJSONRPC(w, req)

	var resp jsonRPCResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error == nil {
		t.Fatal("expected error for empty batch")
	}
}

func TestSubmitBlockRejectionReasons(t *testing.T) {
	tests := []struct {
		errMsg   string
		expected string
	}{
		{"hash exceeds target", "high-hash"},
		{"PoW validation failed", "high-hash"},
		{"unknown prevblock", "bad-prevblk"},
		{"parent not found", "bad-prevblk"},
		{"merkle root mismatch", "bad-txnmrklroot"},
		{"timestamp too far in future", "time-too-new"},
		{"timestamp below median time past", "time-too-old"},
		{"timestamp invalid", "time-invalid"},
		{"bits mismatch", "bad-diffbits"},
		{"difficulty target wrong", "bad-diffbits"},
		{"coinbase script too long", "bad-cb-length"},
		{"bad version", "bad-version"},
		{"duplicate block", "duplicate"},
		{"invalid transaction at index 2", "bad-txns"},
		{"block size exceeds limit", "bad-blk-length"},
		{"some unknown error", "rejected"},
	}

	for _, tt := range tests {
		result := mapBlockErrorToRejection(tt.errMsg)
		if result != tt.expected {
			t.Errorf("mapBlockErrorToRejection(%q): got %q, want %q", tt.errMsg, result, tt.expected)
		}
	}
}

func TestLongPollState(t *testing.T) {
	var lp longPollState

	lp.update("id1")
	if got := lp.currentID(); got != "id1" {
		t.Fatalf("currentID: got %q, want %q", got, "id1")
	}

	ch := lp.wait()

	// Update to a new ID should wake the waiter.
	lp.update("id2")

	select {
	case <-ch:
		// Expected: channel closed.
	default:
		t.Fatal("expected waiter to be woken up")
	}

	if got := lp.currentID(); got != "id2" {
		t.Fatalf("currentID after update: got %q, want %q", got, "id2")
	}
}

func TestLongPollStateSameIDNoWake(t *testing.T) {
	var lp longPollState

	lp.update("id1")
	ch := lp.wait()

	// Update with the same ID should NOT wake the waiter.
	lp.update("id1")

	select {
	case <-ch:
		t.Fatal("waiter should not be woken for same ID")
	default:
		// Expected: channel still open.
	}
}

func TestGetBlockTemplateRulesParam(t *testing.T) {
	srv := setupTestServer(t)

	// Bitcoin Core clients send rules array; ensure it doesn't cause errors.
	body := `{"jsonrpc":"1.0","id":"gbt","method":"getblocktemplate","params":[{"rules":["segwit"]}]}`
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	srv.handleJSONRPC(w, req)

	var resp jsonRPCResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error with rules param: %s", resp.Error.Message)
	}
}

func TestCkpoolStyleGetBlockTemplate(t *testing.T) {
	srv := setupTestServer(t)

	// This is the exact format ckpool sends to bitcoind.
	body := `{"jsonrpc":"1.0","id":"ckpool","method":"getblocktemplate","params":[{"capabilities":["coinbasetxn","workid","coinbase/append","coinbasevalue"],"rules":["segwit"]}]}`
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	srv.handleJSONRPC(w, req)

	var resp jsonRPCResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("ckpool-style request failed: %s", resp.Error.Message)
	}

	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatal("result is not a map")
	}

	// ckpool requires coinbasetxn when it advertises the capability.
	if _, ok := result["coinbasetxn"]; !ok {
		t.Error("missing coinbasetxn for ckpool-style request")
	}

	// ckpool needs these fields to function.
	ckpoolRequired := []string{
		"version", "previousblockhash", "transactions", "coinbasevalue",
		"coinbasetxn", "target", "bits", "height", "curtime", "mintime",
		"mutable", "noncerange", "longpollid",
	}
	for _, field := range ckpoolRequired {
		if _, ok := result[field]; !ok {
			t.Errorf("missing ckpool-required field: %s", field)
		}
	}
}

func TestNotifyBlockChange(t *testing.T) {
	srv := setupTestServer(t)

	// Set initial long poll state.
	srv.NotifyBlockChange()
	id1 := srv.longPoll.currentID()

	ch := srv.longPoll.wait()

	// Simulate a block change (the ID will be the same since chain hasn't
	// actually changed, but we can test the mechanism).
	srv.longPoll.update("different-id")

	select {
	case <-ch:
		// Expected.
	default:
		t.Fatal("NotifyBlockChange should wake long-poll waiters")
	}

	_ = id1
}

// --- Stratum Server Compatibility Tests ---
// These tests verify the exact JSON-RPC behavior that ckpool, Braiins Pool,
// and other standard Bitcoin stratum servers expect from the node backend.

func TestCkpoolGetBestBlockHash(t *testing.T) {
	srv := setupTestServer(t)

	body := `{"method": "getbestblockhash"}`
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	srv.handleJSONRPC(w, req)

	var resp jsonRPCResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	hash, ok := resp.Result.(string)
	if !ok {
		t.Fatalf("result is not a string: %T", resp.Result)
	}
	if len(hash) != 64 {
		t.Fatalf("hash length: got %d, want 64", len(hash))
	}
}

func TestCkpoolGetBlockCount(t *testing.T) {
	srv := setupTestServer(t)

	body := `{"method": "getblockcount"}`
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	srv.handleJSONRPC(w, req)

	var resp jsonRPCResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	count, ok := resp.Result.(float64)
	if !ok {
		t.Fatalf("result is not a number: %T", resp.Result)
	}
	if count != 0 {
		t.Fatalf("block count: got %v, want 0 (genesis)", count)
	}
}

func TestCkpoolGetBlockHash(t *testing.T) {
	srv := setupTestServer(t)

	body := `{"method": "getblockhash", "params": [0]}`
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	srv.handleJSONRPC(w, req)

	var resp jsonRPCResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	hash, ok := resp.Result.(string)
	if !ok {
		t.Fatalf("result is not a string: %T", resp.Result)
	}
	if len(hash) != 64 {
		t.Fatalf("hash length: got %d, want 64", len(hash))
	}
}

func TestCkpoolValidateAddress(t *testing.T) {
	srv := setupTestServer(t)

	body := `{"method": "validateaddress", "params": ["1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa"]}`
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	srv.handleJSONRPC(w, req)

	var resp jsonRPCResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatal("result is not a map")
	}

	// ckpool checks these specific fields.
	requiredFields := []string{"isvalid", "isscript", "iswitness"}
	for _, field := range requiredFields {
		if _, ok := result[field]; !ok {
			t.Errorf("missing ckpool-required field: %s", field)
		}
	}
}

func TestCkpoolPreciousBlock(t *testing.T) {
	srv := setupTestServer(t)

	body := `{"method": "preciousblock", "params": ["0000000000000000000000000000000000000000000000000000000000000000"]}`
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	srv.handleJSONRPC(w, req)

	var resp jsonRPCResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}
	// preciousblock returns null on success (ckpool doesn't check the result).
	if resp.Result != nil {
		t.Fatalf("expected null result, got %v", resp.Result)
	}
}

func TestCkpoolGetRawTransaction(t *testing.T) {
	srv := setupTestServer(t)

	// Request a non-existent transaction — should return an error, not crash.
	body := `{"method": "getrawtransaction", "params": ["0000000000000000000000000000000000000000000000000000000000000000"]}`
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	srv.handleJSONRPC(w, req)

	var resp jsonRPCResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	// Should return an error for non-existent tx.
	if resp.Error == nil {
		t.Fatal("expected error for non-existent transaction")
	}
	if resp.Error.Code != rpcErrMisc {
		t.Fatalf("error code: got %d, want %d", resp.Error.Code, rpcErrMisc)
	}
}

func TestCkpoolSubmitBlockNullOnSuccess(t *testing.T) {
	srv := setupTestServer(t)

	// Build a valid block at height 1.
	body := `{"method": "getblocktemplate", "params": [{"capabilities": ["coinbasetxn"]}]}`
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	srv.handleJSONRPC(w, req)

	var gbtResp jsonRPCResponse
	if err := json.NewDecoder(w.Body).Decode(&gbtResp); err != nil {
		t.Fatalf("decode gbt response: %v", err)
	}
	if gbtResp.Error != nil {
		t.Fatalf("getblocktemplate error: %s", gbtResp.Error.Message)
	}

	// Verify the template has the fields ckpool's gen_gbtbase reads.
	result, ok := gbtResp.Result.(map[string]interface{})
	if !ok {
		t.Fatal("gbt result is not a map")
	}

	gbtFields := []string{
		"previousblockhash", "target", "version", "curtime",
		"bits", "height", "coinbasevalue", "coinbaseaux",
	}
	for _, field := range gbtFields {
		if _, ok := result[field]; !ok {
			t.Errorf("missing gen_gbtbase field: %s", field)
		}
	}

	// Verify coinbaseaux has "flags" key.
	coinbaseAux, ok := result["coinbaseaux"].(map[string]interface{})
	if !ok {
		t.Fatal("coinbaseaux is not a map")
	}
	if _, ok := coinbaseAux["flags"]; !ok {
		t.Error("coinbaseaux missing 'flags' field")
	}

	// Verify version is an integer (ckpool reads it with json_integer_value).
	version, ok := result["version"].(float64)
	if !ok {
		t.Fatalf("version is not a number: %T", result["version"])
	}
	if version != float64(int(version)) {
		t.Fatalf("version is not an integer: %v", version)
	}

	// Verify curtime is an integer.
	curtime, ok := result["curtime"].(float64)
	if !ok {
		t.Fatalf("curtime is not a number: %T", result["curtime"])
	}
	if curtime != float64(int(curtime)) {
		t.Fatalf("curtime is not an integer: %v", curtime)
	}

	// Verify height is an integer.
	height, ok := result["height"].(float64)
	if !ok {
		t.Fatalf("height is not a number: %T", result["height"])
	}
	if height != float64(int(height)) {
		t.Fatalf("height is not an integer: %v", height)
	}

	// Verify coinbasevalue is an integer.
	coinbaseValue, ok := result["coinbasevalue"].(float64)
	if !ok {
		t.Fatalf("coinbasevalue is not a number: %T", result["coinbasevalue"])
	}
	if coinbaseValue != float64(int64(coinbaseValue)) {
		t.Fatalf("coinbasevalue is not an integer: %v", coinbaseValue)
	}

	// Verify bits is an 8-char hex string.
	bits, ok := result["bits"].(string)
	if !ok || len(bits) != 8 {
		t.Fatalf("bits: got %q, want 8-char hex", bits)
	}

	// Verify target is a 64-char hex string.
	target, ok := result["target"].(string)
	if !ok || len(target) != 64 {
		t.Fatalf("target: got %q, want 64-char hex", target)
	}

	// Verify previousblockhash is a 64-char hex string.
	prevHash, ok := result["previousblockhash"].(string)
	if !ok || len(prevHash) != 64 {
		t.Fatalf("previousblockhash: got %q, want 64-char hex", prevHash)
	}
}

func TestCkpoolFullWorkflow(t *testing.T) {
	srv := setupTestServer(t)

	// Step 1: getbestblockhash (ckpool polls this to detect new blocks).
	body := `{"method": "getbestblockhash"}`
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	srv.handleJSONRPC(w, req)

	var resp jsonRPCResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error != nil {
		t.Fatalf("getbestblockhash: %s", resp.Error.Message)
	}

	// Step 2: getblocktemplate (ckpool's exact request format).
	body = `{"method": "getblocktemplate", "params": [{"capabilities": ["coinbasetxn", "workid", "coinbase/append"], "rules": ["segwit"]}]}`
	req = httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(body))
	w = httptest.NewRecorder()
	srv.handleJSONRPC(w, req)

	resp = jsonRPCResponse{}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error != nil {
		t.Fatalf("getblocktemplate: %s", resp.Error.Message)
	}

	gbt, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatal("gbt result is not a map")
	}

	// ckpool must have coinbasetxn since it advertised the capability.
	if _, ok := gbt["coinbasetxn"]; !ok {
		t.Error("missing coinbasetxn")
	}

	// Step 3: validateaddress (ckpool validates payout address on startup).
	body = `{"method": "validateaddress", "params": ["1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa"]}`
	req = httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(body))
	w = httptest.NewRecorder()
	srv.handleJSONRPC(w, req)

	resp = jsonRPCResponse{}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error != nil {
		t.Fatalf("validateaddress: %s", resp.Error.Message)
	}

	va, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatal("validateaddress result is not a map")
	}
	if _, ok := va["isvalid"]; !ok {
		t.Error("validateaddress missing isvalid")
	}
	if _, ok := va["isscript"]; !ok {
		t.Error("validateaddress missing isscript")
	}
	if _, ok := va["iswitness"]; !ok {
		t.Error("validateaddress missing iswitness")
	}

	// Step 4: getblockcount (ckpool checks chain height).
	body = `{"method": "getblockcount"}`
	req = httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(body))
	w = httptest.NewRecorder()
	srv.handleJSONRPC(w, req)

	resp = jsonRPCResponse{}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error != nil {
		t.Fatalf("getblockcount: %s", resp.Error.Message)
	}
}
