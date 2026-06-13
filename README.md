# Forge — AI 开发质量门禁引擎

AI 写的代码，你放心直接提交吗？

Forge 在 AI 编码过程中自动插入结构化质量门禁——从任务创建到代码提交，确保每一步产出物都经过验证。配合 Claude Code 的 Hook 系统实现实时拦截，不需要你手动检查。

## 核心功能

- **项目级管道** — 从需求定义到发布的全流程质量门禁，按项目规模自动选择
- **任务级门禁** — 每个开发任务走 5 道轻量门禁：理解 → 方案 → 实现 → 验证 → 完成
- **实时 Hook 拦截** — 8 个内置 Hook，在 AI 写代码的同时自动检查质量、防止绕过
- **安全纵深防御** — 三层防御架构：工具拦截 → 文件监控 → 自身保护
- **跨项目经验库** — 提取项目踩坑经验，在新项目中自动提示
- **质量评分** — 每个任务完成后自动评分，量化 AI 编码质量

## 快速开始

需要 [Claude Code](https://docs.anthropic.com/en/docs/claude-code) 已安装。

```bash
# 安装
npm install -g @agentfare/forge

# 在项目目录初始化
cd your-project
forge init

# 在 Claude Code 中开始工作
# AI 会自动读取 Forge 生成的 Skill 并驱动门禁流程
```

初始化后 Forge 会创建：
- `.forge/` — 管道定义、Hook 脚本、任务状态
- `.claude/settings.local.json` — Hook 集成配置
- `.claude/CLAUDE.md` — 质量协议引用
- `.claude/skills/` — 管道编排 Skill

## 工作流程

### 项目级管道

```
forge init → 自动检测项目规模 → 生成门禁管道

gate-1-prd (需求定义) → gate-3-plan (实现计划) → gate-4-implement (代码实现) → gate-5-test (测试验证) → gate-6-acceptance (项目验收)
```

`forge init` 根据项目规模自动选择门禁数量（small / medium / large），`forge status` 查看所有启用的 gate。

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
| **task-guard** | Write/Edit 前 | 阻止无活跃任务时写代码，保护 Forge 配置不被篡改 |
| **bash-guard** | Bash 前 | 检测命令中的写文件模式（writeFile、cat >、sed -i 等），无任务时阻止 |
| **assertion-check** | Write/Edit 后 | 检测断言弱化（t.Fatal → t.Log、assert! 被删除等） |
| **auto-compile** | Write/Edit 后 | 自动编译检查（Go、Rust、TypeScript） |
| **experience-check** | Write/Edit 后 | 匹配经验库中的踩坑模式并提示 |
| **file-sentinel** | Bash 后 | 监控文件变更，发现未授权修改自动 revert |
| **tool-track** | 工具使用后 | 记录工具使用数据，用于门禁活动检查和评分 |
| **task-verify** | 会话结束前 | 最终检查：任务门禁、主分支保护、二进制版本 |

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
| `forge init [--mode small\|medium\|large]` | 初始化管道（自动检测项目规模） |
| `forge status [--json]` | 查看管道状态 |
| `forge gate <gate-id>` | 验证指定门禁 |
| `forge validate` | 检查 pipeline.yml 配置 |
| `forge verify` | 项目完整性检查 + 回归测试 |
| `forge snapshot` | 检测项目开发阶段 |
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
npm install -g @agentfare/forge

# 或从 GitHub Releases 下载二进制
# https://github.com/MjxUpUp/forge/releases

# 支持平台：macOS (x86_64/ARM64)、Linux (x86_64/ARM64)、Windows (x86_64)
```

## 项目门禁管道

| Gate ID | 名称 | small | medium | large |
|---------|------|:-----:|:------:|:-----:|
| gate-1-prd | 需求定义 | ✓ | ✓ | ✓ |
| gate-3-plan | 实现计划 | ✓ | ✓ | ✓ |
| gate-4-implement | 代码实现 | | ✓ | ✓ |
| gate-5-test | 测试验证 | | ✓ | ✓ |
| gate-6-acceptance | 项目验收 | | ✓ | ✓ |
| gate-8-release | 发布 | | | ✓ |
| gate-0-research | 立项调研 | | | ✓ |
| gate-2-design | 技术方案 | | | ✓ |
| gate-7-archive | 经验归档 | | | ✓ |

## License

MIT
