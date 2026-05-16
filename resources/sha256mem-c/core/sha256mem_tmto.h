#ifndef SHA256MEM_TMTO_H
#define SHA256MEM_TMTO_H

#include <stddef.h>
#include <stdint.h>

/*
 * Time–memory tradeoff sha256mem (consensus-equivalent to linear sha256mem /
 * sha256mem_v4_tmto_gpu.cl). Scratch is 16384 checkpoints × 8 × uint32.
 */
#define SHA256MEM_TMTO_CHECKPOINTS   (2097152 / 128)
#define SHA256MEM_TMTO_SCRATCH_BYTES ((size_t)SHA256MEM_TMTO_CHECKPOINTS * 8u * sizeof(uint32_t))

void sha256mem_tmto_hash_with_scratch(const uint8_t *data, size_t len, uint8_t out[32], void *scratch);

#endif /* SHA256MEM_TMTO_H */
