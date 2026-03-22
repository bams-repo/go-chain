# All binary names derived from internal/coinparams/coinparams.go.
# To rebrand: edit coinparams.go — everything else follows automatically.
#
# Build workflow:
#   ./configure              # detect environment, write config.mk
#   ./configure --with-qt    # enable GUI wallet
#   make build               # build according to config
.PHONY: all build build-core deps qt qt-dev daemon cli genesis adversary \
        test test-short bench lint fmt tidy clean \
        run-regtest run-regtest2 run-testnet run-testnet2 \
        testnet-status chaos modularity mine-genesis mine-genesis-testnet status

MODULE := $(shell grep '^module' go.mod | awk '{print $$2}')
BINDIR := bin

# --- Include configure output (if present) ---
-include config.mk

# --- Defaults for anything configure didn't set ---
GO           ?= go
WAILS        ?=
NPM          ?= npm
NODE         ?=
WITH_QT      ?= 0
WITH_WALLET  ?= 1
WITH_MINING  ?= 1
WEBKIT_TAG   ?=
PREFIX       ?= /usr/local
NVM_DIR      ?= $(HOME)/.nvm
NVM_NODE_VERSION ?=

# Shell preamble that activates nvm if configure detected it.
NVM_SHELL :=
ifneq ($(NVM_NODE_VERSION),)
NVM_SHELL := export NVM_DIR="$(NVM_DIR)" && . "$(NVM_DIR)/nvm.sh" --no-use && nvm use $(NVM_NODE_VERSION) --silent &&
endif

# --- Extract branding from coinparams.go ---
coinparams.mk: internal/coinparams/coinparams.go scripts/coinparams.sh
	@bash scripts/coinparams.sh > coinparams.mk

-include coinparams.mk
$(shell bash scripts/coinparams.sh > coinparams.mk)

# --- Vendored Wails source (go.mod replace directive points here) ---
WAILS_DEPENDS := depends/wails
WAILS_DEPENDS_TAG := v2.11.0

# --- Wails build flags ---
WAILS_BUILD_FLAGS :=
ifneq ($(WEBKIT_TAG),)
WAILS_BUILD_FLAGS += -tags $(WEBKIT_TAG)
endif

# --- Default target ---
all: build genesis adversary

# --- Core build ---
build: build-core
ifeq ($(WITH_QT),1)
build: qt
endif

build-core: daemon cli

daemon:
	$(GO) build -o $(BINDIR)/$(DAEMON_NAME) ./cmd/node

cli:
	$(GO) build -o $(BINDIR)/$(CLI_NAME) ./cmd/cli

genesis:
	$(GO) build -o $(BINDIR)/$(GENESIS_NAME) ./cmd/genesis

adversary:
	$(GO) build -o $(BINDIR)/$(ADVERSARY_NAME) ./cmd/adversary

# --- Vendored dependencies (fetched on demand) ---
deps:
	@if [ ! -d "$(WAILS_DEPENDS)" ]; then \
		echo "Cloning Wails $(WAILS_DEPENDS_TAG) into $(WAILS_DEPENDS)..."; \
		git clone --depth 1 --branch $(WAILS_DEPENDS_TAG) \
			https://github.com/wailsapp/wails.git $(WAILS_DEPENDS); \
	fi

# --- GUI wallet (requires ./configure --with-qt) ---
qt: deps
ifeq ($(WAILS),)
	$(error Wails not found. Run: ./configure --with-qt)
endif
	$(NVM_SHELL) cd cmd/qt && $(WAILS) build $(WAILS_BUILD_FLAGS)
	@mkdir -p $(BINDIR)
	cp cmd/qt/build/bin/$(GUI_NAME) $(BINDIR)/$(GUI_NAME)

qt-dev:
ifeq ($(WAILS),)
	$(error Wails not found. Run: ./configure --with-qt)
endif
	$(NVM_SHELL) cd cmd/qt && $(WAILS) dev $(WAILS_BUILD_FLAGS)

# --- Test / lint / fmt ---
test:
	$(GO) test ./... -v -count=1

test-short:
	$(GO) test ./... -count=1

bench:
	$(GO) test ./... -bench=. -benchmem

lint:
	$(GO) vet ./...

fmt:
	gofmt -w .

tidy:
	$(GO) mod tidy

clean:
	rm -rf $(BINDIR) coinparams.mk cmd/qt/build/bin
	$(GO) clean ./...

distclean: clean
	rm -f config.mk
	rm -rf depends/

# --- Run targets ---
run-regtest:
	mkdir -p /tmp/$(COIN_NAME_LOWER)-regtest
	$(BINDIR)/$(DAEMON_NAME) \
		-network regtest \
		-datadir /tmp/$(COIN_NAME_LOWER)-regtest \
		-listen 0.0.0.0:19444 \
		-rpcbind 127.0.0.1 \
		-rpcport 19445 \
		-mine

run-regtest2:
	mkdir -p /tmp/$(COIN_NAME_LOWER)-regtest2
	$(BINDIR)/$(DAEMON_NAME) \
		-network regtest \
		-datadir /tmp/$(COIN_NAME_LOWER)-regtest2 \
		-listen 0.0.0.0:19446 \
		-rpcbind 127.0.0.1 \
		-rpcport 19447 \
		-addnode 127.0.0.1:19444

run-testnet:
	mkdir -p /tmp/$(COIN_NAME_LOWER)-testnet
	$(BINDIR)/$(DAEMON_NAME) \
		-network testnet \
		-datadir /tmp/$(COIN_NAME_LOWER)-testnet \
		-listen 0.0.0.0:19334 \
		-rpcbind 127.0.0.1 \
		-rpcport 19335 \
		-mine

run-testnet2:
	mkdir -p /tmp/$(COIN_NAME_LOWER)-testnet2
	$(BINDIR)/$(DAEMON_NAME) \
		-network testnet \
		-datadir /tmp/$(COIN_NAME_LOWER)-testnet2 \
		-listen 0.0.0.0:19336 \
		-rpcbind 127.0.0.1 \
		-rpcport 19337 \
		-addnode 127.0.0.1:19334

testnet-status:
	$(BINDIR)/$(CLI_NAME) -rpcconnect=127.0.0.1 -rpcport=19335 getblockchaininfo

chaos: build adversary
	bash scripts/chaos_test.sh

modularity:
	bash scripts/modularity_test.sh

mine-genesis:
	$(BINDIR)/$(GENESIS_NAME) --network regtest

mine-genesis-testnet:
	$(BINDIR)/$(GENESIS_NAME) --network testnet --timestamp 1773212867 --message "$(COIN_NAME_LOWER) testnet genesis"

status:
	$(BINDIR)/$(CLI_NAME) -rpcconnect=127.0.0.1 -rpcport=19445 getinfo
