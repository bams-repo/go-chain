package difficulty

import (
	"fmt"

	"github.com/bams-repo/fairchain/internal/difficulty/bitcoin"
	"github.com/bams-repo/fairchain/internal/difficulty/lwma"
	"github.com/bams-repo/fairchain/internal/params"
	"github.com/bams-repo/fairchain/internal/types"
)

// Retargeter computes the next difficulty target for a blockchain.
// Implementations must be deterministic: same chain state always produces the
// same result. Implementations must be safe for concurrent use.
type Retargeter interface {
	// CalcNextBits computes the compact target (nBits) for the next block
	// given the current tip, its height, an ancestor lookup function, and
	// the chain parameters.
	CalcNextBits(tip *types.BlockHeader, tipHeight uint32, getAncestor func(height uint32) *types.BlockHeader, p *params.ChainParams) uint32

	// Name returns the algorithm identifier (e.g., "bitcoin", "lwma", "digishield").
	Name() string
}

// GetRetargeter returns a Retargeter for the named difficulty algorithm.
// Adding a new algorithm requires a new sub-package and a new case here.
func GetRetargeter(name string) (Retargeter, error) {
	switch name {
	case "bitcoin":
		return bitcoin.New(), nil
	case "lwma":
		return lwma.New(), nil
	default:
		return nil, fmt.Errorf("unknown difficulty algorithm: %q", name)
	}
}
