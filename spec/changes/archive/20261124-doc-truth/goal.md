# goal.md — doc-truth

## Objective

把「文档教了不存在的东西」这条漂移链彻底修掉，并让守卫覆盖到用户真正会读的文档：

1. **入口文档改写**（P0）：`README.md`、`READMEs/README.zh-CN.md`、`npm/README.md`、`homepage/index.md` 不再出现已删命令、已删能力（质量评分/三门禁）与已删路径；`README.md` 增「从 2.x 升到 3.0（core-only）」段，把 1.84 宿主层清理手册移出主 README，版本号不再硬编码。
2. **合同/合规/演示文档如实化**（P1）：`docs/design/compat-commitments.md` 只承诺 3.0 仍在的三面（数据面 / 命令面 / 分发面），历史裁决移入 `docs/archive/`；`docs/compliance/standards-crosswalk.md` 只把合规映射到现存证据链；`docs/design/task-tree-guard.md` 修正状态行与出路；`docs/wedge-demo-anti-false-green.md` 改成用现命令面可复现的反假绿演示。
3. **历史设计记录标明状态**（P2）：9 份描述已移除子系统的设计文档加 `状态：历史` 头，正文保留（设计理由不销毁），死引用逐处标注。
4. **守卫扩面 + 清理**（P3）：`capability.LiveDocs` 覆盖 `READMEs/**`、`npm/README.md`、`homepage/index.md`、`docs/design/**`、`docs/compliance/**`，并把带 `状态：历史` 的文档排除；删 `docs/plans/` 两份 untracked 残留；ADR 补死引用注；`CHANGELOG.md` 的 Unreleased 补 core-only 破坏性变化。

判定标准（本 change 的目标形态）：`forge docs closure --strict` 在**扩面后的守卫**下 drift 为 0；四份入口文档与三份合同类文档不再出现「读者照做会失败」的陈述；`TestLiveDocsCoversEntryDocs` 钉住扩面本身。

## Out of Scope

- 不重写历史设计文档正文（只加状态头与必要的死引用标注）；不删除 `docs/adr`、`CHANGELOG.md`、`spec/changes/archive`。
- 不改任何命令语义、门禁判定与 compat 快照（本批只改文档与 `LiveDocs` 的扫描范围，命令面净增 0）。
- 不把 `docs/design` 的历史文档搬进 `docs/archive/`（仅 compat-commitments 的历史裁决段外移一份，避免正文与承诺混在一起）。
- 不追溯修复 `skills/`、`skills-forge/` 的产品内容（那是技能产物，不是 forge 文档）。

## Check

- 扩面后的守卫能抓到扩展前漏掉的那类漂移（红判据：`TestLiveDocsCoversEntryDocs` 实现前必须失败）。
- `forge docs closure --strict` 输出 drift 0，且 `docs closure` 的面板计数显示活文档份数明显增加（不再是 7 份）。
- 逐份入口文档与真命令面（`forge compat snapshot` 的 87 条）对读，无「已删命令当现行」。