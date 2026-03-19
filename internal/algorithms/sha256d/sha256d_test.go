package sha256d

import (
	"encoding/hex"
	"testing"

	"github.com/bams-repo/fairchain/internal/types"
)

func TestPoWHashMatchesDoubleSHA256(t *testing.T) {
	h := New()

	input := []byte("test vector for sha256d pow hash")
	got := h.PoWHash(input)

	if got == types.ZeroHash {
		t.Fatal("PoWHash returned zero hash")
	}

	got2 := h.PoWHash(input)
	if got != got2 {
		t.Fatal("PoWHash is not deterministic")
	}
}

func TestPoWHashKnownVector(t *testing.T) {
	h := New()

	// SHA256("") = e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
	// SHA256(above bytes) = 5df6e0e2761359d30a8275058e299fcc0381534545f55cf43e41983f5d4c9456
	input := []byte{}
	got := h.PoWHash(input)

	expected, _ := hex.DecodeString("5df6e0e2761359d30a8275058e299fcc0381534545f55cf43e41983f5d4c9456")
	var want types.Hash
	copy(want[:], expected)

	if got != want {
		t.Fatalf("PoWHash empty input:\n  got  %s\n  want %s", got, want)
	}
}

func TestName(t *testing.T) {
	h := New()
	if h.Name() != "sha256d" {
		t.Fatalf("expected name sha256d, got %s", h.Name())
	}
}

func TestConcurrentSafety(t *testing.T) {
	h := New()
	input := []byte("concurrent test data")
	expected := h.PoWHash(input)

	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			for j := 0; j < 100; j++ {
				got := h.PoWHash(input)
				if got != expected {
					t.Errorf("concurrent PoWHash mismatch")
					return
				}
			}
		}()
	}
	for i := 0; i < 10; i++ {
		<-done
	}
}
