---
name: design-artifact-standards
description: "设计产物编写期的质量标准入口，按产物类型路由到对应环节清单（phase-*.md）。Use when: 写设计产物——PRD/需求文档/user story（requirement）｜API 契约/OpenAPI/proto/接口定义（api）｜建表/migration/schema（database）｜页面/组件/路由设计（frontend）｜service/domain/业务逻辑设计（backend）｜测试方案/用例/计划（test-design）——按对应清单搭骨架并自查。SKIP: 代码实现怎么写（backend-development/database-design/frontend-development/system-architecture 管 HOW，本 skill 管产物该有什么）、代码或产物审查（code-review-gate）、查事实（fact-research）、需求未清要先澄清（requirement-clarification）。与 doc-generator 是 producer-chain：doc-generator 按模板填结构骨架，本 skill 按标准自查达标度（非互斥，先填后查）。"
metadata:
  pattern: routing
  domain: design
requires: code-review-gate
---

# 设计产物质量标准（编写期入口）

phase-*.md 是「好设计产物该有什么」的标准清单（IEEE 830 / Google API Design Guide / DDD / WCAG / ISTQB 等提炼）。**编写期当骨架和自查清单，审查期 `code-review-gate` 步骤 1.5 引用同一份**——一份标准两处用。单一真相源在 `code-review-gate/references/phase-*.md`，本 skill 只做编写期路由，不复制内容。

> **安装依赖（声明性，需手动满足）**：本 skill 跨 skill 引用 `code-review-gate/references/phase-*.md`，依赖 code-review-gate 同 host 安装。frontmatter `requires: code-review-gate` 是声明性字段——当前 `forge skills install` **不强制**同装（requires 字段无 enforce，是 forge 既有现状），单装本 skill（`forge skills install --skill design-artifact-standards`）不会自动拉 code-review-gate，下表链接会全部断链。务必同时装 code-review-gate，或直接读开发态 `skills/code-review-gate/references/`。

## 核心原则

- **编写期一次到位 > 写完审查退回**：返工是最贵的质量成本。审查期才发现缺验收条件 / Out of Scope = 推倒重来；编写期按骨架搭 = 一轮过。
- **phase-*.md 当骨架用，不是事后 checklist**：动笔前读，按它的维度章节组织产物；不是写完才翻它打勾。
- **本 skill 管「产物该有什么」，不管「代码怎么写」**：实现模式 / 框架用法归 backend-development / database-design / frontend-development / system-architecture。两件事不重叠。

## 为什么编写期就要用

审查期才看清单 = 写完才发现不达标 → 返工（task-verify 的 `inferDesignPhases` 按已写文件路径推断阶段 → code-review-gate 加载对应清单）。**编写期就按标准搭骨架 = 一次写到位**。本 skill 把同一份标准在产出期暴露出来，让规范不只服务审查。

## 路由表：你要写的产物 → 对应标准

先识别在写什么，Read 对应 phase-*.md（含分维度 checklist + 确定性机械规则 + 大厂规范映射）。

**路径锚点**：phase-*.md 在 `code-review-gate/references/` 下。下表链接是相对本 skill 的 `../code-review-gate/references/`（同 host 同装时可达）。若按相对路径 Read 失败（agent 以 cwd 为基解析 `..` 可能断），用绝对路径：部署态 `<skills 根>/code-review-gate/references/phase-<name>.md`（Claude Code：`~/.claude/skills/code-review-gate/references/...`），开发态 `skills/code-review-gate/references/phase-<name>.md`（仓库根起）。

| 你在写 | 环节 | 读这个标准 | 核心维度 |
|---|---|---|---|
| PRD / 需求文档 / user story | requirement | [phase-requirement.md](../code-review-gate/references/phase-requirement.md) | 完整 / 可测 / 无歧义 |
| API 契约 / OpenAPI / proto / 接口定义 | api | [phase-api.md](../code-review-gate/references/phase-api.md) | 一致 / 版本 / 兼容 / 契约 |
| 数据库设计 / 建表 / migration / schema | database | [phase-database.md](../code-review-gate/references/phase-database.md) | 范式 / 索引 / 迁移可逆 |
| 前端设计 / 页面 / 组件 / 路由 / 状态 | frontend | [phase-frontend.md](../code-review-gate/references/phase-frontend.md) | 组件 / 状态 / a11y / 性能 |
| 后端设计 / service / domain / 业务逻辑 | backend | [phase-backend.md](../code-review-gate/references/phase-backend.md) | 分层 / 领域 / 状态 / 事务 |
| 测试方案 / 测试用例 / 测试计划 | test-design | [phase-test-design.md](../code-review-gate/references/phase-test-design.md) | 覆盖 / 边界 / 等级 / 独立 |

> 6 个环节枚举与 `taskpipeline` 的 `AllDesignPhases`（`internal/taskpipeline/phase_detect.go`）一致。编写期你按意图选环节；审查期 task-verify 的 `inferDesignPhases` 按**文件路径模式**推断环节（如 PRD 需放 `docs/prd/`、API 需路径含 `openapi/api/proto`）——产物按约定路径落盘时两期环节集合一致，路径不匹配时审查期会回退通用清单（编写期自查的有效性不受影响）。

## 使用流程

1. **识别产物**：你要写的是 PRD？API 契约？表结构？——对应路由表一行。产物类型不清时先问用户，不猜。
2. **读标准**：动笔**前** Read 对应 phase-*.md，不是写完再查。phase-*.md 措辞偏「审查清单」视角，读时把每条「审查项」当「产物该有的章节/属性」看——同一份标准，编写期正向用、审查期反向用。
3. **搭骨架**：按 phase-*.md 的维度章节组织产物。例：PRD 必含 Out of Scope、每条需求有可执行验收条件、覆盖异常流；API 必有版本策略 + 统一错误模型 + 分页/排序约定。
4. **写完自查**：逐条核对 phase-*.md 的 checklist，再过一遍「确定性规则（机械可检）」表——这些是可被脚本扫出的硬指标（如 PRD 含模糊词、公开 API 无 OpenAPI 文档、URL 含动词）。
5. **衔接审查**：产物落地涉及代码后，code-review-gate 步骤 1.5 会按 task 的 `DesignPhases` 加载**同一份** phase-*.md 复核（6 个环节全覆盖）。编写期按标准做，审查期就无惊吓。

## 与其他 skill 的分工

- **code-review-gate**：审查期消费者。步骤 1.5 据 task 的 `DesignPhases` 加载同一批 phase-*.md 做审查。本 skill 是编写期生产者——标准共用，阶段不同。
- **backend-development / database-design / frontend-development / system-architecture**：管「HOW 写代码」（实现模式、框架用法、架构选型）。本 skill 管「产物是否达标」（该有什么、是否满足机械规则）。写代码前先看开发 skill 学怎么写；写设计产物时看本 skill 学该有什么。
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

- **跨 skill 引用首例**：本 skill 是 canonical 内首个跨 skill 相对引用（`../code-review-gate/references/`）的 skill，其余 skill 均引用自身 `references/`。失效条件：`code-review-gate` 改名、phase-*.md 改路径/改名、或单装本 skill 不装 code-review-gate 时，下表 6 个链接断链。
- **同步要求**：phase-*.md 的路径 / 文件名变更时，本 skill 路由表 6 个链接 + `requires` 字段需同步更新；反之本 skill 重命名时 `code-review-gate/SKILL.md` 无反向依赖（consumer 不依赖 producer 的存在）。
- **agent Read 解析未实测**：跨 skill `..` 的 agent Read 行为（cwd 基 vs SKILL.md 目录基）未跨项目实测，已用「路径锚点」段的绝对路径兜底；若实测发现 `..` 普遍断链，应改为在 SKILL.md 内联各 phase 核心条目（牺牲单一真相源换可达性）。

## 参考

6 个环节清单（单一真相源，位于 code-review-gate）：

- requirement：[`code-review-gate/references/phase-requirement.md`](../code-review-gate/references/phase-requirement.md)
- api：[`code-review-gate/references/phase-api.md`](../code-review-gate/references/phase-api.md)
- database：[`code-review-gate/references/phase-database.md`](../code-review-gate/references/phase-database.md)
- frontend：[`code-review-gate/references/phase-frontend.md`](../code-review-gate/references/phase-frontend.md)
- backend：[`code-review-gate/references/phase-backend.md`](../code-review-gate/references/phase-backend.md)
- test-design：[`code-review-gate/references/phase-test-design.md`](../code-review-gate/references/phase-test-design.md)

phase 枚举真相源：`internal/taskpipeline/phase_detect.go`（`AllDesignPhases` + `inferDesignPhases`）。
