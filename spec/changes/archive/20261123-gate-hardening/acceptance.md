# 验收判据

判据要能被命令证明；先失败证明登记第一条（实现前它必须红）。

```bash
go test -v ./internal/taskpipeline -run TestRunSelectorNoMatchJudgedFail -count=1
```

期望：--- PASS: TestRunSelectorNoMatchJudgedFail

```bash
go test -v ./internal/clitask -run TestCompleteRequiresRedFirstDeclaration -count=1
```

期望：--- PASS: TestCompleteRequiresRedFirstDeclaration

```bash
go test -v ./internal/cli -run TestReviewPassRequiresCriteriaReviewed -count=1
```

期望：--- PASS: TestReviewPassRequiresCriteriaReviewed

```bash
go test ./internal/taskpipeline ./internal/clitask ./internal/cli -count=1
```

期望：ok（三个包全过，无 FAIL）
