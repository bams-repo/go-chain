.PHONY: all build test clean node genesis cli adversary chaos

MODULE := github.com/bams-repo/fairchain
BINDIR := bin

all: build

build: node genesis cli

node:
	go build -o $(BINDIR)/fairchain-node ./cmd/node

genesis:
	go build -o $(BINDIR)/fairchain-genesis ./cmd/genesis

cli:
	go build -o $(BINDIR)/fairchain-cli ./cmd/cli

adversary:
	go build -o $(BINDIR)/fairchain-adversary ./cmd/adversary

chaos: build adversary
	bash scripts/chaos_test.sh

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
	$(BINDIR)/fairchain-node \
		--network regtest \
		--datadir /tmp/fairchain-regtest \
		--listen 0.0.0.0:19444 \
		--rpc 127.0.0.1:19445 \
		--mine

# Run a second regtest node that connects to the first.
run-regtest2:
	mkdir -p /tmp/fairchain-regtest2
	$(BINDIR)/fairchain-node \
		--network regtest \
		--datadir /tmp/fairchain-regtest2 \
		--listen 0.0.0.0:19446 \
		--rpc 127.0.0.1:19447 \
		--seed-peers 127.0.0.1:19444

# --- Testnet targets ---

# Run a testnet node with mining enabled.
run-testnet:
	mkdir -p /tmp/fairchain-testnet
	$(BINDIR)/fairchain-node \
		--network testnet \
		--datadir /tmp/fairchain-testnet \
		--listen 0.0.0.0:19334 \
		--rpc 127.0.0.1:19335 \
		--mine

# Run a second testnet node that connects to the first.
run-testnet2:
	mkdir -p /tmp/fairchain-testnet2
	$(BINDIR)/fairchain-node \
		--network testnet \
		--datadir /tmp/fairchain-testnet2 \
		--listen 0.0.0.0:19336 \
		--rpc 127.0.0.1:19337 \
		--seed-peers 127.0.0.1:19334

# Query testnet node status.
testnet-status:
	$(BINDIR)/fairchain-cli --rpc http://127.0.0.1:19335 getinfo

# --- Genesis & status ---

# Mine a genesis block for a given network.
mine-genesis:
	$(BINDIR)/fairchain-genesis --network regtest

mine-genesis-testnet:
	$(BINDIR)/fairchain-genesis --network testnet --timestamp 1773212867 --message "fairchain testnet genesis"

# Query node status (regtest default).
status:
	$(BINDIR)/fairchain-cli --rpc http://127.0.0.1:19445 getinfo
