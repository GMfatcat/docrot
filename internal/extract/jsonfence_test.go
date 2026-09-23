package extract

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"docrot/internal/model"
)

func TestWalkJSONKeys(t *testing.T) {
	src := `{
  // the listener
  "server": {"addr": ":8080", "timeout_ms": 5000,},
  "log": {
    "level": "info", /* multi
    line */ "format": "json"
  },
  "servers": [{"addr": "a"}, {"addr": "b", "tls": {"cert": "x"}}],
  "tags": ["a", "b"],
  "rest": ...
}`
	keys := walkJSONKeys(lenientJSON(strings.Split(src, "\n")))
	var got []string
	for _, k := range keys {
		got = append(got, k.path+"@"+itoa(k.line))
	}
	want := []string{
		"server@2", "server.addr@2", "server.timeout_ms@2",
		"log@3", "log.level@4", "log.format@5",
		"servers@7", "servers.addr@7", "servers.addr@7", "servers.tls@7", "servers.tls.cert@7",
		"tags@8", "rest@9",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("keys =\n  %v\nwant\n  %v", got, want)
	}
	if walkJSONKeys(`{"a": }`) != nil {
		t.Error("invalid JSON should yield nothing")
	}
	// a fragment is wrapped
	if got := walkJSONKeys(lenientJSON([]string{`"addr": ":8080",`, `"level": "info"`})); len(got) != 2 || got[1].path != "level" {
		t.Errorf("fragment keys = %+v", got)
	}
}

func TestJSONFenceRefs(t *testing.T) {
	h := goHints()
	h.jsonKeys = map[string]bool{"server": true, "server.addr": true, "server.timeout_ms": true, "log": true, "log.level": true, "log.format": true}
	h.cfgKeys = map[string]bool{"retention": true, "retention.days": true}
	md := "# Config\n\n```json\n{\n  \"server\": {\"addr\": \":8080\", \"timeout\": 5},\n  \"log\": {\"level\": \"info\", \"fmt\": \"json\"},\n  \"retention\": {\"days\": 7},\n  \"metrics\": {\"enabled\": true, \"path\": \"/metrics\"}\n}\n```\n\n" +
		"An API response, not config:\n\n```json\n{\"id\": 1, \"items\": [{\"name\": \"x\"}]}\n```\n\n" +
		"Mostly unknown keys:\n\n```json\n{\"server\": {}, \"a\": 1, \"b\": 2, \"c\": 3}\n```\n"
	refs := run(t, h, md)
	type got struct {
		norm string
		conf model.Confidence
		line int
	}
	var keys []got
	for _, r := range refs {
		if r.Kind == model.KindConfigKey && r.Lang == "json" {
			keys = append(keys, got{r.Norm, r.Confidence, r.Loc.Line})
		}
	}
	want := []got{
		{"server", model.High, 5}, {"server.addr", model.High, 5}, {"server.timeout", model.High, 5},
		{"log", model.High, 6}, {"log.level", model.High, 6}, {"log.fmt", model.High, 6},
		{"retention", model.High, 7}, {"retention.days", model.High, 7},
		{"metrics", model.High, 8}, // metrics.* are not emitted: their parent is unknown
		{"a", model.Medium, 21}, {"b", model.Medium, 21}, {"c", model.Medium, 21}, {"server", model.Medium, 21},
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].line != keys[j].line {
			return keys[i].line < keys[j].line
		}
		return keys[i].norm < keys[j].norm
	})
	sort.Slice(want, func(i, j int) bool {
		if want[i].line != want[j].line {
			return want[i].line < want[j].line
		}
		return want[i].norm < want[j].norm
	})
	if !reflect.DeepEqual(keys, want) {
		t.Errorf("json keys =\n  %+v\nwant\n  %+v", keys, want)
	}
}
