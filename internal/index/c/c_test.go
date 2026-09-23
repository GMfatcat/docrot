package c

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

const curlH = `#ifndef FIX_CURL_H
#define FIX_CURL_H
#include <stddef.h>

#define FIX_VERSION "1.0"
#define FIX_MAX(a, b) ((a) > (b) ? (a) : (b))

#ifdef __cplusplus
extern "C" {
#endif

typedef void FIX;
typedef int (*fix_progress_callback)(void *clientp, double dltotal);

typedef enum {
  FIXOPT(FIXOPT_URL, FIXOPTTYPE_STRING, 2),
  FIXOPT(FIXOPT_PORT, FIXOPTTYPE_LONG, 3),
  FIXOPT_LASTENTRY
} FIXoption;

typedef struct fix_slist {
  char *data;
  struct fix_slist *next;
} fix_slist_t;

struct fix_httppost {
  struct fix_httppost *next;
  char *name;
};

/* fix_easy_init returns a handle. */
FIX_EXTERN FIX *fix_easy_init(void);
FIX_EXTERN int fix_easy_setopt(FIX *handle, FIXoption option, ...);
FIX_EXTERN void fix_easy_cleanup(FIX *handle);

#ifdef __cplusplus
}
#endif
#endif
`

const easyC = `#include "fix/fix.h"

static int global_init(long flags, bool memoryfuncs)
{
  return 0;
}

/**
 * fix_easy_init allocates a handle.
 */
FIX *fix_easy_init(void)
{
  if(!initialized) {
    struct local { int x; } l = {0};
  }
  return NULL;
}

int fix_easy_setopt(FIX *handle, FIXoption option, ...)
{
  const char *home = getenv("FIX_HOME");
  return home ? 0 : 1;
}

void
fix_easy_cleanup(FIX *handle)
{
}

static const struct LongShort aliases[] = {
  {"verbose",   ARG_BOOL, 'v', C_VERBOSE},
  {"output",    ARG_FILE, 'o', C_OUTPUT},
};
`

const jsonHPP = `#pragma once
#include <string>

NLOHMANN_JSON_NAMESPACE_BEGIN
namespace detail
{
template<typename T>
struct is_std_optional : std::false_type {};

inline bool parse_helper(const std::string& s) { return true; }
}  // namespace detail

/// @brief a class to store JSON values
template<typename T = int>
class basic_json
{
  private:
    template<typename U> friend struct detail::external_constructor;
    int m_data = 0;

  public:
    using string_t = std::string;

    /// @brief create a JSON value from an input
    /// @param[in] i the input
    static basic_json parse(const std::string& i,
                            bool allow_exceptions = true)
    {
        return basic_json();
    }

    string_t dump(const int indent = -1) const
    {
        std::string s = "{";
        return s;
    }

    basic_json() = default;
    ~basic_json() {}
    bool operator==(const basic_json& other) const { return true; }
    reference at(size_type idx);

  protected:
    void secret() {}
};

using json = basic_json<>;
NLOHMANN_JSON_NAMESPACE_END

namespace CLI {
class App {
  public:
    Option *add_option(std::string option_name, std::string help_str = "");
    Option *add_flag(std::string flag_name);
};
CLI11_INLINE std::string simple(const App *app, const Error &e);
}  // namespace CLI

namespace a::b {
enum class Mode { Fast, Slow = 2 };
}
`

const mainCPP = `#include "fix/json.hpp"
#include <CLI/CLI.hpp>

int main(int argc, char **argv)
{
    CLI::App app{"fixture"};
    std::string file;
    app.add_option("-f,--file", file, "A help string")->default_val("x.json")->envname("FIX_FILE");
    app.add_flag("--verbose,-v", "log more");
    int retries = 3;
    app.add_option("--retries", retries)->default_val(3);
    const char *region = std::getenv("FIX_REGION");
    return 0;
}

std::string CLI::simple(const App *app, const Error &e)
{
    return "";
}
`

func buildTest(t *testing.T) *Index {
	t.Helper()
	root := writeTree(t, map[string]string{
		"include/fix/fix.h":     curlH,
		"lib/easy.c":            easyC,
		"include/fix/json.hpp":  jsonHPP,
		"src/main.cpp":          mainCPP,
		"tests/unit/test_x.cpp": "namespace tests {\nvoid run_case() {}\n}\n",
		"build/gen.c":           "void built(void) {}\n",
		"third_party/x.h":       "void vendored(void);\n",
	})
	ix, err := Build(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	return ix
}

func TestBuildAndLookup(t *testing.T) {
	ix := buildTest(t)
	if st := ix.Stats(); st.Files != 5 {
		t.Errorf("stats = %+v", st)
	}
	for _, q := range []string{
		"fix_easy_init", "fix_easy_init()", "fix_easy_init(3)", "fix_easy_setopt", "fix_easy_cleanup", "global_init",
		"FIX_VERSION", "FIX_MAX", "FIX", "fix_progress_callback", "FIXoption", "FIXOPT_URL", "FIXOPT_PORT", "FIXOPT_LASTENTRY", "FIXoption::FIXOPT_URL",
		"fix_slist", "fix_slist_t", "fix_slist::next", "fix_httppost", "fix_httppost::name",
		"nlohmann::basic_json", "basic_json", "basic_json::parse", "basic_json::dump", "basic_json::at", "basic_json::secret", "basic_json::m_data", "basic_json::string_t",
		"nlohmann::json", "json", "nlohmann::detail::parse_helper", "detail::parse_helper", "detail::is_std_optional",
		"CLI::App", "App::add_option", "add_option", "CLI::simple", "simple", "a::b::Mode", "Mode::Fast", "b::Mode::Slow",
		"tests::run_case", "main",
	} {
		if !ix.Has(q) {
			t.Errorf("Has(%q) = false", q)
		}
	}
	for _, q := range []string{"built", "vendored", "basic_json::basic_json", "basic_json::operator", "nlohmann::nope", "l", "local"} {
		if ix.Has(q) {
			t.Errorf("Has(%q) = true", q)
		}
	}
	if f, line, ok := ix.File("fix_easy_init"); !ok || f != "lib/easy.c" || line != 11 {
		t.Errorf("File(fix_easy_init) = %q, %d, %v (the definition wins over the prototype)", f, line, ok)
	}
	want := []string{"CLI", "a", "a::b", "b", "detail", "nlohmann", "nlohmann::detail", "tests"}
	if got := ix.Namespaces(); !reflect.DeepEqual(got, want) {
		t.Errorf("Namespaces() = %v, want %v", got, want)
	}
	if !ix.IsNamespace("nlohmann") || !ix.IsNamespace("nlohmann::detail") || ix.IsNamespace("basic_json") {
		t.Error("IsNamespace")
	}
	if !ix.IsExample("tests") || ix.IsExample("nlohmann") {
		t.Error("IsExample")
	}
	if ix.Opaque("basic_json") || !ix.Opaque("json") || !ix.Opaque("fix_slist_t") || ix.Opaque("Mode") {
		t.Error("Opaque: a class lists its members; an alias or typedef does not")
	}
}

func TestSpans(t *testing.T) {
	ix := buildTest(t)
	sp, ok := ix.Span("fix_easy_init")
	if !ok {
		t.Fatal("Span(fix_easy_init) not found")
	}
	if sp.File != "lib/easy.c" || sp.DeclLine != 11 || sp.BodyEnd != 17 || sp.DocStart != 8 || sp.DocEnd != 10 || !sp.Exported {
		t.Errorf("span = %+v", sp)
	}
	if !reflect.DeepEqual(sp.Doc, []string{"fix_easy_init allocates a handle."}) {
		t.Errorf("doc = %v", sp.Doc)
	}
	sp, _ = ix.Span("fix_easy_setopt")
	if !reflect.DeepEqual(sp.Params, []string{"handle", "option"}) || sp.BodyEnd != 23 {
		t.Errorf("setopt span = %+v", sp)
	}
	if sp, _ := ix.Span("global_init"); sp.Exported {
		t.Error("a static function is not exported")
	}
	sp, _ = ix.Span("basic_json::parse")
	if sp.DeclLine != 26 || sp.BodyEnd != 30 || sp.DocStart != 24 || sp.DocEnd != 25 || !reflect.DeepEqual(sp.Params, []string{"i", "allow_exceptions"}) || !sp.Exported {
		t.Errorf("parse span = %+v", sp)
	}
	if !reflect.DeepEqual(sp.Doc, []string{"create a JSON value from an input", "the input"}) {
		t.Errorf("doxygen doc = %v", sp.Doc)
	}
	if sp, _ := ix.Span("basic_json::secret"); sp.Exported {
		t.Error("a protected member is not exported")
	}
	if sp, _ := ix.Span("basic_json"); sp.DocStart != 13 || sp.BodyEnd != 45 {
		t.Errorf("class span = %+v (template line between doc and declaration)", sp)
	}
	var names []string
	for _, s := range ix.AllSpans() {
		names = append(names, s.Qualified)
	}
	want := []string{"CLI::App", "CLI::App::add_flag", "CLI::App::add_option", "CLI::simple", "a::b::Mode", "fix_easy_cleanup", "fix_easy_init", "fix_easy_setopt", "fix_httppost", "fix_slist", "main",
		"nlohmann::basic_json", "nlohmann::basic_json::at", "nlohmann::basic_json::dump", "nlohmann::basic_json::parse", "nlohmann::detail::is_std_optional", "nlohmann::detail::parse_helper"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("AllSpans() =\n %v\nwant\n %v", names, want)
	}
}

func TestSimilarFlagsEnvsLiterals(t *testing.T) {
	ix := buildTest(t)
	if got := ix.Similar("fix_easy_inti", 2); len(got) == 0 || got[0] != "fix_easy_init" {
		t.Errorf("Similar = %v", got)
	}
	if got := ix.Similar("basic_json::dmup", 2); len(got) == 0 || got[0] != "nlohmann::basic_json::dump" {
		t.Errorf("Similar = %v", got)
	}
	if got := ix.Flags(); !reflect.DeepEqual(got, []string{"file", "output", "retries", "verbose"}) {
		t.Errorf("Flags() = %v", got)
	}
	if got := ix.Envs(); !reflect.DeepEqual(got, []string{"FIX_FILE", "FIX_HOME", "FIX_REGION"}) {
		t.Errorf("Envs() = %v", got)
	}
	if v, ok := ix.Defaults().Get("flag", "retries"); !ok || v != "3" {
		t.Errorf("retries default = %q, %v", v, ok)
	}
	if v, ok := ix.Defaults().Get("flag", "file"); !ok || v != "x.json" {
		t.Errorf("file default = %q, %v", v, ok)
	}
	if !ix.Literals().Has("FIX_HOME") || !ix.Literals().Has("x.json") {
		t.Error("literals")
	}
	if syms := ix.Symbols(); len(syms) == 0 || syms[0] != "CLI::App" {
		t.Errorf("Symbols() = %v", syms[:3])
	}
}

func TestStripParens(t *testing.T) {
	cases := map[string]string{"curl_easy_init()": "curl_easy_init", "CURLOPT_URL(3)": "CURLOPT_URL", "basic_json<>::parse": "basic_json::parse", "std::vector<int>::size": "std::vector::size", "::global": "global"}
	for in, want := range cases {
		if got := stripParens(in); got != want {
			t.Errorf("stripParens(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMaskLine(t *testing.T) {
	masked := maskAll([]string{`char *s = "a { b"; /* { x`, `y } */ int c = '{'; // {`})
	if masked[0] != `char *s = "     ";       ` || masked[1] != `       int c = ' ';     ` {
		t.Errorf("maskAll = %q", masked)
	}
}
