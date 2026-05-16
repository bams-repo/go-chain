# sha256mem-c

C reference implementation and mining/benchmark tooling for consensus **sha256mem**.

## Directory layout

```
sha256mem-c/
├── Makefile      # build from this directory
├── build/        # binaries and generated test_vectors.txt (gitignored)
├── core/         # sha256mem.c, v2, TMTO, SHANI, bare/fast/asm variants
├── sha/          # SHA-256 primitives (SHANI, bare, asm)
├── opencl/       # OpenCL kernels (v4 consensus, v2 TMTO for mining)
├── bench/        # CPU and GPU benchmark harnesses
├── miners/       # gpu_miner (RPC v1), stratum_miner (pool v2 TMTO)
├── test/         # vector harness, TMTO vs linear verifier
└── tools/        # verify_stratum.go (header reconstruction helper)
```

## Make targets

| Target | Output | Description |
|--------|--------|-------------|
| `test` | — | Regenerate vectors + run `test/test_sha256mem` |
| `bench_cpu` | `build/bench_cpu` | Single-thread OpenSSL-backed bench |
| `bench_cpu_mt` | `build/bench_cpu_mt` | Multi-thread TMTO + SHA-NI |
| `bench_cpu_mt_linear` | `build/bench_cpu_mt_linear` | Multi-thread 64 MiB linear scratch |
| `verify_tmto_vs_linear` | `build/verify_tmto_vs_linear` | Sanity: TMTO ≡ linear on sample inputs |
| `gpu_v4` | `build/bench_gpu_v4` | OpenCL v4 kernel vs `core/sha256mem.c` |
| `bench_gpu_tmto` | `build/bench_gpu_tmto` | OpenCL **v2** TMTO hashrate bench (`make gpu`) |
| `stratum_miner` | `build/stratum_miner` | Stratum v1 pool miner (**v2** TMTO kernel) |
| `verify_v2_tmto` | `build/verify_v2_tmto` | CPU: linear v2 ≡ TMTO v2 |
| `gpu_miner` | `build/gpu_miner` | RPC miner (`--tmto` for TMTO kernel) |

**v2 (experimental):** `core/sha256mem_v2.c` + `core/sha256mem_v2_tmto.c` + `opencl/sha256mem_v2_tmto_gpu.cl` — progression harden, `2×16384` mix, raw SHA256 final (no byte-reverse). `make verify_v2_tmto` then `make bench_gpu_tmto`.

Run GPU benches and miners from **this directory** so `opencl/*.cl` resolves.

### Android bench

`bench/bench_android.c` is self-contained for Termux (not wired into the Makefile):

```bash
clang -O3 -march=armv8-a+crypto -o sha256mem_bench bench/bench_android.c -lssl -lcrypto -lm -pthread
```

### Stratum header tool

```bash
go run tools/verify_stratum.go <prevhash> <cb1> <cb2> <en1> <en2> <version> <bits> <ntime>
```
