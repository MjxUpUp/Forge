# 验收判据：1.88.1 交付单披露已钉 change；MSYS 路径告警

change: `fix-1881`

这里的每个 ```bash 围栏是一条可执行判据，`forge change verify` 会按出现顺序编号并依次实跑。

```bash
go build ./...
```

```bash
go test ./internal/clitask/ -run 'TestBuildTaskReport_PinnedChangeDisclosed|TestBuildTaskReport_PinnedChangeBeatsSpecApproval' -count=1
```

```bash
go test ./internal/forgedata/ -run 'TestLooksMSYSPath|TestGlobalHome_WarnsOnMSYSDataHome|TestGlobalHome_MSYSWarnOncePerProcess|TestGlobalHome_NoWarnOnWindowsPath' -count=1
```

```bash
sh scripts/checks/gofmt-clean.sh
```

```bash
go run ./cmd/forge docs lint docs/reference/cli.md
```