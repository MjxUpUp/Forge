---
name: subagent-orchestration
description: "会话内子 agent（subagent/background agent）派生与收拢纪律：派生前过四问 gate（输入体积/独立性/输出契约/兜底），大输入先压缩再派，fan-out 有上限，派完必须收拢汇总。Use when: 准备派子 agent 或后台 agent 做调研/审查/扫描时、想并行多个 agent 时、子 agent 一直等待拿不到结果时、子 agent 撞上下文上限死亡时、用户抱怨'并发多个 agent 没收拢''不要再等 agent team'时、要设计 fan-out 编排时。SKIP: 跨会话/跨机器的任务分派与状态机（用 agent-delegation）、历史会话转录取证（用 transcript-forensics）、单个简单事实查询（直接查，不派）。"
metadata:
  pattern: pipeline + gate
  domain: workflow
  composes: [agent-delegation]
  triggers: [{"event":"PreToolUse","match":"Agent|Task","when":"source_changed_uncommitted","reason":"派生子 agent 前过四问 gate（输入体积/独立性/输出契约/兜底），防失控与上下文爆掉","cooldown":300},{"event":"UserPromptSubmit","keywords":["派子agent","派生子","子 agent","后台agent","fan-out","并行agent","多agent","agent team","收拢"],"cooldown":600}]
---

# 子 agent 派生与收拢纪律

子 agent 的正确用途是**隔离上下文**（不让大量中间材料污染主线）与**真并行**（独立子任务同时推进）。错误用法会产出三类真实事故：子 agent 读大文件撞上下文上限死亡、派完干等主线空转、fan-out 过宽失控到用户手动逐个停止。本 skill 把"派不派、怎么派、怎么收"变成派生前硬 gate + 收拢义务。

## 派生前四问 gate（任一答不上就不派，走 inline）

1. **输入体积**：子 agent 要读的东西有多大？>2MB 的原始数据（jsonl 转录/日志/生成物）禁止直接丢给子 agent"自己想办法"——必须先在主线用流式脚本（python/grep）压缩成 <100KB 的中间语料再派，或干脆 inline 处理。子 agent 没有主线的外部记忆，撞窗即死且不可恢复。
2. **独立性**：子任务之间是否无顺序依赖、无共享可变状态？强顺序依赖的小任务串行 inline 更快；有共享状态的并行会互相覆盖。
3. **输出契约**：子 agent 的产出是否有明确 schema（结构化字段/固定章节）？无契约的"去调研一下"会返回散文，无法机器合并，只能人工重读——fan-out 越大税越重。
4. **兜底路径**：子 agent 失败/超时/返回空时主线怎么办？答不出就先想好（inline 兜底 / 放弃该分叉 / 降级到单 agent）再派。

## 决策树：派还是不派

- 输入是大数据文件 → **不派**。主线先流式抽取压缩（参 `transcript-forensics` 的嗅探-过滤-统计手法），压缩产物小到主线能直接看就不需要 agent 了。
- 只读探索、可并行、产出是"结论不是过程" → 派，每个子 agent 给定边界（读哪些路径、多深）+ 输出 schema。
- 单文件/单符号/单事实查询 → 不派，主线一次 Grep/Read 更快。
- 任务本身是"等待外部结果" → 不派后台干等；要么 inline 阻塞做，要么明确轮询边界，绝不停下来空等（用户视角=卡死）。
- 需要"另一个模型视角"复核（红队/评审） → 派，但配对抗性指令与独立证据要求（参 `adversarial-verification`）。

## fan-out 与收拢纪律

- 并发上限 **3~5**：超过这个数，收拢成本与失焦风险超过并行收益（实测 8~11 个并发调研 agent 被用户整批手动停止）。
- 派生前先声明**收拢点**：所有分叉的结果汇到哪、按什么 schema 合并、谁负责去重与冲突裁决（通常是主线自己）。
- 部分失败容忍：某分叉返回 null/报错不阻塞其余分叉合并；但报告里必须如实列出失败分叉，不许静默丢弃。
- 派完 ≠ 完成：主线必须逐分叉核对产出（非空、合 schema、结论有证据），再写汇总。没有收拢汇总的 fan-out 等于没做。

## 执行后自查清单

- [ ] 每个子 agent 的输入都 <2MB 或已是压缩语料
- [ ] 每个子 agent 有输出 schema，汇总时逐个核对过
- [ ] 并发数 ≤5，且声明了收拢点
- [ ] 失败/空返回的分叉在汇总中如实标注
- [ ] 没有处于"派完等待"的空转状态（有轮询边界或已 inline 兜底）

## Gotchas（真实事故）

| 坑 | 现象 | 解决 |
|---|---|---|
| 大文件直塞子 agent | 子 agent `context window limit` 死亡，产出全失 | 主线流式脚本压缩后再派；压缩后往往不再需要 agent |
| 派完干等 | 主线停住等结果，用户视角卡死，被批"等待拿不到结果" | inline 做或有界轮询；小任务根本不该派 |
| fan-out 过宽 | 8~11 个后台 agent 同时跑，用户整批手动停止 | 上限 3~5；派之前问"收得回来吗" |
| 无输出契约 | 各分叉返回格式不一，无法合并只能人工重读 | 派生前写死 schema/固定章节 |
| 无收拢点 | "并发多个 agent 但没有收拢"，结论散落各分叉 | 派生前声明收拢点与裁决人（主线） |
| 子 agent 结果未校验 | 空结果/幻觉结论直接进主线报告 | 收拢时逐分叉非空+合 schema 检查 |

## 与其他 skill 的分工

- **agent-delegation**：跨会话/跨机器的任务分派（A2A 状态机、claim/deliver）；本 skill 管单次会话内的 subagent 编排纪律。
- **transcript-forensics**：事后从转录取证子 agent 到底干了什么；本 skill 在事前预防事故。
- **adversarial-verification**：红蓝对抗类派生的内容纪律（外部证据锚定）；本 skill 管派生的结构纪律，二者叠加使用。
