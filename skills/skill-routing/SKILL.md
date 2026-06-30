---
name: skill-routing
description: "强制 skill 路由：把用户输入按关键词映射到对应 skill，避免 agent 瞎搞不走 skill。Use when: 配置/排查/理解各 agent(pi/claude/cursor/codex) 的 skill 强制路由机制时、改路由表时、路由不生效排查时、给新 agent 接 skill 路由时、用户输入没命中预期 skill 时。SKIP: 写具体 skill 内容（用 skill-authoring-standard）、skill 质量审查（用 registry.py）、跨会话上下文恢复（用 session-continuity）。"
metadata:
  pattern: tool-wrapper
  domain: agent-routing
---

# 强制 Skill 路由

解决"agent 有时不走 skill 自己瞎搞"。核心思路：**单一真相源（共享路由表）+ 各 agent 薄适配层**，路由规则只维护一份，所有 agent 消费同一份。

## 架构：单一真相 + 多 agent 适配

```
E:\Forge\skills\skill-routing\       ← CANONICAL（sync 分发的真相源）
├── routes.json                           # 共享路由表（关键词→skill 映射）
├── scripts/route-match.sh                # 共享匹配引擎（bash，所有适配层调用）
└── adapters/                             # 各 agent 适配层源
    ├── pi/index.ts                       # → ~/.pi/agent/extensions/skill-router/ (input transform)
    ├── claude/skill-router-claude.sh     # → ~/.claude/hooks/ (UserPromptSubmit additionalContext)
    ├── cursor/skill-routing.mdc          # → ~/.cursor/rules/ (alwaysApply)
    └── codex/README.md                   # → AGENTS.md 注入片段（Codex 无 hook）
```

## 各 agent 机制差异（决定强制强度，必须先理解）

| Agent | 机制 | 强度 | 能改写输入？ |
|---|---|---|---|
| **pi** | extension `input` 事件 transform | **硬强制** | ✅ 改写成 `/skill:name <原文>` |
| **Claude Code** | UserPromptSubmit hook `additionalContext` 注入 | 中 | ❌ 只能注入提示/阻断，不能改写 |
| **Cursor** | `rules/*.mdc` alwaysApply | 软 | ❌ 纯 prompt |
| **Codex** | AGENTS.md 文字 | 软（最弱） | ❌ 纯 prompt，无命中针对性 |

**关键事实（实测）**：除 pi 外，其他 agent 都**做不到改写用户输入强制展开 skill**。这是它们的机制上限，不是配置问题。给它们上"等价硬路由"是不可能的——只能走各自支持的最强机制。

## 共享路由表（routes.json）

```json
[
  { "match": ["日程", "会议室", "忙闲"], "skill": "lark-calendar", "reason": "日程/会议室类" }
]
```

- `match`：关键词数组，任一命中（子串匹配，不区分大小写）即触发
- `skill`：transform/注入的目标 skill 名
- `reason`：仅记录，用于提示

**改路由就改这一个文件**，然后 `sync.py --apply` 分发（pi `/reload`，其他 agent 重启会话生效）。

## 匹配引擎（scripts/route-match.sh）

各 bash 适配层（Claude/Codex 兜底）调用本引擎做匹配，逻辑只维护一份：

```bash
# 输入 prompt（参数或 stdin），输出 "skill|reason" 或空（exit 1）
echo "查日程" | bash scripts/route-match.sh
# → lark-calendar|日程/会议室类
```

**三条防误触发**：跳过 `/` 开头（尊重显式命令）、跳过 <2 字符、跳过 extension 源消息。

## 适配层职责分工

### pi（adapters/pi/index.ts）—— 硬强制
监听 `input` 事件，命中即 `transform` 成 `/skill:name <原文>`，pi 强制展开 skill 注入上下文。agent 绕不开。详见 extension 内联文档。

### Claude Code（adapters/claude/skill-router-claude.sh）—— 中等
UserPromptSubmit hook，命中即输出 `additionalContext` JSON，注入"必须先 read SKILL.md"的强制提示。Claude 不能改写输入，只能针对性命中注入。注册在 `~/.claude/settings.json` 的 `hooks.UserPromptSubmit`。

### Cursor（adapters/cursor/skill-routing.mdc）—— 软
alwaysApply 规则，路由表内联（sync 时从 routes.json 生成）。Cursor 无 shell hook，纯 prompt 软强制。部署到 `~/.cursor/rules/`。

### Codex（adapters/codex/README.md）—— 最弱
Codex 不消费 SKILL.md、无 hook，唯一入口是 AGENTS.md。只能把路由表作为文字段落注入 AGENTS.md（canonical `~/.agents/AGENTS.md`，4 处硬链接统一）。

## 与其他 skill 的分工

- **skill-enforcer**（pi extension）：通用软提醒"有 skill 体系，该查"——管模糊意图
- **本 skill 路由层**：确定性关键词→skill 映射——管精确命中
- 两者并存：enforcer 管广度，router 管精度

## 易错点（Gotchas）

- **Claude 不能改写输入**：UserPromptSubmit 只支持 `additionalContext`（注入）和 `decision:block`（阻断），没有 `updatedInput`。别幻想做等价 pi 的硬 transform。
- **Codex 完全不读 SKILL.md**：sync.py 的 TARGETS 不含 codex，Codex 路由只能进 AGENTS.md 文字。
- **jq @tsv 在 git-bash 带 CR**：route-match.sh 必须剥离 `\r`，否则中文关键词匹配全失败（已处理）。
- **`_comment` 字段别和 match 混在一个对象**：routes.json 的注释要独立成对象，否则污染 match 数组。
- **Cursor mdc 路由表是生成的**：手动改 mdc 会被 sync 覆盖，改 routes.json 再 sync。
- **AGENTS.md 是硬链接统一的**：给 Codex 注入路由表会让 pi/家目录 AGENTS.md 也获得，对 pi 无害（pi 有更强 extension）。
- **路由命中优先级 = routes.json 顺序**：靠前的先匹配，调整顺序即调优先级。

## 路由表源解析（各 adapter 候选链，前者不存在则降级）

1. `$FORGE_SKILLS_CANONICAL/skill-routing/routes.json` — 开发者显式覆盖（如指自己 fork 的 skill 库）
2. `E:\Forge\skills\skill-routing\routes.json` — 本机源（最新）
3. `~/.forge/skills-cache/embedded/skill-routing/routes.json` — forge 二进制自带快照（跨机器通用，随 release 更新，可能滞后于源）
4. `~/.pi/agent/skill-routes.json` — pi 标准位置（旧 fallback）

普通用户机器无 E:\Forge，走 #3 embedded。开发者本机 #2 优先拿最新路由。

## 部署与同步

```bash
# 改路由表
vim E:\Forge\skills\skill-routing\routes.json

# sync.py 分发（扩展后支持适配层资产，见 sync.py 的 ADAPTERS 逻辑）
cd ~/.agents/scripts && python sync.py --apply

# pi 热加载
# TUI 内 /reload

# Claude/Cursor 重启会话生效
```

## 参考

- 各 agent 机制实测依据：pi docs/extensions.md（input 事件）、Claude Code 官方 hooks 文档（UserPromptSubmit additionalContext，无 updatedInput）、Cursor rules 规范、Codex AGENTS.md 约定
- 共享匹配引擎：[scripts/route-match.sh](scripts/route-match.sh)
- 路由表真相源：[routes.json](routes.json)
