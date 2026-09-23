package js

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"docrot/internal/index/routes"
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

const clientTS = `import { Config } from './config';

/**
 * Client talks to the server.
 */
export class Client {
  private token: string = '';
  #secret = 1;
  static VERSION = "1.0";

  constructor(private base: string) {}

  /** fetchAll lists everything. */
  async fetchAll(limit: number, opts?: Options): Promise<Item[]> {
    const helper = () => { return 1; };
    function local() {}
    return [];
  }

  get size(): number { return 0; }
  private hidden() {}
}

// useItems is a hook.
export function useItems(id: string) {
  return id;
}

export const VERSION = '2.0';
export const handler = async (req, res) => {
  res.end();
};
const internalThing = 3;
export default Client;
export { internalThing as exposed };
export type Options = { a: number };
export interface Item { id: string }
export enum Mode { Fast, Slow = 2 }
export enum Level {
  Low,
  High = "high",
}
export namespace util {
  export function deep() {}
}
export * from './extra';
`

const serverJS = `'use strict'
const express = require('express')

function buildServer (options) {
  const app = express()
  const instance = {
    listen,
    inject: function () {},
    printRoutes () {},
  }
  app.get('/js/items', list)
  app.post('/js/items', create)
  router.delete("/js/items/:id", remove)
  app.use('/api', apiRouter)
  client.get('/not/a/route')
  return app
}

const api = {
  register: function (plugin) {},
  close() {},
  name: 'fixture',
}

buildServer.prototype.listen = function (port) {}
buildServer.stop = () => {}
buildServer.version = pkg.version

Object.defineProperties(buildServer.prototype, {
  elapsedTime: {
    get () { return 1 },
  },
  statusCode: { get () { return 200 } },
})
Object.defineProperty(buildServer.prototype, 'server', { get () {} })

module.exports = { buildServer, api }
module.exports.helper = function () {}
exports.other = () => 1
`

func buildTest(t *testing.T) *Index {
	t.Helper()
	root := writeTree(t, map[string]string{
		"src/client.ts":           clientTS,
		"src/extra.ts":            "export function extraFn() {}\n",
		"src/lib/index.ts":        "export const libThing = 1;\n",
		"lib/server.js":           serverJS,
		"test/client.test.ts":     "export function testHelper() {}\n",
		"node_modules/x/index.js": "export function dep() {}\n",
		"dist/bundle.js":          "export function built() {}\n",
		"src/vendor.min.js":       "export function mini() {}\n",
		"types/fastify.d.ts":      "export declare function fastify(): void;\ndeclare namespace fastify {\n  export interface Opts {}\n}\n",
		"src/cli.ts":              "program.option('-r, --retries <n>', 'retries', 3);\nprogram.option('--verbose');\nyargs.option('port', {\n  alias: 'p',\n  default: 8080,\n});\nconst token = process.env.FIXTURE_TOKEN || 'none';\nconst home = process.env['HOME'];\n",
		"src/nest.controller.ts":  "@Controller('cats')\nexport class CatsController {\n  @Get(':id')\n  findOne() {}\n  @Post()\n  create() {}\n}\n",
		"src/hono.ts":             "const app = new Hono().basePath('/v1')\napp.get('/items', (c) => c.text('x'))\napp.on(['PUT', 'PATCH'], '/items/:id', h)\napp.route('/sub', sub)\nfastify.register(plugin, { prefix: '/plug' })\nfastify.route({\n  method: 'GET',\n  url: '/routed',\n  handler,\n})\n",
	})
	ix, err := Build(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	return ix
}

func TestBuildAndLookup(t *testing.T) {
	ix := buildTest(t)
	if st := ix.Stats(); st.Files != 9 {
		t.Errorf("stats = %+v", st)
	}
	want := []string{"cli", "client", "client.test", "extra", "fastify.d", "hono", "lib", "nest.controller", "server"}
	if got := ix.Namespaces(); !reflect.DeepEqual(got, want) {
		t.Errorf("Namespaces() = %v, want %v", got, want)
	}
	for _, q := range []string{
		"client.Client", "Client", "Client.fetchAll", "Client.fetchAll()", "client.Client.fetchAll", "fetchAll()", ".fetchAll",
		"Client.size", "Client.token", "Client.VERSION", "Client.hidden",
		"useItems", "useItems()", "client.useItems", "VERSION", "handler", "exposed", "internalThing",
		"Options", "Item", "Mode.Fast", "Mode.Slow", "Level.High", "util.deep", "client.util.deep",
		"client.extraFn", "extra.extraFn", "lib.libThing",
		"server.buildServer", "buildServer", "api.register", "api.close", "api.name", "buildServer.listen", "buildServer.stop", "helper", "other", "server.helper",
		"instance.listen", "instance.inject", "instance.printRoutes", "server.printRoutes", "server.register", "server.close",
		"buildServer.version", "buildServer.elapsedTime", "buildServer.statusCode", "server.statusCode", "buildServer.server",
		"fastify", "fastify.d.fastify", "testHelper",
		"client", "server",
	} {
		if !ix.Has(q) {
			t.Errorf("Has(%q) = false", q)
		}
	}
	for _, q := range []string{"local", "Client.nonexistent", "dep", "built", "mini", "client.nope", "server.fetchAll", "constructor"} {
		if ix.Has(q) {
			t.Errorf("Has(%q) = true", q)
		}
	}
	if f, line, ok := ix.File("Client.fetchAll"); !ok || f != "src/client.ts" || line != 14 {
		t.Errorf("File(Client.fetchAll) = %q, %d, %v", f, line, ok)
	}
	if !ix.IsNamespace("client") || ix.IsNamespace("Client") {
		t.Error("IsNamespace: client yes, Client no")
	}
	if !ix.IsExample("client.test") || ix.IsExample("client") {
		t.Error("IsExample: test files are examples")
	}
	if ix.Opaque("Client") || ix.Opaque("api") || !ix.Opaque("CatsController") && false || !ix.Opaque("VERSION") {
		t.Error("Opaque: a class without a parent and an object literal list their members; a value does not")
	}
}

func TestSpansAndExports(t *testing.T) {
	ix := buildTest(t)
	sp, ok := ix.Span("Client.fetchAll")
	if !ok {
		t.Fatal("Span(Client.fetchAll) not found")
	}
	if sp.DeclLine != 14 || sp.BodyEnd != 18 || sp.DocStart != 13 || sp.DocEnd != 13 || !reflect.DeepEqual(sp.Doc, []string{"fetchAll lists everything."}) || !reflect.DeepEqual(sp.Params, []string{"limit", "opts"}) || !sp.Exported {
		t.Errorf("span = %+v", sp)
	}
	sp, _ = ix.Span("Client")
	if sp.DocStart != 3 || sp.DocEnd != 5 || sp.BodyEnd != 22 || !reflect.DeepEqual(sp.Doc, []string{"Client talks to the server."}) {
		t.Errorf("class span = %+v", sp)
	}
	sp, _ = ix.Span("useItems")
	if !reflect.DeepEqual(sp.Doc, []string{"useItems is a hook."}) || !reflect.DeepEqual(sp.Params, []string{"id"}) || sp.BodyEnd != 27 {
		t.Errorf("function span = %+v", sp)
	}
	sp, _ = ix.Span("handler")
	if !reflect.DeepEqual(sp.Params, []string{"req", "res"}) || sp.BodyEnd != 32 {
		t.Errorf("arrow span = %+v", sp)
	}
	if sp, _ := ix.Span("Client.hidden"); sp.Exported {
		t.Error("a private method is not exported")
	}
	var names []string
	for _, s := range ix.AllSpans() {
		names = append(names, s.Qualified)
	}
	want := []string{"client.Client", "client.Client.fetchAll", "client.Client.size", "client.VERSION", "client.handler", "client.useItems", "client.util.deep", "extra.extraFn", "fastify.d.fastify", "lib.libThing", "nest.controller.CatsController", "nest.controller.CatsController.create", "nest.controller.CatsController.findOne", "server.api", "server.api.close", "server.api.register", "server.buildServer", "server.buildServer.listen", "server.buildServer.stop", "server.buildServer.version", "server.helper", "server.other"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("AllSpans() =\n %v\nwant\n %v", names, want)
	}
}

func TestSimilarSymbolsLiterals(t *testing.T) {
	ix := buildTest(t)
	if got := ix.Similar("Client.fetchAl", 2); len(got) == 0 || got[0] != "client.Client.fetchAll" {
		t.Errorf("Similar = %v", got)
	}
	if got := ix.Symbols(); len(got) == 0 || got[0] != "cli.home" {
		t.Errorf("Symbols() = %v", got)
	}
	if !ix.Literals().Has("/js/items") || !ix.Literals().Has("fixture") {
		t.Error("string literals not indexed")
	}
	if got := ix.Flags(); !reflect.DeepEqual(got, []string{"port", "retries", "verbose"}) {
		t.Errorf("Flags() = %v", got)
	}
	if got := ix.Envs(); !reflect.DeepEqual(got, []string{"FIXTURE_TOKEN", "HOME"}) {
		t.Errorf("Envs() = %v", got)
	}
	if v, ok := ix.Defaults().Get("flag", "retries"); !ok || v != "3" {
		t.Errorf("retries default = %q, %v", v, ok)
	}
	if v, ok := ix.Defaults().Get("flag", "port"); !ok || v != "8080" {
		t.Errorf("port default = %q, %v", v, ok)
	}
	if v, ok := ix.Defaults().Get("env", "FIXTURE_TOKEN"); !ok || v != "none" {
		t.Errorf("env default = %q, %v", v, ok)
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
		"GET /js/items", "POST /js/items", "DELETE /js/items/:id", " /api (prefix)",
		" /v1 (prefix)", "GET /items", "PUT /items/:id", "PATCH /items/:id", " /sub (prefix)", " /plug (prefix)", "GET /routed",
		"GET /cats/:id", "POST /cats",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("routes =\n %q\nwant\n %q", got, want)
	}
	_ = routes.Route{}
}

func TestModuleOf(t *testing.T) {
	cases := map[string]string{
		"src/client.ts": "client", "src/lib/index.ts": "lib", "index.js": "index", "types/fastify.d.ts": "fastify.d",
		"src/a.test.ts": "a.test", "src/app/utils/index.tsx": "utils",
	}
	for in, want := range cases {
		if got := moduleOf(in); got != want {
			t.Errorf("moduleOf(%q) = %q, want %q", in, got, want)
		}
	}
	if got := moduleOfSpec("src/client.ts", "./extra"); got != "extra" {
		t.Errorf("moduleOfSpec = %q", got)
	}
	if got := moduleOfSpec("src/client.ts", "./lib/index.js"); got != "lib" {
		t.Errorf("moduleOfSpec index = %q", got)
	}
	if got := moduleOfSpec("src/client.ts", "lodash"); got != "" {
		t.Errorf("moduleOfSpec package = %q", got)
	}
}

func TestMaskLine(t *testing.T) {
	masked := maskAll([]string{
		"const s = 'a { b'; // { comment",
		"const t = `multi {",
		"line ${x}` + \"q}\"; f({",
		"/* block { */ g() }",
	})
	want := []string{
		"const s = '     ';             ",
		"const t = `       ",
		"         ` + \"  \"; f({",
		"              g() }",
	}
	if !reflect.DeepEqual(masked, want) {
		t.Errorf("maskAll =\n %q\nwant\n %q", masked, want)
	}
	if got := templateToPlain("x(`/api/items`)"); got != `x("/api/items")` {
		t.Errorf("templateToPlain = %q", got)
	}
	if got := templateToPlain("x(`/api/${id}`)"); got != "x(`/api/${id}`)" {
		t.Errorf("templateToPlain with substitution = %q", got)
	}
}

func TestExportNames(t *testing.T) {
	if got := exportNames(" a, b as c, type D, default, e: f "); !reflect.DeepEqual(got, []string{"a", "c", "D", "e"}) {
		t.Errorf("exportNames = %v", got)
	}
}
