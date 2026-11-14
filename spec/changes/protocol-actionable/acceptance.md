# 协议可执行化：acceptance

> 判据必须能被命令证明；每条围栏就是一条判据。

## 1. 构建

```bash
go build ./...
```

## 2. 模板回归测试

```bash
go test ./internal/change/ -run 'TestAgentsProtocol' -count=1
```

## 3. 模板守卫（可执行性 + 无 hooks 残留）

```bash
sh scripts/checks/protocol-actionable.sh
```

## 4. 迁移说明守卫

```bash
sh scripts/checks/migration-doc.sh
```

## 5. 托管块同步回归（CLI 面）

```bash
go test ./internal/cli/ -run 'TestChangeAgents|TestChangeInit' -count=1
```

## 6. deadcode 归零

```bash
sh scripts/checks/deadcode-zero.sh
```

## 人工检查

- 迁移节覆盖 1.84 的实际残留：四个入口的 `FORGE:START` 段、四处 hook 配置只删条目
  （不删整文件、回滚 `[features] hooks`）、插件登记、skills 链接与缓存；删除前都有备份。
- 托管块顶部的硬规则不会把「一行文案」也逼进全套门禁：`forge task wild` 与
  「这次不用走 forge」两个出口在块内可见。