package crypto

import (
	"testing"

	"github.com/bams-repo/fairchain/internal/types"
)

func TestDoubleSHA256Deterministic(t *testing.T) {
	data := []byte("deterministic hash test data")
	h1 := DoubleSHA256(data)
	h2 := DoubleSHA256(data)
	if h1 != h2 {
		t.Fatal("DoubleSHA256 is not deterministic")
	}
}

func TestDoubleSHA256DifferentInputs(t *testing.T) {
	h1 := DoubleSHA256([]byte("input1"))
	h2 := DoubleSHA256([]byte("input2"))
	if h1 == h2 {
		t.Fatal("different inputs should produce different hashes")
	}
}

func TestHashBlockHeaderDeterministic(t *testing.T) {
	hdr := types.BlockHeader{
		Version:   1,
		Timestamp: 1700000000,
		Bits:      0x207fffff,
		Nonce:     42,
	}
	h1 := HashBlockHeader(&hdr)
	h2 := HashBlockHeader(&hdr)
	if h1 != h2 {
		t.Fatal("HashBlockHeader is not deterministic")
	}
}

func TestMerkleRootEmpty(t *testing.T) {
	root := MerkleRoot(nil)
	if !root.IsZero() {
		t.Fatal("empty merkle root should be zero")
	}
}

func TestMerkleRootSingle(t *testing.T) {
	h := DoubleSHA256([]byte("tx1"))
	root := MerkleRoot([]types.Hash{h})
	if root != h {
		t.Fatal("single-element merkle root should equal the element")
	}
}

func TestMerkleRootDeterministic(t *testing.T) {
	hashes := make([]types.Hash, 4)
	for i := range hashes {
		hashes[i] = DoubleSHA256([]byte{byte(i)})
	}
	r1 := MerkleRoot(hashes)
	r2 := MerkleRoot(hashes)
	if r1 != r2 {
		t.Fatal("MerkleRoot is not deterministic")
	}
}

func TestMerkleRootOddCount(t *testing.T) {
	hashes := make([]types.Hash, 3)
	for i := range hashes {
		hashes[i] = DoubleSHA256([]byte{byte(i)})
	}
	root := MerkleRoot(hashes)
	if root.IsZero() {
		t.Fatal("merkle root of 3 elements should not be zero")
	}

	// Verify determinism with odd count.
	root2 := MerkleRoot(hashes)
	if root != root2 {
		t.Fatal("odd-count MerkleRoot is not deterministic")
	}
}

func TestMerkleRootDoesNotMutateInput(t *testing.T) {
	hashes := make([]types.Hash, 3)
	for i := range hashes {
		hashes[i] = DoubleSHA256([]byte{byte(i)})
	}
	original := make([]types.Hash, len(hashes))
	copy(original, hashes)

	MerkleRoot(hashes)

	for i := range hashes {
		if hashes[i] != original[i] {
			t.Fatalf("MerkleRoot mutated input at index %d", i)
		}
	}
}

func TestCompactBitsRoundtrip(t *testing.T) {
	tests := []uint32{
		0x1d00ffff, // Standard mainnet initial difficulty.
		0x207fffff, // Very easy (regtest).
		0x1e0fffff, // Testnet.
		0x1b0404cb, // Arbitrary difficulty.
	}

	for _, bits := range tests {
		target := CompactToBig(bits)
		back := BigToCompact(target)
		// Re-expand to verify equivalence (compact form may normalize).
		target2 := CompactToBig(back)
		if target.Cmp(target2) != 0 {
			t.Errorf("CompactBits roundtrip failed for 0x%08x: target=%s target2=%s", bits, target, target2)
		}
	}
}

func TestCompactToHashAndValidation(t *testing.T) {
	bits := uint32(0x207fffff)
	target := CompactToHash(bits)

	// A zero hash should be below any non-zero target.
	if err := ValidateProofOfWork(types.ZeroHash, bits); err != nil {
		t.Fatalf("zero hash should pass easy target: %v", err)
	}

	// A max hash should fail.
	var maxHash types.Hash
	for i := range maxHash {
		maxHash[i] = 0xFF
	}
	if err := ValidateProofOfWork(maxHash, bits); err == nil {
		t.Fatal("max hash should fail PoW validation")
	}

	_ = target
}

func TestCalcWork(t *testing.T) {
	w1 := CalcWork(0x207fffff) // Easy
	w2 := CalcWork(0x1d00ffff) // Hard
	if w1.Cmp(w2) >= 0 {
		t.Fatal("easier target should produce less work")
	}
}

func TestComputeMerkleRootWithTransactions(t *testing.T) {
	txs := []types.Transaction{
		{
			Version: 1,
			Inputs:  []types.TxInput{{PreviousOutPoint: types.CoinbaseOutPoint, SignatureScript: []byte("cb"), Sequence: 0xFFFFFFFF}},
			Outputs: []types.TxOutput{{Value: 100, PkScript: []byte{0x00}}},
		},
	}
	root1, err := ComputeMerkleRoot(txs)
	if err != nil {
		t.Fatalf("ComputeMerkleRoot: %v", err)
	}
	root2, err := ComputeMerkleRoot(txs)
	if err != nil {
		t.Fatalf("ComputeMerkleRoot: %v", err)
	}
	if root1 != root2 {
		t.Fatal("ComputeMerkleRoot is not deterministic")
	}
	if root1.IsZero() {
		t.Fatal("merkle root should not be zero for non-empty tx list")
	}
}
