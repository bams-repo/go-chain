package algorithms

import (
	"fmt"

	"github.com/bams-repo/fairchain/internal/algorithms/argon2id"
	"github.com/bams-repo/fairchain/internal/algorithms/scrypt"
	"github.com/bams-repo/fairchain/internal/algorithms/sha256d"
	"github.com/bams-repo/fairchain/internal/algorithms/sha256mem"
	"github.com/bams-repo/fairchain/internal/types"
)

// Hasher computes the proof-of-work hash for a serialized block header.
// Implementations must be deterministic: same input always produces same output.
// Implementations must be safe for concurrent use.
type Hasher interface {
	// PoWHash computes the proof-of-work hash of the given data.
	// For header validation, data is the canonical 80-byte serialized header.
	PoWHash(data []byte) types.Hash

	// Name returns the algorithm identifier (e.g., "sha256d", "argon2id").
	Name() string
}

// GetHasher returns a Hasher for the named algorithm.
// Adding a new algorithm requires a new sub-package and a new case here.
func GetHasher(name string) (Hasher, error) {
	switch name {
	case "sha256d":
		return sha256d.New(), nil
	case "argon2id":
		return argon2id.New(), nil
	case "scrypt":
		return scrypt.New(), nil
	case "sha256mem":
		return sha256mem.New(), nil
	default:
		return nil, fmt.Errorf("unknown PoW algorithm: %q", name)
	}
}
