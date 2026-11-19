# acceptance：仓库级回归基线

```bash
go build ./...
```

```bash
go test -v ./internal/evalsuite/ -count=1
```

```bash
go test -v ./internal/cli/ -run 'TestEvalSuite|TestEvalTrend' -count=1
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
