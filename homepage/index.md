# Forge — AI 开发质量门禁引擎

> Quality gates for AI-driven development. Stop trusting AI-generated code; start gating it.

## 一分钟简介

Forge 是 AI 编码工作流的"质量层"。在 Claude Code / Codex / Cursor / Copilot 写代码的过程中，它提供结构化门禁——从任务创建到代码提交，task 流程内由门禁做编译自检与断言扫描；非 task 流程没有门禁。不是替代你写代码，是替你的代码把关。

## 三步上手

```bash
# 1. 装 binary（机器级，一次性）
npm install -g @agent_forge/forge

# 2. 在仓库里声明契约 —— 只写仓库内产物，不碰 ~/.forge
cd your-project && forge init

# 3. 立第一个 change：产物随仓库走，可审可回溯
forge change new demo
```

## 核心能力

- **任务级门禁** — 每个开发任务走两道门禁（实现 → 完成），门禁是命令不是 hook，跑没跑留下证据
- **change 证据封印** — `forge change verify` 实跑 acceptance 判据，证据绑定提交内容，改文件即失效
- **cheat-scan 机械检测** — AI 作弊模式（断言弱化、吞错、死分支）一次判准
- **测试账本与能力面闭合** — `forge test ledger` 看测试四态（活/孤儿/被跳过/幻影），`forge docs closure` 让文档与命令树对账

## 设计定位

Forge = Loop Engineering 的 **验证 + 状态层**。AI 编码是一个循环：写代码 → 跑 → 读反馈 → 修正 → 再写。**Forge 不替代循环本身**，它补上循环最容易缺的两层：每一轮的产出物是否真可信（验证）、跨轮的状态是否持久可追（状态）。

## 设计原则

执法协议的设计共识，可链接的设计文档（设计资产即门面）：

- [兼容承诺](../docs/design/compat-commitments.md) — 兼容快照棘轮：数据面、命令面、分发面只增不删
- [定位与边界](../docs/design/positioning.md) — 验证层与状态层到底覆盖什么、不覆盖什么
- [测试账本](../docs/design/test-ledger.md) — 测试四态判定、Skip 审批、增量变异探针
- [命令参考](../docs/reference/cli.md) — 由命令树生成的全表，与二进制同步

## 多 agent 支持

Forge 不分发宿主插件、不写宿主配置：任何 agent/IDE 都在 shell 里调用同一组 `forge task` / `forge change` 命令，证据与状态仍然落盘可审计。

`forge init` 只写仓库内产物（`spec/forge.yaml`、`spec/changes/`、`AGENTS.md` 托管块与已存在指针），**不写** `~/.forge`、`~/.claude`、`~/.codex`。团队要 git 共享契约：提交 `spec/` 与 `AGENTS.md`。

## 真实证据

- 自举（dogfood）：Forge 用 Forge 自身管住质量，每 PR 过两道门禁并实跑验收
- 验收不可伪造：`forge change verify` 的判据是命令，`-run` 匹配不到测试按失败计（不会空过）
- code-review 确定性：`forge review pass` 绑定代码快照，拒绝改码后空过

## 文档资源

- 主文档：[README.md](../README.md)
- 中文精简：[READMEs/README.zh-CN.md](../READMEs/README.zh-CN.md)
- 协议：[internal/protocol](../internal/protocol)
- 从 1.84 及更早版本升级：[docs/migration/upgrade-from-1.84.md](../docs/migration/upgrade-from-1.84.md)

## 许可

[Apache-2.0](../LICENSE)
