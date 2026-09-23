#!/usr/bin/env python3
"""End-to-end verification for docrot (stdlib Python only).

Steps:
  1. gofmt -l            (no unformatted files)
  2. go vet ./...
  3. go test ./...       (-race when available)
  4. go build            → dist/docrot(.exe)
  5. docrot check on the fixture repo (must exit 1: it contains known lies)
  6. docrot check on docrot's own repo (must exit 0 with --fail-on error)
  7. docrot check --format md/json/sarif/html/github/junit on the fixture (must produce valid output)

Exit code 0 when everything passes; 1 otherwise. Run from anywhere:
    python scripts/verify.py [--no-race] [--keep]
"""
from __future__ import annotations

import json
import xml.etree.ElementTree as ET
import os
import shutil
import subprocess
import sys
import tempfile
import time
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
DIST = ROOT / "dist"
EXE = DIST / ("docrot.exe" if os.name == "nt" else "docrot")
FIXTURE = ROOT / "testdata" / "fixture"

GREEN, RED, DIM, RESET = "\033[32m", "\033[31m", "\033[2m", "\033[0m"
if not sys.stdout.isatty() or os.environ.get("NO_COLOR"):
    GREEN = RED = DIM = RESET = ""


def run(cmd: list[str], cwd: Path = ROOT, check: bool = True, env: dict | None = None) -> subprocess.CompletedProcess:
    t0 = time.time()
    p = subprocess.run(cmd, cwd=str(cwd), capture_output=True, text=True, encoding="utf-8", errors="replace", env=env)
    dt = time.time() - t0
    label = f"{DIM}$ {' '.join(cmd)}  ({dt:.1f}s){RESET}"
    print(label)
    if check and p.returncode != 0:
        print(p.stdout)
        print(p.stderr, file=sys.stderr)
        raise SystemExit(f"{RED}FAILED{RESET}: {' '.join(cmd)} (exit {p.returncode})")
    return p


def step(name: str) -> None:
    print(f"\n{GREEN}==>{RESET} {name}")


def main(argv: list[str]) -> int:
    no_race = "--no-race" in argv
    keep = "--keep" in argv
    failures: list[str] = []

    step("gofmt")
    p = run(["gofmt", "-l", "cmd", "internal"])
    if p.stdout.strip():
        failures.append("gofmt: unformatted files:\n" + p.stdout)
        print(p.stdout)

    step("go vet")
    run(["go", "vet", "./..."])

    step("go test")
    args = ["go", "test", "./..."]
    if not no_race:
        # -race needs cgo/gcc on Windows; fall back silently when unavailable.
        probe = run(["go", "env", "CGO_ENABLED"], check=False)
        if probe.stdout.strip() == "1" and shutil.which("gcc"):
            args.insert(2, "-race")
    p = run(args, check=False)
    print(p.stdout[-4000:])
    if p.returncode != 0:
        print(p.stderr, file=sys.stderr)
        failures.append("go test failed")

    step("go build")
    DIST.mkdir(exist_ok=True)
    run(["go", "build", "-trimpath", "-ldflags", "-s -w", "-o", str(EXE), "./cmd/docrot"])
    print(run([str(EXE), "version"]).stdout.strip())

    step("docrot check on fixture (expects exit 1 with known findings)")
    # --no-out everywhere the fixture is scanned: testdata is checked in, and
    # a report directory has no business appearing inside it.
    p = run([str(EXE), "check", str(FIXTURE), "--no-git", "--no-out"], check=False)
    print(p.stdout)
    if p.returncode != 1:
        failures.append(f"fixture check: expected exit 1, got {p.returncode}\n{p.stderr}")
    expected_rules = ["missing-path", "missing-symbol", "unknown-flag", "unknown-env", "broken-anchor",
                      "missing-command", "missing-import", "pair-heading", "pair-code", "pair-link"]
    for rule in expected_rules:
        if rule not in p.stdout:
            failures.append(f"fixture check: rule {rule} not reported")

    step("docrot check on itself (must be clean at --fail-on error)")
    p = run([str(EXE), "check", str(ROOT), "--fail-on", "error"], check=False)
    print(p.stdout)
    if p.returncode != 0:
        failures.append(f"self check: exit {p.returncode}\n{p.stderr}")

    step("output formats")
    tmp = Path(tempfile.mkdtemp(prefix="docrot-verify-"))
    try:
        for fmt in ("md", "json", "sarif", "html", "github", "junit"):
            out = tmp / f"report.{fmt}"
            p = run([str(EXE), "check", str(FIXTURE), "--no-git", "--no-out", "--format", fmt, "--output", str(out)], check=False)
            if p.returncode not in (0, 1):
                failures.append(f"format {fmt}: exit {p.returncode}\n{p.stderr}")
                continue
            data = out.read_text(encoding="utf-8")
            if fmt in ("json", "sarif"):
                try:
                    doc = json.loads(data)
                except json.JSONDecodeError as e:
                    failures.append(f"format {fmt}: invalid JSON: {e}")
                    continue
                if fmt == "sarif" and doc.get("version") != "2.1.0":
                    failures.append("sarif: version is not 2.1.0")
                if fmt == "json" and "findings" not in doc:
                    failures.append("json: no findings key")
            elif fmt == "github":
                bad = [l for l in data.splitlines() if l and not l.startswith("::")]
                if bad or "::error file=" not in data:
                    failures.append(f"github: not every line is a workflow command: {bad[:3]}")
            elif fmt == "junit":
                try:
                    tree = ET.fromstring(data)
                except ET.ParseError as e:
                    failures.append(f"junit: invalid XML: {e}")
                    continue
                if tree.tag != "testsuites" or tree.find("testsuite") is None:
                    failures.append("junit: no <testsuites>/<testsuite>")
            elif fmt == "md":
                for want in ("# docrot report", "## How to read this", "## Findings", "## Fix checklist", "missing-path"):
                    if want not in data:
                        failures.append(f"md: report is missing {want!r}")
            else:
                if "<html" not in data.lower() or "missing-path" not in data:
                    failures.append("html: report looks empty")
            print(f"  {fmt}: {out.stat().st_size} bytes")
    finally:
        if not keep:
            shutil.rmtree(tmp, ignore_errors=True)

    print()
    if failures:
        print(f"{RED}VERIFY FAILED{RESET}")
        for f in failures:
            print(" -", f)
        return 1
    print(f"{GREEN}VERIFY OK{RESET}")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
