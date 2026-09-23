package coverage

import (
	"reflect"
	"testing"

	"docrot/internal/model"
)

func TestRoutes(t *testing.T) {
	listed := []string{
		"GET /items", "POST /items", "GET /items/{}", "DELETE /items/{}",
		"/healthz", "/readyz", "/api/**", "/admin/**",
	}
	documented := map[string]bool{
		"GET /items":     true, // mentioned with its method: POST /items stays undocumented
		"/items/{}":      true, // mentioned without a method: covers GET and DELETE
		"PUT /healthz":   true, // any method covers a method-less route
		"GET /api/users": true, // covers the /api prefix
	}
	got := Routes(listed, documented)
	want := Group{Name: RoutesGroup, Total: 8, Documented: 5, Missing: []string{"/admin/**", "/readyz", "POST /items"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Routes = %+v, want %+v", got, want)
	}
	// a router mounted under a prefix: the document names the full path
	got = Routes([]string{"POST /items/{}"}, map[string]bool{"POST /py/items/{}": true})
	want = Group{Name: RoutesGroup, Total: 1, Documented: 1}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Routes = %+v, want %+v", got, want)
	}
	if g := Routes(nil, nil); g.Total != 0 || g.Percent() != 100 {
		t.Errorf("empty = %+v", g)
	}
}

func TestConfigs(t *testing.T) {
	keys := []string{"server", "server.addr", "server.timeout_ms", "log", "log.level", "retention", "retention.days", "Debug"}
	mentioned := map[string]bool{"server.addr": true, "log.level": true, "debug": true}
	got := Configs(keys, mentioned)
	want := Group{Name: ConfigGroup, Total: 5, Documented: 3, Missing: []string{"retention.days", "server.timeout_ms"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Configs = %+v, want %+v", got, want)
	}
}

func TestFindingsRoutesAndConfigs(t *testing.T) {
	res := Result{
		Routes:  Group{Name: RoutesGroup, Total: 1, Missing: []string{"POST /items"}},
		Configs: Group{Name: ConfigGroup, Total: 1, Missing: []string{"retention.days"}},
		Locations: map[string]model.Location{
			"POST /items":           {File: "server.go", Line: 12},
			"config|retention.days": {File: "config.json"},
		},
	}
	got := Findings(res, nil, model.SevInfo)
	if len(got) != 2 {
		t.Fatalf("got %d findings, want 2: %+v", len(got), got)
	}
	if got[0].Message != "route POST /items is not mentioned in any document" || got[0].Loc.File != "server.go" || got[0].Loc.Line != 12 {
		t.Errorf("route finding = %+v", got[0])
	}
	if got[1].Message != "config key retention.days is not mentioned in any document" || got[1].Loc.File != "config.json" {
		t.Errorf("config finding = %+v", got[1])
	}
	if got[0].Fingerprint == got[1].Fingerprint {
		t.Error("fingerprints collide")
	}
}
