# dev-workflow · Forge 集成

仅 forge 项目适用。skills 零反向依赖契约（CONVENTIONS §13）下，本 skill 的 forge 集成知识由 forge 侧维护。

## 验收标准实跑门禁（spec-as-gate）

把 Plan（含 `Run:/Expected:` 块）写成 markdown 文件，`forge task start --plan-file <plan.md>` 时 forge 自动从里面提取每条 Run/Expected 持久化进任务——无需手抄 `--accept`（手抄靠 agent 自觉必漏，是 acceptance 维度空转的根因）。随后 `forge task verify-acceptance` 实跑每条 Run、比对 Expected、回填结果并记 `checklog:acceptance`（deterministic，不可伪造）。plan 文件位置灵活（临时文件或 `docs/`），forge 提取后全文已存 `task.Plan`（`forge task resume` 可拉回），用完可不留。需手动微调/覆盖某条时才用 `--accept "run :: expected"`（显式 `--accept` 优先于 plan 提取，按 Run 去重）。

## scope 声明白名单（可度量契约）

`forge task start` 时用 `--scope <glob>`（可重复，或中途 `forge task scope add <glob>`）把 plan 里"要改哪些文件"持久化进任务的 PlanScope 白名单。task-verify 比对实改源码与声明的差集，偏离时记 `checklog:scope-drift`（advisory 不阻塞——scope 是 prediction 非 contract）。Plan 里的文件清单直接对应 `--scope` glob：如"改 internal/auth/login.rs 和 session.rs" → `--scope "internal/auth/login.rs" --scope "internal/auth/session.rs"`（或目录前缀 `--scope "internal/auth/"`）。
