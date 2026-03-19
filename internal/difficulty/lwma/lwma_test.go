package lwma

import (
	"testing"
	"time"

	"github.com/bams-repo/fairchain/internal/crypto"
	"github.com/bams-repo/fairchain/internal/params"
	"github.com/bams-repo/fairchain/internal/types"
)

func TestName(t *testing.T) {
	r := New()
	if r.Name() != "lwma" {
		t.Fatalf("expected name 'lwma', got %q", r.Name())
	}
}

func TestNoRetarget(t *testing.T) {
	r := New()
	p := &params.ChainParams{
		InitialBits: 0x207fffff,
		NoRetarget:  true,
	}
	tip := &types.BlockHeader{Bits: 0x1e0fffff}
	bits := r.CalcNextBits(tip, 100, nil, p)
	if bits != p.InitialBits {
		t.Fatalf("NoRetarget: expected 0x%08x, got 0x%08x", p.InitialBits, bits)
	}
}

func TestInsufficientHistory(t *testing.T) {
	r := New()
	p := &params.ChainParams{
		InitialBits:      0x1e0fffff,
		MinBits:          0x1e0fffff,
		NoRetarget:       false,
		RetargetInterval: 20,
		TargetBlockSpacing: 60 * time.Second,
	}
	tip := &types.BlockHeader{Bits: p.InitialBits, Timestamp: 1700000000}
	bits := r.CalcNextBits(tip, 10, nil, p)
	if bits != p.InitialBits {
		t.Fatalf("insufficient history: expected 0x%08x, got 0x%08x", p.InitialBits, bits)
	}
}

func TestSteadyState(t *testing.T) {
	// When blocks arrive exactly on schedule, difficulty should stay roughly
	// the same (within compact encoding precision).
	r := New()
	N := uint32(20)
	T := 60 // seconds
	initialBits := uint32(0x1e0fffff)

	p := &params.ChainParams{
		InitialBits:        initialBits,
		MinBits:            0x1f7fffff,
		NoRetarget:         false,
		RetargetInterval:   N,
		TargetBlockSpacing: time.Duration(T) * time.Second,
	}

	baseTime := uint32(1700000000)
	headers := make(map[uint32]*types.BlockHeader)
	for i := uint32(0); i <= N; i++ {
		headers[i] = &types.BlockHeader{
			Bits:      initialBits,
			Timestamp: baseTime + i*uint32(T),
		}
	}
	getAncestor := func(h uint32) *types.BlockHeader { return headers[h] }

	bits := r.CalcNextBits(headers[N], N, getAncestor, p)

	// The result should be close to the initial bits. The 99/100 factor
	// means it won't be exactly the same, but should be within ~2%.
	oldTarget := crypto.CompactToBig(initialBits)
	newTarget := crypto.CompactToBig(bits)

	// newTarget / oldTarget should be approximately 0.99
	ratio := float64(newTarget.Int64()) / float64(oldTarget.Int64())
	if ratio < 0.95 || ratio > 1.05 {
		t.Fatalf("steady state: target ratio %.4f is too far from 1.0 (bits: 0x%08x -> 0x%08x)", ratio, initialBits, bits)
	}
}

func TestFastBlocks(t *testing.T) {
	// When blocks arrive twice as fast as target, difficulty should increase.
	r := New()
	N := uint32(20)
	T := 60 // seconds
	initialBits := uint32(0x1e0fffff)

	p := &params.ChainParams{
		InitialBits:        initialBits,
		MinBits:            0x1f7fffff,
		NoRetarget:         false,
		RetargetInterval:   N,
		TargetBlockSpacing: time.Duration(T) * time.Second,
	}

	baseTime := uint32(1700000000)
	headers := make(map[uint32]*types.BlockHeader)
	for i := uint32(0); i <= N; i++ {
		headers[i] = &types.BlockHeader{
			Bits:      initialBits,
			Timestamp: baseTime + i*uint32(T/2), // half the target time
		}
	}
	getAncestor := func(h uint32) *types.BlockHeader { return headers[h] }

	bits := r.CalcNextBits(headers[N], N, getAncestor, p)

	// Difficulty should increase (target should decrease).
	oldTarget := crypto.CompactToBig(initialBits)
	newTarget := crypto.CompactToBig(bits)
	if newTarget.Cmp(oldTarget) >= 0 {
		t.Fatalf("fast blocks: new target should be lower (harder), old=0x%08x new=0x%08x", initialBits, bits)
	}
}

func TestSlowBlocks(t *testing.T) {
	// When blocks arrive twice as slow as target, difficulty should decrease.
	r := New()
	N := uint32(20)
	T := 60 // seconds
	initialBits := uint32(0x1e0fffff)

	p := &params.ChainParams{
		InitialBits:        initialBits,
		MinBits:            0x1f7fffff,
		NoRetarget:         false,
		RetargetInterval:   N,
		TargetBlockSpacing: time.Duration(T) * time.Second,
	}

	baseTime := uint32(1700000000)
	headers := make(map[uint32]*types.BlockHeader)
	for i := uint32(0); i <= N; i++ {
		headers[i] = &types.BlockHeader{
			Bits:      initialBits,
			Timestamp: baseTime + i*uint32(T*2), // double the target time
		}
	}
	getAncestor := func(h uint32) *types.BlockHeader { return headers[h] }

	bits := r.CalcNextBits(headers[N], N, getAncestor, p)

	// Difficulty should decrease (target should increase).
	oldTarget := crypto.CompactToBig(initialBits)
	newTarget := crypto.CompactToBig(bits)
	if newTarget.Cmp(oldTarget) <= 0 {
		t.Fatalf("slow blocks: new target should be higher (easier), old=0x%08x new=0x%08x", initialBits, bits)
	}
}

func TestMinBitsClamp(t *testing.T) {
	// When blocks are extremely slow, target should be clamped at MinBits.
	r := New()
	N := uint32(10)
	T := 60
	initialBits := uint32(0x1e0fffff)
	minBits := uint32(0x1e0fffff) // same as initial — can't go easier

	p := &params.ChainParams{
		InitialBits:        initialBits,
		MinBits:            minBits,
		NoRetarget:         false,
		RetargetInterval:   N,
		TargetBlockSpacing: time.Duration(T) * time.Second,
	}

	baseTime := uint32(1700000000)
	headers := make(map[uint32]*types.BlockHeader)
	for i := uint32(0); i <= N; i++ {
		headers[i] = &types.BlockHeader{
			Bits:      initialBits,
			Timestamp: baseTime + i*uint32(T*100), // extremely slow
		}
	}
	getAncestor := func(h uint32) *types.BlockHeader { return headers[h] }

	bits := r.CalcNextBits(headers[N], N, getAncestor, p)

	maxTarget := crypto.CompactToBig(minBits)
	newTarget := crypto.CompactToBig(bits)
	if newTarget.Cmp(maxTarget) > 0 {
		t.Fatalf("min bits clamp: new target exceeds max, bits=0x%08x minBits=0x%08x", bits, minBits)
	}
}

func TestOutOfOrderTimestamps(t *testing.T) {
	// LWMA should handle out-of-order timestamps gracefully.
	r := New()
	N := uint32(10)
	T := 60
	initialBits := uint32(0x1e0fffff)

	p := &params.ChainParams{
		InitialBits:        initialBits,
		MinBits:            0x1f7fffff,
		NoRetarget:         false,
		RetargetInterval:   N,
		TargetBlockSpacing: time.Duration(T) * time.Second,
	}

	baseTime := uint32(1700000000)
	headers := make(map[uint32]*types.BlockHeader)
	for i := uint32(0); i <= N; i++ {
		ts := baseTime + i*uint32(T)
		// Introduce some out-of-order timestamps.
		if i == 5 {
			ts = baseTime + 3*uint32(T) // jump backwards
		}
		headers[i] = &types.BlockHeader{
			Bits:      initialBits,
			Timestamp: ts,
		}
	}
	getAncestor := func(h uint32) *types.BlockHeader { return headers[h] }

	// Should not panic.
	bits := r.CalcNextBits(headers[N], N, getAncestor, p)
	if bits == 0 {
		t.Fatal("out-of-order timestamps: got zero bits")
	}
}
