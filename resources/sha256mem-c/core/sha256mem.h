#ifndef SHA256MEM_H
#define SHA256MEM_H

#include <stddef.h>
#include <stdint.h>

/* Consensus parameters — must match internal/algorithms/sha256mem/sha256mem.go */
#define SHA256MEM_SLOTS             2097152
#define SHA256MEM_HARDEN_INTERVAL   128
#define SHA256MEM_MIX_ROUNDS        32768

/* Scratch buffer size for sha256mem_hash_with_scratch (2097152 × 32 bytes). */
#define SHA256MEM_SCRATCH_BYTES     ((size_t)SHA256MEM_SLOTS * 32)

/*
 * sha256mem_hash computes the memory-hard SHA256 proof-of-work hash.
 *
 * Parameters:
 *   data   - input bytes (block header)
 *   len    - length of data in bytes
 *   out    - 32-byte output buffer for the resulting hash
 */
void sha256mem_hash(const uint8_t *data, size_t len, uint8_t out[32]);

/* Same as sha256mem_hash but reuses scratch (SHA256MEM_SCRATCH_BYTES). Not thread-safe per scratch. */
void sha256mem_hash_with_scratch(const uint8_t *data, size_t len, uint8_t out[32], void *scratch);

#endif /* SHA256MEM_H */
