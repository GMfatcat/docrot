package gosym

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestRoutes(t *testing.T) {
	root := t.TempDir()
	src := `package main

import "net/http"

const itemsPath = "/const/items"

func routes(mux *http.ServeMux, r router) {
	mux.HandleFunc("/healthz", nil)
	mux.HandleFunc("GET /v1/items/{id}", nil)
	mux.Handle("POST /v1/items", nil)
	http.HandleFunc("example.com/host", nil)
	mux.HandleFunc(itemsPath, nil) // constant: left to the literal index
	r.Get("/chi", nil)
	r.Post("/chi/{id}", nil)
	r.Method("DELETE", "/chi/{id}", nil)
	r.Handle("PUT", "/gin", nil)
	r.Mount("/api", nil)
	r.Group("/admin")
	r.Route("/v2", nil)
	cache.Get("key")
	http.Get("https://example.com/x")
	m.Get("a/b")
	viper.Get("server.port")
}

type router interface{}
`
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	ix, errs := Build(root, Options{})
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	var got []string
	for _, r := range ix.Routes() {
		s := r.Method + " " + r.Path
		if r.Prefix {
			s += " (prefix)"
		}
		got = append(got, s)
	}
	want := []string{
		" /healthz", "GET /v1/items/{id}", "POST /v1/items", " /host",
		"GET /chi", "POST /chi/{id}", "DELETE /chi/{id}", "PUT /gin",
		" /api (prefix)", " /admin (prefix)", " /v2 (prefix)",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("routes =\n  %q\nwant\n  %q", got, want)
	}
	if ix.Routes()[0].File != "main.go" || ix.Routes()[0].Line != 8 {
		t.Errorf("first route at %s:%d", ix.Routes()[0].File, ix.Routes()[0].Line)
	}
}
