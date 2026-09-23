package extract

import (
	"testing"

	"docrot/internal/model"
)

func TestRouteSpans(t *testing.T) {
	h := goHints()
	h.routes = true
	md := "# API\n\n" +
		"- `GET /v1/items` lists items; `POST /v1/items/{id}` updates one.\n" +
		"- `/healthz` and `/readyz` are probes; `/items/<int:id>` is Django style.\n" +
		"- Not routes: `/usr/local/bin`, `/etc/hosts`, `/tmp/x`, `/`, `/cmd/app/main.go`, `/c/Users/me`.\n" +
		"- Prose: the server answers GET /v1/items and DELETE /v1/items/{id}?force=1 too.\n" +
		"- Table row below.\n\n" +
		"| Method | Path | Notes |\n|---|---|---|\n| PUT | `/v1/items/{id}` | replace |\n| PATCH | /v1/items/{id} | partial |\n\n" +
		"```http\nGET /v1/items HTTP/1.1\nHost: localhost\n```\n\n" +
		"```sh\ncurl -X POST http://localhost:8080/v1/items -d '{}'\ncurl localhost:8080/healthz\ncurl https://api.github.com/repos\n```\n"
	refs := run(t, h, md)
	want := map[string]model.Confidence{
		"GET /v1/items":         model.High,
		"POST /v1/items/{id}":   model.High,
		"/healthz":              model.Medium, // the curl example below,
		"/readyz":               model.Low,
		"/items/<int:id>":       model.Low,
		"DELETE /v1/items/{id}": model.Medium,
		"PUT /v1/items/{id}":    model.High,
		"PATCH /v1/items/{id}":  model.High,
	}
	got := map[string]model.Confidence{}
	for _, r := range refs {
		if r.Kind == model.KindRoute {
			if c, ok := got[r.Norm]; !ok || r.Confidence > c {
				got[r.Norm] = r.Confidence
			}
		}
	}
	for norm, conf := range want {
		if got[norm] != conf {
			t.Errorf("route %q: confidence %v, want %v", norm, got[norm], conf)
		}
	}
	for _, bad := range []string{"/usr/local/bin", "/etc/hosts", "/tmp/x", "/", "/cmd/app/main.go", "/c/Users/me", "/repos"} {
		if _, ok := got[bad]; ok {
			t.Errorf("%q should not be a route claim", bad)
		}
	}
	// http and shell fences
	var fenceRoutes []string
	for _, r := range refs {
		if r.Kind == model.KindRoute && r.Lang != "" {
			fenceRoutes = append(fenceRoutes, r.Lang+":"+r.Norm)
		}
	}
	wantFence := []string{"http:GET /v1/items", "sh:POST /v1/items", "sh:/healthz"}
	if len(fenceRoutes) != len(wantFence) {
		t.Fatalf("fence routes = %v, want %v", fenceRoutes, wantFence)
	}
	for i := range wantFence {
		if fenceRoutes[i] != wantFence[i] {
			t.Errorf("fence route %d = %q, want %q", i, fenceRoutes[i], wantFence[i])
		}
	}
	// the prose "GET /v1/items" on line 6 must not be reported twice
	seen := 0
	for _, r := range refs {
		if r.Kind == model.KindRoute && r.Norm == "GET /v1/items" && r.Loc.Line == 3 {
			seen++
		}
	}
	if seen != 1 {
		t.Errorf("GET /v1/items on line 3 extracted %d times", seen)
	}
}

func TestRouteSpansWithoutRoutes(t *testing.T) {
	// a repo that registers no routes: "/x" stays an absolute path, unchecked
	refs := run(t, goHints(), "See `/healthz` and `GET /v1/items`.\n")
	for _, r := range refs {
		if r.Kind == model.KindRoute {
			t.Errorf("unexpected route reference %q", r.Norm)
		}
	}
}
