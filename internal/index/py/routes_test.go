package py

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseRoutes(t *testing.T) {
	src := `from fastapi import APIRouter, FastAPI
from starlette.routing import Route, Mount
from django.urls import path

app = FastAPI()
router = APIRouter(prefix="/api")


@app.get("/items/{item_id}")
async def read_item(item_id: int):
    return {}


@router.post(
    "/items",
    response_model=Item,
)
async def create_item():
    return {}


@app.route("/legacy", methods=["GET", "POST"])
def legacy():
    pass


@app.api_route("/multi",
    methods=[
        "PUT",
    ],
)
def multi():
    pass


@app.websocket("/ws")
async def ws():
    pass


app.add_api_route("/added", handler, methods=["DELETE"])
app.add_url_rule('/flask', view_func=v)
app.include_router(other, prefix="/v2")

routes = [
    Route("/", homepage),
    Mount("/static", app=StaticFiles(directory="static")),
]

urlpatterns = [
    path("articles/<int:year>/", views.year),
    path(r"^regex$", views.re),
]

client.get("http://example.com/x")
os.path.join("a", "b")
@app.get(variable)
def not_literal():
    pass
`
	got := parseRoutes(strings.Split(src, "\n"), "app.py")
	var lines []string
	for _, r := range got {
		s := r.Method + " " + r.Path
		if r.Prefix {
			s += " (prefix)"
		}
		lines = append(lines, s)
	}
	want := []string{
		" /api (prefix)",
		"GET /items/{item_id}",
		"POST /items",
		"GET /legacy", "POST /legacy",
		"PUT /multi",
		" /ws",
		"DELETE /added",
		" /flask",
		" /v2 (prefix)",
		" /",
		" /static (prefix)",
		" articles/<int:year>/",
	}
	if !reflect.DeepEqual(lines, want) {
		t.Errorf("routes =\n  %q\nwant\n  %q", lines, want)
	}
	if got[1].Line != 9 {
		t.Errorf("read_item route at line %d, want 9", got[1].Line)
	}
}
