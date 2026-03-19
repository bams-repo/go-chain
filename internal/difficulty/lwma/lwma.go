package lwma

import (
	"math/big"
	"time"

	"github.com/bams-repo/fairchain/internal/crypto"
	"github.com/bams-repo/fairchain/internal/logging"
	"github.com/bams-repo/fairchain/internal/params"
	"github.com/bams-repo/fairchain/internal/types"
)

// Retargeter implements zawy12's LWMA-1 (Linearly Weighted Moving Average)
// difficulty adjustment algorithm. Unlike Bitcoin's epoch-based retarget,
// LWMA adjusts difficulty every block using a weighted moving average of
// recent solve times, giving higher weight to more recent blocks.
//
// This makes it far more responsive to hash rate changes — critical for
// small networks where miners can appear and disappear rapidly.
//
// Reference: https://github.com/zawy12/difficulty-algorithms/issues/3
// License: MIT (zawy12)
//
// The window size N is taken from ChainParams.RetargetInterval.
// The target solve time T is taken from ChainParams.TargetBlockSpacing.
type Retargeter struct{}

func New() *Retargeter { return &Retargeter{} }

func (r *Retargeter) Name() string { return "lwma" }

// CalcNextBits computes the compact target for the next block using LWMA-1.
//
// Algorithm (per zawy12's reference):
//
//	For each of the last N blocks, compute solve_time[i] = TS[i] - TS[i-1],
//	clamped to prevent timestamp manipulation. Weight each solve time by its
//	position (1..N), so recent blocks count more. The weighted sum L is:
//	  L = sum(i * clamp(solve_time[i], 1, 6*T))  for i in 1..N
//	The next target is:
//	  next_target = avg_target * (k / L)
//	where k = N*(N+1)*T/2 and avg_target is the arithmetic mean of the N
//	block targets in the window.
//
// LWMA adjusts every block, so RetargetInterval is used as the window size N,
// not as an adjustment frequency.
func (r *Retargeter) CalcNextBits(tip *types.BlockHeader, tipHeight uint32, getAncestor func(height uint32) *types.BlockHeader, p *params.ChainParams) uint32 {
	if p.NoRetarget {
		return p.InitialBits
	}

	N := p.RetargetInterval
	T := int64(p.TargetBlockSpacing / time.Second)

	// Not enough history yet — return initial difficulty.
	if tipHeight < N {
		return p.InitialBits
	}

	// Collect N+1 headers: from (tipHeight - N) through tipHeight.
	// We need N+1 timestamps to compute N solve times.
	windowStart := tipHeight - N
	headers := make([]*types.BlockHeader, N+1)
	for i := uint32(0); i <= N; i++ {
		h := getAncestor(windowStart + i)
		if h == nil {
			logging.L.Error("nil ancestor in LWMA window — possible data corruption",
				"component", "difficulty", "height", windowStart+i)
			return tip.Bits
		}
		headers[i] = h
	}

	// Compute the linearly-weighted sum of solve times and the sum of
	// targets across the window.
	//
	// L = sum(i * clamped_solvetime)  for i in 1..N
	// sumTarget += target[i]          for i in 1..N
	//
	// Solve times are clamped to [1, 6*T] per zawy12's recommendation to
	// prevent timestamp manipulation attacks. Out-of-order timestamps are
	// handled by enforcing a minimum solve time of 1 second.
	maxST := 6 * T
	var weightedSolveTimeSum int64
	sumTarget := new(big.Int)

	prevTS := int64(headers[0].Timestamp)
	for i := uint32(1); i <= N; i++ {
		thisTS := int64(headers[i].Timestamp)

		// Safely handle out-of-sequence timestamps.
		if thisTS <= prevTS {
			thisTS = prevTS + 1
		}

		solveTime := thisTS - prevTS
		if solveTime > maxST {
			solveTime = maxST
		}

		weightedSolveTimeSum += int64(i) * solveTime
		prevTS = thisTS

		blockTarget := crypto.CompactToBig(headers[i].Bits)
		sumTarget.Add(sumTarget, blockTarget)
	}

	// Floor: prevent the weighted sum from being unreasonably small, which
	// would cause an extreme difficulty spike. zawy12 uses N*N*T/20.
	minL := int64(N) * int64(N) * T / 20
	if weightedSolveTimeSum < minL {
		weightedSolveTimeSum = minL
	}

	// zawy12's reference computes in difficulty space:
	//   next_D = avg_D * k / L
	// where k = N*(N+1)*T/2.
	//
	// Since we work in target space (target = 1/difficulty), we invert:
	//   next_target = avg_target * L / k
	//              = (sumTarget / N) * L / (N*(N+1)*T/2)
	//              = sumTarget * 2 * L / (N * N * (N+1) * T)
	//
	// zawy12 applies a 99/100 factor to slightly overestimate difficulty
	// (underestimate target), which empirically reduces solve time variance
	// in small networks. In target space this becomes:
	//   next_target = sumTarget * 200 * L / (N * N * (N+1) * T * 99)

	nBig := int64(N)
	nPlus1 := int64(N + 1)
	numerator := new(big.Int).Mul(sumTarget, big.NewInt(200*weightedSolveTimeSum))
	denominator := big.NewInt(nBig * nBig * nPlus1 * T * 99)
	nextTarget := new(big.Int).Div(numerator, denominator)

	// Floor: target must be at least 1.
	if nextTarget.Sign() <= 0 {
		nextTarget.SetInt64(1)
	}

	// Clamp to minimum difficulty (maximum target).
	maxTarget := crypto.CompactToBig(p.MinBits)
	if nextTarget.Cmp(maxTarget) > 0 {
		nextTarget = maxTarget
	}

	return crypto.BigToCompact(nextTarget)
}
