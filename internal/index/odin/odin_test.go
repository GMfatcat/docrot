package odin

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

const mainOdin = `package main

import "core:fmt"

g_state: State
count := 0

Vec2 :: [2]f32
Handle :: distinct u32
MAX_ITEMS :: 42

State :: struct {
	x: int,
	y: int,
}

Color :: enum {
	Red,
	Green,
	Blue,
}

add :: proc(a, b: int) -> int {
	return a + b
}

@(private)
helper :: proc() {
	fmt.println("hi")
}

@(export)
force_add :: #force_inline proc(a, b: i32) -> i32 {
	return a + b
}

native_call :: proc "c" (x: i32) {
}
`

func buildFixture(t *testing.T) *Index {
	t.Helper()
	root := t.TempDir()
	writeFile(t, root, "main.odin", mainOdin)
	writeFile(t, root, "util/helpers.odin", `package util

subtract :: proc(a, b: int) -> int {
	return a - b
}
`)
	ix, err := Build(root, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return ix
}

func TestBuildEmpty(t *testing.T) {
	root := t.TempDir()
	ix, err := Build(root, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !ix.Empty() {
		t.Fatalf("expected Empty() on dir with no .odin files")
	}
}

func TestPackagesAndStats(t *testing.T) {
	ix := buildFixture(t)
	if ix.Empty() {
		t.Fatalf("expected non-empty index")
	}
	pkgs := ix.Packages()
	want := []string{"main", "util"}
	if len(pkgs) != len(want) || pkgs[0] != want[0] || pkgs[1] != want[1] {
		t.Fatalf("Packages() = %v, want %v", pkgs, want)
	}
	stats := ix.Stats()
	if stats.Files != 2 {
		t.Fatalf("Stats.Files = %d, want 2", stats.Files)
	}
	if stats.Packages != 2 {
		t.Fatalf("Stats.Packages = %d, want 2", stats.Packages)
	}
	if stats.Symbols == 0 {
		t.Fatalf("Stats.Symbols = 0, want > 0")
	}
}

func TestHas(t *testing.T) {
	ix := buildFixture(t)
	cases := []struct {
		q    string
		want bool
	}{
		{"main.add", true},
		{"add", true},
		{"add()", true},
		{"add(a, b)", true},
		{"main.helper", true},
		{"main.force_add", true},
		{"main.native_call", true},
		{"main.g_state", true},
		{"main.count", true},
		{"main.Vec2", true},
		{"main.Handle", true},
		{"main.MAX_ITEMS", true},
		{"main.State", true},
		{"main.Color", true},
		{"util.subtract", true},
		{"subtract", true},
		{"main.subtract", false},
		{"main.nope", false},
		{"nope", false},
	}
	for _, c := range cases {
		if got := ix.Has(c.q); got != c.want {
			t.Errorf("Has(%q) = %v, want %v", c.q, got, c.want)
		}
	}
}

func TestFile(t *testing.T) {
	ix := buildFixture(t)
	file, line, ok := ix.File("main.add")
	if !ok {
		t.Fatalf("File(main.add) not found")
	}
	if file != "main.odin" {
		t.Errorf("File() file = %q, want main.odin", file)
	}
	if line <= 0 {
		t.Errorf("File() line = %d, want > 0", line)
	}

	if _, _, ok := ix.File("main.nope"); ok {
		t.Errorf("File(main.nope) found, want not found")
	}
}

func TestProcNames(t *testing.T) {
	ix := buildFixture(t)
	procs := ix.ProcNames()
	want := map[string]bool{
		"add": true, "helper": true, "force_add": true,
		"native_call": true, "subtract": true,
	}
	if len(procs) != len(want) {
		t.Fatalf("ProcNames() = %v, want %d entries", procs, len(want))
	}
	for _, p := range procs {
		if !want[p] {
			t.Errorf("ProcNames() has unexpected %q", p)
		}
	}
	// non-proc declarations must not appear
	for _, notProc := range []string{"State", "Color", "Vec2", "Handle", "MAX_ITEMS", "g_state", "count"} {
		for _, p := range procs {
			if p == notProc {
				t.Errorf("ProcNames() unexpectedly contains %q", notProc)
			}
		}
	}
}

func TestSimilar(t *testing.T) {
	ix := buildFixture(t)
	got := ix.Similar("main.ad", 3)
	found := false
	for _, g := range got {
		if g == "main.add" {
			found = true
		}
	}
	if !found {
		t.Errorf("Similar(main.ad) = %v, want to contain main.add", got)
	}

	got = ix.Similar("subtrct", 3)
	found = false
	for _, g := range got {
		if g == "util.subtract" {
			found = true
		}
	}
	if !found {
		t.Errorf("Similar(subtrct) = %v, want to contain util.subtract", got)
	}

	if got := ix.Similar("totally-unrelated-xyz", 3); len(got) != 0 {
		t.Errorf("Similar(totally-unrelated-xyz) = %v, want empty", got)
	}
}

func TestExclude(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "keep.odin", "package main\nfoo :: proc() {}\n")
	writeFile(t, root, "skip/skip.odin", "package skip\nbar :: proc() {}\n")
	ix, err := Build(root, []string{"skip"})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !ix.Has("foo") {
		t.Errorf("expected foo to be indexed")
	}
	if ix.Has("bar") {
		t.Errorf("expected bar to be excluded")
	}
}

func TestFallbackPackageName(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "nopkg/thing.odin", "lonely :: proc() {}\n")
	ix, err := Build(root, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !ix.Has("nopkg.lonely") {
		t.Errorf("expected fallback package name from directory 'nopkg'")
	}
}
