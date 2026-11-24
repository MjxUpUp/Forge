# change.md — doc-truth

## 改动

**P0 入口文档**：
- `README.md`：删「质量评分」承诺（3.0.0 起合成分已删除，`forge task score` 只打完成事实），图注与能力卡同步；`⬆️ 从 1.84 及更早版本升级` 的 105 行宿主层清理手册移入 `docs/migration/upgrade-from-1.84.md`（主 README 留 5 行摘要 + 指针）；`⬆️ 从 1.85–1.9x 升级到 2.0` 段内硬编码的 `@2.1.3` 改为 `@latest` 并要求 `>= 3.0.1`；新增「3.0 / core-only：命令面收窄」小节（被删命令组清单 + 迁移动作）；`forge change init/new/...` 那格 1000+ 字的巨型表格单元拆成表格一行 + 下方要点列表。
- `READMEs/README.zh-CN.md`：按 3.0 命令面重写（保留安装、两道门禁、change 证据、卸载、多宿主），删 registry/dashboard/trace/eval/update/config/policy 与评分口径，删「与英文版差异」里的 score 描述。
- `npm/README.md`：删 `forge registry add` / `forge verify` / `forge update` / `forge task score` 的现行表述，评分改为「交付证据（判据/红先绿/评审/门禁记录）」；命令参考表只列现存命令。
- `homepage/index.md`：修死链（`plugin-philosophy.md` 已删 → 指向现存设计文档）、「3 道门禁」→ 2 道、「质量评分」→ 交付证据、删 `scoring/testdata/golden_real/`（目录已随 `internal/scoring` 删除）、删 `forge skills list`。
- `docs/design/positioning.md`：验证层与传感器清单改成现存的（门禁 / 验收实跑 / 评审绑定 / cheat-scan / 证据封印），删 freeze-guard、hazard-guard、read-before-edit 与「3 道门禁」。

**P1 合同/合规/演示**：
- `docs/design/compat-commitments.md`：状态改为「生效中（3.0 core-only 收窄后）」；承诺矩阵删「对上：宿主面」一行；删已删出口命令与已删逃生舱 env；`§三` 的历史裁决批次移入 `docs/archive/compat-rulings-2026.md`（该文件带 `状态：历史` 头，不参与漂移判定），正文只留现行口径与指向。
- `docs/compliance/standards-crosswalk.md`：把 14 处映射到已删工具的行改为「能力已移除」或映射到现存证据链（checklog / `change verify` 证据目录 / `test ledger` / `review pass` 快照绑定），并明确「本表是映射与卡位，不是合规认证」。
- `docs/design/task-tree-guard.md`：状态行改为「已随 2.x 发布；worktree 命令面在 core-only 移除，判定逻辑保留（`TreeGuardDisabled` / `TaskTreeStatus`）」；三条出路改为不依赖已删命令的写法；死引用（`artifactchain`、`hook`）逐处标注。
- `docs/wedge-demo-anti-false-green.md`：重写为用 3.0 命令面可复现的三个演示：① `change verify` 的 `-run` 空匹配判负；② 证据绑定内容（改文件后证据失效）；③ `review pass` 绑定快照（改码后拒绝）；删 mutation/trace/eval/project 演示。

**P2 历史设计记录**：`project-policy-layer`、`multi-task-concurrency`、`sync-convergence`、`forge-dsh-provider-roadmap`、`mechanism-hardening`、`output-readability-gates`、`output-readability-gates-v2`、`impossible-task-flag-2026-10`、`loopspec-alignment-roadmap` 各加 `状态：历史（…已在 core-only 移除；本文保留设计理由）` 头，并把指向已删文档的引用标注为「文件已删除」。

**P3 守卫与卫生**：`internal/capability/capability.go` 的 `LiveDocs` 扩面（`READMEs/**`、`npm/README.md`、`homepage/index.md`、`docs/design/**`、`docs/compliance/**`，`状态：历史` 文档豁免）；新增 `internal/capability/livedocs_test.go`；删 `docs/plans/` 两份 untracked 残留；`docs/adr/0001`、`0002` 各补一行「引用文件已删除」注；`CHANGELOG.md` 的 `Unreleased` 补 core-only 破坏性变化条目。

## 影响

- **用户可见**：入口文档与 npm/官网首页的说法与 3.0 命令面一致；升级路径从 2.x 到 3.0 有明确清单。
- **审计面**：`forge docs closure` 的活文档份数从 7 份升到 20+ 份，后续漂移会在 verify 时暴露；`--strict` 成为可用的文档门。
- **文档与测试**：约 20 份 `.md` + `internal/capability` 两文件；`docs/reference/cli.md` 不动（已由 snapshot 生成且 0 stale）。
- **兼容面**：命令面净增 0；`compat.snapshot.json` 无需重钉（本批不改命令树）。

## 验证

- 红判据：`TestLiveDocsCoversEntryDocs`（实现前失败）→ 扩面后通过。
- `forge docs closure --strict` drift 0；`forge docs lint` 对改动的 `.md` 硬失败 0。
- `go test ./internal/capability ./internal/docsconsistency ./internal/change ./internal/cli -count=1` 全绿。