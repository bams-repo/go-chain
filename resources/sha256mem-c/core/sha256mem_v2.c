#include "sha256mem_v2.h"
#include <openssl/sha.h>
#include <string.h>
#include <stdlib.h>

static uint32_t le32_load(const uint8_t *p)
{
	uint32_t v;
	memcpy(&v, p, 4);
	return v;
}

static void le32_store(uint8_t *p, uint32_t v)
{
	memcpy(p, &v, 4);
}

static void arx_fill(uint8_t dst[32], const uint8_t src[32], uint32_t index)
{
	for (int w = 0; w < 8; w++) {
		uint32_t v = le32_load(src + w * 4);
		v ^= index + (uint32_t)w;
		v = (v << 13) | (v >> 19);
		v += le32_load(src + w * 4);
		le32_store(dst + w * 4, v);
	}
}

static void progression_harden_step(uint8_t slot[32], uint32_t index)
{
	uint32_t selector = le32_load(slot + ((index & 7u) * 4u));
	if (((selector ^ index) & 255u) < SHA256MEM_V2_HARDEN_THRESHOLD) {
		SHA256(slot, 32, slot);
	} else {
		uint8_t next[32];
		arx_fill(next, slot, index);
		memcpy(slot, next, 32);
	}
}

void sha256mem_v2_hash(const uint8_t *data, size_t len, uint8_t out[32])
{
	uint8_t (*mem)[32] = malloc(SHA256MEM_V2_SCRATCH_BYTES);
	if (!mem) {
		memset(out, 0, 32);
		return;
	}

	SHA256(data, len, mem[0]);

	for (int i = 1; i < SHA256MEM_V2_SLOTS; i++) {
		if (i % SHA256MEM_V2_HARDEN_INTERVAL == 0) {
			SHA256(mem[i - 1], 32, mem[i]);
		} else {
			memcpy(mem[i], mem[i - 1], 32);
			progression_harden_step(mem[i], (uint32_t)i);
		}
	}

	uint8_t acc[32];
	memcpy(acc, mem[SHA256MEM_V2_SLOTS - 1], 32);

	for (int i = 0; i < SHA256MEM_V2_MIX_ROUNDS; i++) {
		uint32_t idx = le32_load(acc) % SHA256MEM_V2_SLOTS;
		uint8_t buf[64];
		memcpy(buf, acc, 32);
		memcpy(buf + 32, mem[idx], 32);
		SHA256(buf, 64, acc);
	}

	for (int i = 0; i < SHA256MEM_V2_MIX_ROUNDS; i++) {
		uint32_t word_index = (uint32_t)(i % 7);
		int off = (int)(word_index * 4u);
		uint32_t idx = le32_load(acc + off) % SHA256MEM_V2_SLOTS;
		uint8_t buf[64];
		memcpy(buf, acc, 32);
		memcpy(buf + 32, mem[idx], 32);
		SHA256(buf, 64, acc);
	}

	SHA256(acc, 32, out);
	free(mem);
}
