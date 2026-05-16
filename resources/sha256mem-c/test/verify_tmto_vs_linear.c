/* One-shot: TMTO vs linear (OpenSSL) on a few fixed inputs. */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "sha256mem.h"
#include "sha256mem_tmto.h"

static int cmp(const char *label, const uint8_t *data, size_t len)
{
	void *st_t = malloc(SHA256MEM_TMTO_SCRATCH_BYTES);
	void *st_l = malloc(SHA256MEM_SCRATCH_BYTES);
	uint8_t a[32], b[32];
	sha256mem_tmto_hash_with_scratch(data, len, a, st_t);
	sha256mem_hash_with_scratch(data, len, b, st_l);
	free(st_t);
	free(st_l);
	if (memcmp(a, b, 32) != 0) {
		printf("FAIL %s\n", label);
		return 1;
	}
	printf("ok %s\n", label);
	return 0;
}

int main(void)
{
	int e = 0;
	uint8_t h80[80] = { 0 };
	h80[0] = 1;
	e |= cmp("80-byte header", h80, 80);
	e |= cmp("empty", (const uint8_t *)"", 0);
	uint8_t one[] = { 0xab };
	e |= cmp("1-byte", one, 1);
	return e;
}
