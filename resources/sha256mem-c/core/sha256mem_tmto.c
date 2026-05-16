/*
 * CPU TMTO sha256mem — consensus-equivalent to sha256mem_v4_tmto_gpu.cl /
 * internal/algorithms/sha256mem/sha256mem.go (linear reference).
 *
 * Seed + inner 32/64-byte SHA use Intel SHA-NI (build with -msha).
 */

#include "sha256mem_tmto.h"
#include "sha256_shani.h"
#include <string.h>

#define SLOTS           2097152u
#define SLOTS_MASK      (SLOTS - 1u)
#define HARDEN_IV       128u
#define MIX_ROUNDS      32768
#define CHECKPOINTS     (SLOTS / HARDEN_IV)

#define AI __attribute__((always_inline)) static inline

AI void sha256_32(const uint32_t *in, uint32_t *out)
{
	union {
		uint32_t w[8];
		uint8_t b[32];
	} u;
	memcpy(u.w, in, 32);
	uint8_t ob[32];
	sha256_shani(u.b, 32, ob);
	memcpy(out, ob, 32);
}

AI void sha256_64_from_u8(const uint8_t *buf64, uint32_t *out)
{
	uint8_t ob[32];
	sha256_shani(buf64, 64, ob);
	memcpy(out, ob, 32);
}

typedef union {
	uint32_t w[16];
	uint8_t b[64];
} sha_blk64;

static void sha256mem_powhash_le(const uint32_t *acc, uint32_t *out_le)
{
	uint32_t fh_raw[8];
	sha256_32(acc, fh_raw);
	uint8_t fb[32];
	for (int i = 0; i < 8; i++) {
		uint32_t w = fh_raw[i];
		fb[i * 4 + 0] = (uint8_t)(w & 0xFFu);
		fb[i * 4 + 1] = (uint8_t)((w >> 8) & 0xFFu);
		fb[i * 4 + 2] = (uint8_t)((w >> 16) & 0xFFu);
		fb[i * 4 + 3] = (uint8_t)((w >> 24) & 0xFFu);
	}
	uint8_t cons[32];
	for (int j = 0; j < 32; j++)
		cons[j] = fb[31 - j];
	for (int i = 0; i < 8; i++)
		out_le[i] = (uint32_t)cons[i * 4] | ((uint32_t)cons[i * 4 + 1] << 8) |
			    ((uint32_t)cons[i * 4 + 2] << 16) | ((uint32_t)cons[i * 4 + 3] << 24);
}

AI void arx(uint32_t *dst, const uint32_t *src, uint32_t idx)
{
	for (int w = 0; w < 8; w++) {
		uint32_t v = src[w] ^ (idx + (uint32_t)w);
		v = (v << 13) | (v >> 19);
		v += src[w];
		dst[w] = v;
	}
}

AI void slot_at(const uint32_t *ck, uint32_t idx, uint32_t *out)
{
	uint32_t ci = idx >> 7;
	uint32_t rem = idx & 127u;
	const uint32_t *base_ptr = ck + ci * 8u;

	if (rem == 0u) {
		for (int w = 0; w < 8; w++)
			out[w] = base_ptr[w];
		return;
	}

	uint32_t buf[2][8];
	int cur = 0;
	for (int w = 0; w < 8; w++)
		buf[cur][w] = base_ptr[w];

	uint32_t start = (ci << 7) + 1u;
	uint32_t end = (ci << 7) + rem;
	for (uint32_t j = start; j <= end; j++) {
		int nxt = 1 - cur;
		arx(buf[nxt], buf[cur], j);
		cur = nxt;
	}
	for (int w = 0; w < 8; w++)
		out[w] = buf[cur][w];
}

static void build_ck(uint32_t *ck, const uint32_t *seed, uint32_t *last_slot)
{
	uint32_t buf[2][8];
	int cur = 0;
	for (int w = 0; w < 8; w++) {
		buf[cur][w] = seed[w];
		ck[w] = seed[w];
	}

	for (uint32_t i = 1u; i < SLOTS; i++) {
		if ((i & 127u) == 0u) {
			int nxt = 1 - cur;
			sha256_32(buf[cur], buf[nxt]);
			cur = nxt;
			uint32_t o = (i >> 7) * 8u;
			for (int w = 0; w < 8; w++)
				ck[o + w] = buf[cur][w];
		} else {
			int nxt = 1 - cur;
			arx(buf[nxt], buf[cur], i);
			cur = nxt;
		}
	}
	for (int w = 0; w < 8; w++)
		last_slot[w] = buf[cur][w];
}

void sha256mem_tmto_hash_with_scratch(const uint8_t *data, size_t len, uint8_t out[32], void *scratch)
{
	uint32_t *ck = (uint32_t *)scratch;
	uint8_t digest[32];
	sha256_shani(data, len, digest);

	uint32_t seed[8];
	memcpy(seed, digest, 32);

	uint32_t acc[8];
	build_ck(ck, seed, acc);

	sha_blk64 blk;

	for (int r = 0; r < MIX_ROUNDS; r++) {
		uint32_t idx = acc[0] & SLOTS_MASK;
		uint32_t sv[8];
		slot_at(ck, idx, sv);
		for (int w = 0; w < 8; w++) {
			blk.w[w] = acc[w];
			blk.w[8 + w] = sv[w];
		}
		sha256_64_from_u8(blk.b, acc);
	}
	for (int r = 0; r < MIX_ROUNDS; r++) {
		uint32_t idx = acc[r % 7] & SLOTS_MASK;
		uint32_t sv[8];
		slot_at(ck, idx, sv);
		for (int w = 0; w < 8; w++) {
			blk.w[w] = acc[w];
			blk.w[8 + w] = sv[w];
		}
		sha256_64_from_u8(blk.b, acc);
	}

	uint32_t fh[8];
	sha256mem_powhash_le(acc, fh);
	memcpy(out, fh, 32);
}
