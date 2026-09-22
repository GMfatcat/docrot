package coverage

import (
	"reflect"
	"testing"

	"docrot/internal/model"
)

// sample is the exported surface used by most tests.
var sample = []model.Exported{
	{Package: "httpx", Qualified: "httpx.WriteData", Kind: model.KindGoSymbol, File: "internal/httpx/write.go", Line: 10},
	{Package: "httpx", Qualified: "httpx.Server.Start", Kind: model.KindGoSymbol, File: "internal/httpx/server.go", Line: 20},
	{Package: "httpx", Qualified: "httpx.Undocumented", Kind: model.KindGoSymbol, File: "internal/httpx/x.go", Line: 30},
	{Package: "logx", Qualified: "logx.New", Kind: model.KindGoSymbol, File: "internal/logx/logx.go", Line: 5},
	{Qualified: "--addr", Kind: model.KindFlag},
	{Qualified: "--log_level", Kind: model.KindFlag},
	{Qualified: "PORT", Kind: model.KindEnv},
	{Qualified: "SECRET", Kind: model.KindEnv},
}

func TestCompute(t *testing.T) {
	tests := []struct {
		name      string
		exported  []model.Exported
		mentioned map[string]bool
		want      Result
	}{
		{
			name:      "nothing exported",
			exported:  nil,
			mentioned: map[string]bool{},
			want: Result{
				Flags: Group{Name: FlagsGroup},
				Envs:  Group{Name: EnvGroup},
			},
		},
		{
			name:      "nothing mentioned",
			exported:  sample,
			mentioned: map[string]bool{},
			want: Result{
				Packages: []Group{
					{Name: "httpx", Total: 3, Missing: []string{"httpx.Server.Start", "httpx.Undocumented", "httpx.WriteData"}},
					{Name: "logx", Total: 1, Missing: []string{"logx.New"}},
				},
				Flags: Group{Name: FlagsGroup, Total: 2, Missing: []string{"--addr", "--log_level"}},
				Envs:  Group{Name: EnvGroup, Total: 2, Missing: []string{"PORT", "SECRET"}},
			},
		},
		{
			name:     "qualified, unqualified, flag folding",
			exported: sample,
			mentioned: map[string]bool{
				"gosym|httpx.WriteData": true, // qualified
				"gosym|Server.Start":    true, // package-less method
				"gosym|New":             true, // package-less name
				"flag|Addr":             true, // case folded
				"flag|log-level":        true, // '_' folded to '-'
				"env|PORT":              true,
			},
			want: Result{
				Packages: []Group{
					{Name: "httpx", Total: 3, Documented: 2, Missing: []string{"httpx.Undocumented"}},
					{Name: "logx", Total: 1, Documented: 1},
				},
				Flags: Group{Name: FlagsGroup, Total: 2, Documented: 2},
				Envs:  Group{Name: EnvGroup, Total: 2, Documented: 1, Missing: []string{"SECRET"}},
			},
		},
		{
			name:     "env is case sensitive and unrelated norms do not count",
			exported: sample,
			mentioned: map[string]bool{
				"env|port":              true,
				"gosym|httpx.WriteData": false,
				"flag|verbose":          true,
			},
			want: Result{
				Packages: []Group{
					{Name: "httpx", Total: 3, Missing: []string{"httpx.Server.Start", "httpx.Undocumented", "httpx.WriteData"}},
					{Name: "logx", Total: 1, Missing: []string{"logx.New"}},
				},
				Flags: Group{Name: FlagsGroup, Total: 2, Missing: []string{"--addr", "--log_level"}},
				Envs:  Group{Name: EnvGroup, Total: 2, Missing: []string{"PORT", "SECRET"}},
			},
		},
		{
			name: "symbol without package keeps its own name",
			exported: []model.Exported{
				{Qualified: "Handler.ServeHTTP", Kind: model.KindGoSymbol, File: "h.go", Line: 1},
			},
			mentioned: map[string]bool{"gosym|ServeHTTP": true},
			want: Result{
				Packages: []Group{{Name: "", Total: 1, Documented: 1}},
				Flags:    Group{Name: FlagsGroup},
				Envs:     Group{Name: EnvGroup},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Compute(tt.exported, tt.mentioned)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Compute() = %+v\nwant %+v", got, tt.want)
			}
		})
	}
}

func TestGroupPercent(t *testing.T) {
	tests := []struct {
		name string
		g    Group
		want int
	}{
		{"empty", Group{}, 100},
		{"all", Group{Total: 4, Documented: 4}, 100},
		{"none", Group{Total: 4}, 0},
		{"half", Group{Total: 2, Documented: 1}, 50},
		{"rounds down", Group{Total: 3, Documented: 1}, 33},
		{"rounds up", Group{Total: 3, Documented: 2}, 67},
		{"rounds half away from zero", Group{Total: 8, Documented: 3}, 38},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.g.Percent(); got != tt.want {
				t.Errorf("Percent() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestFindings(t *testing.T) {
	r := Compute(sample, map[string]bool{
		"gosym|httpx.WriteData": true,
		"gosym|Server.Start":    true,
		"flag|addr":             true,
		"env|PORT":              true,
	})
	got := Findings(r, sample, model.SevInfo)

	want := []model.Finding{
		{
			Rule:     model.RuleUndocumented,
			Severity: model.SevInfo,
			Message:  "exported symbol httpx.Undocumented is not mentioned in any document",
			Loc:      model.Location{File: "internal/httpx/x.go", Line: 30},
			Fingerprint: model.Fingerprint(model.RuleUndocumented,
				string(model.KindGoSymbol), "httpx.Undocumented"),
		},
		{
			Rule:     model.RuleUndocumented,
			Severity: model.SevInfo,
			Message:  "exported symbol logx.New is not mentioned in any document",
			Loc:      model.Location{File: "internal/logx/logx.go", Line: 5},
			Fingerprint: model.Fingerprint(model.RuleUndocumented,
				string(model.KindGoSymbol), "logx.New"),
		},
		{
			Rule:        model.RuleUndocumented,
			Severity:    model.SevInfo,
			Message:     "flag --log_level is not mentioned in any document",
			Loc:         model.Location{},
			Fingerprint: model.Fingerprint(model.RuleUndocumented, string(model.KindFlag), "--log_level"),
		},
		{
			Rule:        model.RuleUndocumented,
			Severity:    model.SevInfo,
			Message:     "environment variable SECRET is not mentioned in any document",
			Loc:         model.Location{},
			Fingerprint: model.Fingerprint(model.RuleUndocumented, string(model.KindEnv), "SECRET"),
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Findings() = %+v\nwant %+v", got, want)
	}
}

func TestFindingsEmpty(t *testing.T) {
	r := Compute(nil, nil)
	if got := Findings(r, nil, model.SevInfo); got != nil {
		t.Errorf("Findings() = %v, want nil", got)
	}
}
