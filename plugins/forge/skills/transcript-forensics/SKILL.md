---
name: transcript-forensics
description: "AI agent 会话转录（jsonl）取证分析：给定会话文件路径 + 症状假设（token 燃烧过快、工具调用循环、总结后不停调用、hook 误拦、子 agent 时序错乱），重建时间线、统计工具分布、定位根因。Use when: 用户给出会话/协作记录路径要求检查问题时、说'检查这个会话为什么不断工具调用''token 消耗过快''复盘会话记录''审计 session jsonl'时、多 agent 协作后要回溯谁做了什么时。SKIP: 运行中的 bug 现场排查（用 systematic-debugging）、会话结束后的经验沉淀与载体选择（用 session-retrospective）、活任务接续（用 session-continuity）。"
metadata:
  pattern: pipeline
  domain: observability
  steps: 5
  composes: [systematic-debugging]
  triggers: [{"event":"UserPromptSubmit","keywords":["检查会话","检查这个会话","会话记录","协作记录","会话审计","复盘会话","token 消耗","烧 token","工具调用循环","不断的工具调用","session jsonl","transcript"],"cooldown":600}]
---

# 会话转录取证

拿历史会话转录（jsonl）当**数据源**做行为取证：不是调试运行中的代码，而是回答"这个会话里 agent 到底干了什么、为什么偏离预期"。症状通常是用户带着假设来的——取证的第一步是既验证又证伪这个假设。

## 核心原则

- **先看数据结构再写解析**：各 host 的 jsonl 事件格式差异极大（见 references/transcript-formats.md），先 `head -1` 嗅探结构再写提取逻辑，凭想象写解析必错。
- **区分真人输入与注入**：user 消息里混着 tool_result 回填、hook 注入（additionalContext / hook_result）、续传摘要——不过滤会把机器行为算到用户头上。
- **结论挂证据**：每个根因结论附 jsonl 行号/条数统计，与代码审查同标准。

## 流程（5 步）

### 1. 症状分类（决定后续统计口径）

| 症状 | 先看的指标 |
|---|---|
| token 消耗过快 | 消息长度分布、超长消息条数、上下文重读次数 |
| 工具调用循环 | 同名工具+同参数重复次数、相邻调用间隔 |
| 总结后仍不停调用 | Stop 事件后的消息序列、Stop hook 拦截记录 |
| hook 误拦/未拦 | hook 输出事件、block 决策与工具调用的时序配对 |
| 多 agent 时序错乱 | 父子会话 ID 关联、teammate 消息交错点 |

### 2. 定位与嗅探

- 确认 host（claude / kimi / pi / reasonix / codex …）→ 按路径表定位转录文件（见 references/transcript-formats.md）。
- `head -1` / `head -c 800` 嗅探事件结构；大文件绝不整读，用 Python/grep 流式处理。
- Windows 控制台中文输出乱码 → `PYTHONIOENCODING=utf-8` 重定向到文件再读。

### 3. 提取与过滤

- 抽取真人输入（过滤规则按 host：claude 去 tool_result/hook 注入、kimi 只取 turn.prompt 事件、pi/reasonix 按 role:user 且去系统注入）。
- 统计：工具调用分布（Counter）、每消息长度、时间线（timestamp → 行为序列）。
- 注意去重：reasonix 的 recovery/冲突副本会把同会话计双份。

### 4. 根因分析（决策树）

- 同工具同参数重复 ≥3 次 → 循环：查触发它的上下文（通常是上一条指令要求"持续/反复"，或 Stop hook 注入目标未满足）。
- 超长上下文反复重读 → 检索策略问题：缺少索引/摘要，agent 每轮全量重读。
- 注入内容出现在 user 槽位 → hook 协议问题：确认是设计（additionalContext）还是泄漏（stdout 协议违规）。
- 拦截决策与预期不符 → 时序错乱：检查 hook 链顺序与 normalize 步骤先后（承重顺序错会改变语义）。
- 都不匹配 → 如实报告"症状未复现于本转录"，不硬凑根因。

### 5. 报告

产出：症状判定（证实/证伪用户假设）+ 关键统计表 + 根因 + 时间线摘录（带行号）+ 修复建议（分：改 prompt / 改 hook 配置 / 改协议）。修复建议若涉及 skill/hook 行为变更，转交对应维护流程，不在取证会话里顺手改。

## 执行后自查清单

- [ ] 解析前嗅探过文件结构（未凭想象写解析）
- [ ] 真人输入与机器注入已分离计数
- [ ] 副本/recovery 文件已去重
- [ ] 每个结论有行号或计数证据
- [ ] 用户原始假设得到显式证实或证伪

## Gotchas（真实踩坑）

| 坑 | 现象 | 解决 |
|---|---|---|
| user 消息含 tool_result | 用户指令计数虚高数倍 | 按消息结构过滤（content blocks 类型判断） |
| kimi 用户输入不在 message 事件里 | 按 role:user 解析得 0 条 | kimi 用户输入在 turn.prompt 事件（context.append_message 是注入混入点） |
| GBK 控制台 | 统计输出全乱码 | PYTHONIOENCODING=utf-8 输出到文件 |
| recovery 副本 | 同一会话事件计双份 | 按 branch_id/会话 ID 去重 |
| 只看开头几条 | 长会话的异常在尾部（循环/失控在后期） | 头尾都抽 + 全量计数统计 |

## 与其他 skill 的分工

- **systematic-debugging**：运行中代码的 bug 根因排查；本 skill 管历史转录行为取证，数据源是 jsonl 不是活进程。
- **session-retrospective**：会话结束后的经验提炼与载体选择；本 skill 产出的根因报告是它的输入。
- **research-workflow**：调研外部信息；本 skill 的"调研对象"是本机转录文件，不需要外部检索。
- 详细各 host 转录格式与解析代码片段：[references/transcript-formats.md](references/transcript-formats.md)。
