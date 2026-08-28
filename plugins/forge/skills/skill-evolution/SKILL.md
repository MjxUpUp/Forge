---
name: skill-evolution
description: "agent skill 持续进化编排（诊断→优化→记决策→accept/revert）。Use when: skill 在实际任务里漏检或误触发想系统性改进时、跑 forge skills eval-report 发现回归时、想给某 skill 的改动做回归验证防退化时、复盘某次 skill 优化为何这么改时。SKIP: 一次性 typo/格式修复（直接改）、从零新建 skill（用 skill-authoring-standard）、会话结束沉淀经验/记教训（用 session-retrospective）、非 skill 的业务代码优化。"
metadata:
  pattern: pipeline
  domain: skill-engineering
  requires_forge: "true"
  triggers: [{"event":"UserPromptSubmit","keywords":["skill 漏了","没触发","误触发","skill 优化","eval-report","回归验证","skill 进化"],"cooldown":600}]
---

# Skill 持续进化编排

continual skill evolution：每个 skill 在多任务/多轮中持续优化，**决策留痕**让下一轮
agent 理解 why，**eval 回归**防退化。

## 边界（先读，避免踩拆除决策）

- **审计 / 可复现，非泛化学习**。决策历史是「为什么这么改」的备忘录，不是跨 skill 的
  泛化模型。Forge 已拆除 experience/knowledge 闭环（2026-07-09, 8cedc80）——本 skill
  **不复活「学习层」**，只做留痕 + 防退化。
- **半自动**：forge 不能 spawn AI。诊断 / 优化 / 跑 eval 由你（agent）做，forge 只做
  归一化 / 判定 / 记录（与 skillseval 的 half-automatic 定位一致）。

## 核心循环（4 步，严格顺序）

```
1.diagnose → 2.revise → 3.record → 4.accept/revert
 诊断失败    优化 skill  记四元组决策  eval 无回归则 accept；有回归 scoped revert
```

### 1. diagnose（诊断失败模式）

失败信号来源：
- **实际任务漏检**：skill 该触发没触发 / 触发了但输出漏关键检查（看 task transcript）
- **eval-report 回归**：`forge skills eval-report --skill <X>` 显示 trigger/not-trigger
  pass-rate 较 baseline 下降

诊断要具体到「什么输入 × 期望 × 实际」，不要停在「skill 不好用」。

### 2. revise（优化 skill）

改 SKILL.md / scripts / references 修诊断出的失败模式。**改动须过 skill-authoring-standard「验证（新建/修改后）」节的清单**（validate/audit/防注水/TDD 基线），此处不复制。改完重建 case 集：

```bash
forge skills eval-gen --skill <X> --save   # 派生 trigger/not-trigger case 集
```

### 3. record（记四元组决策）

```bash
forge skills decide --skill <X> \
  --diagnosis  "<失败模式：什么输入×期望×实际>" \
  --revision   "<改了哪：SKILL.md/scripts/references>" \
  --evidence   "<脱敏证据：pass-rate / 回归比对>" \
  --outcome    accept|reject|revise|defer \
  --commit     <修订关联的 git commit> \
  --rationale  "<为什么这个 outcome>"
```

四元组对应 decision record h_t = (q_t, r_t, e_t, o_t)。append-only 进 `<skill>/decisions.md`。
`--commit` 是 scoped revert 的锚点——非平凡优化必填。

### 4. accept / revert

先实跑确认（half-automatic：你 dispatch fresh subagent 跑 case，回填 forge 判定）：

```bash
forge skills eval-cases --skill <X>                    # 拿 case 集（case_id + prompt）
# dispatch subagent 跑每个 case → 整批回填：
forge skills eval-record --skill <X> --from results.json --agent-model <模型>
forge skills eval-report --skill <X>                   # 看 pass-rate / 回归 vs baseline
```

- **accept**：pass-rate 升 / 无回归 → 优化落地，`--baseline` 更新基准。
- **scoped revert**：仍有回归，撤销这次优化（不动其他历史积累）：
  ```bash
  forge skills revert --skill <X> --dry-run                  # 先看将 revert 哪个 commit
  forge skills revert --skill <X> --decision <id>            # 实际 git revert
  forge skills decide --skill <X> --outcome reject ...       # 追加 reject 决策留痕
  ```

## 决策树：失败模式 × 处置

| 失败模式 | 处置 |
|---------|------|
| 该触发没触发 | 改 description 的 `Use when`（拓宽触发词）→ eval-gen → record |
| 误触发 | 改 description 的 `SKIP`（加排除场景）→ eval-gen → record |
| 触发对但输出漏检查 | 改 references/checklist → record |
| 优化后其他 case 回归 | scoped revert 这次 → record reject → 重新设计优化 |

## 触发回归（evals）

`evals/evals.json` 是 description 触发行为的回归用例集——改 description 前后跑一遍，
防止「修好一个触发词、撞坏三个路由」。

### Schema（R17 机器校验）

```json
{"trigger_cases": [{"query": "用户自然语言说法", "should_trigger": true}]}
```

- 顶层只能有 `trigger_cases`，每项 `{query: string, should_trigger: boolean}`——不要加
  其他顶层字段，R17 只认这个结构。
- `forge skills validate` 对 schema 做机器校验（R17，advisory），存在即校验，不强制创建。

### 用例配比与取材

- 8-10 条：约 **5 条正例** + **4-5 条 near-miss 负例**。
- 正例：用户自然语言说法的改写（口语化，该触发本 skill）——覆盖 `Use when` 列的主要
  触发词。
- **near-miss 负例从兄弟 skill 的路由撞车点出**：看着像、但应路由到兄弟 skill 的说法
  （如 systematic-debugging 的负例出「测试挂了」→ test-discipline、「编译报错」→
  compile-fix-loop）。负例撞车点即 description `SKIP` 段的逐条镜像——写了 SKIP 就要有
  对应负例，否则 SKIP 是不可验证的措辞。
- query 用中文口语化表达，不用 skill 名字本身（否则测不出路由能力）。

### 纪律

- **改 description 后必须过一遍 trigger_cases 自查**：逐条 query 问自己「新 description
  会不会正确触发/排除」；判不准的 dispatch fresh subagent 实测。
- 用例随 description 演进同步更新：新增触发词补正例，新增 SKIP 补负例。
- 批量实跑回归走核心循环的 step 4（eval-cases → eval-record → eval-report），本节只定
  case 集本身的编写纪律。

## 与 task-verify 的衔接

改 skill 源码时，task-verify gate 会 advisory 提醒「记决策」（skill-decisions-advisory）：
非平凡优化（改了行为/检查项/流程）就 `forge skills decide` 记一条；trivial（typo/格式）忽略。

## 与其他 skill 的衔接

- **skill-authoring-standard**：revise 步的改动规范唯一真相源（验证清单/R1-R11/description 规范都在它那里），本 skill 只管「改完怎么回归验证+留痕」。
- **session-retrospective**：会话复盘判定「经验该沉淀进 skill」时，具体改动走本 skill 的 diagnose→revise→record→accept 循环落地——它管「该不该沉淀、进哪个载体」，本 skill 管「skill 改动怎么安全落地」（双向衔接）。

## 体检：留痕覆盖率（防 decide 机制形同虚设）

定期（建议每 30 天，或发版前）核对「skill 改动」与「决策留痕」是否配对：

```bash
# 近 30 天改过的 skill
git log --since="30 days ago" --name-only --pretty=format: -- 'skills/*/SKILL.md' | sort -u
# 各 skill 最近一次决策时间
grep -h 'DecidedAt' skills/*/decisions.md | sort | tail -5
```

某个 skill 近 N 天有 SKILL.md 改动但 decisions.md 无对应条目 → 留痕漏了：非平凡改动补记 `forge skills decide`，trivial（typo/格式）不改。**有改动无留痕 = 下次退化无从归因**。

## 反模式

- **跳过 eval 直接改**：无法回归，下次退化无信号。改完至少跑一次 eval-report 比对 baseline。
- **whole-candidate revert**（git checkout 旧版整个 skill）：抹掉所有历史优化，用 scoped
  revert（按 decisions.md 的 CommitHash）只撤那一次。
- **把决策历史当训练数据**：它是审计备忘录，不喂模型「学习」——违背拆除决策。
