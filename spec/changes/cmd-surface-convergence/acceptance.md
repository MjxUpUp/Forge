# 验收判据：命令面收敛：去掉宿主层残留文案与死入口

change: `cmd-surface-convergence`

每条围栏一条判据，`forge change verify` 按顺序编号并实跑。

```bash
go build ./...
```

```bash
go test ./internal/clitask/ ./internal/taskpipeline/ -count=1
```

```bash
sh scripts/checks/no-stale-hook-help.sh
```

```bash
sh scripts/checks/resume-hook-flags-gone.sh
```

```bash
go test ./internal/cli/ -run 'TestHelpSurface_|TestCompatSnapshotMatchesGolden' -count=1
```

```bash
sh scripts/checks/deadcode-zero.sh
```

## 人工检查项（可选）

- [ ] 通读 `forge --help`、`forge review gate --help`、`forge verify --help`、`forge config --help`、`forge task resume --help`，确认读起来只有现状、没有 hook 时代的口吻。