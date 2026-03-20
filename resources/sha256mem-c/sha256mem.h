#ifndef SHA256MEM_H
#define SHA256MEM_H

#include <stddef.h>
#include <stdint.h>

#define SHA256MEM_SLOTS      65536
#define SHA256MEM_MIX_ROUNDS 64

/*
 * sha256mem_hash computes the memory-hard SHA256 proof-of-work hash.
 *
 * Parameters:
 *   data   - input bytes (block header)
 *   len    - length of data in bytes
 *   out    - 32-byte output buffer for the resulting hash
 */
void sha256mem_hash(const uint8_t *data, size_t len, uint8_t out[32]);

#endif /* SHA256MEM_H */
