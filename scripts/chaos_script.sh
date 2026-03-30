#!/usr/bin/env bash
# Wrapper for scripts/chaos_test.sh (12-node chaos + adversarial test).
# Miner nodes are started with GOMAXPROCS=1 (and taskset on Linux/WSL).
exec "$(cd "$(dirname "$0")" && pwd)/chaos_test.sh" "$@"
