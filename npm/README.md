# Forge — AI 开发质量门禁引擎

AI 写的代码，你放心直接提交吗？

Forge 在 AI 编码过程中自动插入结构化质量门禁——从任务创建到代码提交，确保每一步产出物都经过验证。配合 Claude Code 的 Hook 系统实现实时拦截，不需要你手动检查。

## 核心功能

- **任务级门禁** — 每个开发任务走 3 道门禁：实现 → 验证 → 完成
- **实时 Hook 拦截** — 26 个内置 Hook，在 AI 写代码的同时自动检查质量、防止绕过
- **安全纵深防御** — 三层防御架构：工具拦截 → 文件监控 → 自身保护
- **质量评分** — 每个任务完成后自动评分，量化 AI 编码质量

## 快速开始

需要 [Claude Code](https://docs.anthropic.com/en/docs/claude-code) 已安装。

```bash
# 安装
npm install -g @agent_forge/forge

# 在仓库里声明 forge 契约（只写仓库内产物）
cd your-project
forge init

# 可选：登记进全局看板
forge registry add .
```

`forge init` 只写仓库内产物，**不写** `~/.forge`、`~/.claude`、`~/.codex`：
- `spec/forge.yaml` — 契约（profile 缺省 mini、`hooks: off` 兼容字段）
- `spec/changes/`（含 `archive/`） — change 生命周期产物
- `AGENTS.md` — forge 托管块；已存在的 `CLAUDE.md` / `.cursor/rules/` 按默认规则补指针

阶段 D 起 forge **没有宿主层**：不装 hook、不写宿主配置。任务与 change 门禁由 agent 在 shell 里显式跑 `forge task` / `forge change` 命令；需要全局视图（`dashboard`、`task list --all-projects`）时用 `forge registry add` 显式登记。

团队要 git 共享契约：提交 `spec/` 与 `AGENTS.md`。

## 工作流程
## 工作流程

### 任务级门禁

每个开发任务自动走 3 道门禁：

```bash
forge task start --ref feat/add-login --branch   # 创建任务 + 分支
# AI 自动完成工作...
forge task gate task-implement    # ✅ 代码实现（advisory：编译/断言自检提醒）
forge task gate task-verify       # ✅ 测试验证
forge task gate task-complete     # ✅ 完成确认
forge task complete               # 🏁 任务完结（自动评分 + 清 active ref；git commit 在此之前）
forge task score                  # 查看质量评分
```

门禁之间有时间和活动检查，防止 AI 跳过阶段直接提交。`task-implement` 的编译/断言检查为 advisory 提醒（agent 自检，不阻塞）——forge 技术栈无关，适配 loop engineering。

## 质量检查

阶段 D 删除了宿主 hook；检查由 `forge task gate` / `forge change verify` 在进程内确定性执行（编译自检、断言弱化扫描、测试伴随变更、证据封印）。原 26 个 hook 清单已随宿主层移除。

## 安全架构

三层纵深防御，监控的是文件而非工具：

```
Layer 1: PreToolUse 快速拦截
  ├─ task-guard: Write/Edit → 检查任务状态 + 自保护（forge 配置层）
  └─ bash-guard: Bash → 检测写文件模式

Layer 2: PostToolUse 文件监控
  └─ file-sentinel: Bash → 对比执行前后 git 状态，未授权变更隔离到 quarantine（可恢复，不删除）

Layer 3: 会话结束验证
  └─ task-verify: 检查任务完成度 + 主分支保护 + 自身版本
```

活跃任务内，Agent 无法通过 `node -e "fs.writeFileSync()"`、`cat > file`、直接编辑 task JSON 等方式绕过——bash-guard 拦截工具层，file-sentinel 监控文件层，task-guard 保护配置层；无活跃任务时 task-guard 只 WARN。

## 命令参考

### 项目管理

| 命令 | 说明 |
|------|------|
| `forge init [--profile mini\|standard\|full]` | 在仓库声明契约：只写 `spec/forge.yaml`、`spec/changes/`、`AGENTS.md` 托管块与已存在指针，不写用户主目录；`--agents`/`--project` 已废弃为 no-op（保留旧脚本兼容） |
| `forge status [--json]` | 查看项目状态（任务管道 + 质量信号） |
| `forge verify` | 项目完整性检查 + 回归测试 |
| `forge update` | 检查并更新到最新版本（按安装通道分流：npm 安装查 npm registry 并打印对应包管理器的更新命令——npm 包不可变，不做原地替换；GitHub Release 安装则从 GitHub 下载自替换） |

### 任务管理

| 命令 | 说明 |
|------|------|
| `forge task start --ref <type/desc> --branch` | 创建任务（自动创建分支） |
| `forge task status` | 查看当前任务门禁状态 |
| `forge task list` | 列出所有任务 |
| `forge task gate <gate-id>` | 验证单道任务门禁 |
| `forge task complete` | 标记任务完成（自动评分） |
| `forge task score` | 查看任务质量评分 |

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
