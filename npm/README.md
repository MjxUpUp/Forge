# Forge — AI 开发质量门禁引擎

AI 写的代码，你放心直接提交吗？

Forge 在 AI 编码过程中自动插入结构化质量门禁——从任务创建到代码提交，确保每一步产出物都经过验证。配合 Claude Code 的 Hook 系统实现实时拦截，不需要你手动检查。

## 核心功能

- **任务级门禁** — 每个开发任务走 3 道门禁：实现 → 验证 → 完成
- **实时 Hook 拦截** — 多个内置 Hook，在 AI 写代码的同时自动检查质量、防止绕过
- **安全纵深防御** — 三层防御架构：工具拦截 → 文件监控 → 自身保护
- **跨项目经验库** — 提取项目踩坑经验，在新项目中自动提示
- **质量评分** — 每个任务完成后自动评分，量化 AI 编码质量

## 快速开始

需要 [Claude Code](https://docs.anthropic.com/en/docs/claude-code) 已安装。

```bash
# 安装
npm install -g @agent_forge/forge

# 在项目目录初始化
cd your-project
forge init

# 在 Claude Code 中开始工作
# AI 会自动读取 Forge 生成的 Skill 并驱动门禁流程
```

初始化后 Forge 会创建：
- `.forge/` — Hook 脚本、任务状态、协议配置
- `.claude/settings.local.json` — Hook 集成配置
- `.claude/CLAUDE.md` — 质量协议引用
- `.claude/skills/` — 质量协议 Skill

## 工作流程

### 任务级门禁

每个开发任务自动走 3 道门禁：

```bash
forge task start --ref feat/add-login --branch   # 创建任务 + 分支
# AI 自动完成工作...
forge task gate task-implement    # ✅ 代码实现（自动检查编译 + 断言）
forge task gate task-verify       # ✅ 测试验证
forge task gate task-complete     # ✅ 完成确认
forge task score                  # 查看质量评分
```

门禁之间有时间和活动检查，防止 AI 跳过阶段直接提交。`task-implement` 是自动门禁，会检查编译通过和断言未被弱化。

## Hook 系统

Forge 通过 Claude Code 的 Hook 机制实现实时质量检查：

| Hook | 触发时机 | 功能 |
|------|----------|------|
| **task-guard** | Write/Edit 前 | 无活跃任务时 WARN（仅 `.forge/*` 自保护文件 FAIL），保护 Forge 配置不被篡改 |
| **assertion-check** | Write/Edit 前 | 检测断言弱化（t.Fatal → t.Log、assert! 被删除等），advisory 提醒不阻塞（agent 自检） |
| **bash-guard** | Bash 前 | 检测命令中的写文件模式（writeFile、cat >、sed -i 等），无任务时 WARN（源码随后被 file-sentinel 隔离） |
| **auto-compile** | Write/Edit 后 | advisory 提醒用对应技术栈编译命令自检（go build / cargo check / mvn / tsc 等），不强制编译 |
| **file-sentinel** | Bash 后 | 监控文件变更，未授权修改隔离到 DataDir/quarantine/（`forge data-dir` 查看路径，可恢复，不删除） |
| **tool-track** | Read 后 | 静默记录 Read 调用到 toollog，供 task-verify 的 read-before-edit 门禁判断（agent 是否先读代码再改） |
| **task-verify** | 会话结束 | advisory：任务门禁/主分支保护/mandatory review 提醒到 stderr+checklog（不阻塞会话结束） |
| **skill-scan** | 会话开始 | advisory：扫描 ~/.claude/skills 安全性（forge audit 19 规则），补 install 门控缺口（手动 clone/junction/git pull 进入的 skill），全局 hook 不依赖 forge project |
| **task-resume** | 会话开始 | advisory：自动注入活跃任务的接续上下文（目标/计划/决策/阻塞/门禁进度/git 已改未提交）+ 锚定当前 session——接手方冷启动即知任务在哪一步；无活跃任务静默；项目级 hook |

### 安全架构

三层纵深防御，监控的是文件而非工具：

```
Layer 1: PreToolUse 快速拦截
  ├─ task-guard: Write/Edit → 检查任务状态 + 保护 .forge/*
  └─ bash-guard: Bash → 检测写文件模式

Layer 2: PostToolUse 文件监控
  └─ file-sentinel: Bash → 对比执行前后 git 状态，未授权变更自动 revert

Layer 3: 会话结束验证
  └─ task-verify: 检查任务完成度 + 主分支保护 + 自身版本
```

Agent 无法通过 `node -e "fs.writeFileSync()"`、`cat > file`、直接编辑 task JSON 等方式绕过——bash-guard 拦截工具层，file-sentinel 监控文件层，task-guard 保护配置层。

## 命令参考

### 项目管理

| 命令 | 说明 |
|------|------|
| `forge init` | 初始化项目（生成 `.forge/` 资产、Hook、质量协议 Skill；旧的 `--mode` 标志已废弃为 no-op） |
| `forge status [--json]` | 查看项目状态（任务管道 + 质量信号） |
| `forge verify` | 项目完整性检查 + 回归测试 |
| `forge update` | 自更新到最新版本 |

### 任务管理

| 命令 | 说明 |
|------|------|
| `forge task start --ref <type/desc> --branch` | 创建任务（自动创建分支） |
| `forge task status` | 查看当前任务门禁状态 |
| `forge task list` | 列出所有任务 |
| `forge task gate <gate-id>` | 验证单道任务门禁 |
| `forge task complete` | 标记任务完成（自动评分） |
| `forge task score` | 查看任务质量评分 |

### 经验库

| 命令 | 说明 |
|------|------|
| `forge experience list` | 查看经验条目 |
| `forge knowledge` | 跨项目经验库管理 |

## 安装

```bash
# npm（推荐）
npm install -g @agent_forge/forge

# 或从 GitHub Releases 下载二进制
# https://github.com/MjxUpUp/Forge/releases

# 支持平台：macOS (x86_64/ARM64)、Linux (x86_64/ARM64)、Windows (x86_64)
```

## License

Apache-2.0
