# Fairchain: The First Fork of go-chain

## Overview

Fairchain will be the first production fork of go-chain. It will inherit the full Bitcoin-parity foundation — UTXO model, P2P networking, block storage, RPC API, wallet — and layer on a fairness-oriented consensus model designed to keep mining accessible to commodity hardware.

## Why Fork From go-chain

go-chain was originally built as the Fairchain node. As development progressed, the codebase was deliberately restructured into a modular toolkit: pluggable PoW algorithms, swappable difficulty retargeting, centralized coin parameters, and clean interface boundaries throughout.

This restructuring revealed that the foundation layer — everything below the consensus model — is useful on its own. Rather than coupling that foundation to a single coin's identity and consensus rules, go-chain became the reusable base and Fairchain became the first consumer of it.

The fork model gives Fairchain a clean separation of concerns:

- **go-chain** owns the transport, storage, validation, and economic plumbing. Improvements here benefit every chain built on top of it.
- **Fairchain** owns the fairness-focused consensus layer, its specific economic parameters, and its network identity.

## What Fairchain Inherits

Everything in go-chain ships with the fork out of the box:

- Full UTXO-based transaction model with script validation
- Bitcoin Core-compatible JSON-RPC API (40+ endpoints, stratum pool compatible)
- P2P networking with peer discovery, gossip, misbehavior scoring, and ban management
- LevelDB-backed block index and chainstate with flat-file block storage
- HD wallet with BIP39 mnemonic, encryption, and WIF key import/export
- Mempool with fee-rate priority, double-spend detection, and policy enforcement
- Three-network model (mainnet, testnet, regtest) with full parameterization
- Reorg support with undo data
- bitcoin-cli compatible command-line client

## What Fairchain Will Add

The core differentiator is a consensus model designed around fairness — reducing the advantage that specialized hardware and large-scale operations have over individual participants.

Specific details on the consensus mechanism will be published closer to launch. The design goals are:

- **Device fairness**: Minimize the performance gap between phones, desktops, and purpose-built mining hardware
- **Decentralization pressure**: Make it economically irrational to concentrate mining in large pools or farms
- **Proven cryptographic primitives**: No novel or unaudited cryptography — build fairness from established building blocks

## How the Fork Will Work

The fork process follows the same steps documented in [How to Fork](how-to-fork.md):

1. Fork the go-chain repository
2. Update the Go module path
3. Set coin parameters in `coinparams.go` (name, ticker, binaries, algorithms)
4. Define network parameters (block timing, economics, seed nodes)
5. Implement and wire the fairness consensus engine via the `consensus.Engine` interface
6. Mine the genesis block
7. Deploy seed nodes and launch

## Timeline

Fairchain is in active development. The go-chain foundation is stable and running a live testnet. The fairness consensus layer is being designed and prototyped. Updates will be posted as milestones are reached.

## Staying Updated

- Watch the [go-chain repository](https://github.com/bams-repo/go-chain) for foundation updates
- The Fairchain fork repository will be published at launch
