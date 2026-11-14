# 协议可执行化：tasks

- [x] 1. 改 `internal/change/templates/agents-protocol.md`：新增「开始之前（硬规则）」，把 `spec/forge.yaml` 条目里的 hooks 执法叙述改为兼容字段说明（判据：acceptance #1、#3）
- [x] 2. 新增模板回归测试 `internal/change/agents_protocol_test.go`，钉住硬规则句、Task 流程段与无 hooks 残留（判据：acceptance #2）
- [x] 3. 新增 `scripts/checks/protocol-actionable.sh` 与 `scripts/checks/migration-doc.sh` 两条守卫（判据：acceptance #3、#4）
- [x] 4. README 增加「从 1.84 及更早版本升级」迁移节（判据：acceptance #4）
- [x] 5. 用 `forge change agents` 同步仓库 AGENTS.md 托管块，跑 build/文档守卫/deadcode（判据：acceptance #1、#5、#6）