# Forge — AI 开发质量门禁引擎

AI 写的代码，你放心直接提交吗？

Forge 在 AI 编码过程中插入结构化质量门禁——从任务创建到代码提交，确保每一步产出物都经过验证。质量检查在 `forge task gate` / `forge change verify` 进程内执行，没有实时拦截层，写操作不经过 forge。

## 核心功能

- **任务级门禁** — 每个开发任务走两道门禁（实现、完成）并实跑验收
- **change 证据封印** — `forge change verify` 实跑 acceptance 判据，证据绑定 commit/diff 并随仓库走；改文件即失效
- **无拦截层** — 写操作不经过 forge，质量只能由 gate/verify 与 CI 检查
- **测试账本** — `forge test ledger` 给出测试四态（活 / 孤儿 / 被跳过 / 幻影），Skip 必须写理由并审批
- **能力面闭合** — `forge docs closure` 把命令树与活文档对账，文档教了不存在的命令就报漂移

## 快速开始

```bash
# 安装
npm install -g @agent_forge/forge

# 在仓库里声明 forge 契约（只写仓库内产物）
cd your-project
forge init

# 立一个 change（goal / change / acceptance 三份产物）
forge change new demo
```

`forge init` 只写仓库内产物，**不写** `~/.forge`、`~/.claude`、`~/.codex`：
- `spec/forge.yaml` — 契约（profile 缺省 mini、`hooks: off` 兼容字段）
- `spec/changes/`（含 `archive/`） — change 生命周期产物
- `AGENTS.md` — forge 托管块；已存在的 `CLAUDE.md` / `.cursor/rules/` 按默认规则补指针

forge **没有宿主层**：不分发宿主插件、不写宿主配置。任务与 change 门禁由 agent 在 shell 里显式跑 `forge task` / `forge change` 命令驱动。

团队要 git 共享契约：提交 `spec/` 与 `AGENTS.md`。

## 工作流程

### 任务级门禁

每个开发任务走两道门禁并实跑验收：

```bash
forge task start --ref feat/add-login --branch --accept "go test ./... :: ok"
# AI 完成工作...
forge task gate task-implement     # ✅ 代码实现（确有代码改动）
forge task verify-acceptance       # ✅ 实跑验收判据，记录确定性证据
git commit -am "..."               # 提交后再评审
forge review pass --reviewer r1 --criteria-reviewed 1
forge task gate task-complete      # ✅ 完成确认（review pass 硬前置）
forge task complete                # 🏁 任务完结（清 active ref）
forge task report --ref feat/add-login   # 交付单
```

门禁之间有时间和活动检查，防止 AI 跳过阶段直接提交。`task-implement` 的编译/断言检查为 advisory 提醒（agent 自检，不阻塞）——forge 技术栈无关，适配 loop engineering。

### change 证据

```bash
forge change new demo          # 生成 goal.md / change.md / acceptance.md
# 填完三份产物、删掉 forge:fill 标记
forge change clarify demo --by-human --note "边界与非目标"
forge change phase demo        # 看还缺什么
forge change verify demo       # 实跑判据并封印证据到 evidence/<UTC>/
```

判据里的 `-run` 选择器匹配不到任何测试时按失败计（go test 退出 0 也不算通过），避免空过。

## 质量检查

检查由 `forge task gate` / `forge change verify` 在进程内确定性执行（编译自检、断言弱化扫描、测试伴随变更、证据封印）。

`forge docs lint <file>` 是文档结构闸；`forge docs closure --strict` 是命令面与活文档的漂移闸（有漂移退出非 0），适合放进 CI。

## 安全边界

没有拦截层：写操作不经过 forge，绕过只能靠 CI required check 与 review。`forge change verify` 的证据绑定提交内容，改码后自动失效；要在 GitHub 上真拦住合并，把对应 workflow 设为 required status check。

## 命令参考

### 项目管理

| 命令 | 说明 |
|------|------|
| `forge init [--profile mini\|standard\|full]` | 在仓库声明契约：只写 `spec/forge.yaml`、`spec/changes/`、`AGENTS.md` 托管块与已存在指针，不写用户主目录 |
| `forge status [--json]` | 查看项目状态（任务管道 + 质量信号） |
| `forge change <new\|clarify\|phase\|verify\|status\|list\|gate\|agents>` | change 生命周期：建产物、记边界、看阶段、实跑判据并封印 |
| `forge impact` | 看改动影响面（哪些文档、哪些源码同目录没有测试） |
| `forge docs <lint\|closure>` | 文档结构闸 / 命令面与活文档漂移闸 |
| `forge test <ledger\|record\|probe>` | 测试账本（四态）、导入 go test -json 记录、增量变异探针 |
| `forge compat <snapshot\|check>` | 兼容快照盘点与比对 |
| `forge version` / `forge next` / `forge on` / `forge off` / `forge review` | 版本、下一步、接管开/关、评审记录（完整表见 docs/reference/cli.md） |

### 任务管理

| 命令 | 说明 |
|------|------|
| `forge task start --ref <type/desc> --branch` | 创建任务（自动创建分支）并预跑验收 |
| `forge task status` | 查看当前任务门禁状态 |
| `forge task list` | 列出所有任务 |
| `forge task gate <gate-id>` | 验证单道任务门禁（task-implement / task-complete） |
| `forge task verify-acceptance` | 实跑验收判据并记录证据 |
| `forge task complete` | 标记任务完成（review pass 为硬前置） |
| `forge task report --ref <ref>` | 出交付单 |

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
