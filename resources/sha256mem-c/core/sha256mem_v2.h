#ifndef SHA256MEM_V2_H
#define SHA256MEM_V2_H

#include <stddef.h>
#include <stdint.h>

/* sha256mem v2 parameters (experimental; not consensus until activated in Go). */
#define SHA256MEM_V2_SLOTS              2097152
#define SHA256MEM_V2_HARDEN_INTERVAL    128
#define SHA256MEM_V2_MIX_ROUNDS         16384
#define SHA256MEM_V2_HARDEN_THRESHOLD   3

#define SHA256MEM_V2_SCRATCH_BYTES      ((size_t)SHA256MEM_V2_SLOTS * 32)

void sha256mem_v2_hash(const uint8_t *data, size_t len, uint8_t out[32]);

#endif /* SHA256MEM_V2_H */
