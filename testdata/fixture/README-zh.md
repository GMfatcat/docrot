# Fixture

docrot golden 測試用的小型多語言 repo。

## 使用方式

用 `go run ./cmd/app --addr :9090 --verbose` 啟動。主迴圈在 `cmd/app/main.go`。

參見[指南](docs/guide.md#setup)與[外部連結](https://example.com/zh-only)。

```sh
./scripts/verify.ps1
./scripts/build.ps1
```

## API

- `httpx.NewServer(addr)` 建立伺服器。

```go
import (
	"example.com/fixture/pkg/httpx"
	"example.com/fixture/pkg/router"
)

s := httpx.NewServer(":8080")
```

## HTTP API

- `GET /v1/items` 列出項目，`POST /v1/items` 建立一筆。

## 設定

Flags：`--addr`、`--config`。

版本 1.2.4 支援 3 次重試。
