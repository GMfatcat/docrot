<!-- docrot:ignore-file -->
<!-- This report quotes paths and symbols from other repositories; they are
     not claims about docrot itself, so docrot skips the whole file. -->
# Field report: docrot on three JavaScript and TypeScript projects

Date: 2026-09-24. Binary: `docrot 0.6.0`, default `.docrot.json`, git
enabled, shallow clones from GitHub. The projects were chosen for how they
are written and documented: fastify (CommonJS with hand-written `.d.ts`
types, a 51-page reference site whose examples name the instance
`fastify`, `request` and `reply`), hono (TypeScript with `#private`
methods and a migration guide) and zod (a TypeScript monorepo whose README
writes `z.string()` after `import * as z from "zod"`).

## Numbers

| project | .js/.ts files | lines | docs | references | errors | warnings | info | time |
|---|---|---|---|---|---|---|---|---|
| fastify | 301 | 78,642 | 51 | 2,810 | 51 | 11 | 62 | 0.21 s |
| hono | 365 | 86,485 | 8 | 100 | 9 | 0 | 1 | 0.20 s |
| zod | 519 | 102,778 | 24 | 1,479 | 7 | 3 | 13 | 0.29 s |

Times are the warm run with git. No project needed configuration.

## What it found (verified by hand)

- **fastify.** Five symbol errors, all real: `reply.getResponseTime()`
  in the v5 migration guide names the method the guide says was removed;
  `reply.sendFile` in `Request.md` belongs to the `@fastify/static`
  plugin, not to this repository; `fastify.display` (twice) is a name
  nothing declares. The eight `npm run` warnings and the one error
  (`npm run dev`, `npm run build`, `npm start`) are commands of the
  *reader's* project in the getting-started and TypeScript guides, not
  scripts of fastify's own `package.json`; docrot cannot tell a tutorial
  project from the repository and says so plainly. The 44 anchor errors
  are one class: `TypeScript.md` links `#fastifyfastifyinstance` to a
  heading written `fastify.FastifyInstance< [RawServer][RawServerGeneric],
  …>`. Rendered by GitHub the generic list is part of the heading text and
  the slug, so the link breaks there; rendered by the MDX site it is
  dropped. docrot follows GitHub's rules, as it always has.
- **hono.** Nine errors, all in `MIGRATION.md`: `hono/mod.ts` and
  `hono/middleware.ts` (the Deno layout of an older version), and three
  middleware packages the guide says moved out (`hono/body-parse`,
  `hono/graphql-server`, `hono/mustache`) — reported twice each, as an
  import sub-path that `package.json` no longer exports and as a name the
  package no longer exports. Accurate; a migration guide is history.
- **zod.** `packages/docs-v3` is the archived v3 documentation: its
  `src/errors.ts` moved to `packages/zod/src/v3/`, and its Korean and
  Chinese READMEs link headings that their tables of contents renamed.
  The warnings are `AGENTS.md` naming `core/regexes.ts` relative to
  `packages/zod/src/v4/`, reported as "exists under" with the full path.
  Every `z.string()`, `z.object()` and `.parse()` in the 1,479 references
  resolved.

## What JavaScript taught the heuristics

The first run on fastify reported 75 errors, 28 of them symbols; zod
produced 298 info findings. Six changes removed the noise, all general.

1. **The instance is named after the module.** fastify's documentation
   writes `reply.send()` and `fastify.register()`; `reply.js` declares
   `class Reply` and `fastify.js` builds the instance as an object literal
   inside its factory function. `module.member` now also matches a member
   of any class or object declared in that module, and object literals
   built inside a factory (`const fastify = { listen, inject, … }`) are
   indexed under the object's name, shorthand properties included.
2. **`Object.defineProperties(Reply.prototype, { statusCode: … })`** and
   `Object.defineProperty` define members; so do `X.prototype.m = …` and
   `X.m = …` on a declared `X`, inside the factory too.
3. **The document's own imports say what belongs to whom.** `import * as
   z from "zod"` in zod's README makes `z.string()` a claim about zod's
   exports; `import express from "express"` makes `express.json()` a claim
   about another package, never reported. Named imports from this package
   (`import { Client, useItem } from "fixture-web"`) are claims that each
   name is exported, at full severity.
4. **Members spelled with their dot.** ``` `.parse()` ```, ``` `.refine` ```
   and ``` `.addSchema` ``` in a JavaScript repository are members, not
   dot-files; a list of real dot-files (`.env`, `.github`, `.npmrc`…) keeps
   those as paths.
5. **Runtime globals are not paths.** `console.log` used to be a file
   named `console` with a `.log` extension; a dotted name whose first
   segment is a global of a present language's runtime (`console`,
   `Math`, `process`, `std`…) is never a path or a symbol claim.
6. **A module named `server` does not own `server.port`.** JavaScript
   members are camelCase; a lower-case or snake_case tail after a module
   name is a configuration key or an instance the resolver already skips,
   so the config-key rules keep working in mixed repositories.

Two rules came from the other side. A class that `extends` another has
inherited members the index cannot see, so an unknown member of it is not
a finding; a class without a parent and an object literal list all their
members, so `Client.fetchAl()` is reported with `Client.fetchAll` as the
suggestion. And an escaped `\<` in a heading is text, not an HTML tag,
which fixed slugs such as `#fastifyrawrequestdefaultexpressionrawserver`.

## What is still noisy

- Commands of a tutorial project (`npm run dev` in a "create your app"
  guide) are checked against this repository's `package.json`. A
  document that is about another project could carry
  `<!-- docrot:ignore missing-target -->`; nothing in the text says so
  reliably.
- Headings with TypeScript generics slug differently on GitHub and in
  MDX-based sites; docrot keeps GitHub's rules.
- Members added by plugins (`reply.sendFile` from `@fastify/static`) are
  errors: the plugin is another repository. The `siblings` setting
  covers paths, not symbols, for now.

## How to reproduce

```sh
git clone --depth 1 https://github.com/fastify/fastify ../surprise-js/fastify   # etc.
python scripts/demo.py ../surprise-js/fastify ../surprise-js/hono ../surprise-js/zod --out reports-js
```
