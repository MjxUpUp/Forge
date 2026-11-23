# 验收判据：能力面闭合表：命令树与活文档的漂移进 verify 摘要

change: `capability-closure`

每个 ```bash 围栏按出现顺序编号并实跑；`tasks.md` 引用的就是这个序号。

```bash
go test -v ./internal/capability -run TestScanDocsClassifies -count=1
```

预期：`--- PASS`

```bash
go test -v ./internal/capability -run TestLiveDocsSkipsDeletedNotes -count=1
```

预期：`--- PASS`

```bash
go test -v ./internal/change -run TestVerifySummaryCarriesCapability -count=1
```

预期：`--- PASS`

```bash
go run ./cmd/forge docs closure
```

预期：`命令面`
