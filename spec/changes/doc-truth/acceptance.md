# acceptance.md — doc-truth

判据逐条可执行；每条都是「实现前红、实现后绿」或纯编译/一致性检查。

## 判据

```bash
go test ./internal/capability -run TestLiveDocsCoversEntryDocs -count=1 :: --- PASS
```

```bash
go run ./cmd/forge docs closure --strict :: drift: 0
```

```bash
go test ./internal/cli -run TestNoDeadCommandHints -count=1 :: --- PASS
```

```bash
go test ./internal/capability ./internal/docsconsistency ./internal/change ./internal/cli -count=1 :: ok
```

```bash
go run ./cmd/forge docs lint README.md :: 硬失败 0
```