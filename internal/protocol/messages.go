// Copyright (c) 2024-2026 The Fairchain Contributors
// Fairchain is an experiment in modularity, designed to improve on the work
// of Satoshi Nakamoto and to inspire more creative genius in the space.
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

package protocol

import (
	"encoding/binary"
	"fmt"
	"io"

	"github.com/bams-repo/fairchain/internal/types"
	"github.com/bams-repo/fairchain/internal/version"
)

// ProtocolVersion re-exports the canonical wire protocol version from the
// version package so callers that already import protocol don't need a
// second import for the constant.
var ProtocolVersion = version.ProtocolVersion

// Maximum payload size to prevent memory exhaustion (4 MB).
const MaxPayloadSize = 4 * 1024 * 1024

// Message commands (fixed 12-byte ASCII, null-padded).
const (
	CmdVersion   = "version"
	CmdVerack    = "verack"
	CmdPing      = "ping"
	CmdPong      = "pong"
	CmdInv       = "inv"
	CmdGetData   = "getdata"
	CmdBlock     = "block"
	CmdTx        = "tx"
	CmdGetBlocks  = "getblocks"
	CmdGetHeaders = "getheaders"
	CmdHeaders    = "headers"
	CmdSendHeaders = "sendheaders"
	CmdAddr       = "addr"
	CmdGetAddr    = "getaddr"
	CmdReject     = "reject"
	CmdProbeReq   = "probereq"
	CmdProbeResp  = "proberesp"
)

// Inventory vector types.
const (
	InvTypeBlock uint32 = 1
	InvTypeTx    uint32 = 2
)

// MessageHeader is the wire message header.
// magic(4) + command(12) + length(4) + checksum(4) = 24 bytes.
const MessageHeaderSize = 24

// MessageHeader represents the header of every wire message.
type MessageHeader struct {
	Magic    [4]byte
	Command  [12]byte
	Length   uint32
	Checksum [4]byte
}

// InvVector is an inventory vector identifying a data object.
type InvVector struct {
	Type uint32
	Hash types.Hash
}

// VersionMsg is the initial handshake message.
type VersionMsg struct {
	Version     uint32
	Services    uint64
	Timestamp   int64
	AddrRecv    string // IP:port of the receiver.
	AddrFrom    string // IP:port of the sender.
	Nonce       uint64 // Random nonce for self-connection detection.
	UserAgent   string
	StartHeight uint32
}

// PingMsg / PongMsg carry a nonce for liveness checking.
type PingMsg struct {
	Nonce uint64
}

type PongMsg struct {
	Nonce uint64
}

// ProbeReqMsg asks a peer to dial back to the sender's advertised listen
// address and report whether the connection succeeded. The Nonce is echoed
// in the response so the requester can correlate it. Addr is the IP:port
// the requester wants probed (typically its own listen address as seen by
// the remote peer).
type ProbeReqMsg struct {
	Nonce uint64
	Addr  string // IP:port to probe (max 256 bytes on wire)
}

// ProbeRespMsg is the reply to a ProbeReqMsg. Reachable is 1 if the probe
// peer could open a TCP connection to the requested address, 0 otherwise.
type ProbeRespMsg struct {
	Nonce     uint64
	Reachable uint8 // 1 = open, 0 = closed/unreachable
}

// InvMsg announces inventory items.
type InvMsg struct {
	Inventory []InvVector
}

// GetDataMsg requests specific inventory items.
type GetDataMsg struct {
	Inventory []InvVector
}

// GetBlocksMsg requests block hashes starting from known hashes.
type GetBlocksMsg struct {
	Version    uint32
	BlockLocatorHashes []types.Hash
	HashStop   types.Hash
}

// AddrMsg gossips peer addresses.
type AddrMsg struct {
	Addresses []string
}

// RejectMsg signals rejection of a message.
type RejectMsg struct {
	Command string
	Code    uint8
	Reason  string
}

// GetHeadersMsg requests block headers starting from known hashes.
// Wire format is identical to GetBlocksMsg: version(4) + varint(count) +
// locator hashes (32 each) + hashstop(32).
type GetHeadersMsg struct {
	Version            uint32
	BlockLocatorHashes []types.Hash
	HashStop           types.Hash
}

// HeadersMsg carries a batch of block headers.
// Each header is serialized as 80 bytes followed by a varint tx count of 0,
// matching Bitcoin Core's wire format.
type HeadersMsg struct {
	Headers []types.BlockHeader
}

// MaxHeadersPerMsg is the maximum number of headers allowed in a single
// headers message (matches Bitcoin Core).
const MaxHeadersPerMsg = 2000

// SendHeadersMsg is an empty message requesting the peer announce new blocks
// via headers messages instead of inv messages (BIP 130).
type SendHeadersMsg struct{}

// ---- GetHeadersMsg encoding ----

func (m *GetHeadersMsg) Encode(w io.Writer) error {
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], m.Version)
	if _, err := w.Write(buf[:]); err != nil {
		return err
	}
	if err := types.WriteVarInt(w, uint64(len(m.BlockLocatorHashes))); err != nil {
		return err
	}
	for _, h := range m.BlockLocatorHashes {
		if _, err := w.Write(h[:]); err != nil {
			return err
		}
	}
	_, err := w.Write(m.HashStop[:])
	return err
}

func (m *GetHeadersMsg) Decode(r io.Reader) error {
	var buf [4]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return err
	}
	m.Version = binary.LittleEndian.Uint32(buf[:])
	count, err := types.ReadVarInt(r)
	if err != nil {
		return err
	}
	if count > 500 {
		return fmt.Errorf("block locator count %d exceeds max", count)
	}
	m.BlockLocatorHashes = make([]types.Hash, count)
	for i := uint64(0); i < count; i++ {
		if _, err := io.ReadFull(r, m.BlockLocatorHashes[i][:]); err != nil {
			return err
		}
	}
	if _, err := io.ReadFull(r, m.HashStop[:]); err != nil {
		return err
	}
	return nil
}

// ---- HeadersMsg encoding ----

func (m *HeadersMsg) Encode(w io.Writer) error {
	if err := types.WriteVarInt(w, uint64(len(m.Headers))); err != nil {
		return err
	}
	for i := range m.Headers {
		if err := m.Headers[i].Serialize(w); err != nil {
			return err
		}
		if err := types.WriteVarInt(w, 0); err != nil {
			return err
		}
	}
	return nil
}

func (m *HeadersMsg) Decode(r io.Reader) error {
	count, err := types.ReadVarInt(r)
	if err != nil {
		return err
	}
	if count > MaxHeadersPerMsg {
		return fmt.Errorf("headers count %d exceeds max %d", count, MaxHeadersPerMsg)
	}
	m.Headers = make([]types.BlockHeader, count)
	for i := uint64(0); i < count; i++ {
		if err := m.Headers[i].Deserialize(r); err != nil {
			return err
		}
		txCount, err := types.ReadVarInt(r)
		if err != nil {
			return err
		}
		if txCount != 0 {
			return fmt.Errorf("headers message: header %d has non-zero tx count %d", i, txCount)
		}
	}
	return nil
}

// ---- SendHeadersMsg encoding ----

func (m *SendHeadersMsg) Encode(w io.Writer) error { return nil }
func (m *SendHeadersMsg) Decode(r io.Reader) error  { return nil }

// ---- Encoding helpers ----

// EncodeMessageHeader writes a message header to w.
func EncodeMessageHeader(w io.Writer, magic [4]byte, cmd string, payload []byte) error {
	var hdr MessageHeader
	hdr.Magic = magic
	copy(hdr.Command[:], cmd)
	hdr.Length = uint32(len(payload))

	// Checksum: first 4 bytes of double-SHA256 of payload.
	// We import crypto here to avoid circular deps — use a local double-SHA256.
	checksum := doubleSHA256Checksum(payload)
	copy(hdr.Checksum[:], checksum[:4])

	var buf [MessageHeaderSize]byte
	copy(buf[0:4], hdr.Magic[:])
	copy(buf[4:16], hdr.Command[:])
	binary.LittleEndian.PutUint32(buf[16:20], hdr.Length)
	copy(buf[20:24], hdr.Checksum[:])

	_, err := w.Write(buf[:])
	return err
}

// DecodeMessageHeader reads a message header from r.
func DecodeMessageHeader(r io.Reader) (*MessageHeader, error) {
	var buf [MessageHeaderSize]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return nil, err
	}
	var hdr MessageHeader
	copy(hdr.Magic[:], buf[0:4])
	copy(hdr.Command[:], buf[4:16])
	hdr.Length = binary.LittleEndian.Uint32(buf[16:20])
	copy(hdr.Checksum[:], buf[20:24])

	if hdr.Length > MaxPayloadSize {
		return nil, fmt.Errorf("payload size %d exceeds max %d", hdr.Length, MaxPayloadSize)
	}
	return &hdr, nil
}

// CommandString extracts the command as a trimmed string.
func (h *MessageHeader) CommandString() string {
	end := 0
	for i, b := range h.Command {
		if b == 0 {
			break
		}
		end = i + 1
	}
	return string(h.Command[:end])
}

// ---- Payload serialization for VersionMsg ----

func (m *VersionMsg) Encode(w io.Writer) error {
	var buf [4 + 8 + 8]byte
	binary.LittleEndian.PutUint32(buf[0:4], m.Version)
	binary.LittleEndian.PutUint64(buf[4:12], m.Services)
	binary.LittleEndian.PutUint64(buf[12:20], uint64(m.Timestamp))
	if _, err := w.Write(buf[:]); err != nil {
		return err
	}
	if err := writeString(w, m.AddrRecv); err != nil {
		return err
	}
	if err := writeString(w, m.AddrFrom); err != nil {
		return err
	}
	var nonceBuf [8]byte
	binary.LittleEndian.PutUint64(nonceBuf[:], m.Nonce)
	if _, err := w.Write(nonceBuf[:]); err != nil {
		return err
	}
	if err := writeString(w, m.UserAgent); err != nil {
		return err
	}
	var heightBuf [4]byte
	binary.LittleEndian.PutUint32(heightBuf[:], m.StartHeight)
	_, err := w.Write(heightBuf[:])
	return err
}

func (m *VersionMsg) Decode(r io.Reader) error {
	var buf [20]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return err
	}
	m.Version = binary.LittleEndian.Uint32(buf[0:4])
	m.Services = binary.LittleEndian.Uint64(buf[4:12])
	m.Timestamp = int64(binary.LittleEndian.Uint64(buf[12:20]))

	var err error
	m.AddrRecv, err = readString(r)
	if err != nil {
		return err
	}
	m.AddrFrom, err = readString(r)
	if err != nil {
		return err
	}
	var nonceBuf [8]byte
	if _, err := io.ReadFull(r, nonceBuf[:]); err != nil {
		return err
	}
	m.Nonce = binary.LittleEndian.Uint64(nonceBuf[:])
	m.UserAgent, err = readString(r)
	if err != nil {
		return err
	}
	var heightBuf [4]byte
	if _, err := io.ReadFull(r, heightBuf[:]); err != nil {
		return err
	}
	m.StartHeight = binary.LittleEndian.Uint32(heightBuf[:])
	return nil
}

// ---- InvMsg encoding ----

func (m *InvMsg) Encode(w io.Writer) error {
	if err := types.WriteVarInt(w, uint64(len(m.Inventory))); err != nil {
		return err
	}
	for _, iv := range m.Inventory {
		if err := encodeInvVector(w, iv); err != nil {
			return err
		}
	}
	return nil
}

func (m *InvMsg) Decode(r io.Reader) error {
	count, err := types.ReadVarInt(r)
	if err != nil {
		return err
	}
	if count > 50000 {
		return fmt.Errorf("inv count %d exceeds max", count)
	}
	m.Inventory = make([]InvVector, count)
	for i := uint64(0); i < count; i++ {
		if err := decodeInvVector(r, &m.Inventory[i]); err != nil {
			return err
		}
	}
	return nil
}

// GetDataMsg uses the same encoding as InvMsg.
func (m *GetDataMsg) Encode(w io.Writer) error {
	inv := InvMsg{Inventory: m.Inventory}
	return inv.Encode(w)
}

func (m *GetDataMsg) Decode(r io.Reader) error {
	var inv InvMsg
	if err := inv.Decode(r); err != nil {
		return err
	}
	m.Inventory = inv.Inventory
	return nil
}

// ---- GetBlocksMsg encoding ----

func (m *GetBlocksMsg) Encode(w io.Writer) error {
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], m.Version)
	if _, err := w.Write(buf[:]); err != nil {
		return err
	}
	if err := types.WriteVarInt(w, uint64(len(m.BlockLocatorHashes))); err != nil {
		return err
	}
	for _, h := range m.BlockLocatorHashes {
		if _, err := w.Write(h[:]); err != nil {
			return err
		}
	}
	_, err := w.Write(m.HashStop[:])
	return err
}

func (m *GetBlocksMsg) Decode(r io.Reader) error {
	var buf [4]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return err
	}
	m.Version = binary.LittleEndian.Uint32(buf[:])
	count, err := types.ReadVarInt(r)
	if err != nil {
		return err
	}
	if count > 500 {
		return fmt.Errorf("block locator count %d exceeds max", count)
	}
	m.BlockLocatorHashes = make([]types.Hash, count)
	for i := uint64(0); i < count; i++ {
		if _, err := io.ReadFull(r, m.BlockLocatorHashes[i][:]); err != nil {
			return err
		}
	}
	if _, err := io.ReadFull(r, m.HashStop[:]); err != nil {
		return err
	}
	return nil
}

// ---- AddrMsg encoding ----

func (m *AddrMsg) Encode(w io.Writer) error {
	if err := types.WriteVarInt(w, uint64(len(m.Addresses))); err != nil {
		return err
	}
	for _, addr := range m.Addresses {
		if err := writeString(w, addr); err != nil {
			return err
		}
	}
	return nil
}

func (m *AddrMsg) Decode(r io.Reader) error {
	count, err := types.ReadVarInt(r)
	if err != nil {
		return err
	}
	if count > 1000 {
		return fmt.Errorf("addr count %d exceeds max", count)
	}
	m.Addresses = make([]string, count)
	for i := uint64(0); i < count; i++ {
		m.Addresses[i], err = readString(r)
		if err != nil {
			return err
		}
	}
	return nil
}

// ---- PingMsg / PongMsg encoding ----

func (m *PingMsg) Encode(w io.Writer) error {
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], m.Nonce)
	_, err := w.Write(buf[:])
	return err
}

func (m *PingMsg) Decode(r io.Reader) error {
	var buf [8]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return err
	}
	m.Nonce = binary.LittleEndian.Uint64(buf[:])
	return nil
}

func (m *PongMsg) Encode(w io.Writer) error {
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], m.Nonce)
	_, err := w.Write(buf[:])
	return err
}

func (m *PongMsg) Decode(r io.Reader) error {
	var buf [8]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return err
	}
	m.Nonce = binary.LittleEndian.Uint64(buf[:])
	return nil
}

// ---- ProbeReqMsg / ProbeRespMsg encoding ----

const maxProbeAddrLen = 256

func (m *ProbeReqMsg) Encode(w io.Writer) error {
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], m.Nonce)
	if _, err := w.Write(buf[:]); err != nil {
		return err
	}
	addrBytes := []byte(m.Addr)
	if len(addrBytes) > maxProbeAddrLen {
		addrBytes = addrBytes[:maxProbeAddrLen]
	}
	var lenBuf [2]byte
	binary.LittleEndian.PutUint16(lenBuf[:], uint16(len(addrBytes)))
	if _, err := w.Write(lenBuf[:]); err != nil {
		return err
	}
	_, err := w.Write(addrBytes)
	return err
}

func (m *ProbeReqMsg) Decode(r io.Reader) error {
	var buf [8]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return err
	}
	m.Nonce = binary.LittleEndian.Uint64(buf[:])
	var lenBuf [2]byte
	if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
		return err
	}
	addrLen := binary.LittleEndian.Uint16(lenBuf[:])
	if addrLen > maxProbeAddrLen {
		return fmt.Errorf("probe addr too long: %d", addrLen)
	}
	addrBytes := make([]byte, addrLen)
	if _, err := io.ReadFull(r, addrBytes); err != nil {
		return err
	}
	m.Addr = string(addrBytes)
	return nil
}

func (m *ProbeRespMsg) Encode(w io.Writer) error {
	var buf [9]byte
	binary.LittleEndian.PutUint64(buf[:8], m.Nonce)
	buf[8] = m.Reachable
	_, err := w.Write(buf[:])
	return err
}

func (m *ProbeRespMsg) Decode(r io.Reader) error {
	var buf [9]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return err
	}
	m.Nonce = binary.LittleEndian.Uint64(buf[:8])
	m.Reachable = buf[8]
	return nil
}

// ---- helpers ----

func encodeInvVector(w io.Writer, iv InvVector) error {
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], iv.Type)
	if _, err := w.Write(buf[:]); err != nil {
		return err
	}
	_, err := w.Write(iv.Hash[:])
	return err
}

func decodeInvVector(r io.Reader, iv *InvVector) error {
	var buf [4]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return err
	}
	iv.Type = binary.LittleEndian.Uint32(buf[:])
	_, err := io.ReadFull(r, iv.Hash[:])
	return err
}

func writeString(w io.Writer, s string) error {
	if err := types.WriteVarInt(w, uint64(len(s))); err != nil {
		return err
	}
	_, err := w.Write([]byte(s))
	return err
}

func readString(r io.Reader) (string, error) {
	length, err := types.ReadVarInt(r)
	if err != nil {
		return "", err
	}
	if length > 1024 {
		return "", fmt.Errorf("string length %d exceeds max", length)
	}
	buf := make([]byte, length)
	if _, err := io.ReadFull(r, buf); err != nil {
		return "", err
	}
	return string(buf), nil
}
