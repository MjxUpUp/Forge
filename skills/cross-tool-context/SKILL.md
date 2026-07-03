---
name: cross-tool-context
description: "跨 AI 工具上下文共享约定：在项目根维护 AI_CONTEXT.md，让 pi / Claude Code / deveco / zcode 等工具发现的问题、修改、决策互相可见，消除手动复制粘贴。Use when: 同时用多个 AI 工具开发同一项目时、把 A 工具的分析结果搬给 B 工具时、说\"其他 agent 分析出的问题\"\"把这个给 deveco 看\"\"工具间传递上下文\"时、多工具协作发现信息不对称时。SKIP: 单工具内跨会话恢复（用 session-continuity）、纯新项目无多工具协作、临时单次问题直接口头说即可。"
metadata:
  pattern: tool-wrapper
  domain: workflow-management
---

# 跨工具上下文共享

解决"4 个 AI 工具并行开发同一项目，但互不感知"的效率瓶颈。当前最大浪费：A 工具发现的问题/做的修改，要靠用户手动复制粘贴给 B 工具，B 还不知道 A 改了什么文件。

## 核心机制：项目根 AI_CONTEXT.md

在**项目根目录**维护一份 `AI_CONTEXT.md`（单文件，git 可追踪），所有工具读写同一份。任何工具开始工作前先读它，发现/修改/决策后追加到它。

```
<project-root>/
└── AI_CONTEXT.md   ← 所有 AI 工具的共同上下文
```

**为什么是文件而不是服务**：文件系统是所有工具的共同底座（pi/claude/deveco/zcode 都能读写文件），零基础设施依赖。这正是 Skill 的设计哲学（见 skill-authoring-standard §1）。

## Forge 任务接续（结构化真相源，优先于 AI_CONTEXT.md）

若项目用 forge，**优先把跨工具信息写进 forge task**（持久化进 `.forge/tasks/<ref>.json`），而非只写 AI_CONTEXT.md：

- **开工前**：`forge task resume` 拉回结构化上下文（替代读 AI_CONTEXT.md 的 Decisions/Findings/Changes）。
- **做了决策**：`forge task decide --content "..." --by [pi]`（替代追加 `## Decisions`）。
- **发现问题**：`forge task finding --content "..." --source [pi] --evidence file:line`（替代追加 `## Findings`，来源工具自动记录）。
- **跨工具锚定**：`forge task attach --ref <ref> --tool pi` 把当前工具的 session 锚定到 task——任意工具 resume 即知"谁参与过、用什么工具"。

AI_CONTEXT.md 降级为 forge task 的 markdown 导出视图（`forge task context` 输出），供无 forge 的工具/人类阅读。forge task 是结构化真相源（可查询、抗压缩丢失、跨工具双向锚定），AI_CONTEXT.md 是靠纪律维护的文本（易漂移、难查询）。两者信息结构同构（Decisions/Findings/Handoff），但 forge task 持久化进 task 而非靠 agent 自觉读写 md。

## AI_CONTEXT.md 标准结构

```markdown
# AI_CONTEXT — <项目名>

> 本文件是多 AI 工具协作的共同上下文。任何工具开工前先读，有产出后追加。
> 最后更新：@<工具名> <YYYY-MM-DD HH:MM>

## Current Handoff（当前交接状态）
[当前主线任务 + 进度 + 下一步，见 session-continuity 的 HANDOFF 格式]

## Decisions（已确认的决策）
- [日期] <决策内容> — 由 <工具/人> 确认，影响 <文件/模块>
- ...

## Findings（各工具发现的问题，未决）
- [日期][<来源工具>] <问题> — 状态: open/fixed/wontfix
  - 证据: <文件:行 / 命令输出>
  - 影响: <哪些模块>

## Changes（近期文件修改记录）
- [日期][<工具>] <文件> — <改了什么，是否验证>
- ...

## Open Questions（待各工具/人确认）
- [ ] <问题> — 阻塞 <什么>
```

## 工作流

### 开工前（任何工具，任何会话）

**第一步永远先读 `AI_CONTEXT.md`**（项目根），不是直接干活：

1. 读 Current Handoff → 知道当前主线和进度
2. 读 Decisions → 不推翻已确认的决策
3. 读 Findings → 知道其他工具发现但未修的问题（别重复发现）
4. 读 Changes → 知道最近哪些文件被改过（别覆盖）

### 工作中（发现问题/做决策/改文件时）

实时追加到对应章节（用 edit 工具精确追加，不重写全文）：

- **发现 bug/架构问题** → 追加到 `## Findings`，标 `[open]`
- **做了技术决策** → 追加到 `## Decisions`
- **修改了文件** → 追加到 `## Changes`
- **有不确定的问题** → 追加到 `## Open Questions`

每条记录**必须标来源工具**（`[pi]`/`[claude]`/`[deveco]`/`[zcode]`），让其他工具知道是谁发现的。

### 会话结束/切换工具时

更新 `## Current Handoff`（用 session-continuity 的标准 HANDOFF 格式），让下一个工具冷启动能续做。

## 决策树

```
用户要在项目里用多个工具
├─ 项目根有 AI_CONTEXT.md？
│  ├─ 有 → 开工前先读它
│  └─ 无 → 创建（用上面的标准结构），首次填充当前状态
├─ 发现了问题/做了决策
│  ├─ 只在本工具会话内相关？→ 不必写，口头说
│  └─ 其他工具也会遇到/需要知道？→ 追加到 AI_CONTEXT.md
├─ 用户说"把这个分析给 deveco 看"
│  ├─ 旧做法：复制粘贴分析全文给 deveco
│  └─ 新做法：把分析写进 AI_CONTEXT.md 的 Findings，让 deveco 自己读
└─ 会话结束
   └─ 更新 Current Handoff
```

## Gotchas（高信号）

### 问题: 把 AI_CONTEXT.md 当日志记流水账
**现象**: 每次 read/write 都记一条，文件膨胀到几千行没人读
**解决**: 只记**跨工具有价值**的信息：决策、未决问题、关键修改。日常 read/build 不记。"其他工具会遇到吗？"——会才记

### 问题: 工具间信息格式不统一
**现象**: pi 写的分析是 markdown，deveco 期待结构化字段，zcode 给纯文本
**解决**: 统一用上面的标准结构（markdown 章节 + 每条带 `[日期][工具]` 前缀）。所有工具都能读 markdown

### 问题: 忘记读就开始干，覆盖了别人的修改
**现象**: claude 改了 A 文件，deveco 没读 AI_CONTEXT 直接重写 A 文件，丢改动
**解决**: 开工前读 Changes 章节。改文件前 `git status` + 读 AI_CONTEXT 双确认

### 问题: 把 AI_CONTEXT.md 和 HANDOFF.md 搞混
**现象**: 不知道写哪个
**解决**: AI_CONTEXT.md 是**项目级长期**共同上下文（决策/问题/修改历史）；HANDOFF 是**任务级临时**交接（当前做到哪）。可以把 Current Handoff 节直接放 AI_CONTEXT.md 里，不必分两文件。详见 session-continuity

## 与其他 skill 的分工

- **session-continuity**：单工具内跨会话恢复。本 skill 是跨工具。两者用同一套 HANDOFF 格式（session-continuity 定义标准，本 skill 复用）
- **session-retrospective**：把教训写进记忆文件（AGENTS.md/CLAUDE.md）。本 skill 写进项目 AI_CONTEXT.md（项目级，非全局记忆）

## 适用边界

- ✅ 多工具开发同一项目（pi+claude+deveco 协作一个鸿蒙项目）
- ✅ 把 A 工具分析喂给 B 工具决策（review 证据：deveco prompt"其他 agent 分析出的问题"）
- ❌ 单工具单会话（不需要，上下文在会话内）
- ❌ 临时一次性问题（直接说，不必落文件）
