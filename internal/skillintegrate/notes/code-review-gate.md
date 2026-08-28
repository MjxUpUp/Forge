# code-review-gate · Forge 集成（仅 forge 项目适用）

本文件收纳 code-review-gate 的 forge 专属机制：环节感知加载、证据强度校准、cheat-scan 预扫、自动触发。非 forge 项目无需阅读——正文对应位置的条件化指针直接跳过即可。

## 环节感知加载（phase-aware，如有设计产物）

如果任务是**设计阶段产物审查**（非纯代码实现），`task-verify` gate 的 `inferDesignPhases` 已根据文件路径推断设计阶段并落盘 `state.DesignPhases`。据此加载对应 checklist，而非只加载通用 `review-checklist.md`：

```bash
# 检查任务是否有 DesignPhases（需有 active task）
forge task status --json 2>&1 | grep -i "design_phase\|DesignPhases"
```

有 `DesignPhases` 时，加载对应环节 checklist 作为补充检查项（清单宿主已迁至 design-artifact-standards skill 的 `references/`，下表链接跨 skill 指向新家——forge 项目全量同装，必然可达）：

| DesignPhase | 加载 checklist | 说明 |
|---|---|---|
| `requirement` | [phase-requirement.md](../../design-artifact-standards/references/phase-requirement.md) | 需求设计产物（PRD/需求文档）审查 |
| `api` | [phase-api.md](../../design-artifact-standards/references/phase-api.md) | API 设计产物（OpenAPI/proto/接口定义）审查 |
| `database` | [phase-database.md](../../design-artifact-standards/references/phase-database.md) | 数据库设计产物（migration/schema/索引）审查 |
| `frontend` | [phase-frontend.md](../../design-artifact-standards/references/phase-frontend.md) | 前端设计产物（组件/页面/路由/状态）审查 |
| `backend` | [phase-backend.md](../../design-artifact-standards/references/phase-backend.md) | 后端设计产物（service/domain/业务逻辑）审查 |
| `test-design` | [phase-test-design.md](../../design-artifact-standards/references/phase-test-design.md) | 测试设计产物（测试用例/计划/矩阵）审查 |
| 其他/无 | 通用 `review-checklist.md` | 代码级审查（默认） |

> 6 个环节与 `design-artifact-standards` skill 的编写期路由表对称——同一批 phase-*.md，编写期当骨架（design-artifact-standards，标准托管方）、审查期当 checklist（本步骤），一份标准两阶段共用。设计产物之外的**文档产物**（PR 描述/commit body/测试报告/复盘报告）审查走 `doc-review` skill（L2 评分门控），不在本 skill 范围。

**加载方式**：把对应 checklist 作为附加检查项，与轨道 A+B 一起执行。不替换而是补充——设计产物审查与代码实现审查关注点不同。

> 无 `DesignPhases` 时（普通代码任务）跳过此步，直接进入双轨审查。

## 证据强度校准（双轨审查前置，必做）

双轨审查之前，先看本任务的「完成」声明有多少 deterministic 证据支撑——证据弱时，审查重心要从「找代码 bug」扩到「核验声称的验证是否真发生过」。这正对冲 LLM-judge 看不出 agent 跳过前置就声明完成的盲区：deterministic 占比是完成可信度的硬信号（hook/gate 实跑，不可伪造），而 agent 自述可信度低。

```bash
forge review status        # task 模式输出末尾含「证据强度: ratio=X <档位>」+ 校准指令
forge trace <task-ref>     # 证据链分桶行 + Weak/Unverified 警告（同样驱动校准）
```

档位与审查动作：

| 档位 | 含义 | 审查动作 |
|---|---|---|
| **Strong**（ratio≥0.5） | deterministic 占多数，声明可信 | 正常双轨审查即可 |
| **Weak**（ratio<0.5） | agent 自述占多数 | **加核**：声称的测试是否真跑过（找 `test-run` 条目）、门禁是否经 deterministic 路径过，而非纯 agent-claim |
| **Unverified**（零 deterministic） | 声明全无实跑支撑 | **必核**：把「声称做了的验证是否真发生」列为首要审查项，必须见到 deterministic 证据才放行 |

证据弱（Weak/Unverified）时，把 `forge review status` 末尾的校准指令注入子 agent 的审查 prompt。

**跨模型 critic（证据弱时升级）**：Weak/Unverified 时，独立子 agent 审查之外再升一级——若所在 host 支持多模型，派一个**不同模型**的只读子 agent 做对抗式 critic：assume-bug 立场（假定声称的验证都没真跑过），逐条要 deterministic 证据才放行。跨模型独立性能打破单模型同源盲区（Self-Correction Illusion：同模型自我复核倾向确认自己已对）。host 仅单模型时退化为同模型的显式对抗 prompt——底线是「对抗式核验」这一动作，跨模型是增强项非硬前置。`forge review pass` 在 Weak/Unverified 时会发 `ADVISORY:` 提醒本次 stamp 盖在盲区证据上，见到即回退执行本升级。

## cheat-scan 预扫（7 类已 deterministic 判定，子 agent 不重复判断）

`task-verify` 的 cheat-scan 已机械扫任务新增行（`+` 行）的 `type-suppression`（`@ts-ignore`/`eslint-disable`/`#[allow]`/`type: ignore`）、`error-swallow`（空 `catch{}`/`except:pass`）、`dead-branch`（`if(false)`/`if(1===2)`）、`comment-only-fix`（某文件新增行全注释零逻辑）、`comment-as-debt`（新增债务注释标记不解决）、`phantom-import`（相对 import 解析不到磁盘文件——幻觉 mock 的机械子集；外部包存在性仍需语义判断）、`path-assumption`（OS 路径分隔符被当内容匹配器——跨平台崩溃指纹），命中记 `checklog:cheat-scan`（`forge trace` 可见）并 stderr 列出。审查前先看 `forge trace` 的 cheat-scan 条目——这 7 类已被 deterministic 判过，子 agent **跳过它们**，把精力放到其余模式（断言弱化/假重构/跨层语义幻觉/测试松绑等需语义判断的）和轨道 B 的设计/架构上。这正是"每轮 review 冒新问题"的根因对策：机械模式一次判准，不靠 LLM 每轮重采样。

## 自动触发：Stop hook 与 task-complete 门禁

本 skill 不再只靠手动唤起——forge 两条自动挡（2026-06-27 落地），让审查成为提交/结束的硬前置：

- **task 流程**：task-complete 门禁有 ReviewPassed 硬前置。派子 agent 审查通过后须运行 `forge review pass` 标记，否则过不了 task-complete 门禁、无法 complete。**多轮盖章**：`forge review pass` 检测到代码快照自上一枚盖章轮以来已变（返工后重新盖章）时发 `ADVISORY:` 提醒复审义务——本枚章只在已重新派只读子 agent 复审过修复时合法（修复者不能自证，见 SKILL.md「审查-修复-复审闭环」节）；同状态重复盖章（瞬态重试/重建基线）静默。
- **非 task 流程**：会话结束前 Stop hook 自动检测未审的源码变更，未审则拦截会话结束，additionalContext 指引加载 code-review-gate + 派子 agent + `forge review pass`。同一 diff 反复未审最多拦截 3 次后 advisory 放行（防 Stop 死循环）。

手动查状态：`forge review status`。

**误触发已防护**：纯文档/配置/生成物变更、无变更、commit 后干净工作区、task 模式（由门禁管而非 Stop hook）都不触发拦截。

> 迁移注记：本文件 2026-08 自 skills/code-review-gate/references/forge-integration.md 迁入（skills 零反向依赖契约，CONVENTIONS §13），并收纳 SKILL.md 迁出的盖章快照协议：

- `forge review pass` 检测到距上次审查基线有源码变更时，裸盖章会被拒绝——正规路径是复审后 `forge review pass --note "<复审结论>"`；确认变更无需复审时用 `--acknowledge-changes` 显式自我承担（checklog 记 WARN 级 self-refresh 审计）。非源码变更（amend commit message、保内容 rebase 等）不改变内容指纹，无需确认；同状态重复盖章保持静默。
- **`--note` 必须记录审查实质，不只结论**：覆盖范围（审了哪些文件/diff、几轨几 pass）、关键验证动作（grep 落位、实跑探针）、分歧与归因（双轨发现集差异、后轮新发现的归因分类）。反例（审计实证，2026-08）：一周 40 次 review-pass 仅 1 次带 note——无实质 note 的盖章 = rubber-stamp 盲区。审查发现同时用 `forge task finding` 录入（自动带轮次 Round 与代码指纹 ChangeHash）。
