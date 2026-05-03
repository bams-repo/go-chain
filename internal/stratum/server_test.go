package stratum

import (
	"math"
	"testing"
)

func TestDecodeSubmitNonceUsesStandardStratumBigEndian(t *testing.T) {
	nonce, err := decodeUint32BE("00000aaf")
	if err != nil {
		t.Fatalf("decode nonce: %v", err)
	}
	if nonce != 2735 {
		t.Fatalf("nonce mismatch: got %d want 2735", nonce)
	}
}

func TestDifficultyToTargetUsesStandardStratumDifficulty(t *testing.T) {
	for _, diff := range []float64{1, 1e-7, 0.000001} {
		target := difficultyToTarget(diff)
		got := targetToDifficulty(target)
		if math.Abs(got-diff)/diff > 1e-12 {
			t.Fatalf("difficulty round trip for %g: got %g", diff, got)
		}
	}
}

func TestCalculateVardiffIncreasesWithObservedRate(t *testing.T) {
	got, changed := calculateVardiff(0.0001, 4.0, 0.0000001, 0, 0, 0)
	if !changed {
		t.Fatal("expected vardiff to change")
	}
	want := 0.0002
	if math.Abs(got-want)/want > 1e-12 {
		t.Fatalf("difficulty mismatch: got %g want %g", got, want)
	}
}

func TestCalculateVardiffDecreasesWithObservedRate(t *testing.T) {
	got, changed := calculateVardiff(0.0004, 0.25, 0.0000001, 0, 0, 0)
	if !changed {
		t.Fatal("expected vardiff to change")
	}
	want := 0.0002
	if math.Abs(got-want)/want > 1e-12 {
		t.Fatalf("difficulty mismatch: got %g want %g", got, want)
	}
}

func TestCalculateVardiffZeroSharesStepsDown(t *testing.T) {
	got, changed := calculateVardiff(0.0004, 0, 0.0000001, 0, 0, 0)
	if !changed {
		t.Fatal("expected vardiff to change")
	}
	want := 0.0002
	if math.Abs(got-want)/want > 1e-12 {
		t.Fatalf("difficulty mismatch: got %g want %g", got, want)
	}
}

func TestCalculateVardiffUsesExactRatioInsideStepLimit(t *testing.T) {
	got, changed := calculateVardiff(0.0001, 1.5, 0.0000001, 0, 0, 0)
	if !changed {
		t.Fatal("expected vardiff to change")
	}
	want := 0.00015
	if math.Abs(got-want)/want > 1e-12 {
		t.Fatalf("difficulty mismatch: got %g want %g", got, want)
	}
}

func TestCalculateVardiffDeadbandDoesNotAdjust(t *testing.T) {
	got, changed := calculateVardiff(0.0001, 1.1, 0.0000001, 0, 0, 0)
	if changed {
		t.Fatal("did not expect vardiff to change inside deadband")
	}
	if got != 0.0001 {
		t.Fatalf("difficulty mismatch: got %g want %g", got, 0.0001)
	}
}

func TestEstimateHashrateLWMAWeightsRecentSamples(t *testing.T) {
	got := estimateHashrateLWMA([]float64{10, 20, 30})
	want := float64(10*1+20*2+30*3) / float64(1+2+3)
	if math.Abs(got-want)/want > 1e-12 {
		t.Fatalf("hashrate mismatch: got %g want %g", got, want)
	}
}

func TestEstimateHashrateLWMAEmptySamples(t *testing.T) {
	if got := estimateHashrateLWMA(nil); got != 0 {
		t.Fatalf("hashrate mismatch: got %g want 0", got)
	}
}
