# Acceptance

```bash
go build ./...
```

```bash
go vet ./...
```

```bash
git grep -l "internal/scoring" -- '*.go' || true
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
