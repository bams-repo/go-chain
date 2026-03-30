/*
 * sha256mem Android/Termux Benchmark — Maximum Performance Edition
 * =================================================================
 * Self-contained benchmark for sha256mem PoW (matches Go consensus).
 * Tracks hashrate, CPU temperature, and thermal throttling.
 *
 * Optimizations over baseline:
 *   1. Per-thread scratchpad: allocated once, reused across all hashes
 *      (eliminates 64 MiB malloc/free per hash — the single biggest win)
 *   2. Huge pages via mmap MAP_HUGETLB (fallback to madvise MADV_HUGEPAGE,
 *      then plain mmap): cuts TLB misses during random 64-byte reads
 *   3. Incremental SHA256_CTX instead of OpenSSL SHA256() one-shot:
 *      avoids repeated stack allocation and function-call overhead
 *      for 65K+ SHA calls per hash
 *   4. Software prefetch (__builtin_prefetch) in mix passes: hides
 *      memory latency by issuing the next random read 1 iteration ahead
 *   5. NEON-accelerated ARX fill on ARM64: processes 4 lanes at once
 *   6. Thread pinning to big cores on big.LITTLE SoCs via
 *      sched_setaffinity / CPU frequency detection
 *   7. Minimized memcpy in mix loop: write acc+slot directly into
 *      a persistent buf[64] without redundant copies
 *
 * Build (Termux):
 *   pkg install clang openssl-tool
 *   # Generic (works everywhere):
 *   clang -O3 -ffunction-sections -fdata-sections -Wl,--gc-sections \
 *     -o sha256mem_bench bench_android.c -lssl -lcrypto -lm -pthread
 *   # ARM64 with SHA2 + NEON (recommended for modern phones):
 *   clang -O3 -march=armv8-a+crypto -o sha256mem_bench bench_android.c \
 *     -lssl -lcrypto -lm -pthread
 *
 * Run:
 *   ./sha256mem_bench              # 60s, one worker per CPU (up to 32)
 *   ./sha256mem_bench 120          # 120s, auto threads
 *   ./sha256mem_bench 300 4        # 300s, exactly 4 threads
 *
 * Copyright (c) 2024-2026 The Fairchain Contributors
 * Distributed under the MIT software license.
 */

#if defined(__ANDROID__) || defined(__linux__)
#define _GNU_SOURCE 1
#define _DEFAULT_SOURCE 1
#endif

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stdint.h>
#include <stdatomic.h>
#include <time.h>
#include <unistd.h>
#include <pthread.h>
#include <dirent.h>
#include <math.h>

#include <openssl/sha.h>

#ifdef __linux__
#include <sys/mman.h>
#include <sched.h>
#endif

#if defined(__aarch64__) && defined(__ARM_NEON)
#include <arm_neon.h>
#define HAVE_NEON 1
#endif

/* ── sha256mem parameters (match internal/algorithms/sha256mem/sha256mem.go) ─ */
#define SHA256MEM_SLOTS             2097152
#define SHA256MEM_HARDEN_INTERVAL   128
#define SHA256MEM_MIX_ROUNDS        32768
#define SHA256MEM_BUF_SIZE          ((size_t)SHA256MEM_SLOTS * 32)

/* ── Portable helpers ──────────────────────────────────────────────────────── */
static inline uint32_t le32_load(const uint8_t *p) {
	uint32_t v;
	memcpy(&v, p, 4);
	return v;
}

static inline void le32_store(uint8_t *p, uint32_t v) {
	memcpy(p, &v, 4);
}

/* ── ARX fill: NEON path for ARM64, scalar fallback otherwise ─────────────── */
#ifdef HAVE_NEON
static inline void arx_fill(uint8_t *restrict dst, const uint8_t *restrict src, uint32_t index) {
	uint32x4_t orig_lo = vld1q_u32((const uint32_t *)src);
	uint32x4_t orig_hi = vld1q_u32((const uint32_t *)(src + 16));

	uint32x4_t idx_lo = {index + 0, index + 1, index + 2, index + 3};
	uint32x4_t idx_hi = {index + 4, index + 5, index + 6, index + 7};

	uint32x4_t v_lo = veorq_u32(orig_lo, idx_lo);
	uint32x4_t v_hi = veorq_u32(orig_hi, idx_hi);

	v_lo = vorrq_u32(vshlq_n_u32(v_lo, 13), vshrq_n_u32(v_lo, 19));
	v_hi = vorrq_u32(vshlq_n_u32(v_hi, 13), vshrq_n_u32(v_hi, 19));

	v_lo = vaddq_u32(v_lo, orig_lo);
	v_hi = vaddq_u32(v_hi, orig_hi);

	vst1q_u32((uint32_t *)dst, v_lo);
	vst1q_u32((uint32_t *)(dst + 16), v_hi);
}
#else
static inline void arx_fill(uint8_t *restrict dst, const uint8_t *restrict src, uint32_t index) {
	for (int w = 0; w < 8; w++) {
		uint32_t orig = le32_load(src + w * 4);
		uint32_t v = orig ^ (index + (uint32_t)w);
		v = (v << 13) | (v >> 19);
		v += orig;
		le32_store(dst + w * 4, v);
	}
}
#endif

/* ── Incremental SHA256 wrappers (avoid one-shot overhead) ────────────────── */
static inline void sha256_32(const uint8_t in[32], uint8_t out[32]) {
	SHA256_CTX ctx;
	SHA256_Init(&ctx);
	SHA256_Update(&ctx, in, 32);
	SHA256_Final(out, &ctx);
}

static inline void sha256_64(const uint8_t in[64], uint8_t out[32]) {
	SHA256_CTX ctx;
	SHA256_Init(&ctx);
	SHA256_Update(&ctx, in, 64);
	SHA256_Final(out, &ctx);
}

static inline void sha256_var(const uint8_t *in, size_t len, uint8_t out[32]) {
	SHA256_CTX ctx;
	SHA256_Init(&ctx);
	SHA256_Update(&ctx, in, len);
	SHA256_Final(out, &ctx);
}

/* ── Scratchpad allocation: huge pages > transparent huge pages > plain ───── */
static uint8_t (*alloc_scratchpad(void))[32] {
#ifdef __linux__
	void *p = mmap(NULL, SHA256MEM_BUF_SIZE, PROT_READ | PROT_WRITE,
	               MAP_PRIVATE | MAP_ANONYMOUS | MAP_HUGETLB, -1, 0);
	if (p != MAP_FAILED) return (uint8_t (*)[32])p;

	p = mmap(NULL, SHA256MEM_BUF_SIZE, PROT_READ | PROT_WRITE,
	         MAP_PRIVATE | MAP_ANONYMOUS, -1, 0);
	if (p != MAP_FAILED) {
		madvise(p, SHA256MEM_BUF_SIZE, MADV_HUGEPAGE);
		return (uint8_t (*)[32])p;
	}
#endif
	void *p2 = aligned_alloc(64, SHA256MEM_BUF_SIZE);
	return (uint8_t (*)[32])p2;
}

static void free_scratchpad(uint8_t (*mem)[32]) {
#ifdef __linux__
	munmap(mem, SHA256MEM_BUF_SIZE);
#else
	free(mem);
#endif
}

/* ── Core hash function (consensus-identical, maximum speed) ──────────────── */
static void sha256mem_hash(uint8_t (*mem)[32], const uint8_t *data, size_t len, uint8_t out[32]) {
	sha256_var(data, len, mem[0]);

	for (int i = 1; i < SHA256MEM_SLOTS; i++) {
		if (i % SHA256MEM_HARDEN_INTERVAL == 0) {
			sha256_32(mem[i - 1], mem[i]);
		} else {
			arx_fill(mem[i], mem[i - 1], (uint32_t)i);
		}
	}

	uint8_t buf[64] __attribute__((aligned(64)));
	uint8_t *acc = buf;
	memcpy(acc, mem[SHA256MEM_SLOTS - 1], 32);

	/* Mix pass A: prefetch next slot 1 iteration ahead. */
	{
		uint32_t idx = le32_load(acc) % SHA256MEM_SLOTS;
		for (int i = 0; i < SHA256MEM_MIX_ROUNDS; i++) {
			memcpy(buf + 32, mem[idx], 32);
			uint32_t next_idx;
			if (i + 1 < SHA256MEM_MIX_ROUNDS) {
				sha256_64(buf, acc);
				next_idx = le32_load(acc) % SHA256MEM_SLOTS;
				__builtin_prefetch(mem[next_idx], 0, 0);
			} else {
				sha256_64(buf, acc);
				next_idx = 0;
			}
			idx = next_idx;
		}
	}

	/* Mix pass B: same prefetch strategy, different index derivation. */
	{
		int off0 = 0;
		uint32_t idx = le32_load(acc + off0) % SHA256MEM_SLOTS;
		for (int i = 0; i < SHA256MEM_MIX_ROUNDS; i++) {
			memcpy(buf + 32, mem[idx], 32);
			uint32_t next_idx;
			if (i + 1 < SHA256MEM_MIX_ROUNDS) {
				sha256_64(buf, acc);
				int next_off = ((i + 1) % 7) * 4;
				next_idx = le32_load(acc + next_off) % SHA256MEM_SLOTS;
				__builtin_prefetch(mem[next_idx], 0, 0);
			} else {
				sha256_64(buf, acc);
				next_idx = 0;
			}
			idx = next_idx;
		}
	}

	sha256_32(acc, out);
}

/* ── Thermal monitoring ───────────────────────────────────────────── */
#define MAX_THERMAL_ZONES 20
#define GRAPH_WIDTH       60
#define GRAPH_HEIGHT      15

typedef struct {
	char path[256];
	char type[64];
} thermal_zone_t;

static thermal_zone_t g_zones[MAX_THERMAL_ZONES];
static int g_zone_count = 0;
static int g_best_zone = -1;

static void discover_thermal_zones(void) {
	g_zone_count = 0;
	for (int i = 0; i < MAX_THERMAL_ZONES; i++) {
		char path[256], type_path[256], type_buf[64];
		snprintf(path, sizeof(path), "/sys/class/thermal/thermal_zone%d/temp", i);
		snprintf(type_path, sizeof(type_path), "/sys/class/thermal/thermal_zone%d/type", i);

		FILE *f = fopen(path, "r");
		if (!f) continue;
		fclose(f);

		type_buf[0] = '\0';
		f = fopen(type_path, "r");
		if (f) {
			if (fgets(type_buf, sizeof(type_buf), f)) {
				type_buf[strcspn(type_buf, "\n")] = '\0';
			}
			fclose(f);
		}

		strncpy(g_zones[g_zone_count].path, path, sizeof(g_zones[g_zone_count].path) - 1);
		strncpy(g_zones[g_zone_count].type, type_buf, sizeof(g_zones[g_zone_count].type) - 1);
		g_zone_count++;
	}

	int best = -1;
	int best_score = -1;
	for (int i = 0; i < g_zone_count; i++) {
		int score = 0;
		if (strstr(g_zones[i].type, "cpu")) score = 10;
		if (strstr(g_zones[i].type, "tsens")) score = 8;
		if (strstr(g_zones[i].type, "soc")) score = 7;
		if (strstr(g_zones[i].type, "mtktscpu")) score = 10;
		if (strstr(g_zones[i].type, "battery")) score = 3;
		if (score == 0) score = 1;
		if (score > best_score) {
			best_score = score;
			best = i;
		}
	}
	g_best_zone = best;
}

static float read_temp_c(int zone_idx) {
	if (zone_idx < 0 || zone_idx >= g_zone_count) return -1.0f;
	FILE *f = fopen(g_zones[zone_idx].path, "r");
	if (!f) return -1.0f;
	long raw = 0;
	if (fscanf(f, "%ld", &raw) != 1) {
		fclose(f);
		return -1.0f;
	}
	fclose(f);
	if (raw > 1000) return raw / 1000.0f;
	if (raw > 200) return raw / 10.0f;
	return (float)raw;
}

/* ── big.LITTLE core detection (Linux/Android) ────────────────────────────── */
#ifdef __linux__
static int g_big_cores[32];
static int g_big_core_count = 0;

static void detect_big_cores(void) {
	int ncpu = (int)sysconf(_SC_NPROCESSORS_ONLN);
	if (ncpu < 1) ncpu = 1;
	if (ncpu > 32) ncpu = 32;

	unsigned long freqs[32];
	unsigned long max_freq = 0;
	for (int i = 0; i < ncpu; i++) {
		freqs[i] = 0;
		char path[128];
		snprintf(path, sizeof(path),
		         "/sys/devices/system/cpu/cpu%d/cpufreq/cpuinfo_max_freq", i);
		FILE *f = fopen(path, "r");
		if (f) {
			if (fscanf(f, "%lu", &freqs[i]) != 1) freqs[i] = 0;
			fclose(f);
		}
		if (freqs[i] > max_freq) max_freq = freqs[i];
	}

	if (max_freq == 0) {
		for (int i = 0; i < ncpu; i++)
			g_big_cores[i] = i;
		g_big_core_count = ncpu;
		return;
	}

	unsigned long threshold = max_freq * 80 / 100;
	g_big_core_count = 0;
	for (int i = 0; i < ncpu; i++) {
		if (freqs[i] >= threshold) {
			g_big_cores[g_big_core_count++] = i;
		}
	}

	if (g_big_core_count == 0) {
		for (int i = 0; i < ncpu; i++)
			g_big_cores[i] = i;
		g_big_core_count = ncpu;
	}
}

static void pin_to_core(int core_id) {
	cpu_set_t set;
	CPU_ZERO(&set);
	CPU_SET(core_id, &set);
	sched_setaffinity(0, sizeof(set), &set);
}
#else
static int g_big_cores[32];
static int g_big_core_count = 0;
static void detect_big_cores(void) {
	int ncpu = (int)sysconf(_SC_NPROCESSORS_ONLN);
	if (ncpu < 1) ncpu = 1;
	if (ncpu > 32) ncpu = 32;
	for (int i = 0; i < ncpu; i++)
		g_big_cores[i] = i;
	g_big_core_count = ncpu;
}
static void pin_to_core(int core_id) { (void)core_id; }
#endif

/* ── Thread state ──────────────────────────────────────────────────────────── */
typedef struct {
	int thread_id;
	volatile int *running;
	_Atomic uint64_t hash_count;
	uint32_t nonce_start;
	int pin_core;
} thread_ctx_t;

static void *mine_thread(void *arg) {
	thread_ctx_t *ctx = (thread_ctx_t *)arg;

	if (ctx->pin_core >= 0)
		pin_to_core(ctx->pin_core);

	uint8_t (*mem)[32] = alloc_scratchpad();
	if (!mem) return NULL;

	uint8_t header[80];
	memset(header, 0, sizeof(header));
	header[0] = (uint8_t)(ctx->thread_id & 0xff);
	header[1] = (uint8_t)((ctx->thread_id >> 8) & 0xff);

	uint32_t nonce = ctx->nonce_start;
	uint8_t out[32];

	while (*(ctx->running)) {
		memcpy(header + 76, &nonce, 4);
		sha256mem_hash(mem, header, 80, out);
		atomic_fetch_add_explicit(&ctx->hash_count, 1, memory_order_relaxed);
		nonce++;
	}

	free_scratchpad(mem);
	return NULL;
}

static void draw_temp_graph(float *temps, float *rates, int count,
                            float temp_min, float temp_max,
                            float rate_min, float rate_max) {
	if (count < 2) return;

	printf("\n");
	printf("  ┌─ Temperature (°C) & Hashrate (H/s) over time ─────────────────┐\n");

	int start = 0;
	if (count > GRAPH_WIDTH) start = count - GRAPH_WIDTH;
	int pts = count - start;

	char grid[GRAPH_HEIGHT][GRAPH_WIDTH + 1];
	memset(grid, ' ', sizeof(grid));
	for (int r = 0; r < GRAPH_HEIGHT; r++)
		grid[r][GRAPH_WIDTH] = '\0';

	float t_range = temp_max - temp_min;
	if (t_range < 5.0f) t_range = 5.0f;
	float r_range = rate_max - rate_min;
	if (r_range < 1.0f) r_range = 1.0f;

	for (int x = 0; x < pts && x < GRAPH_WIDTH; x++) {
		int i = start + x;

		float t_norm = (temps[i] - temp_min) / t_range;
		if (t_norm < 0) t_norm = 0;
		if (t_norm > 1) t_norm = 1;
		int t_row = GRAPH_HEIGHT - 1 - (int)(t_norm * (GRAPH_HEIGHT - 1));
		grid[t_row][x] = '#';

		float r_norm = (rates[i] - rate_min) / r_range;
		if (r_norm < 0) r_norm = 0;
		if (r_norm > 1) r_norm = 1;
		int r_row = GRAPH_HEIGHT - 1 - (int)(r_norm * (GRAPH_HEIGHT - 1));
		if (grid[r_row][x] == '#')
			grid[r_row][x] = '@';
		else
			grid[r_row][x] = '.';
	}

	for (int r = 0; r < GRAPH_HEIGHT; r++) {
		float t_val = temp_max - (float)r / (GRAPH_HEIGHT - 1) * t_range;
		float r_val = rate_max - (float)r / (GRAPH_HEIGHT - 1) * r_range;
		if (r == 0)
			printf("  │%5.1f°C %5.1f H/s│%s│\n", t_val, r_val, grid[r]);
		else if (r == GRAPH_HEIGHT - 1)
			printf("  │%5.1f°C %5.1f H/s│%s│\n", t_val, r_val, grid[r]);
		else if (r == GRAPH_HEIGHT / 2)
			printf("  │%5.1f°C %5.1f H/s│%s│\n", t_val, r_val, grid[r]);
		else
			printf("  │              │%s│\n", grid[r]);
	}

	printf("  └──────────────┴");
	for (int i = 0; i < GRAPH_WIDTH; i++) printf("─");
	printf("┘\n");

	int elapsed = count;
	printf("  Time: 0s");
	int mid = elapsed / 2;
	int pad = GRAPH_WIDTH / 2 - 4;
	for (int i = 0; i < pad; i++) printf(" ");
	printf("%ds", mid);
	for (int i = 0; i < pad; i++) printf(" ");
	printf("%ds\n", elapsed);

	printf("  Legend: # = Temperature   . = Hashrate   @ = Overlap\n");
}

static int default_thread_count(void) {
	long n = sysconf(_SC_NPROCESSORS_ONLN);
	if (n < 1) n = 1;
	if (n > 32) n = 32;
	return (int)n;
}

int main(int argc, char **argv) {
	int duration = 60;
	int num_threads = default_thread_count();

	if (argc > 1) duration = atoi(argv[1]);
	if (argc > 2) num_threads = atoi(argv[2]);
	if (duration < 10) duration = 10;
	if (num_threads < 1) num_threads = 1;
	if (num_threads > 32) num_threads = 32;

	detect_big_cores();

	printf("╔══════════════════════════════════════════════════════════════╗\n");
	printf("║      sha256mem Benchmark — Fairchain PoW (Optimized)       ║\n");
	printf("╠══════════════════════════════════════════════════════════════╣\n");
	printf("║  Buffer:  %d slots × 32 bytes = %d MiB               ║\n",
	       SHA256MEM_SLOTS, (int)(SHA256MEM_BUF_SIZE / (1024 * 1024)));
	printf("║  Mix:     2 × %d SHA256 rounds (pass A + pass B)     ║\n",
	       SHA256MEM_MIX_ROUNDS);
	printf("║  Harden:  every %d slots                                ║\n",
	       SHA256MEM_HARDEN_INTERVAL);
	printf("║  Threads: %-3d    Duration: %ds                         ║\n",
	       num_threads, duration);
	printf("║  Big cores detected: %d", g_big_core_count);
	if (g_big_core_count > 0 && g_big_core_count < default_thread_count()) {
		printf(" (of %d total)", default_thread_count());
	}
	printf("%*s║\n", 38 - (g_big_core_count > 9 ? 2 : 1)
	       - (g_big_core_count > 0 && g_big_core_count < default_thread_count() ?
	          12 + (default_thread_count() > 9 ? 1 : 0) : 0), "");
#ifdef HAVE_NEON
	printf("║  NEON ARX fill: enabled                                    ║\n");
#else
	printf("║  NEON ARX fill: not available (scalar fallback)            ║\n");
#endif
	printf("║  Huge pages: ");
#ifdef __linux__
	printf("attempted (HUGETLB → THP → aligned_alloc)     ║\n");
#else
	printf("not available (aligned_alloc fallback)         ║\n");
#endif
	printf("║  Prefetch: enabled in mix passes                           ║\n");
	printf("╚══════════════════════════════════════════════════════════════╝\n\n");

	discover_thermal_zones();
	if (g_zone_count == 0) {
		printf("  [warn] No thermal zones found (not Android or no permissions)\n");
		printf("         Temperature tracking disabled.\n\n");
	} else {
		printf("  Thermal zones found: %d\n", g_zone_count);
		for (int i = 0; i < g_zone_count; i++) {
			float t = read_temp_c(i);
			printf("    zone %2d: %-24s  %.1f°C%s\n",
			       i, g_zones[i].type, t,
			       (i == g_best_zone) ? "  ← selected" : "");
		}
		printf("\n");
	}

	float baseline_temp = -1;
	if (g_best_zone >= 0)
		baseline_temp = read_temp_c(g_best_zone);

	printf("  Baseline temp: %.1f°C\n", baseline_temp);
	printf("  Warming up (single hash)...\n");
	fflush(stdout);

	{
		uint8_t (*warmup_mem)[32] = alloc_scratchpad();
		if (warmup_mem) {
			uint8_t dummy[80] = {0}, out[32];
			sha256mem_hash(warmup_mem, dummy, 80, out);
			free_scratchpad(warmup_mem);
		}
	}

	printf("  Starting benchmark...\n\n");
	printf("  ┌──────┬──────────┬───────────┬────────┬──────────┐\n");
	printf("  │  sec │  hashes  │    H/s    │  temp  │ throttle │\n");
	printf("  ├──────┼──────────┼───────────┼────────┼──────────┤\n");
	fflush(stdout);

	float *temp_history = calloc((size_t)duration + 10, sizeof(float));
	float *rate_history = calloc((size_t)duration + 10, sizeof(float));
	int history_count = 0;
	float temp_min = 999, temp_max = 0;
	float peak_temp = 0;
	float peak_rate = 0;
	float last_rate = 0;
	int throttle_events = 0;

	volatile int running = 1;
	pthread_t *threads = calloc((size_t)num_threads, sizeof(pthread_t));
	thread_ctx_t *ctxs = calloc((size_t)num_threads, sizeof(thread_ctx_t));

	uint32_t range = (uint32_t)(0xFFFFFFFFu / (uint32_t)num_threads);
	for (int i = 0; i < num_threads; i++) {
		ctxs[i].thread_id = i;
		ctxs[i].running = &running;
		atomic_init(&ctxs[i].hash_count, 0);
		ctxs[i].nonce_start = (uint32_t)((uint64_t)i * range);
		if (g_big_core_count > 0)
			ctxs[i].pin_core = g_big_cores[i % g_big_core_count];
		else
			ctxs[i].pin_core = -1;
		pthread_create(&threads[i], NULL, mine_thread, &ctxs[i]);
	}

	struct timespec ts_start, ts_now;
	clock_gettime(CLOCK_MONOTONIC, &ts_start);

	uint64_t last_total = 0;
	struct timespec ts_last = ts_start;

	for (int sec = 1; sec <= duration; sec++) {
		usleep(1000000);

		clock_gettime(CLOCK_MONOTONIC, &ts_now);
		double dt = (ts_now.tv_sec - ts_last.tv_sec)
		          + (ts_now.tv_nsec - ts_last.tv_nsec) / 1e9;

		uint64_t total = 0;
		for (int i = 0; i < num_threads; i++)
			total += atomic_load_explicit(&ctxs[i].hash_count, memory_order_relaxed);

		double instant_rate = (double)(total - last_total) / dt;

		float temp = -1;
		if (g_best_zone >= 0)
			temp = read_temp_c(g_best_zone);

		int throttled = 0;
		if (sec > 5 && last_rate > 0 && instant_rate < last_rate * 0.85) {
			throttled = 1;
			throttle_events++;
		}

		if (history_count < duration + 10) {
			temp_history[history_count] = temp;
			rate_history[history_count] = (float)instant_rate;
			history_count++;
		}

		if (temp > 0 && temp < temp_min) temp_min = temp;
		if (temp > temp_max) temp_max = temp;
		if (temp > peak_temp) peak_temp = temp;
		if (instant_rate > peak_rate) peak_rate = instant_rate;

		if (temp > 0) {
			printf("  │ %4d │ %8lu │ %7.1f/s │ %5.1f°C │ %s │\n",
			       sec, (unsigned long)total, instant_rate, temp,
			       throttled ? "  YES   " : "   --   ");
		} else {
			printf("  │ %4d │ %8lu │ %7.1f/s │   n/a  │ %s │\n",
			       sec, (unsigned long)total, instant_rate,
			       throttled ? "  YES   " : "   --   ");
		}
		fflush(stdout);

		last_total = total;
		ts_last = ts_now;
		last_rate = instant_rate;
	}

	running = 0;
	for (int i = 0; i < num_threads; i++)
		pthread_join(threads[i], NULL);

	uint64_t grand_total = 0;
	for (int i = 0; i < num_threads; i++)
		grand_total += atomic_load_explicit(&ctxs[i].hash_count, memory_order_relaxed);

	clock_gettime(CLOCK_MONOTONIC, &ts_now);
	double total_wall = (ts_now.tv_sec - ts_start.tv_sec)
	                  + (ts_now.tv_nsec - ts_start.tv_nsec) / 1e9;
	double final_rate = (double)grand_total / total_wall;

	printf("  └──────┴──────────┴───────────┴────────┴──────────┘\n\n");

	printf("╔══════════════════════════════════════════════════════════════╗\n");
	printf("║                     BENCHMARK RESULTS                      ║\n");
	printf("╠══════════════════════════════════════════════════════════════╣\n");
	printf("║  Total hashes:    %-10lu                              ║\n", (unsigned long)grand_total);
	printf("║  Wall time:       %.1f seconds                          ║\n", total_wall);
	printf("║  Avg hashrate:    %.1f H/s  (%.1f H/s per thread)      ║\n",
	       final_rate, final_rate / num_threads);
	printf("║  Peak hashrate:   %.1f H/s                             ║\n", peak_rate);
	printf("║                                                            ║\n");
	if (baseline_temp > 0) {
		printf("║  Baseline temp:   %.1f°C                                ║\n", baseline_temp);
		printf("║  Peak temp:       %.1f°C  (Δ%.1f°C)                    ║\n",
		       peak_temp, peak_temp - baseline_temp);
	}
	printf("║  Throttle events: %d                                     ║\n", throttle_events);
	printf("║  Threads:         %d                                     ║\n", num_threads);
	printf("╚══════════════════════════════════════════════════════════════╝\n");

	printf("\n  Per-thread breakdown:\n");
	printf("  ┌────────┬──────────┬───────────┐\n");
	printf("  │ thread │  hashes  │    H/s    │\n");
	printf("  ├────────┼──────────┼───────────┤\n");
	for (int i = 0; i < num_threads; i++) {
		uint64_t hc = atomic_load_explicit(&ctxs[i].hash_count, memory_order_relaxed);
		printf("  │   %2d   │ %8lu │ %7.1f/s │\n",
		       i, (unsigned long)hc, (double)hc / total_wall);
	}
	printf("  └────────┴──────────┴───────────┘\n");

	if (g_best_zone >= 0 && history_count > 2) {
		draw_temp_graph(temp_history, rate_history, history_count,
		                temp_min - 1, temp_max + 1,
		                0, peak_rate * 1.1f);
	}

	if (throttle_events > 0) {
		printf("\n  THERMAL THROTTLING DETECTED\n");
		printf("  The device throttled %d time(s) during the benchmark.\n", throttle_events);
		printf("  Peak temp reached %.1f°C (Δ%.1f°C from baseline).\n",
		       peak_temp, peak_temp - baseline_temp);
		float first_rate = rate_history[0];
		float last_r = rate_history[history_count - 1];
		if (first_rate > 0) {
			float pct = ((first_rate - last_r) / first_rate) * 100.0f;
			printf("  Hashrate degradation: %.1f%% (%.1f → %.1f H/s)\n",
			       pct, first_rate, last_r);
		}
	} else if (g_best_zone >= 0) {
		printf("\n  No thermal throttling detected during benchmark.\n");
	}

	printf("\n  Fairchain sha256mem — CPU PoW benchmark (optimized)\n");
	printf("  https://github.com/bams-repo/go-chain\n\n");

	free(temp_history);
	free(rate_history);
	free(threads);
	free(ctxs);
	return 0;
}
