# 验收：backlog-closeout

```bash
go build ./...
```

```bash
go test -v ./internal/clitask/ ./internal/tasktypes/ -count=1 :: ok
```

```bash
go test -v ./internal/cli/ ./internal/scoring/ -count=1 :: ok
```

```bash
go run ./cmd/forge docs lint README.md docs/reference/cli.md
```

```bash
sh scripts/checks/gofmt-clean.sh
```

```bash
sh scripts/checks/deadcode-zero.sh
```

```bash
sh scripts/checks/no-removed-checks.sh
```

```bash
go build -o /tmp/forge-backlog.exe ./cmd/forge
```

```bash
sh scripts/checks/red-first-e2e.sh /tmp/forge-backlog.exe
```