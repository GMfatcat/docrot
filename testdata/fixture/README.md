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
```

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

Flags: `--addr`, `--config`, `--confg`, `--port`.
Environment variables: `FIXTURE_DEBUG`, `FIXTURE_TRACE`.
Keys: `server.addr`, `server.timeout_ms`, `server.port`, `log.level`.

<!-- docrot:ignore -->
Ignored line mentions `docs/nonexistent.md` and `--nope`.

## Other languages

Odin: `fixture_odin.render_frame` and `render_frames()`. Python: `helper.summarize`,
`Runner.run_async`, `helper.summarise`.

Version 1.2.3 supports 3 retries.
