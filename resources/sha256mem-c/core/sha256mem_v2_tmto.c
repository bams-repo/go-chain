/*
 * CPU TMTO sha256mem v2 — equivalent to opencl/sha256mem_v2_tmto_gpu.cl /
 * core/sha256mem_v2.c (linear reference).
 */

#include "sha256mem_v2_tmto.h"
#include "sha256_shani.h"
#include <string.h>

#define SLOTS           SHA256MEM_V2_SLOTS
#define SLOTS_MASK      (SLOTS - 1u)
#define HARDEN_IV       SHA256MEM_V2_HARDEN_INTERVAL
#define MIX_ROUNDS      SHA256MEM_V2_MIX_ROUNDS
#define HARDEN_THRESH   SHA256MEM_V2_HARDEN_THRESHOLD

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

AI void arx(uint32_t *dst, const uint32_t *src, uint32_t idx)
{
	for (int w = 0; w < 8; w++) {
		uint32_t v = src[w] ^ (idx + (uint32_t)w);
		v = (v << 13) | (v >> 19);
		v += src[w];
		dst[w] = v;
	}
}

AI void progression_harden(uint32_t *slot, uint32_t index)
{
	uint32_t selector = slot[index & 7u];
	if (((selector ^ index) & 255u) < HARDEN_THRESH) {
		sha256_32(slot, slot);
	} else {
		uint32_t next[8];
		arx(next, slot, index);
		memcpy(slot, next, 32);
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

	uint32_t cur[8];
	for (int w = 0; w < 8; w++)
		cur[w] = base_ptr[w];

	uint32_t start = (ci << 7) + 1u;
	uint32_t end = (ci << 7) + rem;
	for (uint32_t j = start; j <= end; j++) {
		uint32_t prev[8];
		for (int w = 0; w < 8; w++)
			prev[w] = cur[w];
		for (int w = 0; w < 8; w++)
			cur[w] = prev[w];
		progression_harden(cur, j);
	}
	for (int w = 0; w < 8; w++)
		out[w] = cur[w];
}

static void build_ck(uint32_t *ck, const uint32_t *seed, uint32_t *last_slot)
{
	uint32_t cur[8];
	for (int w = 0; w < 8; w++) {
		cur[w] = seed[w];
		ck[w] = seed[w];
	}

	for (uint32_t i = 1u; i < SLOTS; i++) {
		if ((i & 127u) == 0u) {
			uint32_t prev[8];
			for (int w = 0; w < 8; w++)
				prev[w] = cur[w];
			sha256_32(prev, cur);
			uint32_t o = (i >> 7) * 8u;
			for (int w = 0; w < 8; w++)
				ck[o + w] = cur[w];
		} else {
			uint32_t prev[8];
			for (int w = 0; w < 8; w++)
				prev[w] = cur[w];
			for (int w = 0; w < 8; w++)
				cur[w] = prev[w];
			progression_harden(cur, i);
		}
	}
	for (int w = 0; w < 8; w++)
		last_slot[w] = cur[w];
}

void sha256mem_v2_tmto_hash_with_scratch(const uint8_t *data, size_t len, uint8_t out[32], void *scratch)
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
	sha256_32(acc, fh);
	memcpy(out, fh, 32);
}
