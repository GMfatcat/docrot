# Configuration

[繁體中文](configuration-zh.md) · **English**

## `.docrot.json`

`docrot init` writes a `.docrot.json` with the defaults; a file only has to
mention what it changes. Unknown keys are rejected, so a typo is reported
rather than ignored.

```json
{
  "docs": ["**/*.md", "**/*.rst", "**/*.adoc", "llms.txt"],
  "exclude": ["vendor/**", "node_modules/**", "third_party/**", "3rdparty/**", "external/**", "**/testdata/**", "dist/**", ".git/**", ".*/**"],
  "ignore": [],
  "siblings": [],
  "pairs": [],
  "pairPatterns": ["{stem}-zh.md", "{stem}_zh.md", "{stem}.zh.md", "{stem}.zh-TW.md", "{stem}-zh-TW.md"],
  "configSamples": ["config.json", "config*.json", "*.example.json", "*.sample.json", "configs/**/*.json", "appsettings*.json", "**/appsettings.json"],
  "stale": { "enabled": true, "minChurn": 3, "minDays": 90, "exclude": ["CHANGELOG*.md", "ChangeLog*.md", "Changelog*.md", "CHANGES*.md", "HISTORY*.md", "NEWS*.md", "RELEASE*.md", "**/release-notes*.md", "**/release_notes*.md", "**/releases/**", "**/superpowers/**", "**/specs/**", "**/plans/**", "**/research/**", "**/deep-research/**", "**/*-report.md", "**/adr/**"] },
  "coverage": { "report": false, "includeInternal": false },
  "severity": { "stale-section": "warning", "stale-symbol": "warning", "pair-lag": "warning", "pair-number": "info", "pair-missing": "info", "pair-orphan": "warning", "stale-comment": "info", "comment-mentions-missing": "warning" },
  "net": false,
  "failOn": "error",
  "minConfidence": "low",
  "outDir": ".docrot",
  "maxFileMB": 8,
  "comments": { "enabled": true, "minChurn": 2, "minFrac": 0.5 }
}
```

- `docs` selects the documents; `exclude` names paths that are never
  walked or indexed. Both are globs with `**`. A `.txt` file that looks like
  reStructuredText is read as such when `docs` includes it (Django's
  `docs/**/*.txt`).
- `ignore` holds regular expressions matched against the reference text.
- `siblings` lists other repositories (relative to the root) where a path
  missing here may legitimately live — useful when a service documents the
  library it is built on.
- `pairs` ties a source document to its translation explicitly;
  `pairPatterns` derives translations from a source name (`{stem}` is the
  file name without its extension). `docs/en/…` ↔ `docs/<lang>/…` trees <!-- docrot:ignore missing-path -->
  are detected without configuration.
- `configSamples` are the JSON files mined for configuration keys
  (`appsettings*.json` of a .NET project among the defaults).
- `stale.minChurn` and `stale.minDays` are the staleness thresholds;
  `stale.exclude` keeps dated documents (changelogs, release notes, design
  specs) out of the staleness analysis and out of the toolchain-version
  check; they are historical records by nature. `stale.enabled: false`
  turns the git-based staleness rules off.
- `coverage.report` adds `undocumented` findings to every run;
  `includeInternal` counts `internal/` packages too.
- `severity` overrides a rule's level, e.g. `{"stale-section": "info"}`;
  `none` silences a rule.
- `net` enables external URL checks (also `--net`).
- `failOn` is the lowest severity that makes `docrot check` exit 1;
  `minConfidence` drops references below a confidence.
- `outDir` is the directory every run rewrites; see below.
- `maxFileMB` caps the size of any file whose *contents* docrot reads
  (documents, Go/Odin/Python/Rust/JavaScript/C#/C/C++ sources, JSON samples). Binaries are never
  opened at all — only their names enter the path index, so a 4 GB model
  file costs one directory entry, gitignored or not. A text file above the
  cap is skipped with a warning; paths to it still resolve.
- `comments` tunes the code-comment checks that run for every symbol a
  document refers to: `minChurn` newer commits (or one commit rewriting
  `minFrac` of the body) make a comment "stale"; `docrot comments` runs
  the same checks over every exported declaration.

## Silencing a finding where it happens

```markdown
<!-- docrot:ignore -->            the next non-blank line
inline text <!-- docrot:ignore -->  this line
<!-- docrot:ignore-start --> … <!-- docrot:ignore-end -->
<!-- docrot:ignore-file -->
<!-- docrot:ignore missing-path unknown-flag -->   only these rules (also with ignore-start)
```

reStructuredText uses `.. docrot:ignore` comments and AsciiDoc
`// docrot:ignore`, with the same forms. For patterns, add regular
expressions to `ignore`; to change a rule's level, set `severity`.

## Output directory

Every `docrot check` run rewrites one directory, named by the `outDir`
setting, so that a human and an agent always find the current report in the
same place:

```text
.docrot/.gitignore   a single "*", so the reports never reach a commit
.docrot/report.md    for agents: findings by file, how to read them, a checklist
.docrot/report.html  for humans: the filterable single-file page
.docrot/report.json  the stable JSON schema
.docrot/report.txt   the terminal report, with info findings
.docrot/git-cache.json  blame and log answers of the last run (speed only; safe to delete)
```

Each file is rendered into a temporary file and renamed into place, so an
interrupted run never leaves half a report where the next reader expects a
whole one. The directory is excluded from document discovery, so yesterday's
report is never checked as though it were documentation. Pass `--out-dir` to
put it somewhere else, `--no-out` to write nothing this run, or set `outDir`
to the empty string to turn it off for good.

## Git cache

`git-cache.json` holds the blame and log answers of the last run: blame
entries are keyed by the blob hash of the file at HEAD (never for a file
with uncommitted changes), log entries by HEAD, and only the entries a run
used are written back. It is why a second run on a large repository takes a
tenth of the time; findings are identical with or without it, and deleting
it costs nothing but that speed. `--no-out` disables the cache along with
the reports.
