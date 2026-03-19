package protocol

import (
	"bytes"
	"testing"

	"github.com/bams-repo/fairchain/internal/version"
)

func FuzzVersionMsgDecode(f *testing.F) {
	msg := VersionMsg{
		Version: 1, Services: 1, Timestamp: 1700000000,
		AddrRecv: "127.0.0.1:19333", AddrFrom: "127.0.0.1:19334",
		Nonce: 12345, UserAgent: version.UserAgent(), StartHeight: 100,
	}
	var buf bytes.Buffer
	msg.Encode(&buf)
	f.Add(buf.Bytes())
	f.Add([]byte{})
	f.Add(buf.Bytes()[:10])

	f.Fuzz(func(t *testing.T, input []byte) {
		var m VersionMsg
		_ = m.Decode(bytes.NewReader(input))
	})
}

func FuzzInvMsgDecode(f *testing.F) {
	f.Add([]byte{0x00})
	f.Add([]byte{0x01, 0x01, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00})

	f.Fuzz(func(t *testing.T, input []byte) {
		var m InvMsg
		_ = m.Decode(bytes.NewReader(input))
	})
}

func FuzzGetBlocksMsgDecode(f *testing.F) {
	f.Add(make([]byte, 40))
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, input []byte) {
		var m GetBlocksMsg
		_ = m.Decode(bytes.NewReader(input))
	})
}

func FuzzAddrMsgDecode(f *testing.F) {
	f.Add([]byte{0x00})
	f.Add([]byte{0x01, 0x05, 0x68, 0x65, 0x6c, 0x6c, 0x6f})

	f.Fuzz(func(t *testing.T, input []byte) {
		var m AddrMsg
		_ = m.Decode(bytes.NewReader(input))
	})
}

func FuzzMessageHeaderDecode(f *testing.F) {
	magic := [4]byte{0xFA, 0x1C, 0xC0, 0xFF}
	var buf bytes.Buffer
	EncodeMessageHeader(&buf, magic, CmdVersion, []byte("payload"))
	f.Add(buf.Bytes())
	f.Add([]byte{})
	f.Add(make([]byte, 24))

	f.Fuzz(func(t *testing.T, input []byte) {
		_, _ = DecodeMessageHeader(bytes.NewReader(input))
	})
}
