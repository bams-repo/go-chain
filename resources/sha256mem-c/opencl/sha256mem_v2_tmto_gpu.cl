/*
 * sha256mem v2 GPU kernel — TMTO (checkpoint) variant
 * ====================================================
 * Equivalent to core/sha256mem_v2.c / sha256mem_v2_tmto.c.
 * Progression harden (threshold 3) between checkpoints; 16384 mix rounds × 2.
 *
 * Key optimisations over the naive TMTO kernel:
 *   - SHA256 W-schedule uses only 16 registers (circular), not 64
 *   - bswap avoided by operating in BE throughout sha256; LE<->BE at boundary
 *   - slot_at fast-path for checkpoint-aligned indices
 *   - build_checkpoints returns last slot directly
 *   - rotate() intrinsic for arx_fill
 *
 * Copyright (c) 2024-2026 The Fairchain Contributors
 * Distributed under the MIT software license.
 */

#define SLOTS           2097152u
#define SLOTS_MASK      (SLOTS - 1u)
#define HARDEN_IV       128u
#define MIX_ROUNDS      16384
#define HARDEN_THRESH   3u
#define CHECKPOINTS     (SLOTS / HARDEN_IV)

/* ── SHA256 with 16-word circular W schedule ──────────────────────── */

#define RR(x,n) rotate((x),(uint)(32-(n)))
#define Ch(x,y,z)  (((x)&(y))^(~(x)&(z)))
#define Ma(x,y,z)  (((x)&(y))^((x)&(z))^((y)&(z)))
#define E0(x) (RR(x,2)^RR(x,13)^RR(x,22))
#define E1(x) (RR(x,6)^RR(x,11)^RR(x,25))
#define s0(x) (RR(x,7)^RR(x,18)^((x)>>3))
#define s1(x) (RR(x,17)^RR(x,19)^((x)>>10))

__constant uint K[64] = {
    0x428a2f98,0x71374491,0xb5c0fbcf,0xe9b5dba5,
    0x3956c25b,0x59f111f1,0x923f82a4,0xab1c5ed5,
    0xd807aa98,0x12835b01,0x243185be,0x550c7dc3,
    0x72be5d74,0x80deb1fe,0x9bdc06a7,0xc19bf174,
    0xe49b69c1,0xefbe4786,0x0fc19dc6,0x240ca1cc,
    0x2de92c6f,0x4a7484aa,0x5cb0a9dc,0x76f988da,
    0x983e5152,0xa831c66d,0xb00327c8,0xbf597fc7,
    0xc6e00bf3,0xd5a79147,0x06ca6351,0x14292967,
    0x27b70a85,0x2e1b2138,0x4d2c6dfc,0x53380d13,
    0x650a7354,0x766a0abb,0x81c2c92e,0x92722c85,
    0xa2bfe8a1,0xa81a664b,0xc24b8b70,0xc76c51a3,
    0xd192e819,0xd6990624,0xf40e3585,0x106aa070,
    0x19a4c116,0x1e376c08,0x2748774c,0x34b0bcb5,
    0x391c0cb3,0x4ed8aa4a,0x5b9cca4f,0x682e6ff3,
    0x748f82ee,0x78a5636f,0x84c87814,0x8cc70208,
    0x90befffa,0xa4506ceb,0xbef9a3f7,0xc67178f2
};

#define IV0 0x6a09e667u
#define IV1 0xbb67ae85u
#define IV2 0x3c6ef372u
#define IV3 0xa54ff53au
#define IV4 0x510e527fu
#define IV5 0x9b05688cu
#define IV6 0x1f83d9abu
#define IV7 0x5be0cd19u

inline uint bs(uint x) {
    return ((x&0xFFu)<<24)|((x&0xFF00u)<<8)|((x>>8)&0xFF00u)|((x>>24)&0xFFu);
}

/*
 * SHA256 compress: W[16] already loaded (big-endian).
 * Uses circular indexing so only 16 W words live at once.
 */
inline void compress(uint *W, uint *st)
{
    uint a=st[0],b=st[1],c=st[2],d=st[3];
    uint e=st[4],f=st[5],g=st[6],h=st[7];

    #pragma unroll
    for (int i = 0; i < 64; i++) {
        if (i >= 16)
            W[i&15] += s1(W[(i-2)&15]) + W[(i-7)&15] + s0(W[(i-15)&15]);
        uint T1 = h + E1(e) + Ch(e,f,g) + K[i] + W[i&15];
        uint T2 = E0(a) + Ma(a,b,c);
        h=g; g=f; f=e; e=d+T1; d=c; c=b; b=a; a=T1+T2;
    }
    st[0]+=a; st[1]+=b; st[2]+=c; st[3]+=d;
    st[4]+=e; st[5]+=f; st[6]+=g; st[7]+=h;
}

/* SHA256(32 bytes native LE) -> 32 bytes native LE */
inline void sha256_32(const uint *in, uint *out)
{
    uint W[16];
    #pragma unroll
    for (int i=0;i<8;i++) W[i]=bs(in[i]);
    W[8]=0x80000000u;
    W[9]=0; W[10]=0; W[11]=0; W[12]=0; W[13]=0; W[14]=0; W[15]=256u;

    uint st[8]={IV0,IV1,IV2,IV3,IV4,IV5,IV6,IV7};
    compress(W,st);
    #pragma unroll
    for (int i=0;i<8;i++) out[i]=bs(st[i]);
}

/*
 * Final PoW digest: SHA256(acc) then reverse all 32 bytes (matches Go
 * types.Hash.Reversed() in sha256mem.PoWHash and sha256mem.c finalize).
 */
inline void sha256mem_powhash_le(const uint *acc, uint *out_le)
{
    uint fh_raw[8];
    sha256_32(acc, fh_raw);
    uchar fb[32];
    for (int i = 0; i < 8; i++) {
        uint w = fh_raw[i];
        fb[i*4+0] = (uchar)(w & 0xFFu);
        fb[i*4+1] = (uchar)((w >> 8) & 0xFFu);
        fb[i*4+2] = (uchar)((w >> 16) & 0xFFu);
        fb[i*4+3] = (uchar)((w >> 24) & 0xFFu);
    }
    uchar cons[32];
    for (int j = 0; j < 32; j++)
        cons[j] = fb[31 - j];
    for (int i = 0; i < 8; i++)
        out_le[i] = (uint)cons[i*4] | ((uint)cons[i*4+1] << 8) |
                    ((uint)cons[i*4+2] << 16) | ((uint)cons[i*4+3] << 24);
}

/* SHA256(64 bytes native LE) -> 32 bytes native LE */
inline void sha256_64(const uint *in, uint *out)
{
    uint W[16];
    #pragma unroll
    for (int i=0;i<16;i++) W[i]=bs(in[i]);

    uint st[8]={IV0,IV1,IV2,IV3,IV4,IV5,IV6,IV7};
    compress(W,st);

    W[0]=0x80000000u;
    #pragma unroll
    for (int i=1;i<15;i++) W[i]=0;
    W[15]=512u;
    compress(W,st);

    #pragma unroll
    for (int i=0;i<8;i++) out[i]=bs(st[i]);
}

/* SHA256(80-byte header) using precomputed midstate for first 64 bytes */
inline void sha256_80_ms(const uint *mid, const uint *tail, uint *out)
{
    uint W[16];
    W[0]=tail[0]; W[1]=tail[1]; W[2]=tail[2]; W[3]=tail[3];
    W[4]=0x80000000u;
    #pragma unroll
    for (int i=5;i<15;i++) W[i]=0;
    W[15]=640u;

    uint st[8];
    #pragma unroll
    for (int i=0;i<8;i++) st[i]=mid[i];
    compress(W,st);
    #pragma unroll
    for (int i=0;i<8;i++) out[i]=bs(st[i]);
}

/* ── ARX fill (private registers) ─────────────────────────────────── */
inline void arx(uint *dst, const uint *src, uint idx)
{
    #pragma unroll
    for (int w=0;w<8;w++) {
        uint v = src[w] ^ (idx+(uint)w);
        v = rotate(v, 13u) + src[w];
        dst[w] = v;
    }
}

inline void progression_harden(uint *slot, uint idx)
{
    uint selector = slot[idx & 7u];
    if (((selector ^ idx) & 255u) < HARDEN_THRESH) {
        sha256_32(slot, slot);
    } else {
        uint next[8];
        arx(next, slot, idx);
        #pragma unroll
        for (int w=0;w<8;w++) slot[w] = next[w];
    }
}

/* ── Reconstruct mem[idx] from checkpoint table ───────────────────── */
inline void slot_at(__global const uint *ck, uint idx, uint *out)
{
    uint ci = idx >> 7;
    uint rem = idx & 127u;
    __global const uint *base_ptr = ck + ci * 8u;

    if (rem == 0u) {
        #pragma unroll
        for (int w=0;w<8;w++) out[w] = base_ptr[w];
        return;
    }

    uint cur[8];
    #pragma unroll
    for (int w=0;w<8;w++) cur[w] = base_ptr[w];

    uint start = (ci << 7) + 1u;
    uint end   = (ci << 7) + rem;
    for (uint j = start; j <= end; j++) {
        uint prev[8];
        #pragma unroll
        for (int w=0;w<8;w++) prev[w] = cur[w];
        #pragma unroll
        for (int w=0;w<8;w++) cur[w] = prev[w];
        progression_harden(cur, j);
    }
    #pragma unroll
    for (int w=0;w<8;w++) out[w] = cur[w];
}

/*
 * Build checkpoint table. Returns last slot (mem[SLOTS-1]) in last_slot.
 * SHA256 every 128 slots; progression harden between checkpoints.
 */
inline void build_ck(__global uint *ck, const uint *seed, uint *last_slot)
{
    uint cur[8];
    #pragma unroll
    for (int w=0;w<8;w++) { cur[w]=seed[w]; ck[w]=seed[w]; }

    for (uint i=1u; i<SLOTS; i++) {
        if ((i & 127u)==0u) {
            uint prev[8];
            #pragma unroll
            for (int w=0;w<8;w++) prev[w]=cur[w];
            sha256_32(prev,cur);
            uint o=(i>>7)*8u;
            #pragma unroll
            for (int w=0;w<8;w++) ck[o+w]=cur[w];
        } else {
            uint prev[8];
            #pragma unroll
            for (int w=0;w<8;w++) prev[w]=cur[w];
            #pragma unroll
            for (int w=0;w<8;w++) cur[w]=prev[w];
            progression_harden(cur, i);
        }
    }
    #pragma unroll
    for (int w=0;w<8;w++) last_slot[w]=cur[w];
}

/* ══════════════════════════════════════════════════════════════════ */
/*                         MINING KERNEL                             */
/* ══════════════════════════════════════════════════════════════════ */

__kernel void sha256mem_mine(
    __global const uint *g_midstate,
    __global const uint *g_tail,
    __global uint       *g_ck_pool,
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
    __global uint *ck = g_ck_pool + (ulong)gid * (uint)CHECKPOINTS * 8u;

    uint ms[8];
    #pragma unroll
    for (int i=0;i<8;i++) ms[i]=g_midstate[i];

    uint tail[4];
    tail[0]=g_tail[0]; tail[1]=g_tail[1]; tail[2]=g_tail[2];

    uint count = 0u;

    for (uint iter=0u; iter < hashes_per_item; iter++) {
        if (*g_found_flag) break;

        uint nonce = my_nonce + iter;
        tail[3] = bs(nonce);

        uint seed[8];
        sha256_80_ms(ms, tail, seed);
        count++;

#ifdef POOL_MODE
        /* Pool mining: check SHA256(header) seed against share target.
         * The pool validates shares using single SHA256 of the block header
         * (the "seed" hash that feeds into sha256mem's memory-hard phase).
         * seed[i] = bs(st[i]) from sha256_80_ms.
         * Compare st[i] = bs(seed[i]) against g_target[i] (BE uint32 words). */
        int meets=1;
        for (int w=0;w<8;w++) {
            uint hw = bs(seed[w]);
            if (hw < g_target[w]) break;
            if (hw > g_target[w]) { meets=0; break; }
        }
        if (meets) {
            uint old = atomic_cmpxchg(g_found_flag, 0u, 1u);
            if (old==0u) {
                *g_found_nonce = nonce;
                #pragma unroll
                for (int w=0;w<8;w++) g_found_hash[w]=seed[w];
            }
        }
#else
        uint acc[8];
        build_ck(ck, seed, acc);

        /* Mix pass A */
        uint buf[16];
        for (int r=0; r<MIX_ROUNDS; r++) {
            uint idx = acc[0] & SLOTS_MASK;
            uint sv[8]; slot_at(ck, idx, sv);
            #pragma unroll
            for (int w=0;w<8;w++) buf[w]=acc[w];
            #pragma unroll
            for (int w=0;w<8;w++) buf[8+w]=sv[w];
            sha256_64(buf, acc);
        }
        /* Mix pass B */
        for (int r=0; r<MIX_ROUNDS; r++) {
            uint idx = acc[r%7] & SLOTS_MASK;
            uint sv[8]; slot_at(ck, idx, sv);
            #pragma unroll
            for (int w=0;w<8;w++) buf[w]=acc[w];
            #pragma unroll
            for (int w=0;w<8;w++) buf[8+w]=sv[w];
            sha256_64(buf, acc);
        }

        uint fh[8];
        sha256_32(acc, fh);

        int meets=1;
        for (int w=0;w<8;w++) {
            uint hw = bs(fh[w]);
            if (hw<g_target[w]) break;
            if (hw>g_target[w]) { meets=0; break; }
        }
        if (meets) {
            uint old = atomic_cmpxchg(g_found_flag, 0u, 1u);
            if (old==0u) {
                *g_found_nonce = nonce;
                #pragma unroll
                for (int w=0;w<8;w++) g_found_hash[w]=fh[w];
            }
        }
#endif
    }
    g_hash_counts[gid] = count;
}

/* ══════════════════════════════════════════════════════════════════ */
/*                      VALIDATION KERNEL                            */
/* ══════════════════════════════════════════════════════════════════ */

__kernel void sha256mem_validate(
    __global const uint *g_midstate,
    __global const uint *g_tail,
    __global uint       *g_ck_pool,
    __global uint       *g_out_hash
)
{
    __global uint *ck = g_ck_pool;

    uint ms[8];
    #pragma unroll
    for (int i=0;i<8;i++) ms[i]=g_midstate[i];

    uint tail[4];
    #pragma unroll
    for (int i=0;i<4;i++) tail[i]=g_tail[i];

    uint seed[8];
    sha256_80_ms(ms, tail, seed);

    uint acc[8];
    build_ck(ck, seed, acc);

    uint buf[16];
    for (int r=0; r<MIX_ROUNDS; r++) {
        uint idx = acc[0] & SLOTS_MASK;
        uint sv[8]; slot_at(ck, idx, sv);
        #pragma unroll
        for (int w=0;w<8;w++) buf[w]=acc[w];
        #pragma unroll
        for (int w=0;w<8;w++) buf[8+w]=sv[w];
        sha256_64(buf, acc);
    }
    for (int r=0; r<MIX_ROUNDS; r++) {
        uint idx = acc[r%7] & SLOTS_MASK;
        uint sv[8]; slot_at(ck, idx, sv);
        #pragma unroll
        for (int w=0;w<8;w++) buf[w]=acc[w];
        #pragma unroll
        for (int w=0;w<8;w++) buf[8+w]=sv[w];
        sha256_64(buf, acc);
    }

    uint fh[8];
    sha256_32(acc, fh);
    #pragma unroll
    for (int w=0;w<8;w++) g_out_hash[w]=fh[w];
}
