# 验收判据

四条判据都由 `forge task verify-acceptance` 实跑。判据 1-3 是行为判据：实现前引用尚不存在的函数，编译失败即为红。

```bash
go test -v ./internal/testledger -run TestClassifyFourStates -count=1
```

```bash
go test -v ./internal/testledger -run TestLedgerViolations -count=1
```

```bash
go test -v ./internal/mutprobe -run TestChangedGoSourcesSince -count=1
```

```bash
go test ./internal/testledger ./internal/mutprobe ./internal/impact ./internal/cli -count=1
```
