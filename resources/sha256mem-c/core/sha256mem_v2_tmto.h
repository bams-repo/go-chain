#ifndef SHA256MEM_V2_TMTO_H
#define SHA256MEM_V2_TMTO_H

#include <stddef.h>
#include <stdint.h>

#include "sha256mem_v2.h"

/* Same checkpoint layout as v1 TMTO: 16384 × 8 × uint32 = 512 KiB. */
#define SHA256MEM_V2_TMTO_CHECKPOINTS   (SHA256MEM_V2_SLOTS / SHA256MEM_V2_HARDEN_INTERVAL)
#define SHA256MEM_V2_TMTO_SCRATCH_BYTES ((size_t)SHA256MEM_V2_TMTO_CHECKPOINTS * 8u * sizeof(uint32_t))

void sha256mem_v2_tmto_hash_with_scratch(const uint8_t *data, size_t len, uint8_t out[32], void *scratch);

#endif /* SHA256MEM_V2_TMTO_H */
