// Copyright (c) 2024-2026 The Fairchain Contributors
// Fairchain is an experiment in modularity, designed to improve on the work
// of Satoshi Nakamoto and to inspire more creative genius in the space.
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

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
//	clamped to [-6*T, 6*T] to limit timestamp manipulation. Weight each
//	solve time by its position (1..N), so recent blocks count more:
//	  L = sum(i * clamp(solve_time[i], -6*T, 6*T))  for i in 1..N
//	The next target is:
//	  next_target = avg_target * L / k
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
	// Solve times are clamped to [-6*T, 6*T] per zawy12's recommendation.
	// Negative solve times from out-of-order timestamps are allowed so they
	// properly reduce the weighted sum (raising difficulty).
	maxST := 6 * T
	minST := -6 * T
	var weightedSolveTimeSum int64
	sumTarget := new(big.Int)

	for i := uint32(1); i <= N; i++ {
		solveTime := int64(headers[i].Timestamp) - int64(headers[i-1].Timestamp)
		if solveTime > maxST {
			solveTime = maxST
		}
		if solveTime < minST {
			solveTime = minST
		}

		weightedSolveTimeSum += int64(i) * solveTime

		blockTarget := crypto.CompactToBig(headers[i].Bits)
		sumTarget.Add(sumTarget, blockTarget)
	}

	// Floor: prevent the weighted sum from being unreasonably small, which
	// would cause an extreme difficulty spike. zawy12 uses N*N*T/20
	// (limits difficulty increase to ~10x per window).
	minL := int64(N) * int64(N) * T / 20
	if weightedSolveTimeSum < minL {
		weightedSolveTimeSum = minL
	}

	// zawy12's reference computes in difficulty space:
	//   next_D = avg_D * k / L
	// where k = N*(N+1)*T/2.
	//
	// In target space (target = 1/difficulty):
	//   next_target = avg_target * L / k
	//              = (sumTarget / N) * L / (N*(N+1)*T/2)
	//              = sumTarget * 2 * L / (N * N * (N+1) * T)
	//
	// zawy12's adjust=0.99 slightly overestimates difficulty to reduce
	// solve time variance. In target space: multiply L by 0.99, giving
	//   next_target = sumTarget * 198 * L / (N * N * (N+1) * T * 100)
	nBig := int64(N)
	nPlus1 := int64(N + 1)
	numerator := new(big.Int).Mul(sumTarget, big.NewInt(198*weightedSolveTimeSum))
	denominator := big.NewInt(nBig * nBig * nPlus1 * T * 100)
	nextTarget := new(big.Int).Div(numerator, denominator)

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
