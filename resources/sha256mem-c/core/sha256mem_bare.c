#include "sha256mem.h"
#include "sha256_bare.h"
#include <stdlib.h>
#include <string.h>

static uint32_t le32_load(const uint8_t *p) {
	uint32_t v;
	memcpy(&v, p, 4);
	return v;
}

static void le32_store(uint8_t *p, uint32_t v) { memcpy(p, &v, 4); }

static void arx_fill(uint8_t dst[32], const uint8_t src[32], uint32_t index) {
	for (int w = 0; w < 8; w++) {
		uint32_t v = le32_load(src + w * 4);
		v ^= index + (uint32_t)w;
		v = (v << 13) | (v >> 19);
		v += le32_load(src + w * 4);
		le32_store(dst + w * 4, v);
	}
}

void sha256mem_hash(const uint8_t *data, size_t len, uint8_t out[32]) {
	uint8_t (*mem)[32] = malloc((size_t)SHA256MEM_SLOTS * 32);
	if (!mem) {
		memset(out, 0, 32);
		return;
	}

	sha256_bare(data, len, mem[0]);

	for (int i = 1; i < SHA256MEM_SLOTS; i++) {
		if (i % SHA256MEM_HARDEN_INTERVAL == 0) {
			sha256_bare(mem[i - 1], 32, mem[i]);
		} else {
			arx_fill(mem[i], mem[i - 1], (uint32_t)i);
		}
	}

	uint8_t acc[32];
	memcpy(acc, mem[SHA256MEM_SLOTS - 1], 32);

	for (int i = 0; i < SHA256MEM_MIX_ROUNDS; i++) {
		uint32_t idx = le32_load(acc) % SHA256MEM_SLOTS;
		uint8_t buf[64];
		memcpy(buf, acc, 32);
		memcpy(buf + 32, mem[idx], 32);
		sha256_bare(buf, 64, acc);
	}

	for (int i = 0; i < SHA256MEM_MIX_ROUNDS; i++) {
		int off = (i % 7) * 4;
		uint32_t idx = le32_load(acc + off) % SHA256MEM_SLOTS;
		uint8_t buf[64];
		memcpy(buf, acc, 32);
		memcpy(buf + 32, mem[idx], 32);
		sha256_bare(buf, 64, acc);
	}

	sha256_bare(acc, 32, out);
	free(mem);
}
