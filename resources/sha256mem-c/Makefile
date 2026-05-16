# sha256mem C reference — build from this directory: make test
CC       = gcc
CFLAGS   = -O2 -Wall -Wextra
NATIVE_O3_CFLAGS = -O3 -march=native -Wall -Wextra
# Note: clang builds this TU ~2× faster than gcc here; flto on gcc regressed badly.
BENCH_MT_CC := $(shell command -v clang >/dev/null 2>&1 && echo clang || echo gcc)
CPU_MT_CFLAGS = -O3 -march=native -Wall -Wextra -pthread -msha -fomit-frame-pointer -funroll-loops
LDFLAGS  = -lcrypto
CL_FLAGS = -lOpenCL -lcrypto -lm

CORE    = core
SHA     = sha
OPENCL  = opencl
BENCH   = bench
MINERS  = miners
TEST    = test
BUILD   = build

INC = -I$(CORE) -I$(SHA)
KERNEL_V4        = $(OPENCL)/sha256mem_v4_gpu.cl
KERNEL_TMTO_V1   = $(OPENCL)/sha256mem_v4_tmto_gpu.cl
KERNEL_TMTO_V2   = $(OPENCL)/sha256mem_v2_tmto_gpu.cl
VECTORS          = $(BUILD)/test_vectors.txt

$(BUILD):
	mkdir -p $(BUILD)

all: $(BUILD)/test_sha256mem

$(BUILD)/test_sha256mem: $(TEST)/test_sha256mem.c $(CORE)/sha256mem.c $(CORE)/sha256mem.h | $(BUILD)
	$(CC) $(CFLAGS) $(INC) -o $@ $(TEST)/test_sha256mem.c $(CORE)/sha256mem.c $(LDFLAGS)

bench_cpu: $(BENCH)/bench_sha256mem.c $(CORE)/sha256mem.c $(CORE)/sha256mem.h | $(BUILD)
	$(CC) $(NATIVE_O3_CFLAGS) $(INC) -o $(BUILD)/$@ $(BENCH)/bench_sha256mem.c $(CORE)/sha256mem.c $(LDFLAGS)

bench_cpu_mt: $(BENCH)/bench_cpu_mt.c $(CORE)/sha256mem_tmto.c $(CORE)/sha256mem_tmto.h $(SHA)/sha256_shani.h | $(BUILD)
	$(BENCH_MT_CC) $(CPU_MT_CFLAGS) $(INC) -o $(BUILD)/$@ $(BENCH)/bench_cpu_mt.c $(CORE)/sha256mem_tmto.c

bench_cpu_mt_linear: $(BENCH)/bench_cpu_mt_linear.c $(CORE)/sha256mem_shani.c $(CORE)/sha256mem.h $(SHA)/sha256_shani.h | $(BUILD)
	$(CC) $(CPU_MT_CFLAGS) $(INC) -o $(BUILD)/$@ $(BENCH)/bench_cpu_mt_linear.c $(CORE)/sha256mem_shani.c

gpu_miner: $(MINERS)/gpu_miner.c $(KERNEL_V4) $(KERNEL_TMTO_V1) | $(BUILD)
	$(CC) -O3 -Wall -Wextra -o $(BUILD)/$@ $(MINERS)/gpu_miner.c $(CL_FLAGS)

gpu_v4: $(BENCH)/bench_gpu_v4.c $(CORE)/sha256mem.c $(CORE)/sha256mem.h $(KERNEL_V4) | $(BUILD)
	$(CC) -O3 -Wall -Wextra $(INC) -o $(BUILD)/bench_gpu_v4 $(BENCH)/bench_gpu_v4.c $(CORE)/sha256mem.c $(CL_FLAGS)

# v2 TMTO GPU benchmark + stratum miner (default gpu target)
bench_gpu_tmto: $(BENCH)/bench_gpu_tmto.c $(CORE)/sha256mem_v2.c $(CORE)/sha256mem_v2.h $(KERNEL_TMTO_V2) | $(BUILD)
	$(CC) -O3 -Wall -Wextra $(INC) -o $(BUILD)/$@ $(BENCH)/bench_gpu_tmto.c $(CORE)/sha256mem_v2.c $(CL_FLAGS)

stratum_miner: $(MINERS)/stratum_miner.c $(CORE)/sha256mem_v2.c $(CORE)/sha256mem_v2.h $(KERNEL_TMTO_V2) | $(BUILD)
	$(CC) -O3 -Wall -Wextra $(INC) -o $(BUILD)/$@ $(MINERS)/stratum_miner.c $(CORE)/sha256mem_v2.c -lOpenCL -lcrypto -ljansson -lm

gpu: bench_gpu_tmto

vectors: | $(BUILD)
	cd ../.. && go run ./internal/algorithms/sha256mem/c/gen_vectors.go ./resources/sha256mem-c/$(VECTORS)

verify_tmto_vs_linear: $(TEST)/verify_tmto_vs_linear.c $(CORE)/sha256mem_tmto.c $(CORE)/sha256mem.c $(CORE)/sha256mem_tmto.h $(CORE)/sha256mem.h $(SHA)/sha256_shani.h | $(BUILD)
	$(CC) -O2 -march=native -msha $(INC) -o $(BUILD)/$@ $(TEST)/verify_tmto_vs_linear.c $(CORE)/sha256mem_tmto.c $(CORE)/sha256mem.c $(LDFLAGS)

verify_v2_tmto: $(TEST)/verify_v2_tmto.c $(CORE)/sha256mem_v2_tmto.c $(CORE)/sha256mem_v2.c $(CORE)/sha256mem_v2_tmto.h $(CORE)/sha256mem_v2.h $(SHA)/sha256_shani.h | $(BUILD)
	$(CC) -O2 -march=native -msha $(INC) -o $(BUILD)/$@ $(TEST)/verify_v2_tmto.c $(CORE)/sha256mem_v2_tmto.c $(CORE)/sha256mem_v2.c $(LDFLAGS)

test: vectors $(BUILD)/test_sha256mem
	./$(BUILD)/test_sha256mem $(VECTORS)

clean:
	rm -f $(BUILD)/*

.PHONY: all vectors test clean gpu gpu_v4 bench_cpu bench_cpu_mt bench_cpu_mt_linear verify_tmto_vs_linear verify_v2_tmto gpu_miner bench_gpu_tmto stratum_miner
