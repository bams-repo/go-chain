# go-chain

[![CI](https://github.com/bams-repo/go-chain/actions/workflows/ci.yml/badge.svg)](https://github.com/bams-repo/go-chain/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/bams-repo/go-chain?label=latest)](https://github.com/bams-repo/go-chain/releases/latest)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Go](https://img.shields.io/github/go-mod/go-version/bams-repo/go-chain)](go.mod)

A modular blockchain written in Go — designed to be forked, configured, and launched.

## What This Is

go-chain is a complete, Bitcoin-parity blockchain node built from the ground up in Go. Every policy-level decision — proof-of-work algorithm, difficulty retargeting, coin identity, network parameters, and economics — lives behind a clean interface and can be swapped by editing a single file. Clone the repo, change your parameters, mine a genesis block, and you have a working chain.

[Fairchain](DOCS/fairchain-fork.md) is the first production fork of go-chain, with a live testnet and GUI wallet.

## Modular Architecture

### PoW Algorithms (`internal/algorithms/`)

Four built-in algorithms behind a common `Hasher` interface. Set `Algorithm` in `coinparams.go` to switch:

| Algorithm | Value | Description |
|-----------|-------|-------------|
| DoubleSHA256 | `"sha256d"` | Bitcoin-compatible. ASIC-mineable. Fastest validation. |
| Argon2id | `"argon2id"` | CPU-fair, ASIC-resistant. RFC 9106. |
| Scrypt | `"scrypt"` | Memory-hard (Litecoin-style). |
| SHA256-Mem | `"sha256mem"` | Memory-hard SHA256. Designed for device fairness. |

### Difficulty Retargeting (`internal/difficulty/`)

Two algorithms behind a `Retargeter` interface. Set `DifficultyAlgorithm` in `coinparams.go`:

| Algorithm | Value | Description |
|-----------|-------|-------------|
| Bitcoin | `"bitcoin"` | Nakamoto-style epoch retarget with EDA. |
| LWMA | `"lwma"` | Zawy12 LWMA-1. Per-block adjustment, responsive to hash rate swings. |

### Coin Parameters (`internal/coinparams/`)

One file defines the entire coin identity — name, ticker, binary names, data directory, decimal precision, PoW algorithm, and difficulty algorithm. Fork the repo, edit this file, and you have a new chain identity.

### Network Definitions (`internal/params/`)

Mainnet, testnet, and regtest are fully parameterized in a single `ChainParams` struct: block timing, subsidy schedule, halving interval, coinbase maturity, reorg depth, mempool policy, seed nodes. No magic numbers in the codebase.

## Build

```bash
make build
```

Produces two binaries in `bin/`:
- `fairchaind` — full node daemon (binary name is configurable via `coinparams.go`)
- `fairchain-cli` — command-line RPC client

Optional:
```bash
make genesis      # Genesis block mining tool
make adversary    # Adversarial block generator
```

## Quick Start

```bash
# Run a regtest node with mining
make run-regtest

# Query node status
fairchain-cli getblockchaininfo
fairchain-cli getblockcount
fairchain-cli getpeerinfo
```

See [Getting Started](DOCS/getting-started.md) for full setup instructions.

## GUI Wallet

Fairchain includes a native desktop wallet built with Wails (Go + React). It provides a Bitcoin Core-style interface with sync overlay, debug console, peer management, and built-in CPU mining.

```bash
./configure --with-qt
make build
./bin/fairchain-qt
```

Requires Go, Node.js 20+, and WebKit2GTK (Linux). See `./configure --help` for details.

## Join the Testnet

A public testnet is running with seed nodes across multiple regions. To join:

```bash
# Download the latest release
# https://github.com/bams-repo/go-chain/releases/latest

# Run a testnet node
./fairchaind -network testnet -mine

# Or build and run the GUI wallet (defaults to testnet)
./configure --with-qt && make build
./bin/fairchain-qt
```

## What's Implemented

- **Core types**: Hash, BlockHeader, Block, Transaction (UTXO-style), canonical binary serialization
- **Crypto**: Double-SHA256, secp256k1 ECDSA, P2PKH scripts, Merkle roots, compact bits/target
- **Consensus**: Pluggable `consensus.Engine` interface with baseline PoW
- **Validation**: Block structure, coinbase rules, merkle root, duplicate tx, subsidy, timestamps, difficulty retargeting, script execution
- **UTXO set**: In-memory with LevelDB persistence, connect/disconnect per block, undo data for reorgs
- **Mempool**: UTXO-validated, script-validated, fee-rate priority, double-spend detection, eviction
- **Mining**: Block template builder (BIP 22), fee-inclusive coinbase, P2PKH reward scripts
- **P2P networking**: Version handshake, ping/pong keepalive, inventory gossip, block/tx propagation, initial block sync, peer address gossip, misbehavior scoring, IP banning, rate limiting, inbound eviction, exponential reconnection backoff
- **Wire protocol**: Binary message encoding (version, verack, ping/pong, inv, getdata, block, tx, getblocks, addr)
- **RPC API**: Bitcoin Core-compatible HTTP JSON-RPC (40+ endpoints, stratum pool compatible)
- **CLI**: bitcoin-cli compatible command-line client
- **Storage**: LevelDB block index + flat file blocks (blk*.dat/rev*.dat) + LevelDB chainstate
- **Wallet**: HD wallet (BIP39), encryption, backup, WIF import/export
- **Tests**: 60+ unit tests + 9 fuzz targets + 16-phase chaos test

## Documentation

| Document | Description |
|----------|-------------|
| [Getting Started](DOCS/getting-started.md) | Build, run, and configure a node |
| [How to Fork](DOCS/how-to-fork.md) | Step-by-step guide to launching your own chain |
| [RPC Commands](DOCS/rpc-commands.md) | Full API reference (40+ endpoints) |
| [Fairchain Fork](DOCS/fairchain-fork.md) | Roadmap for the Fairchain production fork |

## Project Structure

| Area | Path |
|------|------|
| Core types & serialization | `internal/types/` |
| Hashing & merkle | `internal/crypto/` |
| Coin identity | `internal/coinparams/` |
| PoW algorithms | `internal/algorithms/` |
| Difficulty retargeting | `internal/difficulty/` |
| Chain params & networks | `internal/params/` |
| Consensus interface | `internal/consensus/engine.go` |
| PoW engine | `internal/consensus/pow/` |
| Block validation | `internal/consensus/validation.go` |
| Chain manager | `internal/chain/` |
| Storage | `internal/store/` |
| Wire protocol | `internal/protocol/` |
| P2P networking | `internal/p2p/` |
| Miner | `internal/miner/` |
| RPC API | `internal/rpc/` |
| Daemon entrypoint | `cmd/node/` |
| CLI | `cmd/cli/` |

## Downloads

Pre-built binaries for Linux, macOS, and Windows are available on the
[Releases](https://github.com/bams-repo/go-chain/releases/latest) page.

## Contributing

Contributions are welcome. See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines
on code style, commit messages, and the PR process.

- [Open an Issue](https://github.com/bams-repo/go-chain/issues/new/choose) — bug reports and feature requests
- [Discussions](https://github.com/bams-repo/go-chain/discussions) — questions, ideas, and general conversation
- [Security Policy](SECURITY.md) — responsible disclosure for vulnerabilities

## License

See [LICENSE](LICENSE).
