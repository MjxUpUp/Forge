---
name: design-artifact-standards
description: "设计产物编写期的质量标准入口，按产物类型路由到对应环节清单（phase-*.md）。Use when: 写 PRD/需求文档、API 契约/OpenAPI、建表/schema、页面/组件设计、测试方案等设计产物时——按对应清单搭骨架并自查。SKIP: 写代码实现（backend-development/database-design/frontend-feature-development/system-architecture）、代码审查（code-review-gate）、文档质量审查评分（doc-review）、查事实（research-workflow 轻量档）、需求未清先澄清（requirement-clarification）、按模板批量生成文档（doc-generator）。"
metadata:
  pattern: routing
  domain: design
  triggers: [{"event":"UserPromptSubmit","keywords":["写 PRD","需求文档","API 契约","OpenAPI","proto 定义","测试方案","测试用例","测试计划","user story","migration","接口定义","路由设计","设计文档"],"cooldown":600}]
requires: doc-review
---

# 设计产物质量标准（编写期入口）

phase-*.md 是「好设计产物该有什么」的标准清单（IEEE 830 / Google API Design Guide / DDD / WCAG / ISTQB 等提炼）。**编写期当骨架和自查清单，审查期 `code-review-gate` 引用同一份**——一份标准两处用。清单单一真相源在本 skill 的 `references/phase-*.md`（2026-08 自 code-review-gate 迁入：标准随编写期主场走，审查期消费者跨引），本 skill 做编写期路由与标准托管。

> 依赖：路由表末行「文档产物」按 **doc-review skill** 评审（frontmatter 已声明 `requires: doc-review`，单装本 skill 时宿主的依赖提示会提醒未同装，不阻断）。不装 doc-review 时其余 6 行完全可用，文档产物行降级为「按四维常识自查」。

## 核心原则

- **编写期一次到位 > 写完审查退回**：返工是最贵的质量成本。审查期才发现缺验收条件 / Out of Scope = 推倒重来；编写期按骨架搭 = 一轮过。
- **phase-*.md 当骨架用，不是事后 checklist**：动笔前读，按它的维度章节组织产物；不是写完才翻它打勾。
- **本 skill 管「产物该有什么」，不管「代码怎么写」**：实现模式 / 框架用法归 backend-development / database-design / frontend-feature-development / system-architecture。两件事不重叠。

## 为什么编写期就要用

审查期才看清单 = 写完才发现不达标 → 返工。**编写期就按标准搭骨架 = 一次写到位**。本 skill 把同一份标准在产出期暴露出来，让规范不只服务审查。

> 衔接：审查环境可按已写文件路径推断设计阶段并自动加载对应清单复核（code-review-gate 审查期的自动衔接）；无此机制的环境，审查期人工对照本清单。

## 路由表：你要写的产物 → 对应标准

先识别在写什么，Read 对应 phase-*.md（含分维度 checklist + 确定性机械规则 + 大厂规范映射）。

**路径锚点**：phase-*.md 在本 skill `references/` 下，下表链接为相对本 skill 的本地路径（`references/phase-<name>.md`）。若所在宿主 Read 以 cwd 为基解析相对路径导致断链，用绝对路径兜底：部署态 `<skills 根>/design-artifact-standards/references/phase-<name>.md`（Claude Code：`~/.claude/skills/design-artifact-standards/references/...`），开发态 `skills/design-artifact-standards/references/phase-<name>.md`（仓库根起）。

| 你在写 | 环节 | 读这个标准 |
|---|---|---|
| PRD / 需求文档 / user story | requirement | [phase-requirement.md](references/phase-requirement.md) |
| API 契约 / OpenAPI / proto / 接口定义 | api | [phase-api.md](references/phase-api.md) |
| 数据库设计 / 建表 / migration / schema | database | [phase-database.md](references/phase-database.md) |
| 前端设计 / 页面 / 组件 / 路由 / 状态 | frontend | [phase-frontend.md](references/phase-frontend.md) |
| 后端设计 / service / domain / 业务逻辑 | backend | [phase-backend.md](references/phase-backend.md) |
| 测试方案 / 测试用例 / 测试计划 | test-design | [phase-test-design.md](references/phase-test-design.md) |
| PR 描述 / commit body / 测试报告 / 复盘报告 | 文档产物（不在 6 环节枚举内） | 按 **doc-review skill** 评审（四维评分 + 类型特化）——写前先拿 doc-generator 对应模板；落盘产物先过机器层 lint（宿主提供时），再按 rubric 评分 |

> 6 个环节枚举与审查侧实现的环节集保持一致。编写期你按意图选环节；产物按约定路径落盘（如 PRD 放 `docs/prd/`、API 路径含 `openapi/api/proto`）时两期环节集合一致，路径不匹配时审查期回退通用清单（编写期自查的有效性不受影响）。

## 使用流程

1. **识别产物**：你要写的是 PRD？API 契约？表结构？——对应路由表一行。产物类型不清时先问用户，不猜。
2. **读标准**：动笔**前** Read 对应 phase-*.md，不是写完再查。phase-*.md 措辞偏「审查清单」视角，读时把每条「审查项」当「产物该有的章节/属性」看——同一份标准，编写期正向用、审查期反向用。
3. **搭骨架**：按 phase-*.md 的维度章节组织产物。例：PRD 必含 Out of Scope、每条需求有可执行验收条件、覆盖异常流；API 必有版本策略 + 统一错误模型 + 分页/排序约定。
4. **写完自查**：逐条核对 phase-*.md 的 checklist，再过一遍「确定性规则（机械可检）」表——这些是可被脚本扫出的硬指标（如 PRD 含模糊词、公开 API 无 OpenAPI 文档、URL 含动词）。
5. **衔接审查**：产物落地涉及代码后，code-review-gate 审查期会加载**同一份** phase-*.md 复核（6 个环节全覆盖）。编写期按标准做，审查期就无惊吓。

## 与其他 skill 的分工

- **code-review-gate**：审查期消费者。审查期加载本 skill 托管的同一批 phase-*.md 做审查。本 skill 是编写期生产者与标准托管方——标准共用，阶段不同。
- **doc-review**：文档产物（PR 描述/commit body/测试报告/复盘报告）的质量审查与评分门控。路由表末行指向它；本 skill 的 6 个环节清单管「设计产物该有什么」，doc-review 管「文档写得怎么样」。
- **backend-development / database-design / frontend-feature-development / system-architecture**：管「HOW 写代码」（实现模式、框架用法、架构选型）。本 skill 管「产物是否达标」（该有什么、是否满足机械规则）。写代码前先看开发 skill 学怎么写；写设计产物时看本 skill 学该有什么。
- **doc-generator**：按模板填空生成结构化文档。与本 skill 是 **producer-chain**：doc-generator 先按模板填出结构骨架，本 skill 再按 phase-*.md 标准自查达标度——非互斥，先填后查。
- **requirement-clarification**：需求本身不清时澄清。本 skill 假设需求已清，管需求文档的编写质量；若需求还没想清楚，先 SKIP 到 requirement-clarification 澄清，再回来写文档。
- **evidence-based-proposal**：出技术方案要基于实际验证。本 skill 管方案产物（如 API 设计文档）的达标，不管方案论证过程。

## Rationalizations（堵借口）

| 借口 | 现实 |
|---|---|
| 「审查期会查，编写期不用看标准」 | 审查期才发现不达标 = 返工。编写期按骨架一次到位省一轮 |
| 「照上个项目抄一份 PRD 就行」 | 旧项目的偏差被复制，不等于达标。phase-*.md 是跨项目的客观标准 |
| 「API 字段后加，先占个位」 | 缺版本策略 / 错误模型，破坏性变更埋雷，后期补成本翻倍 |
| 「标准我熟，不用查 phase-*.md」 | phase-*.md 含确定性机械规则（模糊词、URL 含动词、无 Out of Scope），不查会漏 |
| 「设计文档写了就行」 | 写了 ≠ 达标。过一遍 checklist 是分钟级，返工是小时级 |

## Red Flags（写设计产物时的反模式）

- 「PRD 先写个大概，细节以后补」→ 缺验收条件 / Out of Scope，是后面返工的源头
- 「API 先定差不多，字段后加」→ 缺版本策略 / 错误模型，破坏性变更埋雷
- 「表先建起来，索引/约束后加」→ 缺索引 / 约束 / 迁移回滚，数据层债
- 「设计文档写了就行，不用查标准」→ 审查期才被退回，不如编写期一次到位
- 「照着上一个项目抄一份」→ 旧项目的偏差被复制，不等于达标
- 「只写 happy path」→ 异常流 / 边界 / 失败路径缺失，所有环节的共同硬伤

## 维护注记

- **清单已本地化（2026-08 迁入）**：phase-*.md 单一真相源在本 skill `references/`（此前寄居 code-review-gate，靠 `../code-review-gate/references/` 跨 skill 相对链接 + `requires` 提示维持，宿主断链风险实测存在）。审查期消费者 code-review-gate 跨引本 skill（consumer 不依赖 producer 的存在）。
- **同步要求**：phase-*.md 的路径/文件名/环节增删变更时，本 skill 路由表 + 审查侧消费者的加载表需同步更新；6 个环节枚举与审查实现的环节集保持一致。
- **doc-review 为 skill 名引用**：路由表末行不深链 doc-review 内部文件，agent 按 skill 名加载后由其 SKILL.md 内部链接导航——避免再次引入跨 skill 深链断链问题。
- **agent Read 解析实测（2026-08，Kimi 宿主）**：Read 以 cwd 为基解析相对路径时本地 `references/` 链接同样可能断链，断链时走「路径锚点」绝对路径兜底。

## 参考

6 个环节清单（单一真相源，位于本 skill `references/`）：

- requirement：[phase-requirement.md](references/phase-requirement.md)
- api：[phase-api.md](references/phase-api.md)
- database：[phase-database.md](references/phase-database.md)
- frontend：[phase-frontend.md](references/phase-frontend.md)
- backend：[phase-backend.md](references/phase-backend.md)
- test-design：[phase-test-design.md](references/phase-test-design.md)

文档产物评分判据：doc-review skill（`references/rubric-docs.md` 为其内部资产）。

phase 枚举真相源：本 skill 的 6 份 phase-*.md 清单（审查侧实现的环节枚举与本表同步维护）。
