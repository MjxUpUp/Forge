---
name: skill-evolution
description: "agent skill 持续进化编排（诊断→探针→优化→记决策→accept/revert）。Use when: skill 在实际任务里漏检或误触发想系统性改进时、跑 forge skills eval-report 发现行为回归时、想给某 skill 加 behavior probe 防退化时、复盘某次 skill 优化为何这么改时。SKIP: 一次性 typo/格式修复（直接改）、从零新建 skill（用 CONVENTIONS + skill 规范）、非 skill 的业务代码优化。"
metadata:
  pattern: pipeline
  domain: skill-engineering
---

# Skill 持续进化编排

continual skill evolution：每个 skill 在多任务/多轮中持续优化，**决策留痕**让下一轮
agent 理解 why，**行为探针**防退化。

## 边界（先读，避免踩拆除决策）

- **审计 / 可复现，非泛化学习**。决策历史是「为什么这么改」的备忘录，不是跨 skill 的
  泛化模型。Forge 已拆除 experience/knowledge 闭环（2026-07-09, 8cedc80）——本 skill
  **不复活「学习层」**，只做留痕 + 防退化。
- **半自动**：forge 不能 spawn AI。诊断 / 优化 / 跑 probe 由你（agent）做，forge 只做
  归一化 / 判定 / 记录（与 skillseval 的 half-automatic 定位一致）。

## 核心循环（5 步，严格顺序）

```
1.diagnose → 2.probe → 3.revise → 4.record → 5.accept/revert
 诊断失败   写探针捕获  优化 skill  记四元组决策  probe 过则 accept；失败 scoped revert
```

### 1. diagnose（诊断失败模式）

失败信号来源：
- **实际任务漏检**：skill 该触发没触发 / 触发了但输出漏关键检查（看 task transcript）
- **eval-report 回归**：`forge skills eval-report --skill <X>` 显示 trigger/not-trigger/
  behavior pass-rate 较 baseline 下降

诊断要具体到「什么输入 × 期望 × 实际」，不要停在「skill 不好用」。

### 2. probe（写 behavior probe 捕获失败）

在 `<skill>/probes.yaml` 加一条，把诊断固化成可回归断言：

```yaml
probes:
  - id: p-<失败模式名>
    input: |
      <触发失败的具体输入>
    oracle: "contains:<输出应含的关键判断>"   # 或 regex:/exact:/not-contains:
    rationale: <为什么测这个；不含答案原文>
```

**C 组件权限分离纪律（关键）**：oracle 是判定标准，跑 probe 的 subagent 不应看，防
过拟合 oracle 而非真正改进 skill。所以：
- `rationale` 只写「为什么测」，**不写**「oracle 用 contains:X 断言」这类泄露判定标准的话
- 跑 probe 时用 `forge skills eval-cases --skill <X>` 拿**脱敏** case（Oracle 字段
  redact），**不要** `cat cases/<skill>.json`（那含 oracle 原文）

### 3. revise（优化 skill）

改 SKILL.md / scripts / references 修诊断出的失败模式。改完重建 case 集（纳入新 probe）：

```bash
forge skills eval-gen --skill <X> --save   # 派生 trigger/not-trigger + merge probes
```

### 4. record（记四元组决策）

```bash
forge skills decide --skill <X> \
  --diagnosis  "<失败模式：什么输入×期望×实际>" \
  --revision   "<改了哪：SKILL.md/scripts/references>" \
  --evidence   "<脱敏证据：probe pass-rate / 回归比对>" \
  --outcome    accept|reject|revise|defer \
  --commit     <修订关联的 git commit> \
  --rationale  "<为什么这个 outcome>"
```

四元组对应 decision record h_t = (q_t, r_t, e_t, o_t)。append-only 进 `<skill>/decisions.md`。
`--commit` 是 scoped revert 的锚点——非平凡优化必填。

### 5. accept / revert

先实跑确认（half-automatic：你 dispatch fresh subagent 跑 case，回填 forge 判定）：

```bash
forge skills eval-cases --skill <X>                    # 拿脱敏 case（含新 probe 的 input）
# dispatch subagent 跑每个 case → 整批回填：
forge skills eval-record --skill <X> --from results.json --agent-model <模型>
forge skills eval-report --skill <X>                   # 看 behavior pass-rate vs baseline
```

- **accept**：behavior pass-rate 升 / probe 全过 → 优化落地，`--baseline` 更新基准。
- **scoped revert**：probe 仍挂，撤销这次优化（不动其他历史积累）：
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
| 触发对但输出漏检查 | 加 behavior probe + 改 references/checklist → eval-gen → record |
| 优化后其他 case 回归 | scoped revert 这次 → record reject → 重新设计优化 |

## 与 task-verify 的衔接

改 skill 源码时，task-verify gate 会 advisory 提醒「记决策」（skill-decisions-advisory）：
非平凡优化（改了行为/检查项/流程）就 `forge skills decide` 记一条；trivial（typo/格式）忽略。

## 反模式

- **跳过 probe 直接改**：无法回归，下次退化无信号。至少给关键失败模式加一条 probe。
- **cat cases/<skill>.json 看 oracle**：违反权限分离，会让优化过拟合 oracle 字面。
- **whole-candidate revert**（git checkout 旧版整个 skill）：抹掉所有历史优化，用 scoped
  revert（按 decisions.md 的 CommitHash）只撤那一次。
- **把决策历史当训练数据**：它是审计备忘录，不喂模型「学习」——违背拆除决策。
