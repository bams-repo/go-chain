// Copyright (c) 2024-2026 The Fairchain Contributors
// Fairchain is an experiment in modularity, designed to improve on the work
// of Satoshi Nakamoto and to inspire more creative genius in the space.
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

package pow

import (
	"fmt"
	"math/big"

	"github.com/bams-repo/fairchain/internal/algorithms"
	"github.com/bams-repo/fairchain/internal/consensus"
	"github.com/bams-repo/fairchain/internal/crypto"
	"github.com/bams-repo/fairchain/internal/difficulty"
	"github.com/bams-repo/fairchain/internal/params"
	"github.com/bams-repo/fairchain/internal/types"
)

// Engine implements the baseline Nakamoto-style proof-of-work consensus.
// The PoW hash algorithm is injected via the Hasher interface and the
// difficulty retargeting algorithm via the Retargeter interface, allowing
// both to be swapped without modifying consensus logic.
type Engine struct {
	hasher     algorithms.Hasher
	retargeter difficulty.Retargeter
}

var _ consensus.Engine = (*Engine)(nil)

func New(h algorithms.Hasher, r difficulty.Retargeter) *Engine {
	return &Engine{hasher: h, retargeter: r}
}

func (e *Engine) Name() string { return "pow" }

func (e *Engine) Hasher() algorithms.Hasher { return e.hasher }

func (e *Engine) CalcBlockWeight(header *types.BlockHeader) *big.Int {
	return crypto.CalcWork(header.Bits)
}

// ValidateHeader checks PoW-specific header rules:
//   - previous block hash matches parent (uses identity hash, always DoubleSHA256)
//   - bits match expected difficulty for this height
//   - PoW hash meets target (uses the configured algorithm)
//
// On networks with AllowMinDifficultyBlocks (testnet), a block may use MinBits
// if its timestamp exceeds the parent's by more than 2x the target block spacing.
// This matches Bitcoin Core's testnet difficulty reset rule and prevents
// difficulty death spirals when mining is intermittent.
func (e *Engine) ValidateHeader(header *types.BlockHeader, parent *types.BlockHeader, height uint32, getAncestor func(uint32) *types.BlockHeader, p *params.ChainParams) error {
	parentHash := crypto.HashBlockHeader(parent)
	if header.PrevBlock != parentHash {
		return fmt.Errorf("prev block hash mismatch: header=%s expected=%s", header.PrevBlock, parentHash)
	}

	expectedBits := e.calcExpectedBits(header, parent, height, getAncestor, p)
	if header.Bits != expectedBits {
		return fmt.Errorf("incorrect difficulty bits at height %d: got 0x%08x, expected 0x%08x", height, header.Bits, expectedBits)
	}

	var powBuf [types.BlockHeaderSize]byte
	header.SerializeInto(powBuf[:])
	powHash := e.hasher.PoWHash(powBuf[:])
	if err := crypto.ValidateProofOfWork(powHash, header.Bits); err != nil {
		return fmt.Errorf("PoW validation failed at height %d: %w", height, err)
	}

	return nil
}

// calcExpectedBits computes the expected difficulty bits for a block at the
// given height. On testnet (AllowMinDifficultyBlocks), this implements
// Bitcoin Core's min-difficulty reset: if the new block's timestamp is more
// than 2x the target spacing after the parent, MinBits is required. On
// non-retarget boundaries, if the parent used min-difficulty, we scan back
// to find the last block with real difficulty to prevent min-difficulty
// blocks from corrupting the next retarget calculation.
//
// Per-block algorithms (LWMA, DGW, DigiShield) always call CalcNextBits
// every block — the epoch-gating logic only applies to Bitcoin's epoch-based
// retarget where difficulty is constant between boundaries.
func (e *Engine) calcExpectedBits(header *types.BlockHeader, parent *types.BlockHeader, height uint32, getAncestor func(uint32) *types.BlockHeader, p *params.ChainParams) uint32 {
	activationHeight, hasActivation := p.ActivationHeights["mindiffblocks"]
	if !p.AllowMinDifficultyBlocks || !hasActivation || height < activationHeight {
		return e.retargeter.CalcNextBits(parent, height-1, getAncestor, p)
	}

	// Per-block retargeters adjust every block; only apply the min-diff
	// timestamp gap rule, then always compute fresh difficulty.
	if e.retargeter.Name() != "bitcoin" {
		minDiffGap := int64(p.TargetBlockSpacing.Seconds()) * 2
		if int64(header.Timestamp)-int64(parent.Timestamp) > minDiffGap {
			return p.MinBits
		}
		return e.retargeter.CalcNextBits(parent, height-1, getAncestor, p)
	}

	// Bitcoin epoch-based: min-diff gap check, then epoch-boundary retarget.
	minDiffGap := int64(p.TargetBlockSpacing.Seconds()) * 2
	if int64(header.Timestamp)-int64(parent.Timestamp) > minDiffGap {
		return p.MinBits
	}

	if height%p.RetargetInterval == 0 {
		return e.retargeter.CalcNextBits(parent, height-1, getAncestor, p)
	}

	// Non-retarget boundary: scan back past any min-difficulty blocks to
	// find the last block with real difficulty. This prevents a sequence
	// of min-difficulty blocks from artificially lowering difficulty at
	// the next retarget.
	scan := parent
	scanHeight := height - 1
	for scanHeight > 0 && scanHeight%p.RetargetInterval != 0 && scan.Bits == p.MinBits {
		scanHeight--
		scan = getAncestor(scanHeight)
		if scan == nil {
			break
		}
	}
	if scan != nil {
		return scan.Bits
	}
	return parent.Bits
}

// ValidateBlock delegates to the shared structural validation.
func (e *Engine) ValidateBlock(block *types.Block, height uint32, p *params.ChainParams) error {
	return consensus.ValidateBlockStructure(block, height, p, nil, nil)
}

// CalcNextBits delegates difficulty computation to the injected Retargeter.
func (e *Engine) CalcNextBits(tip *types.BlockHeader, tipHeight uint32, getAncestor func(height uint32) *types.BlockHeader, p *params.ChainParams) uint32 {
	return e.retargeter.CalcNextBits(tip, tipHeight, getAncestor, p)
}

// PrepareHeader sets the difficulty bits on a new block header being built for mining.
// The header's Timestamp must already be set before calling this method so that
// the testnet min-difficulty rule can be evaluated.
func (e *Engine) PrepareHeader(header *types.BlockHeader, parent *types.BlockHeader, parentHeight uint32, getAncestor func(height uint32) *types.BlockHeader, p *params.ChainParams) error {
	header.Bits = e.calcExpectedBits(header, parent, parentHeight+1, getAncestor, p)
	return nil
}

// SealHeader iterates the nonce to find a valid PoW solution.
// Returns true if found within maxIterations.
func (e *Engine) SealHeader(header *types.BlockHeader, target types.Hash, maxIterations uint64) (bool, error) {
	var hdrBuf [types.BlockHeaderSize]byte
	for i := uint64(0); i < maxIterations; i++ {
		header.SerializeInto(hdrBuf[:])
		hash := e.hasher.PoWHash(hdrBuf[:])
		if hash.LessOrEqual(target) {
			return true, nil
		}
		header.Nonce++
		if header.Nonce == 0 {
			return false, nil
		}
	}
	return false, nil
}

// SealHeaderCounted is like SealHeader but also returns the number of hashes
// actually computed, for accurate hashrate measurement.
func (e *Engine) SealHeaderCounted(header *types.BlockHeader, target types.Hash, maxIterations uint64) (found bool, hashes uint64, err error) {
	var hdrBuf [types.BlockHeaderSize]byte
	for i := uint64(0); i < maxIterations; i++ {
		header.SerializeInto(hdrBuf[:])
		hash := e.hasher.PoWHash(hdrBuf[:])
		if hash.LessOrEqual(target) {
			return true, i + 1, nil
		}
		header.Nonce++
		if header.Nonce == 0 {
			return false, i + 1, nil
		}
	}
	return false, maxIterations, nil
}

// MineGenesis mines a genesis block by iterating the nonce until the PoW hash
// is below the target defined by the block's Bits field.
func (e *Engine) MineGenesis(block *types.Block) error {
	merkle, err := crypto.ComputeMerkleRoot(block.Transactions)
	if err != nil {
		return fmt.Errorf("compute merkle root: %w", err)
	}
	block.Header.MerkleRoot = merkle

	target := crypto.CompactToHash(block.Header.Bits)

	var hdrBuf [types.BlockHeaderSize]byte
	for {
		block.Header.SerializeInto(hdrBuf[:])
		hash := e.hasher.PoWHash(hdrBuf[:])
		if hash.LessOrEqual(target) {
			return nil
		}
		block.Header.Nonce++
		if block.Header.Nonce == 0 {
			return fmt.Errorf("nonce space exhausted without finding valid genesis")
		}
	}
}
