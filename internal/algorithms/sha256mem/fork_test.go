// Copyright (c) 2024-2026 The Fairchain Contributors
// Distributed under the MIT software license.

package sha256mem

import (
	"testing"

	"github.com/bams-repo/fairchain/internal/params"
)

const testnetForkHeight = 85000

func TestUsesVariantAtTestnetFork(t *testing.T) {
	if !UsesVariantAt(testnetForkHeight, testnetForkHeight) {
		t.Fatal("expected variant at fork height")
	}
	if UsesVariantAt(testnetForkHeight, testnetForkHeight-1) {
		t.Fatal("expected v1 before fork height")
	}
}

func TestUsesVariantAtMainnetNever(t *testing.T) {
	if UsesVariantAt(0, 85000) {
		t.Fatal("activation height 0 must never use variant")
	}
}

func TestTestnetParamsForkHeight(t *testing.T) {
	h, ok := params.Testnet.ActivationHeights[ActivationKey]
	if !ok || h != testnetForkHeight {
		t.Fatalf("testnet ActivationHeights[%q] = %d, want %d", ActivationKey, h, testnetForkHeight)
	}
	if _, ok := params.Mainnet.ActivationHeights[ActivationKey]; ok {
		t.Fatal("mainnet must not set sha256mem activation height")
	}
}

func TestV1AndV2Differ(t *testing.T) {
	input := []byte("fork divergence check")
	v1 := powHashV1(input)
	v2 := powHashV2(input)
	if v1 == v2 {
		t.Fatal("v1 and v2 must produce different PoW hashes for the same input")
	}
}

func TestChainHasherForkGate(t *testing.T) {
	input := []byte("height gate")
	h := NewChainHasher(testnetForkHeight)

	if got := h.PoWHashAtHeight(input, testnetForkHeight-1); got != powHashV1(input) {
		t.Fatal("before fork must use v1")
	}
	if got := h.PoWHashAtHeight(input, testnetForkHeight); got != powHashV2(input) {
		t.Fatal("at fork must use v2")
	}
}
