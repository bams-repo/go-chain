#include "sha256mem.h"
#include <openssl/sha.h>
#include <string.h>
#include <stdlib.h>

void sha256mem_hash(const uint8_t *data, size_t len, uint8_t out[32]) {
    /* Heap-allocate the 2 MiB buffer to avoid stack overflow. */
    uint8_t (*mem)[32] = malloc(SHA256MEM_SLOTS * 32);
    if (!mem) {
        memset(out, 0, 32);
        return;
    }

    /* Phase 1: Seed from input. */
    SHA256(data, len, mem[0]);

    /* Phase 2: Fill buffer with chained SHA256 hashes. */
    for (int i = 1; i < SHA256MEM_SLOTS; i++)
        SHA256(mem[i - 1], 32, mem[i]);

    /* Phase 3: Memory-hard mixing — data-dependent random reads. */
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
        SHA256(buf, 64, acc);
    }

    /* Phase 4: Finalize. */
    SHA256(acc, 32, out);

    free(mem);
}
