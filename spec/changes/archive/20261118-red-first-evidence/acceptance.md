# 验收判据：测试先行三事实 + 评审者连续性

change: `red-first-evidence`

这里的每个 ```bash 围栏是一条可执行判据，`forge change verify` 按出现顺序编号（#1、#2……）并依次实跑。

```bash
go build ./...
```

```bash
go test ./internal/tasktypes/ ./internal/clitask/ -run 'TestRedAttempts|TestTaskStart_ChangeRequiresCriteria|TestTaskComplete_RedFirst' -count=1
```

```bash
go test ./internal/cli/ -run 'TestReviewContinuity|TestReviewFindings|TestReviewCriteriaReviewed' -count=1
```

```bash
go build -o /tmp/forge-redfirst ./cmd/forge
```

```bash
sh scripts/checks/red-first-e2e.sh /tmp/forge-redfirst
```

```bash
go run ./cmd/forge docs lint README.md READMEs/README.zh-CN.md docs/reference/cli.md
```

```bash
sh scripts/checks/gofmt-clean.sh
```

```bash
sh scripts/checks/deadcode-zero.sh
```