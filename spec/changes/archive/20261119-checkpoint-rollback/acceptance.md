# acceptance：工作树存档点

```bash
go build ./...
```

```bash
go test -v ./internal/cli/ -run 'TestTaskCheckpoint' -count=1
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
