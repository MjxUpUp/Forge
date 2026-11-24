# 验收判据

```bash
go build ./...
```

```bash
go test ./internal/cli -run TestReviewHasNoLLMJudgeArm -count=1
```

```bash
go test ./internal/compat -count=1
```

```bash
go run ./cmd/forge docs closure --strict
```

```bash
go test ./internal/cli ./internal/change ./internal/taskpipeline -count=1
```
