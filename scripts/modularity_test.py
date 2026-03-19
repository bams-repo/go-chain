#!/usr/bin/env python3
# Branding values sourced from internal/coinparams/coinparams.go
"""
Cross-platform modularity test entrypoint.

This script provides OS detection and launches the modularity test backend
script in a platform-aware way:
  - Linux/macOS: bash
  - Windows: Git Bash (if available) or WSL bash

Usage:
  python scripts/modularity_test.py [--skip ALGOS] [--debug]
"""

from __future__ import annotations

import argparse
import os
import platform
import re
import shlex
import shutil
import subprocess
import sys
from pathlib import Path

EXPECTED_ALGORITHMS = {"sha256d", "argon2id", "scrypt", "sha256mem"}

EXPECTED_PHASES = {"A", "B", "C", "D", "E", "F", "G"}
EXPECTED_NODE_TYPES = {"SEED", "miner"}


def _is_windows() -> bool:
    return platform.system().lower().startswith("win")


def _project_root() -> Path:
    return Path(__file__).resolve().parent.parent


def _backend_script() -> Path:
    return _project_root() / "scripts" / "modularity_test.sh"


def _find_windows_bash() -> list[list[str]]:
    candidates: list[list[str]] = []

    for name in ("bash", "bash.exe"):
        p = shutil.which(name)
        if p:
            candidates.append([p])

    git_bash = Path("C:/Program Files/Git/bin/bash.exe")
    if git_bash.exists():
        candidates.append([str(git_bash)])

    msys_bash = Path("C:/msys64/usr/bin/bash.exe")
    if msys_bash.exists():
        candidates.append([str(msys_bash)])

    wsl = shutil.which("wsl") or shutil.which("wsl.exe")
    if wsl:
        candidates.append([wsl, "bash"])

    return candidates


def _verify_backend_parity(backend: Path) -> tuple[bool, str]:
    try:
        text = backend.read_text(encoding="utf-8")
    except OSError as exc:
        return False, f"cannot read backend script: {exc}"

    algo_hits = set(re.findall(r'ALGORITHMS=\(([^)]+)\)', text))
    if algo_hits:
        found_algos = set()
        for match in algo_hits:
            found_algos.update(match.split())
        missing_algos = sorted(EXPECTED_ALGORITHMS - found_algos)
    else:
        missing_algos = sorted(EXPECTED_ALGORITHMS)

    phase_hits = set(re.findall(r'Phase ([A-G]):', text))
    missing_phases = sorted(EXPECTED_PHASES - phase_hits)

    if missing_algos or missing_phases:
        details = []
        if missing_algos:
            details.append(f"missing algorithms: {','.join(missing_algos)}")
        if missing_phases:
            details.append(f"missing phases: {','.join(missing_phases)}")
        return False, "; ".join(details)

    return True, "ok"


def _run_backend(skip: str | None, debug: bool) -> int:
    backend = _backend_script()
    if not backend.exists():
        print(f"[modtest] backend script not found: {backend}", file=sys.stderr)
        return 1

    parity_ok, parity_msg = _verify_backend_parity(backend)
    if not parity_ok:
        print(f"[modtest] backend parity check failed: {parity_msg}", file=sys.stderr)
        return 1
    print("[modtest] backend parity: verified")

    extra_args: list[str] = []
    if skip:
        extra_args.extend(["--skip", skip])
    if debug:
        extra_args.append("--debug")

    env = os.environ.copy()
    env["MODTEST_LAUNCHER"] = "python"
    env["MODTEST_HOST_OS"] = platform.system().lower()

    if _is_windows():
        for shell_cmd in _find_windows_bash():
            if shell_cmd[0].lower().endswith("wsl.exe") or shell_cmd[0].lower().endswith("wsl"):
                quoted = " ".join(shlex.quote(x) for x in [backend.as_posix(), *extra_args])
                cmd = shell_cmd + ["-lc", quoted]
            else:
                cmd = shell_cmd + [str(backend)] + extra_args

            try:
                return subprocess.run(cmd, cwd=str(_project_root()), env=env).returncode
            except FileNotFoundError:
                continue

        print(
            "[modtest] no bash-compatible shell found on Windows.\n"
            "Install Git Bash or enable WSL, then re-run:\n"
            "  python scripts/modularity_test.py",
            file=sys.stderr,
        )
        return 1

    bash = shutil.which("bash")
    if not bash:
        print("[modtest] bash not found in PATH", file=sys.stderr)
        return 1

    cmd = [bash, str(backend)] + extra_args
    return subprocess.run(cmd, cwd=str(_project_root()), env=env).returncode


def main() -> int:
    parser = argparse.ArgumentParser(description="Cross-platform Fairchain modularity test launcher")
    parser.add_argument(
        "--skip",
        default="",
        help="Comma-separated algorithm names to skip (e.g., argon2id,scrypt)",
    )
    parser.add_argument(
        "--debug",
        action="store_true",
        help="Enable hyper-verbose node debug output",
    )
    parser.add_argument(
        "--check-parity",
        action="store_true",
        help="Verify phase parity against the shell backend and exit",
    )
    args = parser.parse_args()
    skip = args.skip.strip() or None

    print(f"[modtest] launcher OS: {platform.system()}")
    if args.check_parity:
        ok, msg = _verify_backend_parity(_backend_script())
        if ok:
            print("[modtest] backend parity: verified")
            return 0
        print(f"[modtest] backend parity check failed: {msg}", file=sys.stderr)
        return 1
    return _run_backend(skip, args.debug)


if __name__ == "__main__":
    raise SystemExit(main())
