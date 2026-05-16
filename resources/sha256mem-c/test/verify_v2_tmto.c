/* Verify sha256mem v2 TMTO matches linear reference on sample inputs. */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "sha256mem_v2.h"
#include "sha256mem_v2_tmto.h"

static int check(const char *label, const uint8_t *data, size_t len)
{
	uint8_t linear[32], tmto[32];
	void *scratch = malloc(SHA256MEM_V2_TMTO_SCRATCH_BYTES);
	if (!scratch) {
		fprintf(stderr, "malloc failed\n");
		return 1;
	}

	sha256mem_v2_hash(data, len, linear);
	sha256mem_v2_tmto_hash_with_scratch(data, len, tmto, scratch);
	free(scratch);

	if (memcmp(linear, tmto, 32) != 0) {
		printf("FAIL %s\n  linear: ", label);
		for (int i = 0; i < 32; i++)
			printf("%02x", linear[i]);
		printf("\n  tmto:   ");
		for (int i = 0; i < 32; i++)
			printf("%02x", tmto[i]);
		printf("\n");
		return 1;
	}
	printf("ok %s\n", label);
	return 0;
}

int main(void)
{
	int fail = 0;
	uint8_t empty[1] = {0};
	uint8_t one[1] = {0x42};

	fail |= check("empty", empty, 0);
	fail |= check("1-byte", one, 1);

	uint8_t hdr[80];
	memset(hdr, 0, sizeof(hdr));
	hdr[0] = 0x01;
	fail |= check("80-byte header", hdr, 80);

	return fail ? 1 : 0;
}
