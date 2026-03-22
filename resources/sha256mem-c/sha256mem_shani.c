#include "sha256mem.h"
#include "sha256_shani.h"
#include <stdlib.h>

void sha256mem_hash(const uint8_t *data, size_t len, uint8_t out[32]) {
    uint8_t (*mem)[32] = malloc(SHA256MEM_SLOTS * 32);
    if (!mem) {
        memset(out, 0, 32);
        return;
    }

    /* Phase 1: Seed. */
    sha256_shani(data, len, mem[0]);

    /* Phase 2: Fill. */
    for (int i = 1; i < SHA256MEM_SLOTS; i++)
        sha256_shani(mem[i - 1], 32, mem[i]);

    /* Phase 3: Mix. */
    uint8_t acc[32];
    memcpy(acc, mem[SHA256MEM_SLOTS - 1], 32);

    for (int i = 0; i < SHA256MEM_MIX_ROUNDS; i++) {
        uint32_t idx;
        memcpy(&idx, acc, 4);
        idx %= SHA256MEM_SLOTS;
        for (int hop = 0; hop < SHA256MEM_CHASE_DEPTH; hop++) {
            memcpy(&idx, mem[idx], 4);
            idx %= SHA256MEM_SLOTS;
        }

        uint8_t buf[64];
        memcpy(buf, acc, 32);
        memcpy(buf + 32, mem[idx], 32);
        sha256_shani(buf, 64, acc);
    }

    /* Phase 4: Finalize. */
    sha256_shani(acc, 32, out);

    free(mem);
}
