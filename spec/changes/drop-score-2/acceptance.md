# Acceptance

```bash
go build ./...
```

```bash
go vet ./...
```

```bash
sh scripts/checks/no-scoring-refs.sh
```

```bash
go test ./internal/taskpipeline/ ./internal/dashboard/ ./internal/clitask/ -count=1
```

```bash
sh scripts/checks/gofmt-clean.sh
```

```bash
sh scripts/checks/deadcode-zero.sh
```
