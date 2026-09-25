# Forge design pack（forge-design）

前端/设计专项 skill pack——把设计族 12 个 skill 从核心分发中拆出（决策记录：`docs/plans/feature-focus-2026-09.md` §2.1"拆包"）。

## 定位

核心 `skills/` 库定位是**通用验证/流程/编排/检索**（跨宿主证据与签核层）；前端/设计族是领域专项知识，对非前端项目是常驻路由负担（dogfooding 零触发 + 族内重复审计实证）。本 pack 让设计族按 Agent Skills 开放规范独立流通，核心包不再为少数场景背负 12 个 skill 的路由成本。

## 内容（12 skill）

frontend-feature-development、frontend-stack-selection、frontend-aesthetics-execution、frontend-code-review、ai-generated-ui-review、ai-ui-generation-workflow、design-system-workflow、design-system-migration、design-review-snapshot、design-artifact-standards、design-audit、ui-iteration-feedback-loop

## 安装

plugin 布局（`.claude-plugin/plugin.json` + `skills/`），纳入本仓 marketplace（`/plugin marketplace add MjxUpUp/Forge` 后 `/plugin install forge-design@forge`）。无 marketplace 的宿主可直接把 `skills/<name>/` 复制/链接到宿主 skill 目录（如 `~/.claude/skills/`），或 `forge skills install <skill>`。

> 注意：marketplace.json 的 forge-design 条目目前手工维护——`forge plugin pack` 再生成只写 forge 单条目（生成器多插件支持是后续项），重跑后需补回本条目。

## 与核心 skills/ 的关系

- skill 正文不依赖 forge 二进制（零反向依赖契约同核心库）；`design-artifact-standards` 的 `requires: doc-review` 指向核心库 skill（doc-review 仍在核心 `skills/`，未随迁）。
- 核心 skill 中出现的 `frontend-*` / `design-*` / `ai-*-ui-*` 名字均为指针引用（"属 forge-design pack，未安装则忽略"）。
- 迁移用 `git mv` 保留历史；本 pack 不进 forge 二进制 embed（`skills/embed.go` 只嵌核心 `skills/`）。
