---
name: session-continuity
description: "跨会话开发接力。Use when: 恢复项目工作时、从上次会话中断处继续时、用户说“继续”、“恢复”、“上次到哪了”、“接着做”时。SKIP: 当前会话内已有完整上下文时、纯新项目初始化、当前会话内的任务继续（不需要上下文恢复）、**跨不同 AI 工具交接（pi→Claude Code→deveco 等，用 cross-tool-context）**。"
metadata:
  pattern: inversion + pipeline
  domain: workflow-management
  steps: 6
---

# 跨会话开发接力

从多种来源重建上下文，跨聊天会话恢复工作。对多阶段长周期项目和调研任务至关重要。

## 阶段 0 — 快速评估（Inversion）

在深入上下文恢复之前，先问：

1. "你之前在做什么？（还是让我从 git/文档中推断？）"
2. "是继续之前的任务，还是开始新工作？"

如果用户说"直接继续"或"你自己看"，不再追问，直接进入阶段 1。

## 阶段 1 — 上下文恢复（Pipeline）

```
上下文恢复：
- [ ] 1. 检查项目状态
- [ ] 2. 阅读项目文档
- [ ] 3. 查看代理转录记录
- [ ] 4. 评估构建/运行状态
- [ ] 5. 呈现摘要
- [ ] 6. 确认恢复点
```

### 步骤 1：项目状态

根据项目类型选择对应命令：

**Git 项目**：
```bash
git log --oneline -20
git status
git diff --stat
```

**非 Git 项目 / 调研任务**：
- 检查工作目录下的产出文件
- 检查飞书文档（如有相关 skill）

### 步骤 2：项目文档

检查以下位置（仅在存在时加载）：
- `docs/specs/` — 设计规格文档
- `docs/plans/` — 实现计划（查找未完成的 `- [ ]` 任务）
- `docs/adr/` — 架构决策记录
- `CHANGELOG.md` — 按阶段记录的已完成工作
- `docs/session-log.md` — 上次会话笔记（如有维护）

### 步骤 3：代理转录记录

阅读最近的父转录记录，了解之前讨论和决定了什么。

### 步骤 4：构建/运行状态

根据项目类型自动选择：

| 项目类型 | 检测方式 | 验证命令 |
|---|---|---|
| Rust | `Cargo.toml` 存在 | `cargo test --workspace 2>&1 \| tail -5` + `cargo clippy --workspace -- -D warnings 2>&1 \| tail -10` |
| Node.js | `package.json` 存在 | `npm test 2>&1 \| tail -5` 或 `pnpm test 2>&1 \| tail -5` |
| Go | `go.mod` 存在 | `go test ./... 2>&1 \| tail -5` |
| Python | `pyproject.toml` 或 `setup.py` 存在 | `pytest 2>&1 \| tail -5` |
| 调研任务 | 飞书文档 / 本地 markdown | 检查已有文档的 outline 和最后编辑位置 |
| 无构建系统 | — | 跳过此步骤 |

### 步骤 5：呈现摘要

**门控：呈现以下结构化摘要。用户确认恢复点前不要开始工作。**

```markdown
## 会话恢复摘要

**项目**：[名称]
**项目类型**：[Rust / Node.js / 调研 / 其他]
**当前阶段**：[第 X 阶段 / 共 Y 阶段]
**上次会话**：[简要描述]
**状态**：[构建通过/失败，测试数量 / 文档完成度]

### 已完成
- [x] 任务 A
- [x] 任务 B

### 下一步
- [ ] 任务 C — [描述]

### 已知问题
- [阻塞项]
```

### 步骤 6：确认并恢复

询问：**"从 [下一个任务] 继续？还是需要调整计划？"**

确认后才开始工作。

## 记忆（持久化）

每次重要会话结束后，追加到 `docs/session-log.md`：

```markdown
## YYYY-MM-DD 会话
- 完成了：[做了什么]
- 阻塞项：[问题]（或"无"）
- 下一步：[下次做什么]
```

### 结构化记忆（Memory 模式）

同时追加一条机器可读记录到 `~/.pi/research/session-history.jsonl`（稳定目录，不随项目丢失）：

```json
{"project":"项目名","date":"YYYY-MM-DD","completed":["完成项"],"blocked":["阻塞项"],"next":["下一步"],"commit":"最新 commit SHA"}
```

**下次恢复时（阶段 1 步骤 1 前）**：先读 `session-history.jsonl` 找到本项目的上次记录，对比 git log 看从上次到现在发生了什么变化——比从零重建上下文更快。

## 标准 HANDOFF 格式（跨会话/跨工具交接）

会话结束或切换工具时，写一份结构化 HANDOFF 到项目根 `HANDOFF.md`（或 `AI_CONTEXT.md` 的 `## Current Handoff` 节），让下一个会话/工具能冷启动续做。**统一格式，不要每次手抄不同结构**：

```markdown
# HANDOFF — <项目名> @ <YYYY-MM-DD HH:MM>

## 当前任务
- 主线：[一句话当前在做什么]
- 进度：[做到哪一步 / 完成度]

## 调用栈（恢复时按序读）
1. <文件:行> — <这个文件当前状态/为什么重要>
2. <文件:行> — <同上>

## 已修改未提交
- <文件> — <改了什么，是否验证过>

## 待验证项
- [ ] <编译/测试/端到端验证还没跑的>

## 已知问题/阻塞
- <问题> — < workaround 或状态>

## 下一步
1. <恢复后第一件事>
```

**关键纪律**：
- HANDOFF 是给**冷启动的下一个会话**看的，不是给自己备忘——写清楚“为什么”不只写“是什么”
- 跨工具交接（pi→deveco 等）时，双方都读写同一份 `AI_CONTEXT.md`，见 **cross-tool-context** skill
- HANDOFF 不是永久文档，任务完成后可删或归档到 `docs/session-log.md`

## 易错点

- **不要假设用户记得**：即使他们说"继续"，也要先呈现上下文。
- **检查合并冲突**：运行 `git fetch && git status`。
- **过时的子代理工作**：检查 `git diff` 和 `git stash list`。
- **计划文件漂移**：从磁盘读取计划，不要从记忆中读——可能已被更新。
- **依赖更新**：如果间隔较长，可能需要 `cargo update` / `pnpm install` / `npm install`。
- **调研任务恢复**：恢复调研时重点检查已有文档的结构和最后编辑位置，不要凭记忆假设内容。用 `lark-cli docs +fetch --scope outline` 获取实际结构。
