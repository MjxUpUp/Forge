# acceptance

```bash
go build ./...
```

```bash
go test ./internal/taskpipeline/ -run 'TestExecuteTaskGate_Complete|TestArtifactAdvisoryDetail' -count=1
```

```bash
go test ./internal/clitask/ -run 'TestBuildTaskReport_DocReviewLine|TestRenderTaskReport_DocReviewDisclosure' -count=1
```

```bash
go test ./internal/cli/ -run 'TestTaskStart_EmptyExpectedHint|TestTaskStart_NoEmptyExpectedHint|TestTaskStart_PlanFileEmptyExpectedHint|TestTaskAccept_EmptyExpectedHint' -count=1
```

```bash
sh scripts/checks/gofmt-clean.sh
```

```bash
sh scripts/checks/deadcode-zero.sh
```

```bash
go run ./cmd/forge docs lint docs/reference/cli.md internal/change/templates/agents-protocol.md spec/changes/complete-parity/goal.md spec/changes/complete-parity/proposal.md spec/changes/complete-parity/design.md spec/changes/complete-parity/tasks.md spec/changes/complete-parity/acceptance.md spec/changes/complete-parity/clarify.md
```

```bash
go test ./internal/clitask/ -count=1
```

```bash
go test ./internal/cli/ -run 'TestCompatSnapshotMatchesGolden|TestTaskStart_.*EmptyExpected|TestTaskAccept_EmptyExpectedHint' -count=1
```
