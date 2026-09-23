package routes

import (
	"reflect"
	"testing"
)

func TestNormalize(t *testing.T) {
	cases := map[string][]string{
		"/":                      nil,
		"":                       nil,
		"/items":                 {"items"},
		"/items/":                {"items"},
		"items/":                 {"items"},
		"/items/{id}":            {"items", "{}"},
		"/items/{id:[0-9]+}":     {"items", "{}"},
		"/items/:id":             {"items", "{}"},
		"/items/<int:id>":        {"items", "{}"},
		"/files/{path...}":       {"files", "**"},
		"/files/*":               {"files", "**"},
		"/files/*filepath":       {"files", "**"},
		"/files/{rest:.*}":       {"files", "**"},
		"/items/{$}":             {"items", "{$}"},
		"/items?page=2":          {"items"},
		"//double//slashes":      {"double", "slashes"},
		"/v{version}/x":          {"{}", "x"},
		"/openapi.json":          {"openapi.json"},
		"/users/{user_id}/posts": {"users", "{}", "posts"},
	}
	for in, want := range cases {
		if got := Normalize(in); !reflect.DeepEqual(got, want) {
			t.Errorf("Normalize(%q) = %v, want %v", in, got, want)
		}
	}
}

func build() *Set {
	s := New()
	s.Add(Route{Method: "GET", Path: "/v1/items", File: "a.go", Line: 1})
	s.Add(Route{Method: "POST", Path: "/v1/items", File: "a.go", Line: 2})
	s.Add(Route{Method: "", Path: "/v1/items/{id}", File: "a.go", Line: 3})
	s.Add(Route{Method: "", Path: "/healthz", File: "b.go", Line: 4})
	s.Add(Route{Method: "GET", Path: "/files/{path...}", File: "b.go", Line: 5})
	s.Add(Route{Method: "GET", Path: "items/", File: "urls.py", Line: 6}) // Django style
	s.Add(Route{Method: "", Path: "/api", Prefix: true, File: "r.go", Line: 7})
	s.Add(Route{Method: "GET", Path: "/", File: "b.go", Line: 8})
	s.Add(Route{Method: "GET", Path: "/exact/{$}", File: "b.go", Line: 9})
	return s
}

func TestLookup(t *testing.T) {
	s := build()
	ok := []struct{ m, p string }{
		{"GET", "/v1/items"}, {"POST", "/v1/items"}, {"", "/v1/items"},
		{"GET", "/v1/items/42"}, {"DELETE", "/v1/items/{item_id}"}, {"PUT", "/v1/items/:id"},
		{"", "/healthz"}, {"GET", "/healthz/"}, {"GET", "/files/a/b/c.txt"},
		{"GET", "/items"}, {"", "/api"}, {"GET", "/"}, {"GET", "/exact"}, {"GET", "/exact/{$}"}, {"GET", "/{$}"},
		{"GET", "/v1/items?page=2"},
		// mounted: the trailing segments match a route
		{"GET", "/api/v1/items"}, {"", "/prefix/healthz"},
	}
	for _, c := range ok {
		if m := s.Lookup(c.m, c.p); !m.OK {
			t.Errorf("Lookup(%q, %q) = %+v, want OK", c.m, c.p, m)
		}
	}
	if m := s.Lookup("GET", "/api/v1/items"); !m.Mounted {
		t.Errorf("mounted match not flagged: %+v", m)
	}
	if m := s.Lookup("GET", "/v1/items"); m.File != "a.go" || m.Line != 1 {
		t.Errorf("file/line = %s:%d", m.File, m.Line)
	}
	missing := []struct{ m, p string }{
		{"GET", "/v1/item"}, {"", "/readyz"}, {"GET", "/api/other"}, {"GET", "/exact/more"},
		{"GET", "/v1"}, {"", "/v1/items/1/2"},
	}
	for _, c := range missing {
		if m := s.Lookup(c.m, c.p); m.OK || len(m.Methods) > 0 {
			t.Errorf("Lookup(%q, %q) = %+v, want miss", c.m, c.p, m)
		}
	}
	m := s.Lookup("DELETE", "/v1/items")
	if m.OK || !reflect.DeepEqual(m.Methods, []string{"GET", "POST"}) {
		t.Errorf("method mismatch: %+v", m)
	}
}

func TestList(t *testing.T) {
	s := build()
	want := []string{"/api/**", "/healthz", "/v1/items/{}", "GET /", "GET /exact/{$}", "GET /files/**", "GET /items", "GET /v1/items", "POST /v1/items"}
	if got := s.List(); !reflect.DeepEqual(got, want) {
		t.Errorf("List = %v\nwant %v", got, want)
	}
	if got := s.Paths(); len(got) != 8 {
		t.Errorf("Paths = %v", got)
	}
}

func TestSplitPattern(t *testing.T) {
	cases := []struct {
		in, m, p string
		ok       bool
	}{
		{"/x", "", "/x", true},
		{"GET /x/{id}", "GET", "/x/{id}", true},
		{"post /x", "POST", "/x", true},
		{"example.com/x", "", "/x", true},
		{"GET example.com/", "GET", "/", true},
		{"x", "", "", false},
		{"", "", "", false},
	}
	for _, c := range cases {
		m, p, ok := SplitPattern(c.in)
		if m != c.m || p != c.p || ok != c.ok {
			t.Errorf("SplitPattern(%q) = %q %q %v, want %q %q %v", c.in, m, p, ok, c.m, c.p, c.ok)
		}
	}
}
