# 验收判据：收口 P3 口径分叉

change: `backlog-p3`

```bash
go build ./...
```

```bash
go test ./internal/ghostpath/ ./internal/taskpipeline/ ./internal/change/ -count=1
```

```bash
go test ./internal/cli/ ./internal/scoring/ -count=1
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