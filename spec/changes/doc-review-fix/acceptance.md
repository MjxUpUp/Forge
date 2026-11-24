# 验收判据

```bash
go run ./cmd/forge docs closure --strict
```

```bash
go run ./cmd/forge docs lint README.md
```

```bash
go test ./internal/capability ./internal/docsconsistency ./internal/change ./internal/cli -count=1
```

```bash
go build ./...
```
