# resources/

Non-Go reference code, assets, and standalone tools. Nothing here is built by `go build ./...`.

## Layout

| Path | Purpose |
|------|---------|
| [`sha256mem-c/`](sha256mem-c/) | C/OpenCL reference for the **sha256mem** PoW (parity tests, benchmarks, miners) |
| [`benchmarks/mobile/`](benchmarks/mobile/) | Device benchmark screenshots (e.g. Termux / phone runs) |
| [`branding/`](branding/) | Logos and comparison artwork |

## sha256mem-c

Independent C implementation used to validate `internal/algorithms/sha256mem` and to benchmark CPU/GPU mining paths.

```bash
cd resources/sha256mem-c
make test          # Go vectors + C harness (1000/1000 parity)
make bench_gpu_tmto   # GPU TMTO hashrate bench (default `make gpu`)
make stratum_miner    # Stratum pool miner binary → build/stratum_miner
```

See [`sha256mem-c/README.md`](sha256mem-c/README.md) for directory layout and targets.

Requires `gcc`, `libssl-dev`, and for GPU/miner targets: OpenCL, `libjansson`, `libcurl` (gpu_miner only).
