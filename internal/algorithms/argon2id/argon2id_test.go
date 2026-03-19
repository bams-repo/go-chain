package argon2id

import (
	"testing"

	"github.com/bams-repo/fairchain/internal/types"
)

func TestPoWHashDeterministic(t *testing.T) {
	h := New()
	input := []byte("argon2 determinism test vector")

	got1 := h.PoWHash(input)
	got2 := h.PoWHash(input)

	if got1 == types.ZeroHash {
		t.Fatal("PoWHash returned zero hash")
	}
	if got1 != got2 {
		t.Fatalf("PoWHash not deterministic:\n  first  %s\n  second %s", got1, got2)
	}
}

func TestPoWHashDifferentInputs(t *testing.T) {
	h := New()
	a := h.PoWHash([]byte("input A"))
	b := h.PoWHash([]byte("input B"))

	if a == b {
		t.Fatal("different inputs produced same hash")
	}
}

func TestPoWHashEmptyInput(t *testing.T) {
	h := New()
	got := h.PoWHash([]byte{})
	if got == types.ZeroHash {
		t.Fatal("empty input produced zero hash")
	}

	got2 := h.PoWHash([]byte{})
	if got != got2 {
		t.Fatal("empty input not deterministic")
	}
}

func TestName(t *testing.T) {
	h := New()
	if h.Name() != "argon2id" {
		t.Fatalf("expected name argon2id, got %s", h.Name())
	}
}

func TestConcurrentSafety(t *testing.T) {
	h := New()
	input := []byte("concurrent argon2 test")
	expected := h.PoWHash(input)

	done := make(chan struct{})
	for i := 0; i < 4; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			for j := 0; j < 10; j++ {
				got := h.PoWHash(input)
				if got != expected {
					t.Errorf("concurrent PoWHash mismatch")
					return
				}
			}
		}()
	}
	for i := 0; i < 4; i++ {
		<-done
	}
}
