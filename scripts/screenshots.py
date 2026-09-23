#!/usr/bin/env python3
"""Regenerate the README illustrations under docs/assets/.

    python scripts/screenshots.py

Produces:
  docs/assets/terminal.svg  — `docrot check` on the fixture repository, as a
                              terminal transcript (colours by severity), an
                              SVG so it stays crisp and searchable
  docs/assets/report.png    — the fixture's HTML report, screenshotted with a
                              headless Chrome or Edge when one is installed
                              (skipped otherwise)

Standard library only. The fixture is deterministic, so the images only
change when the checks do.
"""
from __future__ import annotations

import html
import os
import re
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
ASSETS = ROOT / "docs" / "assets"
FIXTURE = ROOT / "testdata" / "fixture"

# The transcript shows a representative slice of the fixture's findings:
# one line per rule family keeps the picture readable.
KEEP = [
    "README.md:11:", "README.md:12:", "README.md:16:", "README.md:18:", "README.md:22:", "README.md:27:",
    "README.md:42:31", "README.md:43:", "README.md:45:", "README.md:51:", "README.md:63:", "README.md:69:",
    "README-zh.md:1: warning pair-heading", "README-zh.md:11:", "docs/api.rst:7:", "docs/notes.adoc:3:54",
    "pkg/httpx/server.go:26", "tools/helper.py:15",
]

MAX_COLS = 128

COLORS = {
    "bg": "#1b1e24", "fg": "#d7dae0", "muted": "#7d8390", "prompt": "#7ee787",
    "error": "#ff7b72", "warning": "#e3b341", "info": "#8b949e", "loc": "#79c0ff", "rule": "#d2a8ff",
}


def build_binary() -> Path:
    exe = ROOT / ("docrot.exe" if os.name == "nt" else "docrot")
    subprocess.run(["go", "build", "-o", str(exe), "./cmd/docrot"], cwd=ROOT, check=True)
    return exe


def transcript(exe: Path) -> list[str]:
    out = subprocess.run([str(exe), "check", str(FIXTURE), "--no-git", "--no-out"],
                         cwd=ROOT, capture_output=True, text=True, encoding="utf-8")
    lines = out.stdout.splitlines()
    kept = [l for l in lines if any(l.startswith(k) for k in KEEP)]
    summary = [l for l in lines if " errors, " in l]
    # a picture has a right edge; a terminal would wrap
    return [l if len(l) <= MAX_COLS else l[:MAX_COLS - 1] + "…" for l in kept + [""] + summary[:1]]


def svg_line(text: str, y: int) -> str:
    """One transcript line as SVG tspans coloured by part."""
    m = re.match(r"^(\S+?:\d+(?::\d+)?): (error|warning|info) (\S+) (.*)$", text)
    if not m:
        if " errors, " in text:
            return f'<text x="16" y="{y}"><tspan fill="{COLORS["fg"]}" font-weight="bold">{html.escape(text)}</tspan></text>'
        return f'<text x="16" y="{y}"><tspan fill="{COLORS["fg"]}">{html.escape(text)}</tspan></text>'
    loc, sev, rule, msg = m.groups()
    return (f'<text x="16" y="{y}">'
            f'<tspan fill="{COLORS["loc"]}">{html.escape(loc)}</tspan>'
            f'<tspan fill="{COLORS["muted"]}">: </tspan>'
            f'<tspan fill="{COLORS[sev]}" font-weight="bold">{sev}</tspan>'
            f'<tspan fill="{COLORS["muted"]}"> </tspan>'
            f'<tspan fill="{COLORS["rule"]}">{html.escape(rule)}</tspan>'
            f'<tspan fill="{COLORS["fg"]}"> {html.escape(msg)}</tspan>'
            f'</text>')


def render_svg(lines: list[str]) -> str:
    width = max(len(l) for l in lines) if lines else 80
    width = min(max(width, 100), MAX_COLS)
    line_h = 20
    char_w = 7.35
    w = int(16 * 2 + width * char_w)
    rows = ['<text x="16" y="34"><tspan fill="%s">$ </tspan><tspan fill="%s">docrot check</tspan></text>'
            % (COLORS["prompt"], COLORS["fg"])]
    y = 34
    for l in lines:
        y += line_h
        if l == "":
            continue
        rows.append(svg_line(l, y))
    h = y + 24
    style = ('font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, "Liberation Mono", monospace; '
             'font-size: 12.5px; white-space: pre;')
    return (f'<svg xmlns="http://www.w3.org/2000/svg" width="{w}" height="{h}" viewBox="0 0 {w} {h}" '
            f'role="img" aria-label="docrot check output on the fixture repository">\n'
            f'<rect width="{w}" height="{h}" rx="8" fill="{COLORS["bg"]}"/>\n'
            f'<g style=\'{style}\'>\n' + "\n".join(rows) + "\n</g>\n</svg>\n")


def browser() -> str | None:
    for name in ("chrome", "google-chrome", "chromium", "msedge"):
        if p := shutil.which(name):
            return p
    for p in (r"C:\Program Files\Google\Chrome\Application\chrome.exe",
              r"C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe",
              "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"):
        if os.path.exists(p):
            return p
    return None


def screenshot(exe: Path) -> bool:
    b = browser()
    if not b:
        print("no Chrome/Edge found; skipping report.png")
        return False
    with tempfile.TemporaryDirectory() as tmp:
        subprocess.run([str(exe), "check", str(FIXTURE), "--no-git", "--out-dir", tmp],
                       cwd=ROOT, capture_output=True)
        # the header shows the absolute root and the build's VCS hash; neither
        # belongs in a picture that ships with the README
        report = Path(tmp, "report.html")
        text = report.read_text(encoding="utf-8")
        text = text.replace(html.escape(str(FIXTURE)), "testdata/fixture")
        text = re.sub(r"version (\d+\.\d+\.\d+)\S*", r"version \1", text)  # "+" arrives as &#43;
        report.write_text(text, encoding="utf-8")
        page = report.resolve().as_uri()
        out = ASSETS / "report.png"
        subprocess.run([b, "--headless=new", "--disable-gpu", "--hide-scrollbars", "--window-size=1200,900",
                        f"--screenshot={out}", page], check=True, capture_output=True)
    return True


def main() -> int:
    ASSETS.mkdir(parents=True, exist_ok=True)
    exe = build_binary()
    lines = transcript(exe)
    (ASSETS / "terminal.svg").write_text(render_svg(lines), encoding="utf-8", newline="\n")
    print(f"wrote {ASSETS / 'terminal.svg'} ({len(lines)} lines)")
    if screenshot(exe):
        print(f"wrote {ASSETS / 'report.png'}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
