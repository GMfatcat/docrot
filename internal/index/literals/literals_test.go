package literals

import (
	"reflect"
	"testing"
)

func TestIdentLike(t *testing.T) {
	yes := []string{"request_id", "X-Request-ID", "/openapi.json", "server.port", "--verbose", "GET", "v1", "{id}", "a/b"}
	no := []string{"", "a", "hello world", "1234", "it's", `say "hi"`, "tab\there", "über", "x\n"}
	for _, s := range yes {
		if !IdentLike(s) {
			t.Errorf("IdentLike(%q) = false", s)
		}
	}
	for _, s := range no {
		if IdentLike(s) {
			t.Errorf("IdentLike(%q) = true", s)
		}
	}
}

func TestScan(t *testing.T) {
	cases := map[string][]string{
		`logger.info("request_id", extra={'trace_id': tid})`:            {"request_id", "trace_id"},
		`headers = {"X-Request-ID": rid, "Accept": "application/json"}`: {"X-Request-ID", "Accept", "application/json"},
		`# "commented_out"`:                 nil,
		`x = "has space" + 'ok_one'`:        {"ok_one"},
		`path := "/openapi.json" // "note"`: {"/openapi.json", "note"},
		`s := "esc\"aped" + "plain"`:        {"plain"},
		`print('')`:                         nil,
	}
	for in, want := range cases {
		if got := Scan(in); !reflect.DeepEqual(got, want) {
			t.Errorf("Scan(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTagValues(t *testing.T) {
	got := TagValues(`json:"docs_path,omitempty" default:"/docs/" yaml:"-"`)
	want := []string{"docs_path", "/docs/"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("TagValues = %q, want %q", got, want)
	}
}

func TestSet(t *testing.T) {
	s := New()
	s.AddAll([]string{"a_b", "no way", "c.d"})
	if !s.Has("a_b") || !s.Has("c.d") || s.Has("no way") || s.Len() != 2 {
		t.Errorf("set = %v", s.m)
	}
}
