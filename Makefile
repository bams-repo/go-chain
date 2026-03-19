# Branding values sourced from internal/coinparams/coinparams.go
.PHONY: all build test clean fairchaind genesis cli adversary chaos modularity

MODULE := github.com/bams-repo/fairchain
BINDIR := bin

# Binary names — change these to rebrand (must match internal/coinparams/coinparams.go).
DAEMON_NAME   := fairchaind
CLI_NAME      := fairchain-cli
GENESIS_NAME  := fairchain-genesis
ADVERSARY_NAME := fairchain-adversary

all: build

build: fairchaind cli

fairchaind:
	go build -o $(BINDIR)/$(DAEMON_NAME) ./cmd/node

genesis:
	go build -o $(BINDIR)/$(GENESIS_NAME) ./cmd/genesis

cli:
	go build -o $(BINDIR)/$(CLI_NAME) ./cmd/cli

adversary:
	go build -o $(BINDIR)/$(ADVERSARY_NAME) ./cmd/adversary

chaos: build adversary
	bash scripts/chaos_test.sh

modularity:
	bash scripts/modularity_test.sh

test:
	go test ./... -v -count=1

test-short:
	go test ./... -count=1

bench:
	go test ./... -bench=. -benchmem

clean:
	rm -rf $(BINDIR)
	go clean ./...

lint:
	go vet ./...

fmt:
	gofmt -w .

tidy:
	go mod tidy

# Run a single regtest node with mining enabled.
run-regtest:
	mkdir -p /tmp/fairchain-regtest
	$(BINDIR)/$(DAEMON_NAME) \
		-network regtest \
		-datadir /tmp/fairchain-regtest \
		-listen 0.0.0.0:19444 \
		-rpcbind 127.0.0.1 \
		-rpcport 19445 \
		-mine

# Run a second regtest node that connects to the first.
run-regtest2:
	mkdir -p /tmp/fairchain-regtest2
	$(BINDIR)/$(DAEMON_NAME) \
		-network regtest \
		-datadir /tmp/fairchain-regtest2 \
		-listen 0.0.0.0:19446 \
		-rpcbind 127.0.0.1 \
		-rpcport 19447 \
		-addnode 127.0.0.1:19444

# --- Testnet targets ---

run-testnet:
	mkdir -p /tmp/fairchain-testnet
	$(BINDIR)/$(DAEMON_NAME) \
		-network testnet \
		-datadir /tmp/fairchain-testnet \
		-listen 0.0.0.0:19334 \
		-rpcbind 127.0.0.1 \
		-rpcport 19335 \
		-mine

run-testnet2:
	mkdir -p /tmp/fairchain-testnet2
	$(BINDIR)/$(DAEMON_NAME) \
		-network testnet \
		-datadir /tmp/fairchain-testnet2 \
		-listen 0.0.0.0:19336 \
		-rpcbind 127.0.0.1 \
		-rpcport 19337 \
		-addnode 127.0.0.1:19334

testnet-status:
	$(BINDIR)/$(CLI_NAME) -rpcconnect=127.0.0.1 -rpcport=19335 getblockchaininfo

# --- Genesis & status ---

mine-genesis:
	$(BINDIR)/$(GENESIS_NAME) --network regtest

mine-genesis-testnet:
	$(BINDIR)/$(GENESIS_NAME) --network testnet --timestamp 1773212867 --message "fairchain testnet genesis"

status:
	$(BINDIR)/$(CLI_NAME) -rpcconnect=127.0.0.1 -rpcport=19445 getinfo
