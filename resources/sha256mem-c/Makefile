CC       = gcc
CFLAGS   = -O2 -Wall -Wextra
LDFLAGS  = -lcrypto
CL_FLAGS = -lOpenCL -lcrypto -lm

all: test_sha256mem

test_sha256mem: test_sha256mem.c sha256mem.c sha256mem.h
	$(CC) $(CFLAGS) -o $@ test_sha256mem.c sha256mem.c $(LDFLAGS)

# CPU benchmark (OpenSSL-backed)
bench_cpu: bench_sha256mem.c sha256mem.c sha256mem.h
	$(CC) $(CFLAGS) -o $@ bench_sha256mem.c sha256mem.c $(LDFLAGS)

# Original (naive) GPU benchmark
gpu_naive: bench_gpu.c sha256mem_gpu.cl
	$(CC) -O2 -o sha256mem_gpu bench_gpu.c $(CL_FLAGS)

# Optimized GPU benchmark
gpu_fast: bench_gpu_fast.c sha256mem_gpu_fast.cl
	$(CC) -O2 -o sha256mem_gpu_fast bench_gpu_fast.c $(CL_FLAGS)

# GPU hash validator (original kernel)
validate_gpu: validate_gpu.c sha256mem_gpu.cl
	$(CC) -O2 -o $@ validate_gpu.c -lssl $(CL_FLAGS)

# GPU miner (connects to daemon, submits blocks)
gpu_miner: gpu_miner.c sha256mem_gpu_fast.cl
	$(CC) -O2 -o $@ gpu_miner.c -lOpenCL -lcrypto -lcurl -lm

# sha256mem v4 GPU benchmark (ChaCha20 fill + XOR-rotate mix)
gpu_v4: bench_gpu_v4.c sha256mem_v4_gpu.cl
	$(CC) -O2 -o bench_gpu_v4 bench_gpu_v4.c $(CL_FLAGS)

gpu: gpu_fast

vectors: gen_vectors.go
	cd ../../../.. && go run ./internal/algorithms/sha256mem/c/gen_vectors.go
	mv ../../../../test_vectors.txt ./ 2>/dev/null || true

test: vectors test_sha256mem
	./test_sha256mem test_vectors.txt

clean:
	rm -f test_sha256mem test_vectors.txt sha256mem_gpu sha256mem_gpu_fast sha256mem_bench bench_cpu validate_gpu gpu_miner bench_gpu_v4

.PHONY: all vectors test clean gpu gpu_naive gpu_fast gpu_v4 bench_cpu gpu_miner
