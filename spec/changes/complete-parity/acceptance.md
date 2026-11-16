# acceptance

```bash
go build ./...
go test ./internal/taskpipeline/ -run 'TestExecuteTaskGate_Complete|TestArtifactAdvisoryDetail' -count=1
go test ./internal/clitask/ -run 'TestRenderTaskReport_DocReviewDisclosure' -count=1
sh scripts/checks/gofmt-clean.sh
sh scripts/checks/deadcode-zero.sh
```
