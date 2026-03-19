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
func (e *Engine) ValidateHeader(header *types.BlockHeader, parent *types.BlockHeader, height uint32, getAncestor func(uint32) *types.BlockHeader, p *params.ChainParams) error {
	parentHash := crypto.HashBlockHeader(parent)
	if header.PrevBlock != parentHash {
		return fmt.Errorf("prev block hash mismatch: header=%s expected=%s", header.PrevBlock, parentHash)
	}

	expectedBits := e.CalcNextBits(parent, height-1, getAncestor, p)
	if header.Bits != expectedBits {
		return fmt.Errorf("incorrect difficulty bits at height %d: got 0x%08x, expected 0x%08x", height, header.Bits, expectedBits)
	}

	powHash := e.hasher.PoWHash(header.SerializeToBytes())
	if err := crypto.ValidateProofOfWork(powHash, header.Bits); err != nil {
		return fmt.Errorf("PoW validation failed at height %d: %w", height, err)
	}

	return nil
}

// ValidateBlock delegates to the shared structural validation.
func (e *Engine) ValidateBlock(block *types.Block, height uint32, p *params.ChainParams) error {
	return consensus.ValidateBlockStructure(block, height, p)
}

// CalcNextBits delegates difficulty computation to the injected Retargeter.
func (e *Engine) CalcNextBits(tip *types.BlockHeader, tipHeight uint32, getAncestor func(height uint32) *types.BlockHeader, p *params.ChainParams) uint32 {
	return e.retargeter.CalcNextBits(tip, tipHeight, getAncestor, p)
}

// PrepareHeader sets the difficulty bits on a new block header being built for mining.
func (e *Engine) PrepareHeader(header *types.BlockHeader, parent *types.BlockHeader, parentHeight uint32, getAncestor func(height uint32) *types.BlockHeader, p *params.ChainParams) error {
	header.Bits = e.CalcNextBits(parent, parentHeight, getAncestor, p)
	return nil
}

// SealHeader iterates the nonce to find a valid PoW solution.
// Returns true if found within maxIterations.
func (e *Engine) SealHeader(header *types.BlockHeader, target types.Hash, maxIterations uint64) (bool, error) {
	for i := uint64(0); i < maxIterations; i++ {
		hash := e.hasher.PoWHash(header.SerializeToBytes())
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

// MineGenesis mines a genesis block by iterating the nonce until the PoW hash
// is below the target defined by the block's Bits field.
func (e *Engine) MineGenesis(block *types.Block) error {
	merkle, err := crypto.ComputeMerkleRoot(block.Transactions)
	if err != nil {
		return fmt.Errorf("compute merkle root: %w", err)
	}
	block.Header.MerkleRoot = merkle

	target := crypto.CompactToHash(block.Header.Bits)

	for {
		hash := e.hasher.PoWHash(block.Header.SerializeToBytes())
		if hash.LessOrEqual(target) {
			return nil
		}
		block.Header.Nonce++
		if block.Header.Nonce == 0 {
			return fmt.Errorf("nonce space exhausted without finding valid genesis")
		}
	}
}
