#!/usr/bin/env python3
"""Cross-compile docrot for the usual platforms into dist/ (stdlib Python only).

    python scripts/release.py [--version 0.1.0] [--targets windows/amd64,linux/amd64,...]

Each binary is named docrot-<os>-<arch>[.exe] and a SHA-256 sidecar file is
written next to it. Uses `go build -trimpath -ldflags "-s -w -X main.version=..."`.
"""
from __future__ import annotations

import hashlib
import os
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
DIST = ROOT / "dist"
DEFAULT_TARGETS = ["windows/amd64", "windows/arm64", "linux/amd64", "linux/arm64", "darwin/amd64", "darwin/arm64"]


def main(argv: list[str]) -> int:
    version = "0.1.0"
    targets = DEFAULT_TARGETS
    it = iter(argv)
    for a in it:
        if a == "--version":
            version = next(it)
        elif a == "--targets":
            targets = next(it).split(",")
        else:
            print(__doc__)
            return 2
    DIST.mkdir(exist_ok=True)
    for t in targets:
        goos, goarch = t.split("/")
        name = f"docrot-{goos}-{goarch}" + (".exe" if goos == "windows" else "")
        out = DIST / name
        env = dict(os.environ, GOOS=goos, GOARCH=goarch, CGO_ENABLED="0")
        cmd = ["go", "build", "-trimpath", "-ldflags", f"-s -w -X main.version={version}", "-o", str(out), "./cmd/docrot"]
        print(f"==> {t}")
        subprocess.run(cmd, cwd=str(ROOT), env=env, check=True)
        digest = hashlib.sha256(out.read_bytes()).hexdigest()
        (DIST / (name + ".sha256")).write_text(f"{digest}  {name}\n", encoding="utf-8")
        print(f"    {name}  {out.stat().st_size // 1024} KiB  sha256 {digest[:16]}...")
    print(f"\nbinaries in {DIST}")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
