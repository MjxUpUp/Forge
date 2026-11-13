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

# 3. 可选：登记进全局看板
forge registry add .
```

## 核心能力

- **任务级门禁** — 每个开发任务走 3 道门禁（实现 → 验证 → 完成），防跳过
- **cheat-scan 机械检测** — AI 作弊模式（断言弱化、吞错、死分支）一次判准
- **质量评分** — 每个任务完成自动量化 score，沉淀项目级质量基线

## 设计定位

Forge = Loop Engineering 的 **验证 + 状态层**。AI 编码是一个循环：写代码 → 跑 → 读反馈 → 修正 → 再写。**Forge 不替代循环本身**，它补上循环最容易缺的两层：每一轮的产出物是否真可信（验证）、跨轮的状态是否持久可追（状态）。

## 设计原则

执法协议的设计共识，四篇可链接的设计文档（对应 dsh 官网把理论论文放在导航位的做法——设计资产即门面）：

- [插件哲学](../docs/design/plugin-philosophy.md) — 扩展面少而统一、静态可审计：不学"一切皆插件"，采纳 Cordis 的 disposer 契约与 seam 三分法
- [演化纪律规范](../docs/design/evolution-discipline-norms.md) — 适应度函数门、deferred 三要素、expand-contract 迁移编舞
- [机制加固](../docs/design/mechanism-hardening.md) — 逃生舱治理与 advisory→BLOCKED 升档承诺
- [兼容承诺](../docs/design/compat-commitments.md) — 兼容快照棘轮（七面盘点，含外部桥契约面），数据只增不删

## 多 agent 支持

Forge 不分发宿主插件、不写宿主配置：任何 agent/IDE 都在 shell 里调用同一组 `forge task` / `forge change` 命令，证据、评分与状态仍然落盘可审计。

`forge init` 只写仓库内产物（`spec/forge.yaml`、`spec/changes/`、`AGENTS.md` 托管块与已存在指针），**不写** `~/.forge`、`~/.claude`、`~/.codex`。团队要 git 共享契约：提交 `spec/` 与 `AGENTS.md`。

## 真实证据

- 自举（dogfood）：Forge 用 Forge 自身管住质量，每 PR 过三门禁评分
- 真实评分 fixture：`scoring/testdata/golden_real/` 固化评分形状防算法隐性漂移
- code-review 确定性：`forge review pass` 绑定代码快照，拒绝改码后空过

## 文档资源

- 主文档：[README.md](../README.md)
- 中文精简：[READMEs/README.zh-CN.md](../READMEs/README.zh-CN.md)
- 协议：[internal/protocol](../internal/protocol)
- 质量协议 Skill：由 `forge skills install` 分发到全局/项目目标目录，见[完整命令参考](../docs/reference/cli.md)

## 许可

[Apache-2.0](../LICENSE)
