#!/usr/bin/env python3
"""Run docrot against one or more repositories and collect reports.

    python scripts/demo.py ../meowbase ../meowshare [--out reports] [--net]

For every target this writes <out>/<name>.txt, <name>.json and <name>.html,
prints the summary line, and finishes with a table across all targets.
Only the Python standard library is used.
"""
from __future__ import annotations

import json
import os
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
EXE = ROOT / "dist" / ("docrot.exe" if os.name == "nt" else "docrot")


def ensure_built() -> None:
    if EXE.exists():
        return
    EXE.parent.mkdir(exist_ok=True)
    subprocess.run(["go", "build", "-o", str(EXE), "./cmd/docrot"], cwd=str(ROOT), check=True)


def main(argv: list[str]) -> int:
    out_dir = Path("reports")
    net = False
    targets: list[Path] = []
    it = iter(argv)
    for a in it:
        if a == "--out":
            out_dir = Path(next(it))
        elif a == "--net":
            net = True
        else:
            targets.append(Path(a))
    if not targets:
        print(__doc__)
        return 2
    ensure_built()
    out_dir.mkdir(parents=True, exist_ok=True)
    rows = []
    for t in targets:
        name = t.resolve().name
        base = [str(EXE), "check", str(t)] + (["--net"] if net else [])
        for fmt in ("text", "json", "html"):
            ext = {"text": "txt", "json": "json", "html": "html"}[fmt]
            dest = out_dir / f"{name}.{ext}"
            subprocess.run(base + ["--format", fmt, "--output", str(dest), "--fail-on", "none"],
                           capture_output=True, text=True, encoding="utf-8", errors="replace")
        data = json.loads((out_dir / f"{name}.json").read_text(encoding="utf-8"))
        s = data["summary"]
        rows.append((name, s["docs"], s["references"], s["errors"], s["warnings"], s["infos"], s["duration_ms"]))
        print(f"{name}: {s['errors']} errors, {s['warnings']} warnings, {s['infos']} info — "
              f"{s['docs']} docs, {s['references']} references, {s['duration_ms']/1000:.2f}s")
    print()
    print(f"{'repo':<24}{'docs':>6}{'refs':>8}{'err':>6}{'warn':>6}{'info':>6}{'ms':>8}")
    for r in rows:
        print(f"{r[0]:<24}{r[1]:>6}{r[2]:>8}{r[3]:>6}{r[4]:>6}{r[5]:>6}{r[6]:>8}")
    print(f"\nreports in {out_dir.resolve()}")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
