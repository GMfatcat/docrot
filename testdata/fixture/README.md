# Fixture

A small multi-language repo used by docrot's golden tests.

## Usage

Run the server with `go run ./cmd/app --addr :9090 --verbose`. The main
loop lives in `cmd/app/main.go`; HTTP helpers live in `pkg/httpx/server.go`
and the router in `pkg/httpx/router.go`.

See the [guide](docs/guide.md#setup) and the [old guide](docs/guid.md) for
details, plus the [install section](docs/guide.md#instal).

```sh
./scripts/verify.ps1
./scripts/build.ps1
go run ./cmd/server
go get example.com/Fixture
pip install fixture_tools
cargo install fixture-rs
cargo add fixture-rss
npm install fixture-webb
npm run lint
npm run deploy
```

Requires Go 1.22 or later and Python 3.10+. The crate requires Rust 1.70+. The web client requires Node 16 or later. Run `make build`; `make lint` was removed.

## API

- `httpx.NewServer(addr)` creates a server; `Server.Addr()` returns the address.
- `httpx.WriteData(w, v)` writes the envelope, `httpx.WriteDatum(w, v)` is the old name.
- `Server.Address()` was removed in v2.
- `store.Open` is internal.

```go
import (
	"example.com/fixture/pkg/httpx"
	"example.com/fixture/pkg/router"
)

s := httpx.NewServer(":8080")
```

## Configuration

Flags: `--addr`, `--config`, `--confg`, `--port`. Rust flags: `--level`, `--workers`, `--worker`. Node flags: `--retries`, `--retrie`.
Environment variables: `FIXTURE_DEBUG`, `FIXTURE_TRACE`, `FIXTURE_LEVEL`, `FIXTURE_HOME`, `FIXTURE_TOKEN`.
Keys: `server.addr`, `server.timeout_ms`, `server.port`, `log.level`.
The `--addr` flag defaults to `:9090`.
Also `--verbose` (default: `false`).
And `server.timeout_ms` (default: `3000`).
The `--level` flag defaults to `debug`.
The `--retries` flag defaults to `5`.

```json
{
  "server": { "addr": ":8080", "timeout": 5000 },
  "log": { "level": "info", "fmt": "json" }
}
```

<!-- docrot:ignore -->
Ignored line mentions `docs/nonexistent.md` and `--nope`.
Scoped: `docs/scoped.md` is ignored but `--scoped` is not. <!-- docrot:ignore missing-path -->

## HTTP API

- `GET /v1/items` lists items, `POST /v1/items` creates one, `GET /v1/items/{id}` fetches one.
- `DELETE /v1/items/{id}` was never implemented; `/healthz` and `/readyz` are the probes.
- Python side: `GET /py/items` and `POST /py/items/{item_id}`.
- Rust side: `GET /rs/items`, `POST /rs/items` and `DELETE /rs/items/{id}`.
- Node side: `GET /js/items` and `PUT /js/items`.

## Other languages

Odin: `fixture_odin.render_frame` and `render_frames()`. Python: `helper.summarize`,
`Runner.run_async`, `helper.summarise`. Rust: `fixture_rs::io::read_all`, `Config::new()`,
`fixture_rs::io::write_al`, `crate::render()` and `std::env::var`. JavaScript: `Client.fetchAll()`,
`useItems()`, `Client.fetchAl()`, `.fetchAll` and `fw.useItems()`.

```ts
import { Client, VERSION, useItem } from 'fixture-web';
import * as fw from 'fixture-web';
import { serve } from 'fixture-web/server';
import nope from 'fixture-web/nope';
import express from 'express';
```

Version 1.2.3 supports 3 retries.
