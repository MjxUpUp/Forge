# 设计：命令面收敛：去掉宿主层残留文案与死入口

change: `cmd-surface-convergence`

## TL;DR

- 改什么（一句话）：删掉 `task resume` 的三个 hook 模式 flag 与其死实现，并把根帮助、review gate、verify、config 里指向已删除 hook 的措辞改成现状。

## Non-goals

- 不重构命令分组，不动 `registry`/`workspace`/`worktree` 的归属。
- 不改 `task resume` 的接续语义、输出格式与其它 flag；不改 init/verify/config 的行为。
- 不重写历史设计文档（`docs/plans/`、`docs/dogfood-findings-*` 保持原样）。

## 结构

- 新增/改动的包与文件：
  - `internal/clitask/task_continuity.go`：删 `--hook`/`--compact-flag`/`--reinject` 注册与 `runTaskResume` 的三个分支；删 `renderHookBanner`/`renderHookCompactFlag`/`renderHookReinject`、`managedSessionBanner`、`passPrefix`；保留 `realNewlineString`（自动认领提示仍在用）。
  - `internal/taskpipeline/state.go`：删 `resumeStaleFile`/`resumeStalePath`/`MarkResumeStale`/`ConsumeResumeStale`。
  - `internal/tasktypes/types.go`：`ResumeStale` 字段保留（兼容旧 state），注释标为「历史字段，阶段 E 起无写入」。
  - `internal/cli/root.go`：快速开始改为「init 声明契约 / next 给下一步 / status 看状态」；`healNpmShimIfNeeded` 去掉 hook 特例参数。
  - `internal/cli/review.go`：`gate` 改为「CI 或 agent 显式调用」，删「Stop hook 自动拦截」的 Long/注释。
  - `internal/cli/verify.go`、`internal/cli/config.go`：Short 改为契约检查 / compat 声明。
  - `scripts/checks/no-stale-hook-help.sh`、`scripts/checks/resume-hook-flags-gone.sh`：两条静态守卫。
- 关键类型与函数签名：`healNpmShimIfNeeded()`（无参数）。
- 数据/接口变化：CLI 表面少三个 flag；`TaskState` 结构不变。

## 验证思路

- 为什么下面的命令足以证明「怎样算解决」：build 保证删干净；clitask/taskpipeline 测试覆盖 resume 的剩余行为与 state 序列化；两条守卫把「帮助里再出现 hook 措辞」「hook flag 复活」钉成回归；docs/cli 测试保证 reference 没被带歪；deadcode 保证没有留下无人调用的实现。
- 反例（什么情况下这些判据会假绿，怎么防）：守卫是静态 grep，若把措辞换成同义词（如「停止钩子」）会漏——由 review 兜；`go test -run 'TestDocs'` 只覆盖文档名册，不覆盖措辞——同样由 review 兜。

## 风险

- 回滚方式：`git revert` 本 change 的提交即可（纯删减 + 文案，无数据迁移）。
- 已知限制：旧 state 里可能残留 `resume_stale: true`，现在没有消费者，读取时被忽略；`TaskState` 字段保留避免 merge/seed 逻辑翻工。