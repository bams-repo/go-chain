package sha256d

import (
	"crypto/sha256"

	"github.com/bams-repo/fairchain/internal/types"
)

// Hasher implements DoubleSHA256 proof-of-work hashing (Bitcoin-parity default).
type Hasher struct{}

func New() *Hasher { return &Hasher{} }

func (h *Hasher) PoWHash(data []byte) types.Hash {
	first := sha256.Sum256(data)
	second := sha256.Sum256(first[:])
	return types.HashFromBytes(second[:])
}

func (h *Hasher) Name() string { return "sha256d" }
