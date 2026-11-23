# acceptance：core-only

```bash
# 1 全仓可编译
go build ./...

# 2 宿主时代检查名不再存在
go test ./internal/checklog -run TestNoHostEraCheckNames -count=1 :: --- PASS

# 3 旧命令组已消失
go run ./cmd/forge workspace :: unknown command

# 4 核心包测试绿
go test ./internal/change ./internal/clitask ./internal/cli -count=1 :: ok

# 5 活文档漂移为 0
go run ./cmd/forge docs closure --strict :: drift: 0
```
