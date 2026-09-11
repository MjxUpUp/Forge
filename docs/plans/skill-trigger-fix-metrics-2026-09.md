# skill-trigger/forge next 修复效果度量基线（2026-09-10 钉死）

> **口径已被取代（2026-09-11 注记，L2 回检 Critical 1 修复）**：回测以
> `forge eval harness-audit`（设计 M）钉的口径与 `evals/harness-audit-{6d5e,8db5}-baseline-202609.json`
> 为准——见 docs/design/harness-fixes-a-g-2026-09.md M 节。**本文审计口径仅背景**：
> 两口径已知分歧（A1 24.3 vs 18.9/日、总转化 5% vs 12.0%、C1 13% vs 28.8%、D1 0/8 vs 2/10），
> 按本文数字回测会做出相反判定。

事前成功度量门载体：四项修复（A skill-trigger 重构 / B forge next 推送化 / C 门禁命令管道纪律 / D references 下钻）在实现合入**之前**钉死基线，合入后按本文件指标回测。数据源与口径与 2026-09-10 全量审计一致。

## 口径（回测必须复用，否则对比无效）

- 数据范围：`~/.forge/projects/6d5e51a0f9d4`（Forge 自举），checklog 全量（3689 行）+ toollog 去重（14830 → 11292）。
- **toollog 去重规则**：同 session_id + tool_name + tool_input 且时间戳差 ≤2s 记一条（Bash 被 Pre/Post 双记）。
- 加载判定：Skill 工具调用；trigger→load 匹配窗口 [-5min, +30min]、同 session、skill 名相等。
- 回测窗口：修复合入后 **30 天或 25 个完成任务，先到为准**（对齐 metrics.yaml min_samples≥20 惯例；checklog 类指标样本下限 30 条）。

## 基线（2026-08-17 ~ 09-10，18 个活跃日）

```json
{
  "trigger_total": 437,
  "trigger_per_active_day": 24.3,
  "trigger_channel": {
    "UserPromptSubmit": {"events": 180, "converted": 18, "rate": 0.10},
    "PreToolUse":       {"events": 134, "converted": 2,  "rate": 0.01},
    "PostToolUse":      {"events": 89,  "converted": 0,  "rate": 0.00},
    "Stop":             {"events": 34,  "converted": 0,  "rate": 0.00}
  },
  "trigger_top_skills": {
    "implementation-discipline": "4/145",
    "test-discipline": "2/92",
    "verification-driver": "0/53",
    "compile-fix-loop": "0/36",
    "merge-release-choreography": "10/25",
    "release-readiness": "0/25"
  },
  "verification_driver_precision_proxy": "20/53（触发前5min存在测试命令）",
  "skill_loads_dedup": 43,
  "skill_loads_class": {"trigger_driven": 19, "banner_driven": 10, "autonomous": 14},
  "references_drilldown": "3/43",
  "refs_critical_drilldown": {"transcript-forensics": "0/2", "release-readiness": "0/5", "test-discipline": "0/1"},
  "gate_cmd_forms_n278": {"chained_and": 125, "multi_gate": 41, "piped_headtail": 74, "semicolon": 34, "standalone": 3, "grep_mask": 1},
  "advisory_followup": {"test-nudge": "60/75", "conventions-lint-fail": "3/8", "skill-trigger": "20/437", "stop-verify-reminder": "0/20"},
  "forge_next_organic_uses": 0,
  "verify_acceptance_uses": 57
}
```

## A｜skill-trigger 重构

**假设**：转化失败的主因不是关键词不精确，而是**通道时机**——"请加载 skill"只在决策点（UserPromptSubmit）有效；动作执行中（PostToolUse）/会话收尾（Stop）的推送应改为 test-nudge 式内联动作指令，不再请求加载。
（审计修正记录：初版建议"保留 PostToolUse/Stop 事件型、砍关键词型"，通道归因数据推翻——PostToolUse 0/89、Stop 0/34 全零转化，唯一有效通道是 UserPromptSubmit 18/180。merge-release-choreography 10/25=40% 证明"低频、场景强绑定、决策点推送"三条件齐备时该通道可用。）

| 字段 | 值 |
|---|---|
| 指标 A1 trigger 日均量 | 基线 24.3/日 → 目标 ≤8/日 |
| 指标 A2-背景 总转化率（全通道分母，仅审计口径参考） | 基线 20/437 ≈ 5% |
| 指标 A2 UPS 决策点转化率（回测判分行，M 口径） | 基线 18/180 = 10% → 目标 ≥25% |
| 指标 A3 verification-driver 精确率（代理：触发前5min有测试命令） | 基线 38% → 目标 ≥80% |
| 指标 A4 改内联指令后的动作跟随率（新，参照 test-nudge 形态） | 无基线 → 目标 ≥50% |
| source | checklog(skill-trigger/新 check 名) × toollog |
| 防伪护栏 | A2 不得靠砍事件换高转化：A1 下限 3/日（活跃日），且 PostToolUse 测试失败场景的**动作跟随**（A4）不得低于 50% |

## B｜forge next 推送化（范式再锚定）

**定位修正**（2026-09-10 与维护者对齐）：forge next 的目的不是"agent 不会主动问"，而是**范式注入的稳定性**——task start 横幅只在会话开头生效，长会话中范式漂移（41 次多门禁连刷、34 次 `;` 链续行即证据）。next 是状态驱动的再锚定点。
**投放面选择依据**：Stop 提醒通道实测 0/20 响应，不可用；gate 命令输出是 agent 确定性阅读面（去重后 139 次 gate 运行、拦截后 100% 重跑）——**next 行应挂在 gate/task status 输出末尾**，而非新增通道。

| 字段 | 值 |
|---|---|
| 指标 B1 next 行采纳率（gate 输出的 next 建议在后续 10min 被同类命令执行的占比） | 无基线（新功能）→ 目标 ≥50% |
| 指标 B2 范式漂移·多门禁连刷率 | 基线 41/278=15% → 目标 ≤8% |
| 指标 B3 范式漂移·`;` 链续行率 | 基线 34/278=12% → 目标 ≤5%（与 C 联动） |
| source | toollog（gate 命令序列重建） |
| 防伪护栏 | gate_fire_rate（BLOCKED 周趋势）不得上升——范式更稳不能以拦截变多为代价；task 完成中位耗时（基线 37min）不得恶化超 20% |

## C｜门禁命令管道/分号纪律

协议文本落点：forge-quality skill 会话行为规则区。约定：`forge task gate|complete|review pass|docs lint` 只允许单独执行或仅接 `&&`；禁止 `;`、`| tail/head/grep`。

| 字段 | 值 |
|---|---|
| 指标 C1 越规形态占比（`;`+grep 掩蔽） | 基线 35/278=13% → 目标 ≤2% |
| 指标 C2 管道截断占比（`| tail/head`） | 基线 74/278=27% → 目标 ≤15% |
| 指标 C3 standalone+&& 合规占比 | 基线 128/278=46% → 目标 ≥80% |
| source | toollog Bash 命令形态分类 |
| 防伪护栏 | 单任务 Bash 调用总数不得显著上升（禁管道不应导致拆分刷量：基线中位 ~40 次/任务上浮 ≤30%）；BLOCKED 后重跑率维持现状（拦截后 100% 重跑）不劣化 |

## D｜references 下钻（防膨胀设计）

**设计约束（回应关键词膨胀担忧）**：路由关键词活在 frontmatter description + triggers 配置，**正文不参与触发匹配**——内联对象是"步骤 0 必读指令"（≤5 行，含精确相对路径），不是 references 内容本身。膨胀由三道机器检查钉住，不靠自觉。

| 字段 | 值 |
|---|---|
| 指标 D1 refs-critical skill 下钻率（load 后 20min 内 Read references/） | 基线 0/8 → 目标 ≥50% |
| 指标 D2 SKILL.md 行数 | 不超 skill-authoring-standard 上限（validate 机器检查，改动前后 diff ≤5 行） |
| 指标 D3 eval-gen trigger case 集 | `forge skills eval-gen --save` 前后 case 集指纹不变（关键词零漂移的机器证明） |
| source | toollog(Read) × skills validate/eval-gen |
| 防伪护栏 | D1 不得靠把 skill 改成"一次性全读完"实现——references 内文件单次加载后 Read ≤2 个 |

## 回测流程

1. 窗口到期用 `forge eval harness-audit --json`（设计 M，两机同脚本可复算）产出对照 JSON——勿用本文审计口径的一次性脚本。
2. 逐修复记 skill-evolution 四元组：`forge skills decide --skill <X> --diagnosis/--revision/--evidence/--outcome`，evidence 附本文件指标的前后值。
3. 任一防伪护栏越线 → 该修复按 reject 记决策，scoped revert。
