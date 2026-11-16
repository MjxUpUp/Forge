# 澄清：边界与非目标

change: `dogfood-fixes`

## 边界（做什么 / 不做什么）

用户 2026-11-16 明确选择『现在修（走正常 change）』：范围=修 next 干净工作区自循环 + dogfood 暴露的 6 处文档缺口（AGENTS 模板与 cli.md）+ 契约仓库 protocol.yml 告警；边界=不改 --accept 解析语义、不新增 flag、不改 acceptance 语法、不实现 change 与任务的强制绑定；非目标=只改文档示例性文案而不动实现。

## 非目标

- 不实现 open change 与任务之间的显式绑定（`--change` 仍是可选链接）。
- 不改 `forge status` 的 harness 建议（human-only，保留）。
- 不把 `--accept` 改成经 shell 执行（安全考虑）。
- 不重写 quality/skills 相关文档之外的内容。

人确认：2026-11-16T01:18:20Z
