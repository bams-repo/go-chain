package argon2id

import (
	"golang.org/x/crypto/argon2"

	"github.com/bams-repo/fairchain/internal/types"
)

// Consensus-critical Argon2id parameters. Changing any of these is a hard fork.
const (
	TimeCost    = 1   // single pass — fast enough for block validation
	MemoryCost  = 256 // 256 KiB — light for validation, heavy enough for ASIC resistance
	Parallelism = 1   // single-threaded for determinism
	KeyLen      = 32  // 256-bit output to match types.Hash
)

// Hasher implements Argon2id proof-of-work hashing.
// Uses Argon2id (RFC 9106 recommended hybrid) which combines data-independent
// memory access (ASIC resistance) with data-dependent access (GPU resistance).
type Hasher struct{}

func New() *Hasher { return &Hasher{} }

func (h *Hasher) PoWHash(data []byte) types.Hash {
	// Salt is the input data itself — each header is unique due to prevblock+nonce+timestamp.
	out := argon2.IDKey(data, data, TimeCost, MemoryCost, Parallelism, KeyLen)
	return types.HashFromBytes(out)
}

func (h *Hasher) Name() string { return "argon2id" }
