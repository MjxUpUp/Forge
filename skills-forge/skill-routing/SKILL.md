---
name: skill-routing
description: "skill 软路由（提示注入）：把用户输入按关键词映射到对应 skill，命中即注入提示引导 agent 走 skill，避免 agent 瞎搞不走 skill。机制上限是软注入（无支持宿主能硬改写输入），不是硬强制。Use when: 配置/排查/理解各 agent(claude/cursor/codex) 的 skill 路由机制时、改路由表时、路由不生效排查时、给新 agent 接 skill 路由时、用户输入没命中预期 skill 时。SKIP: 写具体 skill 内容（用 skill-authoring-standard）、skill 质量审查（用 forge skills audit）、跨会话上下文恢复（用 session-continuity）。"
metadata:
  pattern: tool-wrapper
  domain: skill-engineering
  requires_forge: "true"
---

# Skill 软路由（提示注入）

解决"agent 有时不走 skill 自己瞎搞"。核心思路：**单一真相源（共享路由表）+ 各 agent 薄适配层**，路由规则只维护一份，所有 agent 消费同一份。

**能力上限先说清**：本机制是软路由——命中后注入提示引导 agent 走 skill，不能硬改写用户输入强制展开（详见「各 agent 机制差异」节，当前无支持宿主具备硬 transform 能力）。名字里的"路由"指提示注入，不是拦截式强制。

## 架构：单一真相 + 多 agent 适配

```
<skills-root>/skill-routing/       ← CANONICAL（forge skills adapters 分发的真相源）
├── routes.json                           # 共享路由表（关键词→skill 映射，只收录已分发 skill）
├── scripts/route-match.sh                # 共享匹配引擎（bash，所有适配层调用）
├── scripts/check-routes.sh               # 发布守卫（校验 routes.json 引用的 skill 都存在于 skills/）
├── examples/personal-overlay.example.json # 个人覆盖层示例（未分发 skill 的路由放这里）
└── adapters/                             # 各 agent 适配层源
    ├── claude/skill-router-claude.sh     # → ~/.claude/hooks/ (UserPromptSubmit additionalContext)
    ├── cursor/skill-routing.mdc          # → ~/.cursor/rules/ (alwaysApply)
    └── codex/README.md                   # → AGENTS.md 注入片段（Codex 无 hook）
```

> `<skills-root>` = `$FORGE_SKILLS_CANONICAL`（开发者显式覆盖）或 `~/.forge/skills-cache/embedded`（forge 二进制自带快照，跨机器通用）。同 codex/cursor adapter 定义。

## 各 agent 机制差异（决定提示强度，必须先理解）

| Agent | 机制 | 强度 | 能改写输入？ |
|---|---|---|---|
| **Claude Code** | UserPromptSubmit hook `additionalContext` 注入 | 中 | ❌ 只能注入提示/阻断，不能改写 |
| **Cursor** | `rules/*.mdc` alwaysApply | 软 | ❌ 纯 prompt |
| **Codex** | AGENTS.md 文字 | 软（最弱） | ❌ 纯 prompt，无命中针对性 |

**关键事实（实测）**：当前 3 家支持 agent（claude/cursor/codex）都**做不到改写用户输入强制展开 skill**——只能注入提示/阻断/纯 prompt。这是机制上限，不是配置问题。曾仅 pi 能硬 transform 输入（`input` 事件改写成 `/skill:name <原文>`），pi 已退出专精名单（commit 34c68b8），硬强制路径暂无支持 agent。

**未覆盖宿主**（Kimi Code、Cline、Copilot、Windsurf 等）：adapters/ 暂无专用适配层。这些宿主里凡读 AGENTS.md 的，走 codex 同款 AGENTS.md 文字注入兜底（软中最软，无命中针对性）；要更精确的注入需各宿主的 hook/规则机制，接入时按本架构补一个薄适配层即可。

## 共享路由表（routes.json）

```json
[
  { "match": ["代码审查", "code review", "审查代码"], "skill": "code-review-gate", "reason": "纯代码审查" }
]
```

- `match`：关键词数组，任一命中（子串匹配，不区分大小写）即触发
- `skill`：注入的目标 skill 名
- `reason`：仅记录，用于提示

**canonical 约束**：routes.json 只收录指向本库 `skills/` 目录已分发 skill 的路由。个人/环境专属路由（如依赖本机另装的 lark-* skill 族）**不要进 canonical**——复制 [examples/personal-overlay.example.json](examples/personal-overlay.example.json) 到 `~/.forge/skill-routes.json` 作覆盖层（路由表源解析的末位候选，见下）。覆盖层引用的 skill 不在分发库内，换机器/未安装即为死路由，自行维护。

**发布守卫**：改 routes.json 后跑 `bash scripts/check-routes.sh`——校验每个被引用 skill 的 `skills/<name>/SKILL.md` 存在，有死路由即 exit 1 拦下发布。

**改路由就改这一个文件**，然后 `forge skills adapters --apply` 分发（claude/cursor 重启会话生效）。

## 匹配引擎（scripts/route-match.sh）

各 bash 适配层（Claude/Codex 兜底）调用本引擎做匹配，逻辑只维护一份：

```bash
# 输入 prompt（参数或 stdin），输出 "skill|reason" 或空（exit 1）
echo "查日程" | bash scripts/route-match.sh
# → lark-calendar|日程/会议室类
```

**三条防误触发**：跳过 `/` 开头（尊重显式命令）、跳过 <2 字符、跳过 extension 源消息。

## 适配层职责分工

### Claude Code（adapters/claude/skill-router-claude.sh）—— 中等（当前最强）
UserPromptSubmit hook，命中即输出 `additionalContext` JSON，注入"必须先 read SKILL.md"的引导提示。Claude 不能改写输入，只能针对性命中注入。注册在 `~/.claude/settings.json` 的 `hooks.UserPromptSubmit`。

### Cursor（adapters/cursor/skill-routing.mdc）—— 软
alwaysApply 规则，路由表内联（adapters 部署时从 routes.json 同步生成）。Cursor 无 shell hook，纯 prompt 软强制。部署到 `~/.cursor/rules/`。

### Codex（adapters/codex/README.md）—— 最弱
Codex 不消费 SKILL.md、无 hook，唯一入口是 AGENTS.md。只能把路由表作为文字段落注入 AGENTS.md（项目根 `AGENTS.md`，跨 agent 通用指令源）。

## 与其他 skill 的分工

- **本 skill 路由层**：确定性关键词→skill 映射——管精确命中（命中即注入/提示对应 skill）
- **AGENTS.md / CLAUDE.md 协议层**：通用软提醒"有 skill 体系，该查"——管模糊意图
- 两者并存：协议层管广度，router 管精度

## 易错点（Gotchas）

- **Claude 不能改写输入**：UserPromptSubmit 只支持 `additionalContext`（注入）和 `decision:block`（阻断），没有 `updatedInput`。当前无支持 agent 能硬 transform 输入。
- **Codex 完全不读 SKILL.md**：`forge skills adapters` 的部署目标不含 codex，Codex 路由只能进 AGENTS.md 文字。
- **jq @tsv 在 git-bash 带 CR**：route-match.sh 必须剥离 `\r`，否则中文关键词匹配全失败（已处理）。
- **`_comment` 字段别和 match 混在一个对象**：routes.json 的注释要独立成对象，否则污染 match 数组。
- **Cursor mdc 路由表是生成的**：手动改 mdc 会被重新部署覆盖，改 routes.json 再 `forge skills adapters --apply`。
- **AGENTS.md 是跨 agent 通用指令源**：给 Codex 注入路由表写入项目根 AGENTS.md，所有读 AGENTS.md 的 agent（codex/cursor/copilot/windsurf/cline）都获得。
- **路由命中优先级 = routes.json 顺序**：靠前的先匹配，调整顺序即调优先级。
- **canonical 不得引用未分发 skill**：routes.json 里指向 `skills/` 不存在 skill 的条目对外部用户是死路由——个人路由放覆盖层 `~/.forge/skill-routes.json`，发布前跑 scripts/check-routes.sh。

## 路由表源解析（各 adapter 候选链，前者不存在则降级）

1. `$FORGE_SKILLS_CANONICAL/skill-routing/routes.json` — 开发者显式覆盖（本机调试时 export 指自己 fork 的 skill 库根：`export FORGE_SKILLS_CANONICAL=<你的 skills 根目录>`）
2. `~/.forge/skills-cache/embedded/skill-routing/routes.json` — forge 二进制自带快照（跨机器通用，随 release 更新，可能滞后于源）
3. `~/.forge/skill-routes.json` — 用户级自定义路由（手动覆盖）

不把本机源路径硬编码进候选链——普通用户机器没有开发者的本地路径；开发者本机想拿最新路由，显式设 `$FORGE_SKILLS_CANONICAL`。

## 部署与同步

```bash
# 改路由表（开发者本机先 export FORGE_SKILLS_CANONICAL=<你的 skills 根>）
vim "$FORGE_SKILLS_CANONICAL/skill-routing/routes.json"

# 发布守卫：被引用 skill 必须都存在于 skills/，有死路由即 exit 1
bash "$FORGE_SKILLS_CANONICAL/skill-routing/scripts/check-routes.sh"

# forge 分发适配层资产（claude/cursor adapter + routes.json 一次部署）
forge skills adapters --apply

# Claude/Cursor 重启会话生效
```

## 参考

- 各 agent 机制实测依据：Claude Code 官方 hooks 文档（UserPromptSubmit additionalContext，无 updatedInput）、Cursor rules 规范、Codex AGENTS.md 约定
- 共享匹配引擎：[scripts/route-match.sh](scripts/route-match.sh)
- 发布守卫：[scripts/check-routes.sh](scripts/check-routes.sh)
- 路由表真相源：[routes.json](routes.json)（canonical，只含已分发 skill）
- 个人覆盖层示例：[examples/personal-overlay.example.json](examples/personal-overlay.example.json)
