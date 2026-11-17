# 改动：激进减法

## 改动

- 删除六类实测零信号/可被绕过的检查与三道 pre-flight：doc gate（末端打回 5 次）、artifact-chain（16/16 advisory 假阳性）、unused-scan（7/7 假阳性）、test-coverage 阻断（13 拦 / 12 次 env 绕过）、mutation gate（2 任务、1 次空转）、plan-first（1 次）。
- 删除 `forge task doc-review`、`forge task mutation`、`forge task artifact`、`forge task chain-init` 与 `forge eval artifact-drill`；`review pass` 去掉 `--note` 自刷新基线，改为必须换新 `--reviewer`。
- 门禁从 3 道降为 2 道（task-implement → task-complete）；`task-verify` 整体移除，验收由 `task verify-acceptance` 实跑。
- change 产物三份化：`goal.md`（Objective / Out of Scope / Check）+ `change.md`（改动 / 影响 / 验证）+ `acceptance.md`；`tasks.md` 可选；老五份布局继续可读。
- 新增 `forge impact`（按 diff 报改动文件、文档命中、源码无配对测试）并在 `task complete` 打印影响面；`task start` 增加验收预跑（15s 超时、失败只记录基线、不阻断）。
- 对外评分只保留确定性口径与 agent-claim 计数，`Score 0–100` 降级为内部信号。

## 影响

- 命令面：`task doc-review` / `task mutation` / `task artifact` / `task chain-init` / `eval artifact-drill` 移除；compat 快照重钉（192 命令 / 16 阻断位点）。
- 文档：README、`docs/reference/cli.md`、`internal/change/templates/agents-protocol.md` 按两道门禁与三份产物重写；新增 `scripts/checks/no-removed-checks.sh` 守卫。
- 测试：删除对应检查的测试文件与夹具，新增 impact/preflight 测试；`internal/taskpipeline`、`internal/clitask`、`internal/cli`、`internal/e2e` 全量绿。

## 验证

- acceptance 8 条围栏（build、taskpipeline、clitask、change、cli `TestImpact|TestHelpSurface`、gofmt、deadcode、no-removed-checks）。
- 手工：`forge task start` 预跑输出、`forge impact` 输出、缺 review 时 `task gate task-complete` 仍被 review 前置拦下。
