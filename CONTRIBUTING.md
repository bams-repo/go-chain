# Contributing to Fairchain

Thank you for considering contributing to Fairchain. This project is built in the
open and welcomes contributions of all kinds — code, documentation, testing,
bug reports, and ideas.

## Getting Started

1. Fork the repository and clone your fork.
2. Run `./configure --with-qt` (or `./configure` for daemon-only builds).
3. Run `make build` to compile.
4. Run `make test` to verify everything passes.

See [Getting Started](DOCS/getting-started.md) for detailed build instructions.

## Development Guidelines

### Bitcoin Core Parity

Fairchain maintains parity with Bitcoin wherever possible. When implementing
new features or fixing bugs, refer to Bitcoin Core's approach first. Deviations
are acceptable only when Bitcoin's design doesn't make sense for a smaller
network — document the reasoning in your PR.

### Code Style

- Follow standard Go conventions (`gofmt`, `go vet`).
- No unnecessary comments — code should be self-documenting. Comments should
  explain *why*, not *what*.
- Keep functions focused. If a function does too many things, split it.

### Commit Messages

- Use a short imperative summary line (50 chars or less).
- Add a blank line followed by a longer description if needed.
- Focus on *why* the change was made, not *what* changed.
- Reference issue numbers where applicable (`Fixes #123`).

### Pull Requests

- One logical change per PR. Don't bundle unrelated fixes.
- Include a clear summary of what the PR does and why.
- Add a test plan describing how you verified the change.
- Ensure CI passes before requesting review.
- Use the PR template — it will guide you through the process.

## What to Work On

- **Good first issues**: Look for issues labeled `good first issue`.
- **Testing**: Run the testnet, report bugs, improve test coverage.
- **Documentation**: Improve docs, fix typos, add examples.
- **Code review**: Review open PRs — fresh eyes catch bugs.

## Reporting Bugs

Open an issue using the bug report template. Include:
- Steps to reproduce
- Expected vs actual behavior
- OS, Go version, and Fairchain version
- Relevant log output

## Security Vulnerabilities

Do **not** open a public issue for security vulnerabilities. See
[SECURITY.md](SECURITY.md) for responsible disclosure instructions.

## License

By contributing, you agree that your contributions will be licensed under the
[MIT License](LICENSE).
