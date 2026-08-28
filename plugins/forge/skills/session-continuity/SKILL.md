---
name: session-continuity
description: "跨会话开发接力。Use when: 恢复项目工作时、从上次会话中断处继续时、用户说“继续”、“恢复”、“上次到哪了”、“接着做”时。SKIP: 当前会话内已有完整上下文时、纯新项目初始化、当前会话内的任务继续（不需要上下文恢复）、**跨不同 AI 工具交接（用 cross-tool-context）**。"
metadata:
  pattern: inversion + pipeline
  domain: workflow
  steps: 6
  适用前提: 多阶段长周期任务或跨会话协作；恢复源按可用性降级——有 forge 任务优先 `forge task resume`，无 forge 走 git/文档/转录完整 pipeline；纯单会话短任务不适用
---

# 跨会话开发接力

从多种来源重建上下文，跨聊天会话恢复工作。对多阶段长周期项目和调研任务至关重要。

## 阶段 0 — 快速评估（Inversion）

在深入上下文恢复之前，先问：

1. "你之前在做什么？（还是让我从 git/文档中推断？）"
2. "是继续之前的任务，还是开始新工作？"

如果用户说"直接继续"或"你自己看"，不再追问，直接进入阶段 1。

## 阶段 1 — 上下文恢复（Pipeline）

> Forge 项目——**优先 `forge task resume`**：若项目有 forge 任务，先 `forge task resume [--ref <ref>]` 拉回结构化接续真相源（目标/计划/已确认决策/下一步/阻塞/参与工具 + 门禁进度 + git 已改未提交）——比下面 parse markdown/转录更快更可靠、抗压缩丢失、跨工具双向锚定。下面的 git/文档/转录步骤作为补充（覆盖 resume 未纳入的工作）。非 forge 项目走完整 pipeline。

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

## 标准 HANDOFF 格式（跨会话/跨工具交接）

> Forge 项目——**forge task 是真相源，HANDOFF.md 是导出视图**：决策/下一步/阻塞优先写进 `forge task decide/next/block`（持久化进 forge task；refactor-data-home 后落用户级 DataDir，跨会话/跨工具 resume 即拉回）。`HANDOFF.md` 降级为 `forge task context` 的文本导出，供无 forge 环境/人类阅读——别再靠纪律手写两份。非 forge 项目 HANDOFF.md 本身就是主载体，按模板维护。

会话结束或切换工具时，写一份结构化 HANDOFF 到项目根 `HANDOFF.md`（或 `AI_CONTEXT.md` 的 `## Current Handoff` 节），让下一个会话/工具能冷启动续做。**统一格式，不要每次手抄不同结构**——完整模板与填写纪律见 [references/handoff-format.md](references/handoff-format.md)，此处不复制。

## 易错点

- **不要假设用户记得**：即使他们说"继续"，也要先呈现上下文。
- **检查合并冲突**：运行 `git fetch && git status`。
- **过时的子代理工作**：检查 `git diff` 和 `git stash list`。
- **计划文件漂移**：从磁盘读取计划，不要从记忆中读——可能已被更新。
- **依赖更新**：如果间隔较长，可能需要 `cargo update` / `pnpm install` / `npm install`。
- **调研任务恢复**：恢复调研时重点检查已有文档的结构和最后编辑位置，不要凭记忆假设内容。若调研产出在飞书（且有 `lark-cli`），用 `lark-cli docs +fetch --doc-format markdown` 获取实际内容（若你的 lark-cli 版本支持 `--scope outline`，可只取结构大纲）；否则读本地 run_dir 的报告文件。
