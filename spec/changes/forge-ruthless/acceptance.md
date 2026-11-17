# acceptance：forge-ruthless

## bash

```bash
go build ./...
```

```bash
go test -v ./internal/taskpipeline/ -count=1
```

```bash
go test -v ./internal/clitask/ -count=1
```

```bash
go test -v ./internal/change/ -count=1
```

```bash
go test -v ./internal/cli/ -run 'TestImpact|TestHelpSurface' -count=1
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

## 人工检查

- `forge --help` 不再出现 doc-review / mutation / task-verify 门禁；`forge impact --json` 输出 changed/docs/sources_without_tests。
- 新 change 只生成 goal.md / change.md / acceptance.md。
