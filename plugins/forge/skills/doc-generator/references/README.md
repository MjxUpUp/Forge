# Doc Generator 模板库

本目录（`references/`，平铺一层）存放各文档类型的模板，文件名以 `template-` 前缀。每个模板三段：变量列表（必填/选填）、章节结构（固定骨架）、风格示例（定调）。

## 当前模板

| 文件 | 文档类型 | 状态 |
|---|---|---|
| `template-prd.md` | PRD/需求文档 | ✅ |
| `template-weekly-report.md` | 周报 | ✅ |
| `template-acceptance-report.md` | 验收报告 | ⏳ 待补（可参考 project-acceptance skill 的 5 维度） |
| `template-meeting-notes.md` | 会议纪要 | ⏳ 待补（可参考 lark-workflow-meeting-summary） |

## 模板补充规则

- 每次用户提供了好的文档范例，提炼成模板存此（剥离具体内容留结构）
- 模板只定**结构和变量**，不定具体业务内容（业务内容靠 Inversion 采访）
- 风格示例用脱敏的范文，一段即可定调
- 新增模板后更新本 README 表格 + doc-generator SKILL.md 的模板库清单

## 与其他 skill 模板的关系

- `architecture-decision-record/templates/adr-template.md`：ADR 专用模板（独立管理，ADR skill 自包含）
- `project-acceptance`：验收报告的 5 维度框架（acceptance-report.md 模板应引用它，不重复）
- `lark-workflow-meeting-summary`：会议纪要工作流（meeting-notes.md 模板若与它重叠，优先用工作流 skill）
