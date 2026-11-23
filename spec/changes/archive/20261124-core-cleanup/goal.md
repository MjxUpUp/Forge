# core-cleanup — 目标契约

## Objective

core-only 之后的自查暴露三类残留，本 change 一次收清：

1. **change 侧判据假绿**：`go test -run <不存在的测试>` 退出 0（[no tests to run]），change verify 只看退出码 → 判据指向不存在/写错的测试也会封印。task 侧 B 批已判负，change 侧没有。
2. **死代码与死提示**：testcoverage 的死门禁函数、无调用者的死文件（workspace doctor、registry audit、skills 命令树）、以及用户可见文案里指向 core-only 已删命令的 `forge <死命令>` 提示（真实踩到：task complete 提示跑 `forge act retro-done`）。统计账本把 51 条条件跳闸（`if runtime.GOOS != "windows" { t.Skip }` 之类）算成「未审批的 skipped」，噪声淹没信号。
3. **技能脚手架退场**：`forge skills` 在 core-only 已从命令树摘掉，但 `internal/cliskills`（3219 行）与它独占依赖的 skillseval/skillsdecisions/skilltrigger/skillscanonical/skillsqa/skillmetrics/skillintegrate/skillsdist/skillsfm 仍是可编译的死包——「命令没了、实现还在」。

## Out of Scope

- 不改任何仍活着的能力语义（门禁判定口径、change 生命周期、task 状态机）。
- 不删 `skills/`、`skills-forge/` 下的技能内容（taskpipeline 的 skill-decisions 门禁按路径读 `skills/<name>/SKILL.md`）；只删它们的 embed 包装。
- 不动 `internal/act/disposition.go`（零调用但仍在 act 包内，需要单独判断是否属于经验层）。
- 不改 evidence/archive 的封印格式，不重写历史 change。

## Check

- change verify 对 `-run` 空匹配判负，证据里记 `selector_empty`，不再封印。
- 仓库内非测试 Go 源码的 fmt 打印面不再出现 `forge <core-only 已删命令>`（守卫测试钉死）。
- `forge test ledger` 的条件跳过不再计入 skipped 违规。
- `go list -deps ./cmd/forge` 之外不再有 skills 脚手架包；`go test ./...` 全绿。
