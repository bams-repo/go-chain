# Fairchain TODO — Prioritized Milestones

## Phase 1: Deterministic Core (COMPLETE)

- [x] Hash type with comparison, hex encoding, zero detection
- [x] BlockHeader with canonical 80-byte serialization
- [x] Block with header + transaction list serialization
- [x] Transaction, TxInput, TxOutput, OutPoint with canonical binary encoding
- [x] VarInt encoding (Bitcoin-style compact size)
- [x] Double-SHA256 hashing
- [x] Merkle root computation
- [x] Compact bits ↔ big.Int target conversion
- [x] Chainwork calculation
- [x] PoW validation (hash ≤ target)
- [x] Genesis block builder from config
- [x] Genesis mining tool
- [x] Chain params: mainnet, testnet, regtest
- [x] Subsidy schedule with halving
- [x] Block structure validation (coinbase, merkle, size, duplicates, subsidy)
- [x] Timestamp validation (median-11, prev+1, future drift)
- [x] Difficulty retargeting
- [x] bbolt persistent storage (blocks, headers, chain state, peers)
- [x] Tests: serialization, hashing, merkle, bits, genesis, validation, store

## Phase 2: P2P Networking (COMPLETE)

- [x] Wire protocol message definitions and binary encoding
- [x] TCP peer connections with read/write loops
- [x] Version/verack handshake with self-connection detection
- [x] Network magic separation per chain params
- [x] Ping/pong liveness
- [x] Inventory announcements (inv)
- [x] Data requests (getdata)
- [x] Block propagation
- [x] Transaction propagation
- [x] Block sync via getblocks
- [x] Peer address gossip (addr)
- [x] Seen inventory caches to prevent rebroadcast storms
- [x] Per-peer known inventory tracking
- [x] Seed peers from config
- [x] Persistent peer store
- [x] Reconnection loop
- [x] Inbound/outbound connection limits
- [x] Tests: message encode/decode roundtrips

## Phase 3: Mining & Mempool (COMPLETE)

- [x] Mempool with thread-safe admission, deduplication, size limits
- [x] Deterministic transaction ordering for block templates
- [x] Block template builder (coinbase + mempool txs)
- [x] Mining loop with context cancellation
- [x] Nonce iteration with batch checking
- [x] Mined block submission through validation pipeline
- [x] RPC API: getinfo, getblockcount, getbestblockhash, getpeerinfo, getblock, submitblock, getmempoolinfo
- [x] CLI query tool

## Phase 4: Hardening & Testing (COMPLETE)

- [x] Chain manager with orphan pool and basic reorg
- [x] Chain integration tests (init, process, multi-block, duplicate, orphan)
- [x] Multi-node chaos test (10-node, 16 phases: network chaos + adversarial attacks)
- [x] Adversarial testing tool (bad nonce, bad merkle, duplicate, time-warp, orphan flood, inflated coinbase, empty block)
- [x] Stress test: rapid block production, kill/restart chaos, seed swaps
- [x] Fuzz testing for Block, Transaction, BlockHeader, VarInt, and wire protocol messages
- [x] Peer disconnect and reconnect resilience testing (kill/restart phases)
- [x] Chain reorg with competing forks (natural reorgs in chaos test)
- [x] Graceful shutdown: explicit ordered teardown with per-stage logging
- [x] Structured logging (log/slog) with --log-level flag (debug/info/warn/error)
- [x] Metrics collection skeleton: atomic counters for blocks, peers, reorgs, orphans, rejections + /metrics RPC endpoint

## Phase 5: UTXO & Transaction Validation (COMPLETE)

- [x] UTXO set tracking (in-memory + persistent via bbolt)
- [x] Input validation: referenced UTXO exists and is unspent
- [x] Output validation: no negative values, total input ≥ total output
- [x] Fee calculation (input sum - output sum)
- [x] Coinbase maturity enforcement
- [x] Double-spend detection in mempool
- [x] Transaction priority by fee rate
- [x] Mempool eviction policy
- [x] UTXO-aware reorg with disconnect/reconnect and undo data
- [x] Coinbase value validation: subsidy + fees cap
- [x] Miner includes fees in coinbase reward
- [x] Bitcoin Core-style RPC: gettxout, gettxoutsetinfo, getrawmempool, getmempoolentry

## Phase 6: Future Fair-Consensus Extension Points

- [ ] Identity registration transaction type
- [ ] Identity store (pubkey → registration height, collateral)
- [ ] Epoch seed computation interface
- [ ] Per-identity ticket allocation interface
- [ ] VRF-based eligibility check interface
- [ ] Sequential memory-hard proof interface
- [ ] Reward damping function interface
- [ ] Collateral-backed mining identity lifecycle
- [ ] Consensus engine swap: PoW → fair-consensus
- [ ] Activation height gating for consensus upgrades

## Phase 7: Production Hardening

- [ ] Headers-first sync
- [ ] Checkpoint support
- [ ] Peer banning and scoring
- [ ] Rate limiting
- [ ] TLS for P2P connections (optional)
- [ ] Prometheus metrics endpoint
- [ ] Structured JSON logging
- [ ] Systemd service file
- [ ] Docker container
- [ ] CI/CD pipeline
