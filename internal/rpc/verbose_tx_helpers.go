// Copyright (c) 2024-2026 The Fairchain Contributors
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

package rpc

import (
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/bams-repo/fairchain/internal/crypto"
	"github.com/bams-repo/fairchain/internal/types"
)

// outPointKV is a map key for (txid, vout) in internal hash byte order.
type outPointKV struct {
	h types.Hash
	i uint32
}

func scriptPubKeyType(pk []byte) string {
	if crypto.ExtractP2PKHHash(pk) != nil {
		return "pubkeyhash"
	}
	return "nonstandard"
}

func scriptPubKeyASM(pk []byte) string {
	if h := crypto.ExtractP2PKHHash(pk); h != nil {
		return fmt.Sprintf("OP_DUP OP_HASH160 %x OP_EQUALVERIFY OP_CHECKSIG", h)
	}
	return scriptToPushASM(pk)
}

// scriptToPushASM renders unknown scripts as push / opcode tokens (explorer-friendly).
func scriptToPushASM(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	var parts []string
	for i := 0; i < len(b); {
		op := b[i]
		switch {
		case op >= 0x01 && op <= 0x4b:
			n := int(op)
			if i+1+n > len(b) {
				parts = append(parts, "INVALID_PUSH")
				return strings.Join(parts, " ")
			}
			data := b[i+1 : i+1+n]
			i += 1 + n
			parts = append(parts, fmt.Sprintf("%x", data))
		case op == 0x4c:
			if i+2 > len(b) {
				parts = append(parts, "INVALID_PUSHDATA1")
				return strings.Join(parts, " ")
			}
			n := int(b[i+1])
			if i+2+n > len(b) {
				parts = append(parts, "INVALID_PUSHDATA1")
				return strings.Join(parts, " ")
			}
			data := b[i+2 : i+2+n]
			i += 2 + n
			parts = append(parts, fmt.Sprintf("%x", data))
		default:
			i++
			switch op {
			case 0x76:
				parts = append(parts, "OP_DUP")
			case 0xa9:
				parts = append(parts, "OP_HASH160")
			case 0x88:
				parts = append(parts, "OP_EQUALVERIFY")
			case 0x87:
				parts = append(parts, "OP_EQUAL")
			case 0xac:
				parts = append(parts, "OP_CHECKSIG")
			case 0xae:
				parts = append(parts, "OP_CHECKMULTISIG")
			case 0x6a:
				parts = append(parts, "OP_RETURN")
			case 0x61:
				parts = append(parts, "OP_NOP")
			default:
				parts = append(parts, fmt.Sprintf("OP_%02x", op))
			}
		}
	}
	return strings.Join(parts, " ")
}

func addressesFromPkScript(pk []byte, addrVer byte) []string {
	h := crypto.ExtractP2PKHHash(pk)
	if h == nil {
		return nil
	}
	var pkh [crypto.PubKeyHashSize]byte
	copy(pkh[:], h)
	addr := crypto.PubKeyHashToAddress(pkh, addrVer)
	if addr == "" {
		return nil
	}
	return []string{addr}
}

func (s *Server) buildPrevOutIndexThroughHeight(maxHeight uint32) map[outPointKV]struct {
	pkScript []byte
	value    uint64
} {
	out := make(map[outPointKV]struct {
		pkScript []byte
		value    uint64
	})
	for h := uint32(0); h <= maxHeight; h++ {
		block, _, err := s.chain.GetBlockByHeight(h)
		if err != nil {
			continue
		}
		for ti := range block.Transactions {
			tx := &block.Transactions[ti]
			txHash, err := crypto.HashTransaction(tx)
			if err != nil {
				continue
			}
			for oi, o := range tx.Outputs {
				key := outPointKV{h: txHash, i: uint32(oi)}
				out[key] = struct {
					pkScript []byte
					value    uint64
				}{pkScript: append([]byte(nil), o.PkScript...), value: o.Value}
			}
		}
	}
	return out
}

func (s *Server) scriptPubKeyVerboseMap(pk []byte) map[string]interface{} {
	addrVer := s.params.AddressPrefix
	addrs := addressesFromPkScript(pk, addrVer)
	if addrs == nil {
		addrs = []string{}
	}
	m := map[string]interface{}{
		"hex":       hex.EncodeToString(pk),
		"asm":       scriptPubKeyASM(pk),
		"type":      scriptPubKeyType(pk),
		"addresses": addrs,
	}
	return m
}

func (s *Server) enrichVerboseTxIO(tx *types.Transaction, result map[string]interface{}, blockHeight uint32, blockHash types.Hash) {
	addrVer := s.params.AddressPrefix
	isMempool := blockHash.IsZero()

	var prevIdx map[outPointKV]struct {
		pkScript []byte
		value    uint64
	}
	if !tx.IsCoinbase() && !isMempool {
		prevIdx = s.buildPrevOutIndexThroughHeight(blockHeight)
	}

	vins, _ := result["vin"].([]map[string]interface{})
	for i := range tx.Inputs {
		if i >= len(vins) {
			break
		}
		in := &tx.Inputs[i]
		vin := vins[i]

		if tx.IsCoinbase() {
			if cbHex, ok := vin["coinbase"].(string); ok {
				cbBytes, err := hex.DecodeString(cbHex)
				if err == nil {
					vin["coinbase_asm"] = scriptToPushASM(cbBytes)
				}
			}
			continue
		}

		key := outPointKV{h: types.Hash(in.PreviousOutPoint.Hash), i: in.PreviousOutPoint.Index}
		var pk []byte
		var val uint64
		var ok bool
		if prevIdx != nil {
			ent, hit := prevIdx[key]
			if hit {
				pk, val, ok = ent.pkScript, ent.value, true
			}
		}
		if !ok && isMempool {
			utxoEnt := s.chain.UtxoSet().Get(types.Hash(in.PreviousOutPoint.Hash), in.PreviousOutPoint.Index)
			if utxoEnt != nil {
				pk = utxoEnt.PkScript
				val = utxoEnt.Value
				ok = true
			}
		}

		if sigMap, ok2 := vin["scriptSig"].(map[string]interface{}); ok2 {
			if hstr, ok3 := sigMap["hex"].(string); ok3 {
				if b, err := hex.DecodeString(hstr); err == nil {
					sigMap["asm"] = scriptToPushASM(b)
				}
			}
		}

		if ok && len(pk) > 0 {
			vin["prevout"] = map[string]interface{}{
				"value":        val,
				"scriptPubKey": s.scriptPubKeyVerboseMap(pk),
			}
			if addrs := addressesFromPkScript(pk, addrVer); len(addrs) > 0 {
				vin["address"] = addrs[0]
			}
		}
	}

	vouts, _ := result["vout"].([]map[string]interface{})
	for i := range tx.Outputs {
		if i >= len(vouts) {
			break
		}
		pk := tx.Outputs[i].PkScript
		vouts[i]["scriptPubKey"] = s.scriptPubKeyVerboseMap(pk)
	}
}
