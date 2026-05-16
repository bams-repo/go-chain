/* Linear sha256mem (64 MiB scratch) multithread benchmark — SHA-NI path. */
#define _GNU_SOURCE
#include <pthread.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include <unistd.h>

#include "sha256mem.h"

static const uint8_t g_hdr[80] = { 0x01, 0x00, 0x00, 0x00 };

typedef struct {
	int tid;
	double duration_sec;
	unsigned long *local_out;
} worker_arg;

static void *worker_main(void *vp)
{
	worker_arg *a = (worker_arg *)vp;
	void *scratch = malloc(SHA256MEM_SCRATCH_BYTES);
	if (!scratch) {
		fprintf(stderr, "alloc failed\n");
		return NULL;
	}
	uint8_t hdr[80];
	memcpy(hdr, g_hdr, sizeof(hdr));
	uint8_t out[32];
	struct timespec t0;
	clock_gettime(CLOCK_MONOTONIC, &t0);
	unsigned long n = 0;
	for (;;) {
		for (int k = 0; k < 16; k++) {
			uint32_t base = (uint32_t)(a->tid * 0x1000000u + (unsigned)n);
			hdr[76] = (uint8_t)(base & 0xff);
			hdr[77] = (uint8_t)((base >> 8) & 0xff);
			hdr[78] = (uint8_t)((base >> 16) & 0xff);
			hdr[79] = (uint8_t)((base >> 24) & 0xff);
			sha256mem_hash_with_scratch(hdr, sizeof(hdr), out, scratch);
			n++;
		}
		struct timespec t1;
		clock_gettime(CLOCK_MONOTONIC, &t1);
		double elapsed = (t1.tv_sec - t0.tv_sec) + (t1.tv_nsec - t0.tv_nsec) / 1e9;
		if (elapsed >= a->duration_sec)
			break;
	}
	free(scratch);
	*a->local_out = n;
	return NULL;
}

int main(int argc, char **argv)
{
	int threads = (int)sysconf(_SC_NPROCESSORS_ONLN);
	double dur = 3.0;
	if (argc > 1)
		threads = atoi(argv[1]);
	if (argc > 2)
		dur = atof(argv[2]);
	if (threads < 1)
		threads = 1;

	unsigned long *counts = calloc((size_t)threads, sizeof(unsigned long));
	worker_arg *args = calloc((size_t)threads, sizeof(worker_arg));
	pthread_t *ths = calloc((size_t)threads, sizeof(pthread_t));
	struct timespec t0, t1;
	clock_gettime(CLOCK_MONOTONIC, &t0);
	for (int i = 0; i < threads; i++) {
		args[i].tid = i;
		args[i].duration_sec = dur;
		args[i].local_out = &counts[i];
		pthread_create(&ths[i], NULL, worker_main, &args[i]);
	}
	for (int i = 0; i < threads; i++)
		pthread_join(ths[i], NULL);
	clock_gettime(CLOCK_MONOTONIC, &t1);
	double wall = (t1.tv_sec - t0.tv_sec) + (t1.tv_nsec - t0.tv_nsec) / 1e9;
	unsigned long total = 0;
	for (int i = 0; i < threads; i++)
		total += counts[i];
	printf("linear+SHA-NI threads=%d wall=%.3fs hashes=%lu aggregate=%.1f H/s\n",
	       threads, wall, total, (double)total / wall);
	free(ths);
	free(args);
	free(counts);
	return 0;
}
