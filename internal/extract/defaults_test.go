package extract

import (
	"reflect"
	"sort"
	"testing"

	"docrot/internal/model"
)

func TestDefaultValue(t *testing.T) {
	cases := map[string]string{
		"The `--addr` flag defaults to `:9090`.": ":9090",
		"`--verbose` (default: `false`)":         "false",
		"`--timeout` (default 30s) waits":        "30s",
		"Set `log.level` (default: \"info\").":   "info",
		"default: 8080":                          "8080",
		"`--x` defaults to including everything": "",
		"the default is notably slow":            "",
		"`--mode` defaults to `fast-path`":       "fast-path",
		"`--out` (default: none)":                "none",
	}
	for in, want := range cases {
		if got := defaultValue(in); got != want {
			t.Errorf("defaultValue(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDefaultsPass(t *testing.T) {
	md := "# Flags\n\n" +
		"The `--addr` flag defaults to `:9090` and `--verbose` defaults to `false`.\n" + // two subjects: ambiguous, skipped
		"`--addr` (default: `:8080`).\n" +
		"`FIXTURE_DEBUG` defaults to `0`.\n" +
		"`server.addr` (default `:8080`).\n\n" +
		"| Flag | Default | Notes |\n|---|---|---|\n| `--config` | `config.json` | file |\n| `--port` | 8080 | port |\n| `--out` | — | none |\n"
	refs := run(t, goHints(), md)
	var got []string
	for _, r := range refs {
		if r.Kind == model.KindDefault {
			got = append(got, r.Norm+"|"+r.Confidence.String())
		}
	}
	sort.Strings(got)
	want := []string{
		"env:FIXTURE_DEBUG|0|medium",
		"flag:addr|:8080|medium",
		"flag:config|config.json|high",
		"flag:out|none|high",
		"flag:port|8080|high",
		"key:server.addr|:8080|medium",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("defaults =\n  %v\nwant\n  %v", got, want)
	}
}
