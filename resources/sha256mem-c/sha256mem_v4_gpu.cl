/*
 * sha256mem GPU kernel — optimized for maximum GPU throughput
 * ============================================================
 * Algorithm (improved v3):
 *   1. Seed:     SHA256(80-byte header) via midstate
 *   2. Fill:     Sequential dependent fill over 64 MiB:
 *                - Every 256 slots: SHA256(previous) -> anchor
 *                - Between anchors: ARX(previous, index) -> slot
 *   3. Mix:      2,048 rounds: SHA256(acc || mem[idx]) -> acc
 *   4. Finalize: SHA256(acc) -> PoW hash
 *
 * Copyright (c) 2024-2026 The Fairchain Contributors
 * Distributed under the MIT software license.
 */

#define SHA256MEM_SLOTS           2097152
#define SHA256MEM_HARDEN_INTERVAL 256
#define SHA256MEM_MIX_ROUNDS      32768

/* ── SHA256 core ─────────────────────────────────────────────────── */

#define ROTR(x, n) (((x) >> (n)) | ((x) << (32 - (n))))
#define Ch(x,y,z)  (((x) & (y)) ^ (~(x) & (z)))
#define Maj(x,y,z) (((x) & (y)) ^ ((x) & (z)) ^ ((y) & (z)))
#define Sigma0(x)  (ROTR(x,2)  ^ ROTR(x,13) ^ ROTR(x,22))
#define Sigma1(x)  (ROTR(x,6)  ^ ROTR(x,11) ^ ROTR(x,25))
#define sigma0(x)  (ROTR(x,7)  ^ ROTR(x,18) ^ ((x) >> 3))
#define sigma1(x)  (ROTR(x,17) ^ ROTR(x,19) ^ ((x) >> 10))

__constant uint SHA256_K[64] = {
    0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5,
    0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5,
    0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3,
    0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174,
    0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc,
    0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
    0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7,
    0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967,
    0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13,
    0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
    0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3,
    0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
    0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5,
    0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
    0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208,
    0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2
};

#define SHA256_IV0 0x6a09e667
#define SHA256_IV1 0xbb67ae85
#define SHA256_IV2 0x3c6ef372
#define SHA256_IV3 0xa54ff53a
#define SHA256_IV4 0x510e527f
#define SHA256_IV5 0x9b05688c
#define SHA256_IV6 0x1f83d9ab
#define SHA256_IV7 0x5be0cd19

inline void sha256_transform(uint *W, uint *st)
{
    for (int i = 16; i < 64; i++)
        W[i] = sigma1(W[i-2]) + W[i-7] + sigma0(W[i-15]) + W[i-16];

    uint a = st[0], b = st[1], c = st[2], d = st[3];
    uint e = st[4], f = st[5], g = st[6], h = st[7];

    for (int i = 0; i < 64; i++) {
        uint T1 = h + Sigma1(e) + Ch(e,f,g) + SHA256_K[i] + W[i];
        uint T2 = Sigma0(a) + Maj(a,b,c);
        h = g; g = f; f = e; e = d + T1;
        d = c; c = b; b = a; a = T1 + T2;
    }

    st[0] += a; st[1] += b; st[2] += c; st[3] += d;
    st[4] += e; st[5] += f; st[6] += g; st[7] += h;
}

inline uint bswap32(uint x) {
    return ((x & 0xFFu) << 24) | ((x & 0xFF00u) << 8) |
           ((x >> 8) & 0xFF00u) | ((x >> 24) & 0xFFu);
}

inline void sha256_32(const uint *in, uint *out)
{
    uint W[64];
    for (int i = 0; i < 8; i++) W[i] = bswap32(in[i]);
    W[8]  = 0x80000000u;
    for (int i = 9; i < 15; i++) W[i] = 0;
    W[15] = 256;

    uint st[8] = { SHA256_IV0, SHA256_IV1, SHA256_IV2, SHA256_IV3,
                   SHA256_IV4, SHA256_IV5, SHA256_IV6, SHA256_IV7 };
    sha256_transform(W, st);

    for (int i = 0; i < 8; i++) out[i] = bswap32(st[i]);
}

inline void sha256_64(const uint *in, uint *out)
{
    uint W[64];
    for (int i = 0; i < 16; i++) W[i] = bswap32(in[i]);

    uint st[8] = { SHA256_IV0, SHA256_IV1, SHA256_IV2, SHA256_IV3,
                   SHA256_IV4, SHA256_IV5, SHA256_IV6, SHA256_IV7 };
    sha256_transform(W, st);

    uint W2[64];
    W2[0] = 0x80000000u;
    for (int i = 1; i < 15; i++) W2[i] = 0;
    W2[15] = 512;
    sha256_transform(W2, st);

    for (int i = 0; i < 8; i++) out[i] = bswap32(st[i]);
}

inline void sha256_80_midstate(const uint *midstate, const uint *tail, uint *out)
{
    uint W[64];
    W[0] = tail[0]; W[1] = tail[1]; W[2] = tail[2]; W[3] = tail[3];
    W[4] = 0x80000000u;
    for (int i = 5; i < 15; i++) W[i] = 0;
    W[15] = 640;

    uint st[8];
    for (int i = 0; i < 8; i++) st[i] = midstate[i];
    sha256_transform(W, st);

    for (int i = 0; i < 8; i++) out[i] = bswap32(st[i]);
}

/* ── ARX fill step (matches Go arxFill) ──────────────────────────── */

inline void arx_fill(__global uint *dst, __global const uint *src, uint index)
{
    for (int w = 0; w < 8; w++) {
        uint v = src[w];
        v ^= index + (uint)w;
        v = (v << 13) | (v >> 19);
        v += src[w];
        dst[w] = v;
    }
}

/* ── Mining kernel ───────────────────────────────────────────────── */

__kernel void sha256mem_mine(
    __global const uint *g_midstate,
    __global const uint *g_tail,
    __global uint       *g_mem_pool,     /* SHA256MEM_SLOTS * 8 uints per worker */
    __global uint       *g_hash_counts,
    __global uint       *g_found_flag,
    __global uint       *g_found_nonce,
    __global uint       *g_found_hash,
    __global const uint *g_target,
    uint                 nonce_start,
    uint                 hashes_per_item
)
{
    uint gid = get_global_id(0);
    uint my_nonce = nonce_start + gid * hashes_per_item;

    __global uint *my_mem = g_mem_pool + (ulong)gid * SHA256MEM_SLOTS * 8;

    uint ms[8];
    for (int i = 0; i < 8; i++) ms[i] = g_midstate[i];

    uint tail[4];
    tail[0] = g_tail[0];
    tail[1] = g_tail[1];
    tail[2] = g_tail[2];

    uint count = 0;

    for (uint iter = 0; iter < hashes_per_item; iter++) {
        if (*g_found_flag) break;

        uint nonce = my_nonce + iter;
        tail[3] = ((nonce & 0xFFu) << 24) | ((nonce & 0xFF00u) << 8) |
                  ((nonce >> 8) & 0xFF00u) | ((nonce >> 24) & 0xFFu);

        /* Phase 1: Seed */
        uint seed[8];
        sha256_80_midstate(ms, tail, seed);

        /* Phase 2: Sequential dependent fill */
        for (int w = 0; w < 8; w++) my_mem[w] = seed[w];

        for (uint i = 1; i < SHA256MEM_SLOTS; i++) {
            __global uint *cur  = my_mem + i * 8;
            __global uint *prev = my_mem + (i - 1) * 8;

            if (i % SHA256MEM_HARDEN_INTERVAL == 0) {
                /* SHA256 hardening */
                uint prev_val[8];
                for (int w = 0; w < 8; w++) prev_val[w] = prev[w];
                uint hashed[8];
                sha256_32(prev_val, hashed);
                for (int w = 0; w < 8; w++) cur[w] = hashed[w];
            } else {
                arx_fill(cur, prev, i);
            }
        }

        /* Phase 3: SHA256-per-hop mix */
        __global uint *last_slot = my_mem + (SHA256MEM_SLOTS - 1) * 8;
        uint acc[8];
        for (int w = 0; w < 8; w++) acc[w] = last_slot[w];

        for (int r = 0; r < SHA256MEM_MIX_ROUNDS; r++) {
            uint idx = acc[0] % SHA256MEM_SLOTS;
            __global uint *slot = my_mem + idx * 8;

            uint buf[16];
            for (int w = 0; w < 8; w++) buf[w] = acc[w];
            for (int w = 0; w < 8; w++) buf[8 + w] = slot[w];

            sha256_64(buf, acc);
        }

        /* Phase 4: Finalize */
        uint final_hash[8];
        sha256_32(acc, final_hash);

        count++;

        /* Target check */
        int meets = 1;
        for (int w = 7; w >= 0; w--) {
            if (final_hash[w] < g_target[w]) break;
            if (final_hash[w] > g_target[w]) { meets = 0; break; }
        }

        if (meets) {
            uint old = atomic_cmpxchg(g_found_flag, 0u, 1u);
            if (old == 0) {
                *g_found_nonce = nonce;
                for (int w = 0; w < 8; w++)
                    g_found_hash[w] = final_hash[w];
            }
        }
    }

    g_hash_counts[gid] = count;
}

/* ── Validation kernel ───────────────────────────────────────────── */

__kernel void sha256mem_validate(
    __global const uint *g_midstate,
    __global const uint *g_tail,
    __global uint       *g_mem_pool,
    __global uint       *g_out_hash
)
{
    uint ms[8];
    for (int i = 0; i < 8; i++) ms[i] = g_midstate[i];

    uint tail[4];
    for (int i = 0; i < 4; i++) tail[i] = g_tail[i];

    uint seed[8];
    sha256_80_midstate(ms, tail, seed);

    for (int w = 0; w < 8; w++) g_mem_pool[w] = seed[w];

    for (uint i = 1; i < SHA256MEM_SLOTS; i++) {
        __global uint *cur  = g_mem_pool + i * 8;
        __global uint *prev = g_mem_pool + (i - 1) * 8;

        if (i % SHA256MEM_HARDEN_INTERVAL == 0) {
            uint prev_val[8];
            for (int w = 0; w < 8; w++) prev_val[w] = prev[w];
            uint hashed[8];
            sha256_32(prev_val, hashed);
            for (int w = 0; w < 8; w++) cur[w] = hashed[w];
        } else {
            arx_fill(cur, prev, i);
        }
    }

    __global uint *last_slot = g_mem_pool + (SHA256MEM_SLOTS - 1) * 8;
    uint acc[8];
    for (int w = 0; w < 8; w++) acc[w] = last_slot[w];

    for (int r = 0; r < SHA256MEM_MIX_ROUNDS; r++) {
        uint idx = acc[0] % SHA256MEM_SLOTS;
        __global uint *slot = g_mem_pool + idx * 8;

        uint buf[16];
        for (int w = 0; w < 8; w++) buf[w] = acc[w];
        for (int w = 0; w < 8; w++) buf[8 + w] = slot[w];

        sha256_64(buf, acc);
    }

    uint final_hash[8];
    sha256_32(acc, final_hash);

    for (int w = 0; w < 8; w++)
        g_out_hash[w] = final_hash[w];
}
