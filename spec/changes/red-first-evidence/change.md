# change：red-first-evidence

把「测试先行」从纪律口号变成三个可核对的事实，并把评审从「每轮换人」改成「同一角色复用同一子 agent、承接未闭合项」。

## 改动

1. **红先于绿（F2）**：`task start` 新增 `--accept-red "run :: expected"`（可重复）。start 时实跑每条红判据、把 `{run,exit,head,at}` 记进任务状态；退出码 0（实现前就绿）默认拒绝，`--allow-green` 留痕放行。`task complete` 在验收前置之后核对：每条红判据都要有「实现前的失败记录」，且同一 `Run` 的绿判据已通过。
2. **判据先于实现（F1）**：带 `--change` 启动任务时必须有至少一条 `--accept`（否则拒绝）；不带 change 且零判据时在 start 输出明确警告（complete 仍会拒绝零验收）。
3. **判据溯源（F3）**：`review pass` 新增 `--criteria-reviewed N`，把「评审确认过的判据条数」记进本轮评审记录；`task report` 输出「红先绿」与「评审连续性」两行事实。
4. **评审者复用（连续性）**：`TaskState.Agents` 记录角色→agent id（`code-review` 由 `review pass --reviewer` 绑定）。绑定后源码变更时**必须由同一 id 复审**（不再要求换新 reviewer）；换人需 `--rotate-reviewer --reason` 显式留痕。`--findings <md>` 承接上一轮未闭合项：上一轮仍 open 的 ID 必须在本次清单里关闭（`- [x]`）或显式豁免（`- [~] ... (waived: 原因)`），否则拒绝——这是收敛的机械保证。
5. 文档与协议：`docs/reference/cli.md`、「agents-protocol」模板、README 快速开始；`task report` 与 `forge next` 的提示（复审时提示上次 reviewer id）。

## 影响

- 行为变化：`review pass` 的「必须换新 reviewer」语义**反转**为「必须复用已绑定的 reviewer」，换人需 `--rotate-reviewer`。这是对 2.0.3 复审协议的修正：连续性能收敛，独立性靠「reviewer 不是实现者 + 角色绑定 + findings 承接」保证，而不是靠频繁换人。
- 兼容：老任务没有 `agents`/`review_rounds` 时按首次绑定处理；没用 `--accept-red` 的任务行为不变（不误伤）。
- 风险：红判据可能写成形式化的假红（如 `test -f 不存在`）——机制只能证明「它红过」，判据是否有意义由评审判断并写进 findings；`--criteria-reviewed` 是让人明确认领，不是机器判定。

## 验证

- 单测：`TestRedAttempts_*`（红记录、全绿拒绝、allow-green）、`TestReviewContinuity_*`（同 id 复用、换 id 拒绝、rotate 留痕）、`TestReviewFindings_*`（未闭合项必须关闭/豁免）。
- 行为：`scripts/checks/red-first-e2e.sh` 在临时仓库真跑「init → change → 红判据 → 绿判据 → complete」。
- 回归：`internal/clitask`、`internal/cli`、`internal/tasktypes` 包测试与既有守卫。