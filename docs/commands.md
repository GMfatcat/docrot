# Commands

[繁體中文](commands-zh.md) · **English**

## Synopsis

```text
docrot check [dir] [--format text|md|json|sarif|html|github|junit] [--output FILE]
             [--fail-on error|warning|info|none] [--min-confidence low|medium|high]
             [--out-dir DIR] [--no-out]
             [--no-git] [--net] [--info] [--all] [--coverage] [--quiet] [--config FILE]
             [--changed] [--since REF]
docrot baseline [dir]            write .docrot-baseline.json
docrot coverage [dir]            documentation coverage table (symbols, flags, env, routes, config keys)
docrot pairs [dir]               only the bilingual checks
docrot comments [dir]            comment checks over every exported declaration
docrot explain <doc> [--kind K]  every extracted reference with its verdict
docrot index [dir] --kind symbols|flags|env|paths|anchors|config|routes|targets|defaults|odin|python|rust|js|csharp|c
docrot init [dir]
docrot version
```

`dir` defaults to the current directory. Run `docrot <command> -h` for the
flags of one command.

## Flags

Shared by the scanning commands: `--config` picks the config file,
`--no-git` disables the git rules, `--net` checks URLs, `--verbose` prints
index and git warnings, `--min-confidence` drops weak references.

`check` adds:

- `--format` and `--output` — one report to a file instead of the terminal.
  The output directory is written regardless. `github` prints one workflow
  command per finding (`::error file=README.md,line=12,title=missing-path::…`),
  which GitHub and Gitea Actions turn into pull-request annotations; `junit`
  writes JUnit XML with one test case per document and rule (info findings
  are skipped cases) for the test panels of GitLab, Jenkins and Gitea. Both
  hide info findings unless `--info` and baselined ones unless `--all`.
- `--fail-on` — the lowest severity that makes the exit code 1 (default
  from the config, `error`); `none` never fails.
- `--info` lists info-level findings in the text report; `--all` also
  shows baselined findings.
- `--coverage` appends the documentation-coverage section.
- `--quiet` prints the summary line only.
- `--out-dir` and `--no-out` move or skip the output directory.
- `--changed` checks only the documents modified since HEAD in the work
  tree or index, plus untracked ones; `--since REF` adds the documents
  changed on this branch since the merge base with `REF` (a pull-request
  check). Every document is still parsed so cross-document anchors resolve;
  coverage is skipped because it needs every document; a clean tree checks
  nothing and exits 0. Both need git.

`explain` adds `--kind` (only references of one kind) and `--root` (the
repository root when the document is given by a path outside it); `index`
takes `--kind`.

## Exit codes

| Code | Meaning |
|---|---|
| 0 | No new finding at or above `--fail-on`. |
| 1 | At least one new finding at or above `--fail-on`. Baselined findings never count. |
| 2 | Usage error, bad config, or internal error. |

## Baselines

`docrot baseline` freezes the current findings into `.docrot-baseline.json`;
later runs only fail on findings that are not in it, and `--all` shows the
baselined ones again. Fingerprints exclude line numbers and section
headings, so editing around a finding does not resurrect it.

## CI

```yaml
- run: go run ./cmd/docrot check --format sarif --output docrot.sarif --fail-on error
- run: go run ./cmd/docrot check --changed --since origin/main --fail-on warning   # PR: changed docs only
- run: go run ./cmd/docrot check --format github --fail-on warning                # annotations on the PR, no SARIF upload needed
- uses: github/codeql-action/upload-sarif@v3
  with: { sarif_file: docrot.sarif }
```

The SARIF output uploads directly to GitHub code scanning; baselined
findings carry `baselineState: unchanged`. For a pre-commit hook,
`docrot check --changed --fail-on warning` runs in well under a second on a
clean tree.

## Profiling

`DOCROT_CPUPROFILE=cpu.prof docrot check …` writes a CPU profile of the run
for `go tool pprof`.
