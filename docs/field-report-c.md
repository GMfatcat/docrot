<!-- docrot:ignore-file -->
<!-- This report quotes paths and symbols from other repositories; they are
     not claims about docrot itself, so docrot skips the whole file. -->
# Field report: docrot on three C and C++ projects

Date: 2026-09-24. Binary: `docrot 0.8.0`, default `.docrot.json`, git
enabled, shallow clones from GitHub. The projects were chosen for how they
are written and documented: curl (C, with 928 man pages kept as Markdown
under `docs/`, one per function and per command-line option), nlohmann/json
(a header-only C++ library whose MkDocs site has one page per member of
`basic_json`) and CLI11 (a header-only C++ command-line parser with a
README, a book and a Doxygen main page).

## Numbers

| project | .c/.h/.cpp files | lines | docs | references | errors | warnings | info | time |
|---|---|---|---|---|---|---|---|---|
| curl | 1,049 | 288,412 | 928 | 3,286 | 8 | 68 | 158 | 0.45 s |
| nlohmann/json | 516 | 161,389 | 269 | 8,446 | 6 | 14 | 94 | 0.36 s |
| CLI11 | 111 | 38,921 | 22 | 1,731 | 1 | 12 | 104 | 0.18 s |

Times are the warm run with git. No project needed configuration. curl's
17,000 symbols include the 300 `CURLOPT_*` values of one enum written as
`CURLOPT(CURLOPT_URL, CURLOPTTYPE_STRINGPOINT, 2),` lines inside an
`extern "C" {` block.

## What it found (verified by hand)

- **curl.** Every `curl_easy_*(3)`, `CURLOPT_*(3)` and `CURLINFO_*(3)` in
  the 928 man pages resolved; the symbol checks report nothing. The eight
  errors are paths: `lib/doh.c` (now `lib/vdns/doh.c`) and
  `./tests/ech_test.sh` (now `tests/ech_tests.sh`) in `ECH.md`, an
  `include/openssl` directory that lives in OpenSSL, `projects/OS400/README`,
  and `lib/config-operatingsystem.h` in `PORTING.md` — an illustrative name,
  which docrot cannot tell from a real one. The 43 flag warnings are the
  options of *other* programs documented in the same tree: `testcurl.pl`
  and `runtests.pl` (`--nogitpull`, `--seed=[num]`), `curl-config`
  (`--vernum`), `wcurl`, `git commit --author`. The environment warnings
  are the same shape: `CURL_TEST_MIN` and `CURL_MAKETGZ_VERSION` are read
  by Perl scripts, `QIBM_MULTI_THREADED` by an OS/400 build. curl's own
  tool options (`--verbose`, `--user-agent`, 298 of them) come from the
  `{"verbose", ARG_BOOL, 'v', C_VERBOSE}` table and resolve.
- **nlohmann/json.** The symbol checks are clean across 8,446 references
  (`json::parse`, `basic_json::dump`, `nlohmann::json`,
  `NLOHMANN_DEFINE_TYPE_NON_INTRUSIVE`…). The findings are paths: a
  `workflows/labeler.yml` that is under `.github/`, a NuGet path with
  backslashes, two amalgamation inputs named relative to another
  directory, and two `#overload-4` anchors on a generated page.
- **CLI11.** One error (`lib/cmake/CLI11/CLI11Config.cmake`, an install
  location described in the book) and twelve warnings: header paths named
  relative to `include/` ("exists under include/" says where), CMake
  options of a sub-directory (`CLI11_DISABLE_EXTRA_VALIDATORS`) that the
  root `CMakeLists.txt` does not declare, and `App::get_option_group` in
  the changelog, which was renamed.

## What C and C++ taught the heuristics

The first run on curl reported 13 errors and 161 warnings, CLI11 34
warnings. The noise was not about C declarations at all: a C repository's
documentation describes its build system and its helper scripts as much as
its code.

1. **`extern "C" {` is transparent.** curl.h wraps everything in it; the
   first scanner saw one giant function body and indexed nothing from
   the header. Now the block is a scope that changes nothing.
2. **Enum values behind macros.** `CURLOPT(CURLOPT_URL, …)` lines inside
   `typedef enum { … } CURLoption;` are values of that enum; the typedef
   name is applied when the closing line is reached.
3. **Build-system flags are not this program's flags.** `--enable-x`,
   `--disable-x`, `--with-x`, `--prefix`, `--std=c++17` and flags spelled
   with a dot (`--sub.field`, a syntax illustration in CLI11's README) are
   never claims; `configure`, `meson`, `ninja`, `conan`, `vcpkg` and
   `pkg-config` join the commands whose flags are ignored.
4. **`UPPER_SNAKE` is not always a variable.** `CURLOPT_WRITEDATA` in a
   sentence about environments used to be an environment variable that
   "the code never reads". A name that is a macro, an enum value or a
   CMake `option()` of the repository is never one, and `LD_LIBRARY_PATH`,
   `PKG_CONFIG_PATH`, `CC`, `CFLAGS`, `CMAKE_*` and their kin are
   external. The same rule fixed a comment-mention warning on a `CLI11_PARSE`
   macro.
5. **`::` names that are not C++.** `CLI11::CLI11` is an imported CMake
   target (`add_library(CLI11::CLI11 ALIAS …)`), accepted as such;
   `GooFit::Application` names another library and is skipped, as an
   unknown capitalised owner is in C#.
6. **Make targets without a Makefile.** A CMake build directory's Makefile
   carries every `add_executable`/`add_library`/`add_custom_target` name
   plus `all`, `clean`, `install`, `test` and `package`; `make docs` is
   checked against `CMakeLists.txt` when no Makefile exists — unless the
   tree is autotools (`Makefile.am`, `configure.ac`), where the targets are
   generated and unknown.

One change is general: when the language the classifier picked has no
near-miss for a missing name, the suggestion comes from whichever present
language has one — `fix_easy_perfrom()` in a repository that also holds
Odin code is reported as a C name with the C suggestion, not as an Odin
name with none.

## What is still noisy

- Options of helper programs documented in the same repository
  (`testcurl.pl --nogitpull`) are checked against this program's flags.
  A page that documents another tool could carry
  `<!-- docrot:ignore unknown-flag -->`; nothing in the text says so
  reliably.
- Environment variables read by scripts rather than by the compiled code
  (`CURL_TEST_MIN`) are "never read by the code" — true of the C code.
- An illustrative header name (`config-operatingsystem.h`) is an error
  like a real one.

## How to reproduce

```sh
git clone --depth 1 https://github.com/curl/curl ../surprise-c/curl   # etc.
python scripts/demo.py ../surprise-c/curl ../surprise-c/json ../surprise-c/CLI11 --out reports-c
```
