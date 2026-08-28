# 输出可读性门禁（output-readability-gates）

> 状态：已实施（随 v1.43 发布；main 最新 release 为 1.42.2）。来源设计：飞书《AI 产物可读性差调研设计》（2026-08）。
> 本文档是该设计在 Forge 仓库内的落地版与现状证据索引——回答「项目里有没有相关设计」时以本文档为准。

## 问题

AI 产物可读性差，两个维度：

1. **代码过度设计**——项目已有成体系约束（code-review-gate 的过度工程清单、implementation-discipline 的懒惰阶梯、agent-delegation 的 net-negative 契约），弱点是全部文本自律。
2. **文本冗长**——agent 回复与文档产物事无巨细、结论后置、空转措辞。学界已将「AI 啰嗦」归因到 RLHF/DPO 训练信号的长度偏差（arXiv 2403.19159 / 2310.10076 / 2505.12843）：**靠「让模型自觉」不可解，必须外部约束**。落地前仓库在此维度完全空白——生成的 CLAUDE.md/AGENTS.md 协议段、forge-quality Red Flags、hook 注入文本中无任何一条回复详略规则。

## 设计：三层约束 + 输出→回检循环

约束按可校验性分层，不要把所有约束写成依赖自觉的祈祷文：

| 层级 | 判什么 | 执行机制 | Forge 落地 |
|---|---|---|---|
| L1 机器可判 | 禁令短语、结构（必填章节）、结论枚举、篇幅 | 确定性 lint，不过即打回 | `internal/doclint`（D1-D7）+ `forge docs lint` |
| L2 模型可判 | 重点是否前置、详略是否得当、受众是否匹配 | rubric 档位判据 + 独立评审（产出者不能自检） | `doc-review/references/rubric-docs.md` + `forge task doc-review` |
| L3 需人判断 | 业务价值、对外措辞 | 不假装能自动化；轮次上限后升级人工确认 | doc-review 3 轮未过 → 人工裁定放行 |

回检的流程化（诊断：回检此前**无节点、无判据、无代价**——「检查一下」是不可证伪的祈使句）：

```
输出 → L1 lint（机器）→ L2 rubric 评审（子代理，对抗立场）→ 证据落档
  ↑                                                    │
  └────── 修复（delete-list 指向具体行）←──────────────┘未过
                                                       │过
                            complete 放行（doc gate 校验证据链）
```

## 落地组件与证据

### L1 — internal/doclint（禁令清单单一真相源）

- `internal/doclint/rules.go`：D1 禁令短语（`综上所述`/`基本可以`/`问题不大` 等）、D2 无证据整体结论、D3 围栏外复述 diff、D4 通过断言无证据标记、D5 必填章节、D6 结论枚举（GO/NO-GO）、D7 篇幅上限。硬/建议分级同 skillsqa 的 RuleDescriptions 模式。
- `internal/doclint/lint.go`：`LintFile/LintText`，行级 Issue。豁免三层：路径（vendor/dist/testdata/归档/CHANGELOG/decisions 等，清单见 `internal/doclint/lint.go`）、文件头 skip 标记、行内代码与围栏引用（引用短语是数据不是使用）。模板文件（`template-*.md`）豁免 D5-D7 实例规则——模板是结构定义不是填写实例。
- `forge docs lint [paths|--base <rev>]`（`internal/cli/docs.go`）：exit code 契约 0=通过 2=硬失败（对齐 skills validate）。
- **注入文本从同一表渲染**：`skillgen/qualitygen.go` 的「回复详略规则」章节经 `doclint.RenderBannedPhrasesForSkill()` 渲染，`claudemd.go` 基本规则第 7 条给指针——禁止手抄第二份（漂移由 qualitygen_test 的逐短语断言守卫）。

### L2 — rubric 与证据

- `skills/doc-review/references/rubric-docs.md`：四维各 0-25（结论前置/详略/证据/受众），总分 100，阈值 75（复用 skill 体系经验值，文档场景适用性待实测校准）。评分纪律五条：产出者不能当回检者、对抗立场、发现必须带行号、改进给 delete-list、评审自身分级。
- `forge task doc-review --passed <pass|fail> --score N [--round R] [--reviewer <id>] [--critical <发现>]`：评审后落 `TaskState.DocReview`（快照双键：HEAD commit + 变更文档内容指纹 sha256——提交后改产物动 HEAD 键、不提交地改产物动指纹键，均判过期重评）；Critical 发现落 Findings（Source=doc-review），未决阻断。轮次历史留 `DocReviewHistory`（收敛趋势可观测：两轮之间 Critical 不降即异常信号）。
- 模板（写时给正例）：`skills/doc-generator/references/` 新增 `template-pr/test-report/retrospective/tech-plan/release-notes`——修复了触发表宣称但无模板的三处空挂。

### 门禁 — task-complete doc pre-flight

- `internal/taskpipeline/docgate.go` `CheckDocGate(root, state)`：任务变更了 markdown 产物（`changedMarkdownDocs`：HeadCommit 以来 diff + 未跟踪，减豁免路径）时，complete 前校验 **L1 全过 + L2 证据 fresh/Passed/≥75 + 零未决 Critical**。无文档产物放行（纯代码任务零影响）。
- 挂载：`cli/task.go` runTaskCompleteAt，acceptance pre-flight 之后、MarkComplete 之前（同款顺序契约）。
- 逃生舱：`forge task override --doc-gate disable` / `FORGE_DOC_GATE=disable`——落 checklog CheckEscapeHatch 审计，evidence 强度降 Weak；轮次上限（3 轮）后的放行须人工确认后走这里，不自动放行。
- 提前量：task-verify 在 verify 阶段即发 ADVISORY 提醒回检（执法仍在 complete）。

### 度量 — 表达质量评分维度

- 任务评分六维 → 七维：`scoringtypes` 加 `expression`（权重 0.10，从 process/testing/code-quality/assertions 按比例匀出，合计仍 1.0）；`scoring/evaluator.go` `scoreExpression`——无文档产物中性 100（纯代码任务不受影响），有产物时 lintPart+rubricPart 各半，逃生封顶 60。
- 输入与 doc gate 同源（同 doclint + changedMarkdownDocs 实算），维度与门禁结论不会不一致。
- golden fixtures 已随权重再生成（`internal/scoring/testdata/golden*`）。

### 长期回路 — 约束进化

`session-retrospective` 新增「文档回检模式提炼」：同类文档同类问题被打回 ≥3 次（一次是噪声，三次是模式）→ 升级为 doclint 规则或模板章节——回检发现的每个高频问题让约束进化、让下轮循环更便宜。

## 收敛判据（写死）

放行 = L1 全过 + rubric ≥ 75 + 零 Critical 未决；轮次上限 3 轮，打满仍不过升级人工确认。逃生有代价（checklog 审计 + Weak + 评分封顶 89/维度封顶 60）。

## 已知边界

- PR 描述与 commit message 不是仓库文件：由撰写期模板（`.github/PULL_REQUEST_TEMPLATE.md`、CONTRIBUTING.md commit 段）+ rubric L2 约束，不进 doclint 的 --base 扫描。
- L2 评审是 agent 跑的 rubric，forge 只验证证据存在与 fresh——评审质量靠 doc-review skill 的纪律条款（对抗立场/带行号/delete-list）与「产出者不能自检」约束，无法机器强制评审者身份。
- 模型侧 eval（LLM-as-judge 打表达分）v1 不做：评委自身有 verbosity bias（arXiv 2310.10076），确定性部分（doclint 抓取/门禁拒绝断言）已由单测覆盖（`internal/doclint/lint_test.go`、`internal/taskpipeline/docgate_test.go`）。
- `--exclude-standard` 尊重项目与全局 gitignore：被项目明确忽略的 .md 不算交付物（部分机器全局忽略 `docs/` 只影响未跟踪新文件，已跟踪文件的变更仍被 git diff 覆盖）。已知口子：把产物写进 gitignore 目录可跳过评审——依赖 code review 兜底；新交付物应 `git add` 进跟踪后才受门禁保护（本设计文档自身即以 `git add -f` 入库）。
