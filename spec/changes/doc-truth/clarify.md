# 澄清：边界与非目标

change: `doc-truth`

## 边界（做什么 / 不做什么）

边界（用户 m03615「修复这些所有的文档问题」授权）：(1) 只改文档与 LiveDocs 扫描范围，不改命令语义/门禁判定/compat 快照；(2) 9 份历史设计文档只加「状态：历史」头与死引用标注，不重写正文、不删除设计理由；(3) compat-commitments 的历史裁决段外移到 docs/archive/ 一份带历史头的文件；(4) wedge-demo 重写为现命令面可复现的演示。(5) 非目标：不修 skills/ 与 skills-forge/ 的产品内容、不动 docs/adr 与 CHANGELOG 的历史记录（只补注与 Unreleased）、不扩 docs/plans（已删）。

## 非目标

- 不重写历史设计文档正文（只加状态头与必要的死引用标注）；不删除 `docs/adr`、`CHANGELOG.md`、`spec/changes/archive`。
- 不改任何命令语义、门禁判定与 compat 快照（本批只改文档与 `LiveDocs` 的扫描范围，命令面净增 0）。
- 不把 `docs/design` 的历史文档搬进 `docs/archive/`（仅 compat-commitments 的历史裁决段外移一份，避免正文与承诺混在一起）。
- 不追溯修复 `skills/`、`skills-forge/` 的产品内容（那是技能产物，不是 forge 文档）。

人确认：2026-11-24T02:12:55Z
