# How to Fork go-chain and Launch Your Own Blockchain

This guide walks you through forking go-chain to create your own proof-of-work blockchain. The entire process — from clone to running a multi-node network — can be completed in under an hour.

## Prerequisites

- **Go 1.21+** installed (`go version` to check)
- **Git** installed
- Basic familiarity with Go projects and command-line tools
- A text editor

## Step 1: Clone the Repository

```bash
git clone https://github.com/bams-repo/go-chain.git mychain
cd mychain
```

## Step 2: Update the Go Module Path

Replace the module path in `go.mod` with your own:

```bash
# Replace the module path throughout the codebase.
# Example: changing from github.com/bams-repo/fairchain to github.com/yourorg/mychain
OLD_MODULE="github.com/bams-repo/fairchain"
NEW_MODULE="github.com/yourorg/mychain"

# Update go.mod
sed -i "s|$OLD_MODULE|$NEW_MODULE|g" go.mod

# Update all Go source files
find . -name '*.go' -exec sed -i "s|$OLD_MODULE|$NEW_MODULE|g" {} +
```

Run `go mod tidy` to verify the module resolves correctly.

## Step 3: Rebrand via coinparams

Open `internal/coinparams/coinparams.go` — this is the single source of truth for your chain's identity. Change every constant to match your project:

```go
package coinparams

const (
    Name              = "MyChain"           // Human-readable name
    NameLower         = "mychain"           // Lowercase (paths, configs, CLI)
    Ticker            = "MYC"              // Exchange ticker symbol
    DaemonName        = "mychaind"          // Node binary name
    CLIName           = "mychain-cli"       // CLI binary name
    GenesisToolName   = "mychain-genesis"   // Genesis tool binary name
    AdversaryToolName = "mychain-adversary" // Adversary tool binary name
    DefaultDataDirName = ".mychain"         // Data directory (~/.mychain)
    ConfFileName      = "mychain.conf"      // Config file name
    CoinbaseTag       = "mychain"           // Tag in mined coinbase transactions
    RPCRealm          = "mychain-rpc"       // HTTP Basic Auth realm
    UserAgentPrefix   = "/mychain:"         // BIP-style user agent
    CopyrightHolder   = "MyChain Contributors"
    BaseUnitName      = "sat"               // Smallest unit name
    DisplayUnitName   = "myc"               // Display unit (used in RPC: "balance_myc")
    CoinsPerBaseUnit  = 1e8                 // 100,000,000 base units per display unit
    Algorithm         = "sha256d"           // PoW algorithm (see Step 4)
)
```

All Go source files already reference these constants — changing them here changes the entire project's branding.

## Step 4: Choose Your PoW Algorithm

The `Algorithm` constant in `coinparams.go` selects the proof-of-work hash function.

**Built-in algorithms (ready to use):**

| Algorithm | Value | Memory | Description |
|-----------|-------|--------|-------------|
| DoubleSHA256 | `"sha256d"` | None | Bitcoin-compatible default. ASIC-mineable. Fastest validation. |
| Argon2id | `"argon2d"` | 256 KiB | CPU-fair, ASIC-resistant. RFC 9106. |
| Scrypt | `"scrypt"` | ~128 KiB | Memory-hard (Litecoin-style). ASIC-resistant. |
| SHA256-Mem | `"sha256mem"` | 2 MiB | Memory-hard SHA256. Phone-competitive. No novel crypto. |

`sha256mem` is designed for maximum device fairness — it uses only standard SHA256 but forces miners to hold a 2 MiB buffer in fast memory, compressing the performance gap between phones, desktops, and ASICs to roughly 2-4x instead of 100,000x+.

To use Argon2id (CPU-fair mining):

```go
Algorithm = "argon2d"
```

**Adding a new algorithm:**

1. Create `internal/algorithms/newalgo/newalgo.go` implementing the `Hasher` interface
2. Add a case to the switch in `internal/algorithms/hasher.go`
3. Set `Algorithm = "newalgo"` in `coinparams.go`

The `Hasher` interface requires two methods:

```go
type Hasher interface {
    PoWHash(data []byte) types.Hash  // Compute PoW hash
    Name() string                     // Algorithm identifier
}
```

**Important:** Changing the algorithm after launch is a hard fork. The block identity hash (used for block references, indexing, and merkle trees) always uses DoubleSHA256 regardless of the PoW algorithm.

## Step 5: Define Your Network Parameters

Open `internal/params/networks.go` and create a new network definition. Key parameters to customize:

```go
var MyChainMainnet = &ChainParams{
    Name:         "mainnet",
    DataDirName:  "",
    NetworkMagic: [4]byte{0xAA, 0xBB, 0xCC, 0x01}, // Unique 4-byte identifier
    DefaultPort:  18333,                              // P2P listen port

    // Timing
    TargetBlockSpacing:  2 * time.Minute,             // Time between blocks
    RetargetInterval:    2016,                         // Blocks between difficulty adjustments
    TargetTimespan:      2016 * 2 * time.Minute,       // RetargetInterval * TargetBlockSpacing
    MaxTimeFutureDrift:  2 * time.Hour,
    MinTimestampRule:    "median-11",

    // Difficulty
    InitialBits: 0x1d00ffff,                          // Starting difficulty
    MinBits:     0x1d00ffff,                           // Minimum difficulty (maximum target)
    NoRetarget:  false,

    // Block limits
    MaxBlockSize:    1_000_000,                        // 1 MB max block size
    MaxBlockTxCount: 10_000,

    // Economics
    InitialSubsidy:          50_0000_0000,             // 50 coins in base units
    SubsidyHalvingInterval:  210_000,                  // Blocks between halvings
    CoinbaseMaturity:        100,                      // Blocks before coinbase is spendable

    // Safety
    MaxReorgDepth: 288,

    // Mempool
    MaxMempoolSize: 5000,
    MinRelayTxFee:  1000,

    // Bootstrap peers (add after deploying seed nodes)
    SeedNodes: []string{},

    ActivationHeights: map[string]uint32{},

    // GenesisBlock and GenesisHash will be filled in Step 6
}
```

Add your network to the `NetworkByName` function in the same file:

```go
func NetworkByName(name string) *ChainParams {
    switch name {
    case "mainnet", "main":
        return MyChainMainnet
    // ...
    }
}
```

**Parameter guide:**

- **NetworkMagic**: Must be unique. Prevents cross-chain peer connections.
- **TargetBlockSpacing**: How often blocks should be found. Bitcoin uses 10 minutes.
- **RetargetInterval**: How often difficulty adjusts. Bitcoin uses 2016 blocks.
- **InitialBits**: Starting difficulty. Use `0x207fffff` for regtest (trivial), `0x1e0fffff` for testnet, `0x1d00ffff` for mainnet.
- **InitialSubsidy**: Block reward in base units (1 coin = 100,000,000 base units).
- **SubsidyHalvingInterval**: Blocks between reward halvings. Bitcoin uses 210,000.
- **CoinbaseMaturity**: Blocks before mined coins are spendable. Bitcoin uses 100.

## Step 6: Mine Your Genesis Block

Build the genesis tool and mine a genesis block:

```bash
make genesis
bin/mychain-genesis -network mainnet
```

The tool outputs ready-to-paste Go code. Copy the `GenesisBlock` and `GenesisHash` sections into your network definition in `internal/params/networks.go`.

Example output:

```
// --- Paste into internal/params/networks.go ---

GenesisBlock: types.Block{
    Header: types.BlockHeader{
        Version:   1,
        PrevBlock: types.ZeroHash,
        MerkleRoot: types.Hash{
            0x1a, 0x43, 0xdf, 0x3e, ...
        },
        Timestamp: 1773212462,
        Bits:      0x1e0fffff,
        Nonce:     433076,
    },
    Transactions: []types.Transaction{{ ... }},
},
GenesisHash: types.Hash{
    0x54, 0x70, 0xb8, 0x1f, ...
},
```

Optional flags:

- `-message "your genesis message"` — custom coinbase message
- `-timestamp 1700000000` — specific Unix timestamp (default: now)

## Step 7: Update Build Files

### Makefile

Update the binary name variables at the top of `Makefile` to match your `coinparams.go`:

```makefile
DAEMON_NAME   := mychaind
CLI_NAME      := mychain-cli
GENESIS_NAME  := mychain-genesis
ADVERSARY_NAME := mychain-adversary
```

### Systemd Service (optional)

If deploying on Linux, update `scripts/fairchain-testnet.service`:

- Binary path
- Service name
- Data directory
- Log paths

### CI Workflows (optional)

Update `.github/workflows/` files with your binary names and release artifact names.

## Step 8: Build and Run

```bash
# Build all binaries
make build

# Run a node with mining enabled
bin/mychaind -network mainnet -mine

# In another terminal, check status
bin/mychain-cli getblockchaininfo
```

### Configuration File

Create `~/.mychain/mychain.conf`:

```ini
[main]
network=mainnet
listen=0.0.0.0:18333
rpc=127.0.0.1:18334
mine=1
loglevel=info
```

Or use JSON (`~/.mychain/config.json`):

```json
{
    "network": "mainnet",
    "listen_addr": "0.0.0.0:18333",
    "rpc_addr": "127.0.0.1:18334",
    "mining_enabled": true,
    "log_level": "info"
}
```

## Step 9: Deploy Seed Nodes

For a real network, deploy at least 2-3 seed nodes on different servers:

```bash
# On each seed node server
bin/mychaind -network mainnet -mine -listen 0.0.0.0:18333 -rpcbind 127.0.0.1 -rpcport 18334
```

Add seed node addresses to your `ChainParams`:

```go
SeedNodes: []string{
    "seed1.mychain.org:18333",
    "seed2.mychain.org:18333",
},
```

Rebuild and distribute the updated binary. New nodes will automatically discover peers through the seed nodes.

## Step 10: Verify Your Chain

Use the CLI to verify everything is working:

```bash
# Check chain info
bin/mychain-cli getblockchaininfo

# Check block count
bin/mychain-cli getblockcount

# Check connected peers
bin/mychain-cli getpeerinfo

# Get a specific block
bin/mychain-cli getblock <hash>

# Check wallet balance
bin/mychain-cli getbalance

# Generate a new address
bin/mychain-cli getnewaddress

# Stop the daemon
bin/mychain-cli stop
```

Verify that:

- Blocks are being mined at approximately your target spacing
- Peers are connecting and syncing
- The user agent shows your chain name
- RPC responses use your display unit name (e.g., `balance_myc`)

## Optional: Custom Consensus Engine

go-chain's consensus is pluggable via the `consensus.Engine` interface. To implement a custom consensus mechanism:

1. Create a new package (e.g., `internal/consensus/myengine/`)
2. Implement the `consensus.Engine` interface:

```go
type Engine interface {
    ValidateHeader(...)  error
    ValidateBlock(...)   error
    CalcNextBits(...)    uint32
    PrepareHeader(...)   error
    SealHeader(...)      (bool, error)
    CalcBlockWeight(...) *big.Int
    Hasher()             algorithms.Hasher
    Name()               string
}
```

3. Wire it in `cmd/node/main.go` instead of `pow.New(hasher)`

The chain manager, miner, P2P layer, and RPC server all consume this interface — they have zero knowledge of the specific consensus mechanism.

## Quick Reference

| What to change | Where |
|----------------|-------|
| Chain identity (name, ticker, binaries) | `internal/coinparams/coinparams.go` |
| PoW algorithm | `coinparams.Algorithm` + `internal/algorithms/` |
| Network parameters (timing, difficulty, economics) | `internal/params/networks.go` |
| Genesis block | Run `bin/mychain-genesis`, paste output into `networks.go` |
| Binary names in build | `Makefile` top-level variables |
| Go module path | `go.mod` + all import paths |
