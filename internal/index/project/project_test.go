package project

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func write(t *testing.T, root, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestBuild(t *testing.T) {
	root := t.TempDir()
	write(t, root, "go.mod", "module example.com/thing\n\ngo 1.22.3\n\nrequire x v1\n")
	write(t, root, "pyproject.toml", "[build-system]\nrequires = [\"pdm\"]\n\n[project]\nname = \"my-thing\"\nrequires-python = \">=3.10\"\n\n[tool.poetry]\nname = 'ignored'\n")
	write(t, root, "package.json", `{"name": "@acme/thing", "scripts": {"build": "tsc", "lint": "eslint ."}}`)
	write(t, root, "Makefile", `.DEFAULT_GOAL := all
sources = a b
NUM?=1

.PHONY: .uv  ## Check uv
.uv:
	@uv -V

.PHONY: install  ## Install
install: .uv
	uv sync

format lint: .uv
	ruff

%.o: %.c
	cc

$(BIN): main.go
	go build

test:: unit
	go test ./...
docs: ## build docs
	mkdocs
`)
	write(t, root, "justfile", "set shell := [\"bash\"]\n\n# comment\n[private]\n_helper:\n  echo\n\nbuild target='debug':\n  cargo build\n\n@quiet:\n  echo\n")
	write(t, root, "Taskfile.yml", "version: '3'\n\nvars:\n  X: 1\n\ntasks:\n  default:\n    cmds:\n      - echo\n  build:test:\n    desc: x\n    cmds: []\n\nincludes:\n  other: ./x.yml\n")

	p := Build(root)
	if p.GoModule != "example.com/thing" || p.GoVersion != "1.22" {
		t.Errorf("go: %q %q", p.GoModule, p.GoVersion)
	}
	if p.PyName != "my-thing" || p.PyRequires != ">=3.10" {
		t.Errorf("py: %q %q", p.PyName, p.PyRequires)
	}
	if p.NPMName != "@acme/thing" {
		t.Errorf("npm: %q", p.NPMName)
	}
	want := map[string][]string{
		"npm":  {"build", "lint"},
		"make": {".uv", "docs", "format", "install", "lint", "test"},
		"just": {"_helper", "build", "quiet"},
		"task": {"build:test", "default"},
	}
	if !reflect.DeepEqual(p.Targets, want) {
		t.Errorf("targets =\n  %v\nwant\n  %v", p.Targets, want)
	}
	if p.TargetFiles["make"] != "Makefile" || p.TargetFiles["npm"] != "package.json" {
		t.Errorf("target files = %v", p.TargetFiles)
	}
	if !p.HasTarget("make", "docs") || p.HasTarget("make", "clean") || p.HasTarget("cargo", "x") {
		t.Error("HasTarget")
	}
}

func TestBuildEmptyAndPoetry(t *testing.T) {
	root := t.TempDir()
	p := Build(root)
	if p.GoModule != "" || len(p.Targets) != 0 {
		t.Errorf("empty root: %+v", p)
	}
	write(t, root, "pyproject.toml", "[tool.poetry]\nname = \"poet\"\n\n[tool.poetry.dependencies]\npython = \"^3.9\"\n")
	write(t, root, "setup.cfg", "[metadata]\nname = cfgname\n")
	p = Build(root)
	if p.PyName != "poet" || p.PyRequires != "^3.9" {
		t.Errorf("poetry: %q %q", p.PyName, p.PyRequires)
	}
	os.Remove(filepath.Join(root, "pyproject.toml"))
	if p = Build(root); p.PyName != "cfgname" {
		t.Errorf("setup.cfg: %q", p.PyName)
	}
}
