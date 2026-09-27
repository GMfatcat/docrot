package extract

import (
	"strings"
	"testing"

	"docrot/internal/model"
)

// TestNegatedSentences: a sentence that names a symbol or path to say it
// does not exist is not an existence claim.
func TestNegatedSentences(t *testing.T) {
	h := goHints()
	md := strings.Join([]string{
		"# x",
		"- **不提供 `store.NewTask()`**：`httpx.WriteData` 本來就是那個抽象",
		"- **No `store.Retry()`.** Wire the closure below.",
		"> 關於曾經存在的 `Codec` 介面（`store.Proto`／`store.Raw`）：它被刪除了。",
		"**刪除**：`internal/errs`、`internal/log`（含 `internal/metric/metrictest`）。",
		"`internal/old` was removed in 0.9; use `internal/new` instead.",
		"The old `cmd/legacy` binary is gone.",
		"Still checked: `store.Open()` and `internal/api`.",
		"not thread-safe: `httpx.Server` needs a mutex.",
	}, "\n")
	refs := run(t, h, md)
	for _, gone := range []string{"store.NewTask", "store.Retry", "store.Proto", "store.Raw", "internal/errs", "internal/log", "internal/metric/metrictest", "internal/old", "cmd/legacy"} {
		if find(refs, model.KindGoSymbol, gone) != nil || find(refs, model.KindPath, gone) != nil {
			t.Errorf("%q is negated and must not be a claim", gone)
		}
	}
	for kind, norm := range map[model.Kind]string{model.KindGoSymbol: "store.Open", model.KindPath: "internal/api"} {
		if find(refs, kind, norm) == nil {
			t.Errorf("%s %q must still be a claim", kind, norm)
		}
	}
	if find(refs, model.KindPath, "internal/new") == nil {
		t.Error("`internal/new` after the negated clause must still be a claim")
	}
	if find(refs, model.KindGoSymbol, "httpx.Server") == nil {
		t.Error("`not thread-safe: X` is a property, not a negated existence claim")
	}
	if find(refs, model.KindGoSymbol, "httpx.WriteData") == nil {
		t.Error("the alternative named after the negation must still be a claim")
	}
}

func TestBracePatternsAndSpacedPaths(t *testing.T) {
	h := goHints()
	refs := run(t, h, "# x\n\nRun `scripts/smoke.{sh,ps1}`; the PRD is `docs/Local Artifact Relay PRD v0.1.md`.\n\nA route: `/items/{id}`; a command: `go run ./cmd/x docs/a.md`; a script: `./scripts/run.sh docs/a.md`.\n")
	for _, p := range []string{"scripts/smoke.sh", "scripts/smoke.ps1", "docs/Local Artifact Relay PRD v0.1.md"} {
		if r := find(refs, model.KindPath, p); r == nil {
			t.Errorf("path %q missing", p)
		} else if r.Confidence != model.High {
			t.Errorf("path %q confidence %v, want high", p, r.Confidence)
		}
	}
	for _, r := range refs {
		if r.Kind == model.KindPath && strings.Contains(r.Norm, " ") && !strings.HasPrefix(r.Norm, "docs/Local") {
			t.Errorf("command line taken for a spaced path: %q", r.Norm)
		}
		if r.Kind == model.KindPath && strings.Contains(r.Norm, "{") {
			t.Errorf("brace pattern emitted raw: %q", r.Norm)
		}
	}
}

func TestPlaceholdersAndMembersAreNotPaths(t *testing.T) {
	h := goHints()
	md := "# x\n\n```bash\ngo build -o out ./cmd/<your-service>\n./bin/<name> --help\n```\n\nSee `httpx.Service`, `fs.FS`, `Options.FS`, a `.tmp` file, the `.json` format, `.env` and `.gitignore`, generated `.pb.go` files, `_test.go` files, a `.tar.gz`, and `Date.now` / `console.log`.\n"
	refs := run(t, h, md)
	for _, r := range refs {
		if r.Kind == model.KindCommand || r.Kind == model.KindPath {
			if strings.ContainsAny(r.Norm, "<>") {
				t.Errorf("placeholder %q reported as %s", r.Norm, r.Kind)
			}
			switch r.Norm {
			case "httpx.Service", "fs.FS", "Options.FS", ".tmp", ".json", ".pb.go", "_test.go", ".tar.gz":
				t.Errorf("%q reported as %s", r.Norm, r.Kind)
			}
		}
		if r.Norm == "Date.now" || r.Norm == "console.log" {
			t.Errorf("runtime global %q reported as %s", r.Norm, r.Kind)
		}
	}
	if find(refs, model.KindGoSymbol, "httpx.Service") == nil {
		t.Error("httpx.Service must be classified as a Go symbol")
	}
	if find(refs, model.KindPath, ".gitignore") == nil {
		t.Error("dotfile `.gitignore` must stay a path")
	}
}

func TestBuiltinCallsAreNotSymbols(t *testing.T) {
	h := goHints()
	md := "# x\n\n`MIN(c)`, `MAX(typeof(c))`, `typeof()`, `wal_checkpoint(TRUNCATE)`, `pragma_table_info(?)`, `julianday()`, `make([]byte, size)`, `close(items)`, `recover()`, `cancel()`, `fn(line, item)` are noise; `Ready()` and `NewServer()` are claims.\n"
	refs := run(t, h, md)
	for _, r := range refs {
		if r.Kind == model.KindGoSymbol {
			switch r.Norm {
			case "MIN", "MAX", "typeof", "wal_checkpoint", "pragma_table_info", "julianday", "make", "close", "recover", "cancel", "fn":
				t.Errorf("%q reported as a Go symbol", r.Norm)
			}
		}
	}
	for _, n := range []string{"Ready", "NewServer"} {
		if find(refs, model.KindGoSymbol, n) == nil {
			t.Errorf("%q must still be a claim", n)
		}
	}
}
