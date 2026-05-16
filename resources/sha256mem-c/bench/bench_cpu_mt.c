/*
 * Multi-threaded CPU throughput for consensus sha256mem (TMTO + SHA-NI).
 *
 * Build: make bench_cpu_mt
 * Run:   ./bench_cpu_mt [threads] [seconds]
 *
 * Default threads = min(2 * CPU count, 64) to use SMT without oversaturating
 * memory controllers too badly; override with argv[1].
 */

#define _GNU_SOURCE
#include <pthread.h>
#include <sched.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include <unistd.h>

#include "sha256mem_tmto.h"

static const uint8_t g_hdr[80] = {
	0x01, 0x00, 0x00, 0x00,
};

typedef struct {
	int tid;
	int threads;
	int cpus;
	double duration_sec;
	unsigned long *local_out;
} worker_arg;

static void *worker_main(void *vp)
{
	worker_arg *a = (worker_arg *)vp;
	if (a->cpus > 0) {
		cpu_set_t cpuset;
		CPU_ZERO(&cpuset);
		CPU_SET(a->tid % a->cpus, &cpuset);
		pthread_setaffinity_np(pthread_self(), sizeof(cpuset), &cpuset);
	}

	void *scratch = malloc(SHA256MEM_TMTO_SCRATCH_BYTES);
	if (!scratch) {
		fprintf(stderr, "thread %d: scratch alloc failed\n", a->tid);
		return NULL;
	}

	uint8_t hdr[80];
	memcpy(hdr, g_hdr, sizeof(hdr));

	uint8_t out[32];
	struct timespec t0;
	clock_gettime(CLOCK_MONOTONIC, &t0);

	unsigned long n = 0;
	for (;;) {
		struct timespec tchk;
		clock_gettime(CLOCK_MONOTONIC, &tchk);
		if ((tchk.tv_sec - t0.tv_sec) + (tchk.tv_nsec - t0.tv_nsec) / 1e9 >= a->duration_sec)
			break;
		for (int k = 0; k < 32; k++) {
			uint32_t base = (uint32_t)(a->tid * 0x1000000u + (unsigned)n);
			hdr[76] = (uint8_t)(base & 0xff);
			hdr[77] = (uint8_t)((base >> 8) & 0xff);
			hdr[78] = (uint8_t)((base >> 16) & 0xff);
			hdr[79] = (uint8_t)((base >> 24) & 0xff);
			sha256mem_tmto_hash_with_scratch(hdr, sizeof(hdr), out, scratch);
			n++;
		}
	}

	free(scratch);
	*a->local_out = n;
	return NULL;
}

int main(int argc, char **argv)
{
	int cpus = (int)sysconf(_SC_NPROCESSORS_ONLN);
	if (cpus < 1)
		cpus = 1;
	int threads = cpus * 2;
	if (threads > 64)
		threads = 64;
	double dur = 4.0;
	if (argc > 1)
		threads = atoi(argv[1]);
	if (argc > 2)
		dur = atof(argv[2]);
	if (threads < 1)
		threads = 1;
	if (dur <= 0)
		dur = 1.0;

	unsigned long *counts = calloc((size_t)threads, sizeof(unsigned long));
	worker_arg *args = calloc((size_t)threads, sizeof(worker_arg));
	pthread_t *ths = calloc((size_t)threads, sizeof(pthread_t));
	if (!counts || !args || !ths) {
		fprintf(stderr, "alloc failed\n");
		return 1;
	}

	struct timespec t0, t1;
	clock_gettime(CLOCK_MONOTONIC, &t0);

	for (int i = 0; i < threads; i++) {
		args[i].tid = i;
		args[i].threads = threads;
		args[i].cpus = cpus;
		args[i].duration_sec = dur;
		args[i].local_out = &counts[i];
		if (pthread_create(&ths[i], NULL, worker_main, &args[i]) != 0) {
			fprintf(stderr, "pthread_create failed\n");
			return 1;
		}
	}
	for (int i = 0; i < threads; i++)
		pthread_join(ths[i], NULL);
	clock_gettime(CLOCK_MONOTONIC, &t1);

	double wall = (t1.tv_sec - t0.tv_sec) + (t1.tv_nsec - t0.tv_nsec) / 1e9;
	unsigned long total = 0;
	for (int i = 0; i < threads; i++)
		total += counts[i];

	printf("TMTO+SHA-NI  threads=%d cpus=%d  wall=%.3fs  window=%.1fs  hashes=%lu  aggregate=%.1f H/s\n",
	       threads, cpus, wall, dur, total, (double)total / wall);

	free(ths);
	free(args);
	free(counts);
	return 0;
}
