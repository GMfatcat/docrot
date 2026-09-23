package gosym

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestFlagAndTagDefaults(t *testing.T) {
	root := t.TempDir()
	src := `package main

import (
	"flag"
	"time"
)

type Config struct {
	Level string ` + "`" + `json:"level" default:"info"` + "`" + `
	Nested struct {
		Path string ` + "`" + `json:"path" default:"/docs/"` + "`" + `
	} ` + "`" + `json:"nested"` + "`" + `
}

func main() {
	flag.String("addr", ":8080", "")
	flag.Int("port", 9090, "")
	flag.Bool("verbose", false, "")
	flag.Duration("timeout", 30*time.Second, "")
	var n int
	flag.IntVar(&n, "workers", -1, "")
	flag.Float64("ratio", 0.5, "")
	flag.Func("mode", "", nil)
	flag.String("computed", defaultAddr(), "")
}

func defaultAddr() string { return "" }
`
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	ix, errs := Build(root, Options{})
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	want := []string{
		"flag:addr=:8080", "flag:port=9090", "flag:ratio=0.5", "flag:timeout=30s", "flag:verbose=false", "flag:workers=-1",
		"key:level=info", "key:nested.path=/docs/",
	}
	if got := ix.Defaults().List(); !reflect.DeepEqual(got, want) {
		t.Errorf("defaults = %v, want %v", got, want)
	}
}
