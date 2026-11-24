# 验收判据

```bash
go build ./...
```

```bash
go test ./internal/cli -run TestUnknownSubcommandRejected -count=1
```

```bash
go test ./internal/checklog -run TestNoHistoricalCheckNames -count=1
```

```bash
go test ./internal/checklog ./internal/cli ./internal/compat -count=1
```

```bash
go run ./cmd/forge docs closure --strict
```
