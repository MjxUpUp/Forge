# 判据：complete/report/list 不再出现合成分

```bash
go build ./...
```

```bash
go test ./internal/tasktypes/ ./internal/clitask/ -count=1 :: ok
```

```bash
go test ./internal/cli/ -run 'TestTaskComplete|TestTaskReport|TestTaskList' -count=1 :: ok
```

```bash
sh scripts/checks/gofmt-clean.sh
```

```bash
sh scripts/checks/deadcode-zero.sh
```

```bash
go test ./internal/taskpipeline/ -run 'TestBuildTaskEvidence' -count=1 :: ok
```

```bash
go test ./internal/cli/ -run 'TestTaskScoreWorkflow' -count=1 :: ok
```