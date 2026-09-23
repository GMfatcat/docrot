package csharp

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

const catalogCS = `using System;
using System.Collections.Generic;

namespace Fixture.Api.Services;

/// <summary>
/// Catalog lists the items.
/// </summary>
[Serializable]
public sealed class Catalog
{
    private readonly List<Item> _items = new();
    public const int MaxItems = 100;
    public event EventHandler? Changed;

    public Catalog(string name) { Name = name; }

    /// <summary>Name of the catalog.</summary>
    public string Name { get; init; }

    public int Count => _items.Count;

    /// <summary>ListAll returns every item.</summary>
    /// <param name="limit">how many</param>
    public IEnumerable<Item> ListAll(int limit = 10, CancellationToken token = default)
    {
        if (limit < 0) { throw new ArgumentException("x"); }
        var local = new Helper();
        return _items;
    }

    internal void Hidden() { }

    public static Catalog Empty() => new("empty");

    public class Nested
    {
        public void Inner() { }
    }
}

public interface IStore
{
    Item Get(string id);
    int Size { get; }
}

public enum Mode { Fast, Slow = 2 }

public enum Level
{
    Low,
    High = 5,
}

public record Item(string Id, string Title);

public delegate void Handler(object sender, string text);

internal static class Helpers
{
    public static string Slug(string s) => s.ToLower();
}

public abstract class Base : IStore
{
    public abstract Item Get(string id);
    public int Size { get; } = 0;
}
`

const programCS = `using Microsoft.AspNetCore.Builder;

var builder = WebApplication.CreateBuilder(args);
var app = builder.Build();

var group = app.MapGroup("/cs");
group.MapGet("/items", () => Results.Ok());
group.MapPost("/items", () => Results.Ok());
app.MapDelete("/admin/{id}", (int id) => Results.Ok());
app.Map("/", () => Results.Redirect("/docs"));
app.MapMethods("/ping", new[] { "GET", "HEAD" }, () => "pong");

var region = Environment.GetEnvironmentVariable("FIXTURE_REGION") ?? "eu";
var endpoint = builder.Configuration["OTEL_EXPORTER_OTLP_ENDPOINT"];
var shards = new Option<int>("--shards", getDefaultValue: () => 2, description: "shards");
var verbose = new Option<bool>(name: "--verbose");
var alias = new Option<string>(["--name", "-n"]);
app.Run();
`

const controllerCS = `namespace Fixture.Api.Controllers
{
    [ApiController]
    [Route("api/[controller]")]
    public class TodosController : ControllerBase
    {
        [HttpGet("{id:int}")]
        public IActionResult Get(int id) => Ok();

        [HttpPost]
        public IActionResult Create() => Ok();
    }
}
`

func buildTest(t *testing.T) *Index {
	t.Helper()
	root := writeTree(t, map[string]string{
		"src/Fixture.Api/Services/Catalog.cs":            catalogCS,
		"src/Fixture.Api/Program.cs":                     programCS,
		"src/Fixture.Api/Controllers/TodosController.cs": controllerCS,
		"tests/Fixture.Tests/CatalogTests.cs":            "namespace Fixture.Tests;\npublic class CatalogTests\n{\n    public void Runs() { }\n}\n",
		"src/Fixture.Api/obj/Debug/Gen.cs":               "namespace Gen; public class Built { }\n",
		"src/Fixture.Api/Form.Designer.cs":               "namespace Fixture.Api; public class Designer { }\n",
	})
	ix, err := Build(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	return ix
}

func TestBuildAndLookup(t *testing.T) {
	ix := buildTest(t)
	if st := ix.Stats(); st.Files != 4 {
		t.Errorf("stats = %+v", st)
	}
	want := []string{"Fixture", "Fixture.Api", "Fixture.Api.Controllers", "Fixture.Api.Services", "Fixture.Tests"}
	if got := ix.Namespaces(); !reflect.DeepEqual(got, want) {
		t.Errorf("Namespaces() = %v, want %v", got, want)
	}
	for _, q := range []string{
		"Fixture.Api.Services.Catalog", "Catalog", "Catalog.ListAll", "Catalog.ListAll()", "Fixture.Api.Services.Catalog.ListAll",
		"Catalog.Name", "Catalog.Count", "Catalog.MaxItems", "Catalog.Changed", "Catalog.Hidden", "Catalog.Empty", "Catalog.Nested", "Nested.Inner", "Catalog.Nested.Inner",
		"IStore", "IStore.Get", "IStore.Size", "Mode.Fast", "Mode.Slow", "Level.High", "Item", "Handler", "Helpers.Slug", "Base.Get",
		"ListAll()", "Empty()", "Fixture.Api.Services", "Fixture", "Fixture.Api",
		"Fixture.Catalog", "Fixture.Api.Catalog",
		"TodosController", "TodosController.Get", "CatalogTests.Runs",
	} {
		if !ix.Has(q) {
			t.Errorf("Has(%q) = false", q)
		}
	}
	for _, q := range []string{"local", "Helper", "Built", "Designer", "Fixture.Api.Services.Nope", "Catalog.Nope", "Gen.Built"} {
		if ix.Has(q) {
			t.Errorf("Has(%q) = true", q)
		}
	}
	if f, line, ok := ix.File("Catalog.ListAll"); !ok || f != "src/Fixture.Api/Services/Catalog.cs" || line != 25 {
		t.Errorf("File(Catalog.ListAll) = %q, %d, %v", f, line, ok)
	}
	if !ix.IsNamespace("Fixture.Api") || ix.IsNamespace("Catalog") {
		t.Error("IsNamespace")
	}
	if !ix.IsExample("Fixture.Tests") || ix.IsExample("Fixture.Api.Services") {
		t.Error("IsExample")
	}
	if ix.Opaque("Catalog") || !ix.Opaque("Base") || !ix.Opaque("TodosController") || ix.Opaque("Mode") {
		t.Error("Opaque: a type without a base lists its members; a derived type does not")
	}
}

func TestSpans(t *testing.T) {
	ix := buildTest(t)
	sp, ok := ix.Span("Catalog.ListAll")
	if !ok {
		t.Fatal("Span not found")
	}
	if sp.DeclLine != 25 || sp.BodyEnd != 30 || sp.DocStart != 23 || sp.DocEnd != 24 || !sp.Exported {
		t.Errorf("span = %+v", sp)
	}
	if !reflect.DeepEqual(sp.Doc, []string{"ListAll returns every item.", "how many"}) || !reflect.DeepEqual(sp.Params, []string{"limit", "token"}) {
		t.Errorf("doc/params = %v %v", sp.Doc, sp.Params)
	}
	sp, _ = ix.Span("Catalog")
	if sp.DocStart != 6 || sp.DocEnd != 8 || sp.DeclLine != 10 || sp.BodyEnd != 40 || !reflect.DeepEqual(sp.Doc, []string{"Catalog lists the items."}) {
		t.Errorf("class span = %+v (an attribute line sits between the doc and the declaration)", sp)
	}
	sp, _ = ix.Span("Catalog.Count")
	if sp.BodyEnd != 21 {
		t.Errorf("expression-bodied property span = %+v", sp)
	}
	if sp, _ := ix.Span("Catalog.Hidden"); sp.Exported {
		t.Error("an internal method is not exported")
	}
	if sp, _ := ix.Span("IStore.Get"); !sp.Exported {
		t.Error("interface members are public")
	}
	var names []string
	for _, s := range ix.AllSpans() {
		names = append(names, s.Qualified)
	}
	want := []string{
		"Fixture.Api.Controllers.TodosController", "Fixture.Api.Controllers.TodosController.Create", "Fixture.Api.Controllers.TodosController.Get",
		"Fixture.Api.Services.Base", "Fixture.Api.Services.Base.Get", "Fixture.Api.Services.Base.Size",
		"Fixture.Api.Services.Catalog", "Fixture.Api.Services.Catalog.Changed", "Fixture.Api.Services.Catalog.Count", "Fixture.Api.Services.Catalog.Empty",
		"Fixture.Api.Services.Catalog.ListAll", "Fixture.Api.Services.Catalog.MaxItems", "Fixture.Api.Services.Catalog.Name", "Fixture.Api.Services.Catalog.Nested", "Fixture.Api.Services.Catalog.Nested.Inner",
		"Fixture.Api.Services.IStore", "Fixture.Api.Services.IStore.Get", "Fixture.Api.Services.IStore.Size",
		"Fixture.Api.Services.Item", "Fixture.Api.Services.Level", "Fixture.Api.Services.Mode",
	}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("AllSpans() =\n %v\nwant\n %v", names, want)
	}
}

func TestSimilarFlagsEnvsLiterals(t *testing.T) {
	ix := buildTest(t)
	if got := ix.Similar("Catalog.ListAl", 2); len(got) == 0 || got[0] != "Fixture.Api.Services.Catalog.ListAll" {
		t.Errorf("Similar = %v", got)
	}
	if got := ix.Flags(); !reflect.DeepEqual(got, []string{"name", "shards", "verbose"}) {
		t.Errorf("Flags() = %v", got)
	}
	if got := ix.Envs(); !reflect.DeepEqual(got, []string{"FIXTURE_REGION", "OTEL_EXPORTER_OTLP_ENDPOINT"}) {
		t.Errorf("Envs() = %v", got)
	}
	if v, ok := ix.Defaults().Get("flag", "shards"); !ok || v != "2" {
		t.Errorf("shards default = %q, %v", v, ok)
	}
	if v, ok := ix.Defaults().Get("env", "FIXTURE_REGION"); !ok || v != "eu" {
		t.Errorf("env default = %q, %v", v, ok)
	}
	if !ix.Literals().Has("/cs") || !ix.Literals().Has("FIXTURE_REGION") {
		t.Error("literals")
	}
	if syms := ix.Symbols(); len(syms) == 0 || syms[0] != "Fixture.Api.Controllers.TodosController" {
		t.Errorf("Symbols() = %v", syms[:3])
	}
}

func TestRoutes(t *testing.T) {
	ix := buildTest(t)
	var got []string
	for _, r := range ix.Routes() {
		s := r.Method + " " + r.Path
		if r.Prefix {
			s += " (prefix)"
		}
		got = append(got, s)
	}
	want := []string{
		"GET /api/todos/{id:int}", "POST /api/todos",
		" /cs (prefix)", "GET /cs/items", "POST /cs/items", "DELETE /admin/{id}", " /", "GET /ping", "HEAD /ping",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("routes =\n %q\nwant\n %q", got, want)
	}
}

func TestMaskLine(t *testing.T) {
	masked := maskAll([]string{
		`var s = "a { b"; // { c`,
		`var v = @"multi {`,
		`line "" x"; var c = '{'; /* { */ f(`,
		`var r = """`,
		`raw { text`,
		`"""; g();`,
	})
	want := []string{
		`var s = "     ";       `,
		`var v = @"       `,
		`         "; var c = ' ';         f(`,
		`var r =    `,
		`          `,
		`   ; g();`,
	}
	if !reflect.DeepEqual(masked, want) {
		t.Errorf("maskAll =\n %q\nwant\n %q", masked, want)
	}
}
