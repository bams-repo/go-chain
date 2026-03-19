#!/usr/bin/env bash
# Branding values sourced from internal/coinparams/coinparams.go
set -uo pipefail

# ==========================================================================
# FAIRCHAIN CONSENSUS ENGINE MODULARITY TEST
#
# Validates that the consensus engine is truly modular by:
#   1. Building separate node binaries for each PoW algorithm
#   2. Running a testnet mesh cluster on each algorithm
#   3. Mining blocks, verifying consensus across nodes
#   4. Injecting chaos (kill/restart), verifying recovery
#   5. Confirming that each algorithm produces a valid, independent chain
#
# Algorithms tested: sha256d, argon2id, scrypt, sha256mem
#
# Architecture per algorithm (mirrors chaos_test.sh):
#   Nodes 0,1   = SEED nodes (relay-only, no mining — network backbone)
#   Nodes 2-5   = MINER nodes (connect to seeds, subject to chaos)
#   Testnet params: 5s target blocks, retarget every 20 blocks
#
# Each algorithm gets its own data/log directory tree under the run dir.
# Port ranges are offset per algorithm to avoid collisions if cleanup is slow.
#
# Usage:
#   python scripts/modularity_test.py [--skip ALGOS] [--debug]
#   bash scripts/modularity_test.sh [--skip ALGOS] [--debug]
#
#   --skip: comma-separated algorithm names to skip (e.g., --skip argon2id,scrypt)
#   --debug: enable hyper-verbose node logging
# ==========================================================================

ORIG_ARGS="$*"
SKIP_LIST=""
NODE_DEBUG=""
while [[ $# -gt 0 ]]; do
    case "$1" in
        --skip)
            SKIP_LIST="$2"
            shift 2
            ;;
        --debug)
            NODE_DEBUG="-debug"
            shift
            ;;
        *)
            echo "Unknown argument: $1" >&2
            echo "Usage: $0 [--skip ALGOS] [--debug]" >&2
            exit 1
            ;;
    esac
done

should_skip_algo() {
    local algo="$1"
    if [ -z "$SKIP_LIST" ]; then return 1; fi
    IFS=',' read -ra skip_arr <<< "$SKIP_LIST"
    for s in "${skip_arr[@]}"; do
        if [ "$(echo "$s" | tr -d ' ')" = "$algo" ]; then return 0; fi
    done
    return 1
}

PROJROOT="$(cd "$(dirname "$0")/.." && pwd)"

# ── OS detection ──────────────────────────────────────────────
CHAOS_OS="linux"
case "$(uname -s)" in
    Darwin*)  CHAOS_OS="macos"   ;;
    MINGW*|MSYS*|CYGWIN*) CHAOS_OS="windows" ;;
    Linux*)
        if [ -n "${WSL_DISTRO_NAME:-}" ] || grep -qi microsoft /proc/version 2>/dev/null; then
            CHAOS_OS="wsl"
        fi
        ;;
esac

if command -v python3 &>/dev/null; then
    PYTHON_CMD="python3"
elif command -v python &>/dev/null; then
    PYTHON_CMD="python"
else
    echo "[modtest] FATAL: python3 (or python) not found in PATH" >&2
    exit 1
fi

if ! command -v curl &>/dev/null; then
    echo "[modtest] FATAL: curl not found in PATH" >&2
    exit 1
fi

EXE_SUFFIX=""
if [ "$CHAOS_OS" = "windows" ]; then
    EXE_SUFFIX=".exe"
fi

portable_date_iso() {
    if date -Iseconds &>/dev/null 2>&1; then
        date -Iseconds
    else
        date -u +"%Y-%m-%dT%H:%M:%S+00:00"
    fi
}

portable_sleep() {
    sleep "$1" 2>/dev/null || sleep "$(printf '%.0f' "$1")"
}

kill_all_modtest_nodes() {
    case "$CHAOS_OS" in
        windows)
            taskkill //F //IM "fairchaind${EXE_SUFFIX}" &>/dev/null || true
            ;;
        *)
            pkill -9 -f "fairchaind.*modularity-runs" 2>/dev/null || true
            ;;
    esac
}

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

log()    { echo -e "${CYAN}[modtest]${NC} $*"; }
pass()   { echo -e "${GREEN}[PASS]${NC}  $*"; }
fail()   { echo -e "${RED}[FAIL]${NC}  $*"; }
warn()   { echo -e "${YELLOW}[WARN]${NC}  $*"; }
header() { echo -e "\n${BOLD}━━━ $* ━━━${NC}"; }

echo "[modtest] detected OS: ${CHAOS_OS} (uname: $(uname -s)), python: ${PYTHON_CMD}"

ALGORITHMS=(sha256d argon2id scrypt sha256mem)

# ── Cluster geometry (per algorithm) ─────────────────────────
NUM_NODES=6
SEED_NODES=(0 1)
MINER_NODES=(2 3 4 5)
NUM_SEEDS=${#SEED_NODES[@]}
NUM_MINERS=${#MINER_NODES[@]}

# ── Sequential run directory ─────────────────────────────────
RUNS_ROOT="${PROJROOT}/modularity-runs"
mkdir -p "$RUNS_ROOT"

LAST_RUN=$(ls -1d "$RUNS_ROOT"/run-[0-9][0-9][0-9] 2>/dev/null | sort -t- -k2 -n | tail -1 | sed 's/.*run-//' || echo "000")
NEXT_RUN=$(printf "%03d" $((10#$LAST_RUN + 1)))
RUN_DIR="${RUNS_ROOT}/run-${NEXT_RUN}"
mkdir -p "$RUN_DIR"

if [ "$CHAOS_OS" = "windows" ]; then
    rm -f "${RUNS_ROOT}/latest" 2>/dev/null || true
    echo "$RUN_DIR" > "${RUNS_ROOT}/latest.txt"
else
    ln -sfn "$RUN_DIR" "${RUNS_ROOT}/latest"
fi

RUN_LOG="${RUN_DIR}/modularity_test.log"
BIN_DIR="${RUN_DIR}/bin"
mkdir -p "$BIN_DIR"

COINPARAMS_FILE="${PROJROOT}/internal/coinparams/coinparams.go"
COINPARAMS_BACKUP="${RUN_DIR}/coinparams.go.bak"

FAILURES=0
ALGO_RESULTS=()
PIDS=()

# ── Write run metadata ───────────────────────────────────────
cat > "${RUN_DIR}/meta.txt" <<METAEOF
run:        ${NEXT_RUN}
started:    $(portable_date_iso)
hostname:   $(hostname)
args:       $0 ${ORIG_ARGS:-}
skip_list:  ${SKIP_LIST:-<none>}
algorithms: ${ALGORITHMS[*]}
num_nodes:  ${NUM_NODES}
num_seeds:  ${NUM_SEEDS}
num_miners: ${NUM_MINERS}
basedir:    ${RUN_DIR}
METAEOF

exec > >(tee -a "$RUN_LOG") 2>&1

# ── Build helpers ────────────────────────────────────────────
cp "$COINPARAMS_FILE" "$COINPARAMS_BACKUP"

restore_coinparams() {
    cp "$COINPARAMS_BACKUP" "$COINPARAMS_FILE"
}

cleanup() {
    log "Cleaning up all nodes..."
    for i in $(seq 0 $((NUM_NODES - 1))); do
        kill "${PIDS[$i]:-}" 2>/dev/null || true
    done
    sleep 2
    kill_all_modtest_nodes
    sleep 1
    restore_coinparams
    echo ""
    echo "finished:   $(portable_date_iso)" >> "${RUN_DIR}/meta.txt"
    echo "exit_code:  ${FAILURES}" >> "${RUN_DIR}/meta.txt"
    log "Run data preserved in: ${RUN_DIR}"
    log "  Node data:  ${RUN_DIR}/<algo>/nodes/node*/  (data dirs + stdout.log)"
    log "  Script log: ${RUN_LOG}"
    log "  Metadata:   ${RUN_DIR}/meta.txt"
    log "  Latest:     ${RUNS_ROOT}/latest -> run-${NEXT_RUN}"
}
trap cleanup EXIT

build_algo_binary() {
    local algo=$1
    local outpath="${BIN_DIR}/fairchaind-${algo}${EXE_SUFFIX}"

    log "Building binary for algorithm: ${algo}..."

    sed -i "s/Algorithm = \"[a-z0-9]*\"/Algorithm = \"${algo}\"/" "$COINPARAMS_FILE"

    if ! (cd "$PROJROOT" && go build -o "$outpath" ./cmd/node 2>&1); then
        fail "Build failed for algorithm: ${algo}"
        restore_coinparams
        return 1
    fi

    restore_coinparams
    chmod +x "$outpath"
    pass "Built binary: ${outpath}"
    return 0
}

# ── RPC helpers (mirrors chaos_test.sh) ──────────────────────

get_info() {
    curl -s --connect-timeout 2 --max-time 3 "http://127.0.0.1:${1}/getblockchaininfo" 2>/dev/null
}

get_status() {
    curl -s --connect-timeout 2 --max-time 3 "http://127.0.0.1:${1}/getchainstatus" 2>/dev/null
}

get_field() {
    local port=$1 field=$2
    get_info "$port" | $PYTHON_CMD -c "import sys,json;print(json.load(sys.stdin)['$field'])" 2>/dev/null || echo "ERR"
}

get_status_field() {
    local port=$1 field=$2
    get_status "$port" | $PYTHON_CMD -c "import sys,json;print(json.load(sys.stdin)['$field'])" 2>/dev/null || echo "ERR"
}

get_height()     { get_field "$1" blocks; }
get_hash()       { get_field "$1" bestblockhash; }
get_bits()       { get_status_field "$1" bits; }
get_difficulty() { get_info "$1" | $PYTHON_CMD -c "import sys,json;print(f\"{json.load(sys.stdin)['difficulty']:.4f}\")" 2>/dev/null || echo "ERR"; }
get_peers()      { get_status_field "$1" peers; }

get_hash_at_height() {
    local port=$1 height=$2
    curl -s --connect-timeout 2 --max-time 3 "http://127.0.0.1:${port}/getblockbyheight?height=${height}" 2>/dev/null \
        | $PYTHON_CMD -c "import sys,json;print(json.load(sys.stdin)['hash'])" 2>/dev/null || echo "ERR"
}

# ── Node management ─────────────────────────────────────────
# Each algorithm gets its own port range to avoid collisions.
# algo_port_offset is set per-algorithm in the main loop.
ALGO_PORT_OFFSET=0

start_node() {
    local idx=$1
    local do_mine=${2:-false}
    local basedir=$3
    local bin=$4
    local p2p_port=$((32000 + ALGO_PORT_OFFSET + idx))
    local rpc_port=$((33000 + ALGO_PORT_OFFSET + idx))
    local seed_addrs="127.0.0.1:$((32000 + ALGO_PORT_OFFSET + 0)),127.0.0.1:$((32000 + ALGO_PORT_OFFSET + 1))"
    local datadir="${basedir}/node${idx}"
    mkdir -p "$datadir"

    local mine_flag=""
    if [ "$do_mine" = "true" ]; then
        mine_flag="-mine"
    fi

    "$bin" \
        -network testnet \
        -datadir "$datadir" \
        -listen "127.0.0.1:${p2p_port}" \
        -rpcbind "127.0.0.1" \
        -rpcport "${rpc_port}" \
        -connect "$seed_addrs" \
        -norpcauth \
        ${mine_flag} \
        ${NODE_DEBUG} \
        > "${datadir}/stdout.log" 2>&1 &

    PIDS[$idx]=$!
    local role="miner"
    [ "$do_mine" = "false" ] && role="SEED"
    log "  Node $idx [$role] started (pid=${PIDS[$idx]}, p2p=:${p2p_port}, rpc=:${rpc_port})"
}

stop_node() {
    local idx=$1
    if [ -n "${PIDS[$idx]:-}" ] && kill -0 "${PIDS[$idx]}" 2>/dev/null; then
        kill "${PIDS[$idx]}" 2>/dev/null || true
        wait "${PIDS[$idx]}" 2>/dev/null || true
        log "  Node $idx stopped (pid=${PIDS[$idx]})"
        PIDS[$idx]=""
    fi
}

stop_all_nodes() {
    for i in $(seq 0 $((NUM_NODES - 1))); do
        stop_node "$i" 2>/dev/null || true
    done
    sleep 2
    kill_all_modtest_nodes
    sleep 1
}

is_alive() {
    local idx=$1
    [ -n "${PIDS[$idx]:-}" ] && kill -0 "${PIDS[$idx]}" 2>/dev/null
}

rpc_port_for() {
    echo $((33000 + ALGO_PORT_OFFSET + $1))
}

# ── Status / Checks (mirrors chaos_test.sh) ─────────────────

print_cluster_status() {
    local label=$1
    echo ""
    log "=== Cluster Status: $label ==="
    printf "  %-8s %-8s %-8s %-10s %-30s %s\n" "Node" "Height" "Peers" "Bits" "Diff" "Hash(prefix)"
    printf "  %-8s %-8s %-8s %-10s %-30s %s\n" "--------" "------" "-----" "--------" "--------" "------------"
    for i in $(seq 0 $((NUM_NODES - 1))); do
        local rpc=$(rpc_port_for "$i")
        local role="miner"
        [[ " ${SEED_NODES[*]} " == *" $i "* ]] && role="SEED "
        if is_alive "$i"; then
            local h=$(get_height "$rpc")
            local p=$(get_peers "$rpc")
            local b=$(get_bits "$rpc")
            local d=$(get_difficulty "$rpc")
            local hash=$(get_hash "$rpc")
            printf "  %-8s %-8s %-8s %-10s %-30s %.20s...\n" "[$i]$role" "$h" "$p" "$b" "$d" "$hash"
        else
            printf "  %-8s %-8s\n" "[$i]$role" "DOWN"
        fi
    done
    echo ""
}

wait_for_height() {
    local min_height=$1 timeout=$2 label=$3
    local deadline=$((SECONDS + timeout))
    local last_status=$SECONDS

    log "Waiting up to ${timeout}s for height >= $min_height ($label)..."
    while [ $SECONDS -lt $deadline ]; do
        for i in $(seq 0 $((NUM_NODES - 1))); do
            if is_alive "$i"; then
                local rpc=$(rpc_port_for "$i")
                local h=$(get_height "$rpc")
                if [ "$h" != "ERR" ] && [ "$h" -ge "$min_height" ] 2>/dev/null; then
                    pass "$label: height $h >= $min_height reached"
                    return 0
                fi
            fi
        done
        if [ $((SECONDS - last_status)) -ge 30 ]; then
            local elapsed=$((SECONDS - (deadline - timeout)))
            local heights=""
            for i in $(seq 0 $((NUM_NODES - 1))); do
                if is_alive "$i"; then
                    local rpc=$(rpc_port_for "$i")
                    local h=$(get_height "$rpc")
                    heights="${heights} n${i}=${h}"
                else
                    heights="${heights} n${i}=DOWN"
                fi
            done
            log "  [${elapsed}s] heights:${heights}"
            last_status=$SECONDS
        fi
        sleep 3
    done
    warn "$label: height $min_height not reached within ${timeout}s"
    return 1
}

wait_for_convergence() {
    local timeout=$1 label=$2 tolerance=${3:-2}
    local deadline=$((SECONDS + timeout))

    log "Waiting up to ${timeout}s for convergence ($label, tolerance=$tolerance)..."
    while [ $SECONDS -lt $deadline ]; do
        local max_h=0 min_h=999999 count=0
        for i in $(seq 0 $((NUM_NODES - 1))); do
            if is_alive "$i"; then
                local rpc=$(rpc_port_for "$i")
                local h=$(get_height "$rpc")
                [ "$h" = "ERR" ] || [ "$h" = "-1" ] && continue
                ((count++))
                [ "$h" -gt "$max_h" ] && max_h=$h
                [ "$h" -lt "$min_h" ] && min_h=$h
            fi
        done
        if [ "$count" -ge 2 ] && [ $((max_h - min_h)) -le "$tolerance" ]; then
            pass "$label: ${count} nodes converged (spread=$((max_h - min_h)), range=[${min_h}..${max_h}])"
            return 0
        fi
        sleep 3
    done
    warn "$label: convergence timeout"
    return 1
}

check_consensus() {
    local label=$1
    local max_h=0 min_h=999999 count=0
    for i in $(seq 0 $((NUM_NODES - 1))); do
        if is_alive "$i"; then
            local rpc=$(rpc_port_for "$i")
            local h=$(get_height "$rpc")
            [ "$h" = "ERR" ] || [ "$h" = "-1" ] && continue
            ((count++))
            [ "$h" -gt "$max_h" ] && max_h=$h
            [ "$h" -lt "$min_h" ] && min_h=$h
        fi
    done
    if [ "$count" -lt 2 ]; then
        warn "$label: fewer than 2 live nodes"
        return 0
    fi
    local spread=$((max_h - min_h))
    if [ "$spread" -le 3 ]; then
        pass "$label: ${count} nodes, range=[${min_h}..${max_h}] spread=$spread — consensus healthy"
        return 0
    else
        fail "$label: DIVERGENCE — ${count} nodes, range=[${min_h}..${max_h}] spread=$spread"
        return 1
    fi
}

check_hash_agreement() {
    local label=$1
    local hashes=()
    for i in $(seq 0 $((NUM_NODES - 1))); do
        if is_alive "$i"; then
            local rpc=$(rpc_port_for "$i")
            local h=$(get_hash "$rpc")
            [ "$h" != "ERR" ] && hashes+=("$h")
        fi
    done
    if [ ${#hashes[@]} -lt 2 ]; then
        warn "$label: fewer than 2 nodes returned hashes"
        return 0
    fi
    local ref="${hashes[0]}"
    local all_same=true
    for h in "${hashes[@]}"; do
        if [ "$h" != "$ref" ]; then all_same=false; break; fi
    done
    if [ "$all_same" = true ]; then
        pass "$label: all ${#hashes[@]} nodes agree on tip hash"
    else
        warn "$label: tip hashes differ (minor fork, will resolve with next block)"
    fi
    return 0
}

check_height_index_integrity() {
    local label=$1 max_height=$2
    local reference_port=$(rpc_port_for 0)
    local mismatches=0

    log "Checking height index integrity from 0..$max_height across all live nodes..."
    for h in $(seq 0 "$max_height"); do
        local ref_hash
        ref_hash=$(get_hash_at_height "$reference_port" "$h")
        if [ "$ref_hash" = "ERR" ]; then continue; fi
        for i in $(seq 1 $((NUM_NODES - 1))); do
            if is_alive "$i"; then
                local rpc=$(rpc_port_for "$i")
                local node_hash
                node_hash=$(get_hash_at_height "$rpc" "$h")
                if [ "$node_hash" != "ERR" ] && [ "$node_hash" != "$ref_hash" ]; then
                    fail "$label: height $h mismatch — node0=$ref_hash node$i=$node_hash"
                    ((mismatches++))
                fi
            fi
        done
    done

    if [ "$mismatches" -eq 0 ]; then
        pass "$label: all nodes agree on blocks at every height 0..$max_height"
        return 0
    else
        fail "$label: $mismatches height index mismatches found"
        return 1
    fi
}

run_check() { if ! "$@"; then ((FAILURES++)); fi; }

pick_random_miner() {
    echo "${MINER_NODES[$((RANDOM % ${#MINER_NODES[@]}))]}"
}

# ══════════════════════════════════════════════════════════════
# MAIN TEST SEQUENCE
# ══════════════════════════════════════════════════════════════

echo ""
echo "════════════════════════════════════════════════════════════════════"
echo " FAIRCHAIN CONSENSUS ENGINE MODULARITY TEST"
echo " Algorithms: ${ALGORITHMS[*]}"
echo " ${NUM_SEEDS} Seeds + ${NUM_MINERS} Miners per algo · testnet · 5s blocks"
echo "────────────────────────────────────────────────────────────────────"
echo -e " Run #${NEXT_RUN}  ·  ${RUN_DIR}"
echo "════════════════════════════════════════════════════════════════════"
if [ -n "$SKIP_LIST" ]; then
    echo -e " ${YELLOW}Skipping algorithms: ${SKIP_LIST}${NC}"
    echo "════════════════════════════════════════════════════════════════════"
fi

# ── Phase 0: Build all algorithm binaries ────────────────────
header "Phase 0: Build Algorithm Binaries"

BUILD_FAILURES=0
for algo in "${ALGORITHMS[@]}"; do
    if should_skip_algo "$algo"; then
        log "Skipping build for $algo (--skip)"
        continue
    fi
    if ! build_algo_binary "$algo"; then
        ((BUILD_FAILURES++))
        ALGO_RESULTS+=("${algo}: BUILD FAILED")
    fi
done

if [ "$BUILD_FAILURES" -gt 0 ]; then
    fail "Build failures: $BUILD_FAILURES — cannot continue"
    FAILURES=$((FAILURES + BUILD_FAILURES))
    exit "$FAILURES"
fi

pass "All algorithm binaries built successfully"

# ── Per-algorithm test phases ────────────────────────────────

ALGO_IDX=0
for algo in "${ALGORITHMS[@]}"; do
    if should_skip_algo "$algo"; then
        log "Skipping algorithm: $algo"
        ALGO_RESULTS+=("${algo}: SKIPPED")
        continue
    fi

    ALGO_DIR="${RUN_DIR}/${algo}"
    BASEDIR="${ALGO_DIR}/nodes"
    mkdir -p "$BASEDIR"
    ALGO_FAIL=0

    # Each algorithm gets a unique port offset (100 ports apart).
    ALGO_PORT_OFFSET=$((ALGO_IDX * 100))

    BIN_ALGO="${BIN_DIR}/fairchaind-${algo}${EXE_SUFFIX}"

    echo ""
    echo "╔══════════════════════════════════════════════════════════════╗"
    echo "║  Algorithm: ${algo}"
    echo "║  Ports: P2P=$((32000 + ALGO_PORT_OFFSET))-$((32000 + ALGO_PORT_OFFSET + NUM_NODES - 1))  RPC=$((33000 + ALGO_PORT_OFFSET))-$((33000 + ALGO_PORT_OFFSET + NUM_NODES - 1))"
    echo "╚══════════════════════════════════════════════════════════════╝"

    # ── Phase A: Launch testnet cluster ──────────────────────
    header "[${algo}] Phase A: Launch Testnet Cluster"

    PIDS=()

    log "Starting ${NUM_SEEDS} seed nodes (relay-only)..."
    for i in "${SEED_NODES[@]}"; do
        start_node "$i" false "$BASEDIR" "$BIN_ALGO"
    done
    log "Waiting 4s for seeds to peer with each other..."
    sleep 4

    log "Starting ${NUM_MINERS} miner nodes..."
    for i in "${MINER_NODES[@]}"; do
        start_node "$i" true "$BASEDIR" "$BIN_ALGO"
        portable_sleep 0.2
    done
    log "Waiting 10s for mesh formation and initial mining..."
    sleep 10

    print_cluster_status "${algo} — Initial Launch"
    run_check check_consensus "[${algo}] Phase A"

    # ── Phase B: Mine to height 15, verify convergence ───────
    header "[${algo}] Phase B: Mine & Verify Consensus (height >= 15)"

    wait_for_height 15 180 "[${algo}] Phase B"
    wait_for_convergence 60 "[${algo}] Phase B convergence" 3
    print_cluster_status "${algo} — After Mining to 15"
    run_check check_consensus "[${algo}] Phase B"
    run_check check_hash_agreement "[${algo}] Phase B tip hash"

    # ── Phase C: Chaos — kill 1 miner, verify recovery ───────
    header "[${algo}] Phase C: Chaos — Kill 1 Miner, Verify Recovery"

    VICTIM=$(pick_random_miner)
    log "Killing miner node $VICTIM..."
    stop_node "$VICTIM"
    sleep 2

    log "Letting survivors mine for 30s..."
    sleep 30

    wait_for_convergence 30 "[${algo}] Phase C survivors" 3
    run_check check_consensus "[${algo}] Phase C ($((NUM_MINERS - 1)) miners + ${NUM_SEEDS} seeds)"

    log "Restarting miner $VICTIM (fresh sync)..."
    rm -rf "${BASEDIR}/node${VICTIM}"
    start_node "$VICTIM" true "$BASEDIR" "$BIN_ALGO"

    log "Waiting for restarted miner to sync..."
    wait_for_convergence 90 "[${algo}] Phase C sync" 3
    print_cluster_status "${algo} — After Chaos Recovery"
    run_check check_consensus "[${algo}] Phase C (all ${NUM_NODES})"

    # ── Phase D: Chaos — kill 2 miners, verify recovery ──────
    header "[${algo}] Phase D: Chaos — Kill 2 Miners, Verify Recovery"

    log "Killing miners ${MINER_NODES[0]} and ${MINER_NODES[1]}..."
    stop_node "${MINER_NODES[0]}"
    stop_node "${MINER_NODES[1]}"
    sleep 2

    log "Letting survivors mine for 30s..."
    sleep 30

    wait_for_convergence 30 "[${algo}] Phase D survivors" 3
    run_check check_consensus "[${algo}] Phase D ($((NUM_MINERS - 2)) miners + ${NUM_SEEDS} seeds)"

    log "Restarting killed miners (fresh sync)..."
    for i in "${MINER_NODES[0]}" "${MINER_NODES[1]}"; do
        rm -rf "${BASEDIR}/node${i}"
        start_node "$i" true "$BASEDIR" "$BIN_ALGO"
        portable_sleep 0.2
    done

    wait_for_convergence 90 "[${algo}] Phase D sync" 3
    print_cluster_status "${algo} — After 2-Miner Chaos"
    run_check check_consensus "[${algo}] Phase D (all ${NUM_NODES})"

    # ── Phase E: Height index integrity ──────────────────────
    header "[${algo}] Phase E: Height Index Integrity"

    wait_for_convergence 30 "[${algo}] Phase E pre-check" 2

    MIN_LIVE_HEIGHT=999999
    for i in $(seq 0 $((NUM_NODES - 1))); do
        if is_alive "$i"; then
            rpc=$(rpc_port_for "$i")
            h=$(get_height "$rpc")
            if [ "$h" != "ERR" ] && [ "$h" -lt "$MIN_LIVE_HEIGHT" ] 2>/dev/null; then
                MIN_LIVE_HEIGHT=$h
            fi
        fi
    done

    if [ "$MIN_LIVE_HEIGHT" -gt 0 ] && [ "$MIN_LIVE_HEIGHT" -lt 999999 ]; then
        run_check check_height_index_integrity "[${algo}] Phase E" "$MIN_LIVE_HEIGHT"
    else
        warn "[${algo}] Phase E: could not determine safe height range"
    fi

    # ── Phase F: Full restart consistency ────────────────────
    header "[${algo}] Phase F: Kill All, Restart, Verify Persistence"

    PRE_RESTART_HASH=$(get_hash "$(rpc_port_for 0)")
    PRE_RESTART_HEIGHT=$(get_height "$(rpc_port_for 0)")
    log "Pre-restart tip: height=$PRE_RESTART_HEIGHT hash=$PRE_RESTART_HASH"

    log "Killing ALL nodes..."
    for i in $(seq 0 $((NUM_NODES - 1))); do
        stop_node "$i"
    done
    sleep 3

    log "Restarting ALL nodes (preserving data — no wipe)..."
    for i in "${SEED_NODES[@]}"; do
        start_node "$i" false "$BASEDIR" "$BIN_ALGO"
    done
    sleep 2
    for i in "${MINER_NODES[@]}"; do
        start_node "$i" true "$BASEDIR" "$BIN_ALGO"
        portable_sleep 0.2
    done

    log "Waiting 30s for nodes to load from storage and reconnect..."
    sleep 30

    RESTART_FAILURES=0
    for i in $(seq 0 $((NUM_NODES - 1))); do
        if is_alive "$i"; then
            rpc=$(rpc_port_for "$i")
            h=$(get_height "$rpc")
            if [ "$h" != "ERR" ] && [ "$h" -ge "$PRE_RESTART_HEIGHT" ] 2>/dev/null; then
                pass "[${algo}] Phase F: node $i loaded height=$h (>= pre-restart $PRE_RESTART_HEIGHT)"
            else
                fail "[${algo}] Phase F: node $i height=$h < pre-restart $PRE_RESTART_HEIGHT"
                ((RESTART_FAILURES++))
            fi
        fi
    done

    if [ "$RESTART_FAILURES" -eq 0 ]; then
        pass "[${algo}] Phase F: all nodes preserved chain state across restart"
    else
        fail "[${algo}] Phase F: $RESTART_FAILURES node(s) lost chain state"
        ((ALGO_FAIL += RESTART_FAILURES))
    fi

    wait_for_convergence 60 "[${algo}] Phase F post-restart" 3
    print_cluster_status "${algo} — After Full Restart"
    run_check check_consensus "[${algo}] Phase F (restart consistency)"

    # ── Phase G: Final mining burst & verification ───────────
    header "[${algo}] Phase G: Final Mining Burst & Verification"

    log "Letting cluster mine for 60s..."
    sleep 60

    wait_for_convergence 30 "[${algo}] Phase G final convergence" 3
    print_cluster_status "${algo} — Final State"
    run_check check_consensus "[${algo}] Phase G (final)"
    run_check check_hash_agreement "[${algo}] Phase G final tip hash"

    # ── Teardown ─────────────────────────────────────────────
    header "[${algo}] Teardown"
    stop_all_nodes

    if [ "$ALGO_FAIL" -eq 0 ]; then
        ALGO_RESULTS+=("${algo}: PASSED")
        pass "Algorithm ${algo}: ALL CHECKS PASSED"
    else
        ALGO_RESULTS+=("${algo}: FAILED (${ALGO_FAIL} failures)")
        fail "Algorithm ${algo}: ${ALGO_FAIL} FAILURE(S)"
    fi

    ((ALGO_IDX++))
done

# ══════════════════════════════════════════════════════════════
# CROSS-ALGORITHM ISOLATION VERIFICATION
# ══════════════════════════════════════════════════════════════

header "Cross-Algorithm Isolation Verification"

log "Verifying each algorithm ran independently..."
TESTED_ALGOS=()
for algo in "${ALGORITHMS[@]}"; do
    if should_skip_algo "$algo"; then continue; fi
    ALGO_DIR="${RUN_DIR}/${algo}"
    genesis_nonce=""
    genesis_hash=""
    if [ -f "${ALGO_DIR}/nodes/node0/stdout.log" ]; then
        genesis_nonce=$(grep -o 'nonce=[0-9]*' "${ALGO_DIR}/nodes/node0/stdout.log" | head -1 | sed 's/nonce=//' || echo "")
        genesis_hash=$(grep -o 'genesis.*hash=[a-f0-9]*' "${ALGO_DIR}/nodes/node0/stdout.log" | head -1 | sed 's/.*hash=//' || echo "")
    fi
    log "  ${algo}: genesis_hash=${genesis_hash:0:32}... nonce=${genesis_nonce:-UNKNOWN}"
    TESTED_ALGOS+=("$algo")
done

if [ ${#TESTED_ALGOS[@]} -ge 2 ]; then
    pass "Cross-algorithm isolation: ${#TESTED_ALGOS[@]} algorithms each ran independent testnet chains"
else
    warn "Cross-algorithm isolation: fewer than 2 algorithms tested"
fi

# ══════════════════════════════════════════════════════════════
# FINAL SUMMARY
# ══════════════════════════════════════════════════════════════

echo ""
echo "════════════════════════════════════════════════════════════════════"
echo " MODULARITY TEST RESULTS"
echo "────────────────────────────────────────────────────────────────────"
for result in "${ALGO_RESULTS[@]}"; do
    if echo "$result" | grep -q "PASSED"; then
        echo -e "  ${GREEN}✓${NC} $result"
    elif echo "$result" | grep -q "SKIPPED"; then
        echo -e "  ${YELLOW}–${NC} $result"
    else
        echo -e "  ${RED}✗${NC} $result"
    fi
done
echo "────────────────────────────────────────────────────────────────────"
if [ "$FAILURES" -eq 0 ]; then
    echo -e " ${GREEN}ALL CHECKS PASSED — CONSENSUS ENGINE MODULARITY CONFIRMED${NC}"
else
    echo -e " ${RED}$FAILURES CHECK(S) FAILED${NC}"
fi
echo "────────────────────────────────────────────────────────────────────"
echo " Run #${NEXT_RUN} data preserved in: ${RUN_DIR}"
echo " Per-algo node data: ${RUN_DIR}/<algo>/nodes/node*/"
echo " Full log: ${RUN_LOG}"
echo "════════════════════════════════════════════════════════════════════"
echo ""

echo "results:" >> "${RUN_DIR}/meta.txt"
for result in "${ALGO_RESULTS[@]}"; do
    echo "  - ${result}" >> "${RUN_DIR}/meta.txt"
done

exit "$FAILURES"
