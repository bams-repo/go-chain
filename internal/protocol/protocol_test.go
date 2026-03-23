// Copyright (c) 2024-2026 The Fairchain Contributors
// Fairchain is an experiment in modularity, designed to improve on the work
// of Satoshi Nakamoto and to inspire more creative genius in the space.
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

package protocol

import (
	"bytes"
	"testing"

	"github.com/bams-repo/fairchain/internal/types"
	"github.com/bams-repo/fairchain/internal/version"
)

func TestVersionMsgRoundtrip(t *testing.T) {
	msg := VersionMsg{
		Version:     1,
		Services:    1,
		Timestamp:   1700000000,
		AddrRecv:    "127.0.0.1:19333",
		AddrFrom:    "127.0.0.1:19334",
		Nonce:       12345,
		UserAgent:   version.UserAgent(),
		StartHeight: 100,
	}

	var buf bytes.Buffer
	if err := msg.Encode(&buf); err != nil {
		t.Fatalf("Encode: %v", err)
	}

	var msg2 VersionMsg
	if err := msg2.Decode(&buf); err != nil {
		t.Fatalf("Decode: %v", err)
	}

	if msg.Version != msg2.Version || msg.Services != msg2.Services ||
		msg.Timestamp != msg2.Timestamp || msg.AddrRecv != msg2.AddrRecv ||
		msg.AddrFrom != msg2.AddrFrom || msg.Nonce != msg2.Nonce ||
		msg.UserAgent != msg2.UserAgent || msg.StartHeight != msg2.StartHeight {
		t.Fatal("VersionMsg roundtrip mismatch")
	}
}

func TestInvMsgRoundtrip(t *testing.T) {
	msg := InvMsg{
		Inventory: []InvVector{
			{Type: InvTypeBlock, Hash: types.Hash{0x01}},
			{Type: InvTypeTx, Hash: types.Hash{0x02}},
		},
	}

	var buf bytes.Buffer
	if err := msg.Encode(&buf); err != nil {
		t.Fatalf("Encode: %v", err)
	}

	var msg2 InvMsg
	if err := msg2.Decode(&buf); err != nil {
		t.Fatalf("Decode: %v", err)
	}

	if len(msg2.Inventory) != 2 {
		t.Fatalf("expected 2 inv vectors, got %d", len(msg2.Inventory))
	}
	if msg2.Inventory[0].Type != InvTypeBlock || msg2.Inventory[0].Hash != msg.Inventory[0].Hash {
		t.Fatal("first inv vector mismatch")
	}
	if msg2.Inventory[1].Type != InvTypeTx || msg2.Inventory[1].Hash != msg.Inventory[1].Hash {
		t.Fatal("second inv vector mismatch")
	}
}

func TestGetBlocksMsgRoundtrip(t *testing.T) {
	msg := GetBlocksMsg{
		Version:            1,
		BlockLocatorHashes: []types.Hash{{0xAA}, {0xBB}},
		HashStop:           types.Hash{0xCC},
	}

	var buf bytes.Buffer
	if err := msg.Encode(&buf); err != nil {
		t.Fatalf("Encode: %v", err)
	}

	var msg2 GetBlocksMsg
	if err := msg2.Decode(&buf); err != nil {
		t.Fatalf("Decode: %v", err)
	}

	if msg2.Version != 1 || len(msg2.BlockLocatorHashes) != 2 || msg2.HashStop != msg.HashStop {
		t.Fatal("GetBlocksMsg roundtrip mismatch")
	}
}

func TestAddrMsgRoundtrip(t *testing.T) {
	msg := AddrMsg{Addresses: []string{"127.0.0.1:19333", "192.168.1.1:19334"}}

	var buf bytes.Buffer
	if err := msg.Encode(&buf); err != nil {
		t.Fatalf("Encode: %v", err)
	}

	var msg2 AddrMsg
	if err := msg2.Decode(&buf); err != nil {
		t.Fatalf("Decode: %v", err)
	}

	if len(msg2.Addresses) != 2 || msg2.Addresses[0] != "127.0.0.1:19333" {
		t.Fatal("AddrMsg roundtrip mismatch")
	}
}

func TestPingPongRoundtrip(t *testing.T) {
	ping := PingMsg{Nonce: 99999}
	var buf bytes.Buffer
	if err := ping.Encode(&buf); err != nil {
		t.Fatalf("Encode ping: %v", err)
	}

	var ping2 PingMsg
	if err := ping2.Decode(&buf); err != nil {
		t.Fatalf("Decode ping: %v", err)
	}
	if ping2.Nonce != 99999 {
		t.Fatal("ping nonce mismatch")
	}

	pong := PongMsg{Nonce: 99999}
	buf.Reset()
	pong.Encode(&buf)
	var pong2 PongMsg
	pong2.Decode(&buf)
	if pong2.Nonce != 99999 {
		t.Fatal("pong nonce mismatch")
	}
}

func TestMessageHeaderEncodeDecode(t *testing.T) {
	magic := [4]byte{0xFA, 0x1C, 0xC0, 0xFF}
	payload := []byte("test payload data")

	var buf bytes.Buffer
	if err := EncodeMessageHeader(&buf, magic, CmdVersion, payload); err != nil {
		t.Fatalf("EncodeMessageHeader: %v", err)
	}

	hdr, err := DecodeMessageHeader(&buf)
	if err != nil {
		t.Fatalf("DecodeMessageHeader: %v", err)
	}

	if hdr.Magic != magic {
		t.Fatal("magic mismatch")
	}
	if hdr.CommandString() != CmdVersion {
		t.Fatalf("command = %q, want %q", hdr.CommandString(), CmdVersion)
	}
	if hdr.Length != uint32(len(payload)) {
		t.Fatalf("length = %d, want %d", hdr.Length, len(payload))
	}
}

func TestGetHeadersMsgRoundtrip(t *testing.T) {
	msg := GetHeadersMsg{
		Version:            2,
		BlockLocatorHashes: []types.Hash{{0xAA}, {0xBB}},
		HashStop:           types.Hash{0xCC},
	}

	var buf bytes.Buffer
	if err := msg.Encode(&buf); err != nil {
		t.Fatalf("Encode: %v", err)
	}

	var msg2 GetHeadersMsg
	if err := msg2.Decode(&buf); err != nil {
		t.Fatalf("Decode: %v", err)
	}

	if msg2.Version != 2 || len(msg2.BlockLocatorHashes) != 2 || msg2.HashStop != msg.HashStop {
		t.Fatal("GetHeadersMsg roundtrip mismatch")
	}
	if msg2.BlockLocatorHashes[0] != msg.BlockLocatorHashes[0] ||
		msg2.BlockLocatorHashes[1] != msg.BlockLocatorHashes[1] {
		t.Fatal("GetHeadersMsg locator hash mismatch")
	}
}

func TestGetHeadersMsgWireCompatibility(t *testing.T) {
	msg := GetBlocksMsg{
		Version:            2,
		BlockLocatorHashes: []types.Hash{{0x01}, {0x02}},
		HashStop:           types.Hash{0xFF},
	}

	var bufBlocks bytes.Buffer
	if err := msg.Encode(&bufBlocks); err != nil {
		t.Fatalf("Encode GetBlocksMsg: %v", err)
	}

	hdrMsg := GetHeadersMsg{
		Version:            2,
		BlockLocatorHashes: []types.Hash{{0x01}, {0x02}},
		HashStop:           types.Hash{0xFF},
	}

	var bufHeaders bytes.Buffer
	if err := hdrMsg.Encode(&bufHeaders); err != nil {
		t.Fatalf("Encode GetHeadersMsg: %v", err)
	}

	if !bytes.Equal(bufBlocks.Bytes(), bufHeaders.Bytes()) {
		t.Fatal("GetHeadersMsg wire format differs from GetBlocksMsg")
	}
}

func TestHeadersMsgRoundtripEmpty(t *testing.T) {
	msg := HeadersMsg{Headers: nil}

	var buf bytes.Buffer
	if err := msg.Encode(&buf); err != nil {
		t.Fatalf("Encode: %v", err)
	}

	var msg2 HeadersMsg
	if err := msg2.Decode(&buf); err != nil {
		t.Fatalf("Decode: %v", err)
	}

	if len(msg2.Headers) != 0 {
		t.Fatalf("expected 0 headers, got %d", len(msg2.Headers))
	}
}

func TestHeadersMsgRoundtripSingle(t *testing.T) {
	hdr := types.BlockHeader{
		Version:    1,
		PrevBlock:  types.Hash{0x01},
		MerkleRoot: types.Hash{0x02},
		Timestamp:  1700000000,
		Bits:       0x1d00ffff,
		Nonce:      42,
	}
	msg := HeadersMsg{Headers: []types.BlockHeader{hdr}}

	var buf bytes.Buffer
	if err := msg.Encode(&buf); err != nil {
		t.Fatalf("Encode: %v", err)
	}

	var msg2 HeadersMsg
	if err := msg2.Decode(&buf); err != nil {
		t.Fatalf("Decode: %v", err)
	}

	if len(msg2.Headers) != 1 {
		t.Fatalf("expected 1 header, got %d", len(msg2.Headers))
	}
	got := msg2.Headers[0]
	if got.Version != hdr.Version || got.PrevBlock != hdr.PrevBlock ||
		got.MerkleRoot != hdr.MerkleRoot || got.Timestamp != hdr.Timestamp ||
		got.Bits != hdr.Bits || got.Nonce != hdr.Nonce {
		t.Fatal("HeadersMsg single header roundtrip mismatch")
	}
}

func TestHeadersMsgRoundtripMax(t *testing.T) {
	headers := make([]types.BlockHeader, MaxHeadersPerMsg)
	for i := range headers {
		headers[i] = types.BlockHeader{
			Version:   1,
			Timestamp: uint32(1700000000 + i),
			Bits:      0x1d00ffff,
			Nonce:     uint32(i),
		}
	}
	msg := HeadersMsg{Headers: headers}

	var buf bytes.Buffer
	if err := msg.Encode(&buf); err != nil {
		t.Fatalf("Encode: %v", err)
	}

	var msg2 HeadersMsg
	if err := msg2.Decode(&buf); err != nil {
		t.Fatalf("Decode: %v", err)
	}

	if len(msg2.Headers) != MaxHeadersPerMsg {
		t.Fatalf("expected %d headers, got %d", MaxHeadersPerMsg, len(msg2.Headers))
	}
	if msg2.Headers[0].Nonce != 0 || msg2.Headers[MaxHeadersPerMsg-1].Nonce != uint32(MaxHeadersPerMsg-1) {
		t.Fatal("HeadersMsg max roundtrip nonce mismatch")
	}
}

func TestHeadersMsgDecodeExceedsMax(t *testing.T) {
	var buf bytes.Buffer
	if err := types.WriteVarInt(&buf, uint64(MaxHeadersPerMsg+1)); err != nil {
		t.Fatalf("WriteVarInt: %v", err)
	}

	var msg HeadersMsg
	err := msg.Decode(&buf)
	if err == nil {
		t.Fatal("expected error for headers count exceeding max")
	}
}

func TestHeadersMsgTxCountZero(t *testing.T) {
	hdr := types.BlockHeader{
		Version:   1,
		Timestamp: 1700000000,
		Bits:      0x1d00ffff,
		Nonce:     1,
	}

	var buf bytes.Buffer
	if err := types.WriteVarInt(&buf, 1); err != nil {
		t.Fatal(err)
	}
	if err := hdr.Serialize(&buf); err != nil {
		t.Fatal(err)
	}
	if err := types.WriteVarInt(&buf, 0); err != nil {
		t.Fatal(err)
	}

	var msg HeadersMsg
	if err := msg.Decode(&buf); err != nil {
		t.Fatalf("Decode with txcount=0: %v", err)
	}
	if len(msg.Headers) != 1 {
		t.Fatalf("expected 1 header, got %d", len(msg.Headers))
	}
}

func TestHeadersMsgNonZeroTxCountRejected(t *testing.T) {
	hdr := types.BlockHeader{
		Version:   1,
		Timestamp: 1700000000,
		Bits:      0x1d00ffff,
		Nonce:     1,
	}

	var buf bytes.Buffer
	if err := types.WriteVarInt(&buf, 1); err != nil {
		t.Fatal(err)
	}
	if err := hdr.Serialize(&buf); err != nil {
		t.Fatal(err)
	}
	if err := types.WriteVarInt(&buf, 5); err != nil {
		t.Fatal(err)
	}

	var msg HeadersMsg
	err := msg.Decode(&buf)
	if err == nil {
		t.Fatal("expected error for non-zero tx count in headers message")
	}
}

func TestSendHeadersMsgRoundtrip(t *testing.T) {
	msg := SendHeadersMsg{}

	var buf bytes.Buffer
	if err := msg.Encode(&buf); err != nil {
		t.Fatalf("Encode: %v", err)
	}

	if buf.Len() != 0 {
		t.Fatalf("expected empty payload, got %d bytes", buf.Len())
	}

	var msg2 SendHeadersMsg
	if err := msg2.Decode(&buf); err != nil {
		t.Fatalf("Decode: %v", err)
	}
}

func TestMessageEncodingDeterministic(t *testing.T) {
	msg := VersionMsg{
		Version: 1, Services: 1, Timestamp: 1700000000,
		AddrRecv: "a", AddrFrom: "b", Nonce: 1,
		UserAgent: "test", StartHeight: 0,
	}

	var buf1, buf2 bytes.Buffer
	msg.Encode(&buf1)
	msg.Encode(&buf2)

	if !bytes.Equal(buf1.Bytes(), buf2.Bytes()) {
		t.Fatal("VersionMsg encoding is not deterministic")
	}
}
