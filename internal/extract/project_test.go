package extract

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"docrot/internal/model"
)

func TestProjectRefs(t *testing.T) {
	cases := map[string][]string{
		"go get example.com/x/y@v1.2.0":            {"install|go:example.com/x/y"},
		"go install example.com/x/cmd/tool@latest": {"install|go:example.com/x/cmd/tool"},
		"go install ./cmd/tool":                    nil,
		"go get -u example.com/x/...":              {"install|go:example.com/x"},
		"pip install httpx[cli]==0.27":             {"install|pip:httpx"},
		"pip install 'httpx[http2]' requests":      {"install|pip:httpx", "install|pip:requests"},
		"pip install -r requirements.txt -e .":     nil,
		"python -m pip install -U pydantic":        nil, // python is not an installer token
		"uv pip install --upgrade starlette":       {"install|pip:starlette"},
		"uv add typer":                             {"install|pip:typer"},
		"poetry add fastapi":                       {"install|pip:fastapi"},
		"pipx install docrot":                      {"install|pip:docrot"},
		"npm install @acme/thing@2 --save-dev":     {"install|npm:@acme/thing"},
		"npm i lodash":                             {"install|npm:lodash"},
		"yarn add react":                           {"install|npm:react"},
		"make build test":                          {"target|make:build", "target|make:test"},
		"make -j4 lint VAR=1":                      {"target|make:lint"},
		"make build-dev # during development":      {"target|make:build-dev"},
		"make":                                     nil,
		"npm run lint -- --fix":                    {"target|npm:lint"},
		"npm run":                                  nil,
		"just release":                             {"target|just:release"},
		"task build:test":                          {"target|task:build:test"},
		"pnpm run dev":                             {"target|npm:dev"},
		"go build ./...":                           nil,
	}
	for in, want := range cases {
		var got []string
		for _, r := range projectRefs(strings.Fields(in), model.High) {
			got = append(got, string(r.Kind)+"|"+r.Norm)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%q → %v, want %v", in, got, want)
		}
	}
}

func TestProjectClaimsInDocs(t *testing.T) {
	md := "# X\n\nHTTPX requires Python 3.9+ and Go 1.21 or later; docs mention Go 1.22 wildcards\nand Python 3.12 features without requiring them. Needs golang >= 1.20. Since go ≥1.14 vendoring is automatic. Python 3.8 is no longer supported.\n\n" +
		"Run `make build` or `npm run lint`, then `pip install httpx`.\n\n" +
		"```sh\n$ go get example.com/fixture/pkg/httpx\nmake test\n```\n"
	refs := run(t, goHints(), md)
	var got []string
	for _, r := range refs {
		switch r.Kind {
		case model.KindInstall, model.KindTarget, model.KindToolchain:
			got = append(got, string(r.Kind)+"|"+r.Norm+"|"+r.Confidence.String())
		}
	}
	sort.Strings(got)
	want := []string{
		"install|go:example.com/fixture/pkg/httpx|high",
		"install|pip:httpx|medium",
		"target|make:build|medium",
		"target|make:test|high",
		"target|npm:lint|medium",
		"toolchain|go:1.20|medium",
		"toolchain|go:1.21|medium",
		"toolchain|python:3.9|medium",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("claims =\n  %v\nwant\n  %v", got, want)
	}
}
