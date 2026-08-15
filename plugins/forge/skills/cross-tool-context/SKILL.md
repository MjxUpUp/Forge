---
name: cross-tool-context
description: "跨 AI 工具上下文接续：项目有 forge 时用 forge task 双向锚定（decide/finding/resume/attach），无 forge 时降级为项目根 AI_CONTEXT.md 共享约定，让各 AI 工具（Claude Code / Cursor / Codex / Cline 等）发现的问题、修改、决策互相可见，消除手动复制粘贴。Use when: 同时用多个 AI 工具开发同一项目时、把 A 工具的分析结果搬给 B 工具时、说\"其他 agent 分析出的问题\"\"把这个给别的工具看\"\"工具间传递上下文\"时、多工具协作发现信息不对称时。SKIP: 单工具内跨会话恢复（用 session-continuity）、纯新项目无多工具协作、临时单次问题直接口头说即可。"
metadata:
  pattern: tool-wrapper
  domain: workflow
  forge依赖梯度: 有 forge → 全套双向锚定（本 skill 主路径）；无 forge → 降级为 AI_CONTEXT.md 纯纪律约定（无工具强制，长期易漂移，只作过渡）
  triggers: [{"event":"UserPromptSubmit","keywords":["交接","接力","接手","给别的工具","另一个工具","其他 agent","跨工具","复制给"],"cooldown":600}]
---

# 跨工具上下文接续

解决"多个 AI 工具并行开发同一项目，但互不感知"的效率瓶颈。当前最大浪费：A 工具发现的问题/做的修改，要靠用户手动复制粘贴给 B 工具，B 还不知道 A 改了什么文件。

## 核心机制：forge task 双向锚定（有 forge 时的主路径）

跨工具接续 = 各工具把信息写进同一个 forge task（持久化在用户级 DataDir `~/.forge/projects/<key>/`，不随会话/压缩丢失），任意工具 resume 即拉回：

- **开工前**：`forge task resume [--ref <ref>]` 拉回结构化上下文（目标/决策/发现/下一步/阻塞 + 门禁进度 + git 已改未提交）。
- **做了决策**：`forge task decide --content "..." --by [当前工具]`。
- **发现问题**：`forge task finding --content "..." --source [当前工具] --evidence file:line`（来源工具自动记录）。
- **跨工具锚定**：`forge task attach --ref <ref> --tool <当前工具>` 把当前工具的 session 锚定到 task——任意工具 resume 即知"谁参与过、用什么工具"。

forge task 是结构化真相源（可查询、抗压缩丢失、跨工具双向锚定）。`forge task context` 可导出 markdown 视图，供无 forge 的工具/人类阅读。

## 无 forge 时的降级方案：AI_CONTEXT.md

项目没有 forge 时，退回到纯文件约定：在**项目根目录**维护一份 `AI_CONTEXT.md`（单文件，git 可追踪），所有工具读写同一份。任何工具开始工作前先读它，发现/修改/决策后追加到它。

**为什么降级方案用文件而不是服务**：文件系统是所有工具的共同底座（任意 agent 都能读写文件），零基础设施依赖。这正是 Skill 的设计哲学（见 skill-authoring-standard §1）。注意它是靠纪律维护的文本——无工具强制，长期必漂移；项目引入 forge 后应迁回上面的主路径。

### 标准结构

```markdown
# AI_CONTEXT — <项目名>

> 本文件是多 AI 工具协作的共同上下文。任何工具开工前先读，有产出后追加。
> 最后更新：@<工具名> <YYYY-MM-DD HH:MM>

## Current Handoff（当前交接状态）
[当前主线任务 + 进度 + 下一步，格式见 session-continuity 的 references/handoff-format.md]

## Decisions（已确认的决策）
- [日期] <决策内容> — 由 <工具/人> 确认，影响 <文件/模块>

## Findings（各工具发现的问题，未决）
- [日期][<来源工具>] <问题> — 状态: open/fixed/wontfix
  - 证据: <文件:行 / 命令输出>
  - 影响: <哪些模块>

## Changes（近期文件修改记录）
- [日期][<工具>] <文件> — <改了什么，是否验证>

## Open Questions（待各工具/人确认）
- [ ] <问题> — 阻塞 <什么>
```

### 工作流

- **开工前**（任何工具，任何会话）：先读 `AI_CONTEXT.md`——Current Handoff 知进度、Decisions 不推翻、Findings 别重复发现、Changes 别覆盖他人改动。
- **工作中**：发现 bug → 追加 `## Findings`；做了决策 → `## Decisions`；改了文件 → `## Changes`；不确定 → `## Open Questions`。每条**必须标来源工具**（如 `[claude-code]`/`[cursor]`），用 edit 精确追加，不重写全文。
- **会话结束/切换工具**：更新 `## Current Handoff`（格式见 session-continuity 的 references/handoff-format.md），让下一个工具冷启动能续做。
- **只在本工具会话内相关的信息不必写**，口头说即可——"其他工具会遇到吗？"会才记。

### Gotchas（降级方案专属）

- **别把 AI_CONTEXT.md 当日志记流水账**：每次 read/write 都记一条，文件膨胀到几千行没人读。只记跨工具有价值的信息：决策、未决问题、关键修改。
- **格式必须统一**：统一用上面的标准结构（markdown 章节 + 每条带 `[日期][工具]` 前缀），所有工具都能读 markdown。
- **忘记读就开始干 = 覆盖别人改动**：改文件前 `git status` + 读 AI_CONTEXT 双确认。
- **别和 HANDOFF.md 搞混**：AI_CONTEXT.md 是**项目级长期**共同上下文；HANDOFF 是**任务级临时**交接。可以把 Current Handoff 节直接放 AI_CONTEXT.md 里，不必分两文件。

## 与其他 skill 的分工

- **session-continuity**：单工具内跨会话恢复。本 skill 是跨工具。两者用同一套 HANDOFF 格式（定义在 session-continuity 的 references/handoff-format.md，本 skill 复用）
- **session-retrospective**：把教训写进记忆文件（AGENTS.md/CLAUDE.md）。本 skill 管项目级跨工具上下文（forge task 或 AI_CONTEXT.md），非全局记忆

## 适用边界

- ✅ 多工具开发同一项目（如 Claude Code + Cursor + Codex 协作同一仓库）
- ✅ 把 A 工具分析喂给 B 工具决策（review 证据：另一工具 prompt"其他 agent 分析出的问题"）
- ❌ 单工具单会话（不需要，上下文在会话内）
- ❌ 临时一次性问题（直接说，不必落文件）
