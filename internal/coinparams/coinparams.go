package coinparams

import "math"

var (
	// CoinsPerBaseUnit is 10^Decimals, computed at init.
	CoinsPerBaseUnit float64
)

func init() {
	CoinsPerBaseUnit = math.Pow(10, float64(Decimals))
}

const (
	// Name is the human-readable coin name (e.g., "Fairchain", "Bitcoin", "Litecoin").
	Name = "Fairchain"

	// NameLower is the lowercase form used in paths, config files, and CLI.
	NameLower = "fairchain"

	// Ticker is the exchange ticker symbol (e.g., "FAIR", "BTC", "LTC").
	Ticker = "FAIR"

	// DaemonName is the binary name for the full node daemon.
	DaemonName = "fairchaind"

	// CLIName is the binary name for the command-line RPC client.
	CLIName = "fairchain-cli"

	// GenesisToolName is the binary name for the genesis mining tool.
	GenesisToolName = "fairchain-genesis"

	// AdversaryToolName is the binary name for the adversary testing tool.
	AdversaryToolName = "fairchain-adversary"

	// DefaultDataDirName is the hidden directory name in the user's home (e.g., ".fairchain").
	DefaultDataDirName = ".fairchain"

	// ConfFileName is the INI-style config file name (e.g., "fairchain.conf").
	ConfFileName = "fairchain.conf"

	// CoinbaseTag is the ASCII tag embedded in coinbase transactions.
	CoinbaseTag = "fairchain"

	// RPCRealm is the HTTP Basic Auth realm for the RPC server.
	RPCRealm = "fairchain-rpc"

	// UserAgentPrefix is the BIP-style user agent prefix (e.g., "/fairchain:").
	UserAgentPrefix = "/fairchain:"

	// CopyrightHolder is the name used in LICENSE and legal notices.
	CopyrightHolder = "Fairchain Contributors"

	// BaseUnitName is the name of the smallest indivisible unit (e.g., "satoshi").
	BaseUnitName = "unit"

	// DisplayUnitName is the name of the display unit (e.g., "BTC", "FAIR").
	// Used in RPC responses like "balance_fair" instead of "balance_btc".
	DisplayUnitName = "fair"

	// Decimals is the number of decimal places between the display unit and
	// the smallest indivisible base unit. For example, Bitcoin uses 8 (1 BTC
	// = 100,000,000 satoshi). Valid range: 0–18. Changing this is a
	// consensus / hard-fork change.
	Decimals = 8

	// Algorithm is the PoW hash algorithm name. Must match a registered
	// algorithm in internal/algorithms/. Changing this is a hard fork.
	// Options: "sha256d" (Bitcoin-compatible), "argon2id" (CPU-fair, RFC 9106),
	//          "scrypt" (Litecoin-style), "sha256mem" (memory-hard SHA256)
	Algorithm = "sha256mem"

	// DifficultyAlgorithm is the difficulty retargeting algorithm name.
	// Must match a registered algorithm in internal/difficulty/.
	// Changing this is a consensus / hard-fork change.
	// Options: "bitcoin" (Nakamoto-style epoch retarget with EDA),
	//          "lwma"    (zawy12 LWMA-1, per-block adjustment, responsive to hash rate swings)
	DifficultyAlgorithm = "bitcoin"
)
