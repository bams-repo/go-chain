# Security Policy

## Reporting a Vulnerability

If you discover a security vulnerability in Fairchain, **please do not open a
public issue**. Responsible disclosure helps protect users of the software.

### How to Report

Send an email to **fairchain-security@proton.me** with:

- A description of the vulnerability
- Steps to reproduce or a proof of concept
- The potential impact (e.g., funds at risk, denial of service, consensus failure)
- Your suggested fix, if any

### What to Expect

- **Acknowledgment** within 48 hours of your report.
- We will work with you to understand and validate the issue.
- A fix will be developed privately and disclosed responsibly.
- Credit will be given to the reporter unless anonymity is requested.

## Scope

The following are in scope:

- Consensus bugs (invalid blocks accepted, valid blocks rejected)
- P2P protocol vulnerabilities (remote crash, resource exhaustion, eclipse attacks)
- Wallet security (key leakage, unauthorized spending)
- RPC authentication bypass
- Cryptographic weaknesses in the PoW algorithm or transaction signing

## Supported Versions

Security fixes are applied to the latest release on the `main` branch.
Older releases are not maintained.
