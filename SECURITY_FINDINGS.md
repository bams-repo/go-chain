# Security Findings

This document tracks security-relevant issues discovered during repository review.

## 1) Sensitive RPC parameters exposed in URL query strings (High)

- **Area:** `cmd/cli/main.go`
- **Issue:** The CLI sends all RPC calls with HTTP GET and places sensitive data (for example wallet passphrases and private keys) in URL query parameters.
- **Risk:** Query strings can be logged by shells, proxies, reverse proxies, and observability systems, resulting in credential/private key leakage.
- **Fix status:** Fixed in branch `security/cli-post-sensitive-params`.

## 2) State-changing RPC endpoints allow GET requests (Medium)

- **Area:** `internal/rpc/server.go`
- **Issue:** `/addnode` and `/disconnectnode` are state-changing but do not enforce POST.
- **Risk:** Increases CSRF surface and violates method safety expectations; state changes can be triggered unintentionally through GET-capable clients.
- **Fix status:** Fixed in branch `security/rpc-post-node-admin`.

## 3) API keys persisted in raw ingestion config snapshots (High)

- **Area:** `internal/marketdata/store.go`, `internal/marketdata/ingest.go`
- **Issue:** Ingestion run config snapshots persist cleartext API keys.
- **Risk:** Database compromise or accidental DB exfiltration reveals third-party credentials.
- **Fix status:** Fixed in branch `security/marketdata-redact-api-keys`.

## 4) Full test suite fails by default due C-source package layout (Reliability)

- **Area:** `resources/sha256mem-c`
- **Issue:** `go test ./...` fails in standard non-cgo flows because C sources are included in a Go package without proper build constraints.
- **Risk:** CI drift and reduced confidence in full-repo test health.
- **Fix status:** Not fixed in this set (tracked as hardening follow-up).
