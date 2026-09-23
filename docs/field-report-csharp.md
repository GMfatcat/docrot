<!-- docrot:ignore-file -->
<!-- This report quotes paths and symbols from other repositories; they are
     not claims about docrot itself, so docrot skips the whole file. -->
# Field report: docrot on three C# projects

Date: 2026-09-24. Binary: `docrot 0.7.0`, default `.docrot.json`, git
enabled, shallow clones from GitHub. The projects were chosen for how they
are written and documented: Polly (a resilience library whose reference
site is Markdown under `docs/`, full of base-class-library types), Humanizer
(a library whose Docusaurus site keeps 1,088 Markdown pages, most of them
generated API pages, across five versioned copies) and TodoApi (an ASP.NET
Core minimal-API sample with one README).

## Numbers

| project | .cs files | lines | docs | references | errors | warnings | info | time |
|---|---|---|---|---|---|---|---|---|
| Polly | 801 | 43,755 | 67 | 1,965 | 0 | 4 | 46 | 0.30 s |
| Humanizer | 733 | 154,537 | 1,088 | 27,158 | 73 | 44 | 23 | 0.74 s |
| TodoApi | 40 | 3,441 | 1 | 20 | 0 | 0 | 0 | 0.10 s |

Times are the warm run with git. No project needed configuration.

## What it found (verified by hand)

- **Polly** is clean apart from four warnings. Two are the changelog
  naming members that later releases removed
  (`CircuitBreakerStateProvider.LastHandledOutcome`,
  `TelemetryOptions.OnTelemetryEvent`). Two are a community page listing
  *other* libraries as "targets .NET 4.0", checked against Polly's own
  lowest target framework (4.6): a sentence about another project, which
  docrot cannot tell apart from a requirement of this one.
- **Humanizer**: 63 of the 73 errors are one name, `Humanizer.Core`, the
  NuGet package id the documentation tells readers to install. `Humanizer`
  is a namespace, `Core` is no type in it, and no `.csproj` in the
  repository declares that `PackageId`, so docrot has nothing to match it
  against. The other ten errors and all 44 warnings are relative links in
  the versioned copies of the site (`../api/index.md`, `./languages/index.mdx`)
  that resolve only after the site is built; the warnings say where the
  file exists (`website/docs/`). Every one of the 27,000 references in the
  generated API pages resolved.
- **TodoApi**: the README's 16 routes (`GET /todos`, `POST /todos/{id}`…)
  match the minimal-API registrations through the `MapGroup("/todos")`
  variable, and nothing is reported.

## What C# taught the heuristics

The first run on Polly reported 10 warnings, Humanizer 73 errors. Four
changes removed the noise that could be removed.

1. **Base-class-library types on every page.** `TimeSpan.Zero`,
   `HttpStatusCode.InternalServerError`, `Timeout.InfiniteTimeSpan`: C#
   documentation names framework types constantly. A `Type.Member` whose
   type is declared nowhere in the repository is never a claim, and the
   language's standard set (`System`, `Task`, `TimeSpan`, `HttpClient`…)
   is skipped outright.
2. **Package ids look like namespaces.** `Humanizer.Core`, `Polly.Core`:
   a dotted name equal to a `.csproj` package id is accepted. The `.csproj`
   files are read up to three directories deep, since a solution keeps its
   projects in sub-directories, and the lowest `TargetFramework` among them
   (`net462` counts as 4.6) is the version requirement.
3. **Inheritance.** `class TodosController : ControllerBase` gets members
   from its base; an unknown member of such a type is not reported. A type
   without a base lists all its members, so `Catalog.ListAl()` is.
4. **Groups and controllers.** `var group = app.MapGroup("/todos")` followed
   by `group.MapGet("/")` registers `/todos`; `[Route("api/[controller]")]`
   on `TodosController` plus `[HttpGet("{id}")]` registers
   `/api/todos/{id}`.

## What is still noisy

- A package id the repository never declares (`Humanizer.Core` is defined
  by the publishing pipeline, not by a project file) is an error. A
  `PackageId` in the `.csproj`, or `<!-- docrot:ignore missing-symbol -->`
  on the line, settles it.
- Sentences about other projects' requirements ("library X targets .NET
  4.0") are checked as if they were this project's.
- Bare PascalCase identifiers (`ResiliencePipelineBuilder`, `Delay`) are
  not claims, by the same rule as in every other language; only dotted
  names and calls are. Polly's documentation writes mostly the former, so
  its 1,965 references under-count what the pages actually say.

## How to reproduce

```sh
git clone --depth 1 https://github.com/App-vNext/Polly ../surprise-cs/Polly   # etc.
python scripts/demo.py ../surprise-cs/Polly ../surprise-cs/Humanizer ../surprise-cs/TodoApi --out reports-cs
```
