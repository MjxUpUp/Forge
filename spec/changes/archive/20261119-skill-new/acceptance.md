# Acceptance

```bash
go build ./...
```

```bash
go test ./internal/skillcap/ -count=1
```

```bash
go test ./internal/cli/ -run 'TestSkill' -count=1
```

```bash
go run ./cmd/forge docs lint docs/reference/cli.md
```

```bash
sh scripts/checks/gofmt-clean.sh
```

```bash
sh scripts/checks/deadcode-zero.sh
```
