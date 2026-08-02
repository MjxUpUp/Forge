# forge 集成：验收标准与 scope 的实跑门禁

> 仅在使用 forge 的项目适用；非 forge 项目忽略本文件。

**forge 项目把验收标准变成不可伪造的实跑证据**：把 Plan（含 `Run:/Expected:` 块）写成 markdown 文件，`forge task start --plan-file <plan.md>` 时 forge **自动从里面提取每条 Run/Expected** 持久化进任务——无需手抄 `--accept`（手抄靠 agent 自觉必漏，是 acceptance 维度空转的根因：本项目 28 条任务结论验收维度全 0/0）。随后 `forge task verify-acceptance` 实跑每条 Run、比对 Expected、回填结果并记 `checklog:acceptance`（deterministic——forge 自己跑看结果，不可伪造）。plan 文件位置灵活（临时文件或 `docs/`），forge 提取后全文已存 `task.Plan`（`forge task resume` 可拉回），用完可不留。需手动微调/覆盖某条时才用 `--accept "run :: expected"`（显式 `--accept` 优先于 plan 提取，按 Run 去重）。把 plan 里的验收标准从"文本里飘着"变成"实跑留痕的 spec-as-gate"，对冲 agent 自述"满足验收"却没真跑的盲区。

**forge 项目把"规划前置"变成可度量契约**：`forge task start` 时用 `--scope <glob>`（可重复，或中途 `forge task scope add <glob>`）把 plan 里"要改哪些文件"持久化进任务的 PlanScope 白名单。`task-verify` 比对实改源码与声明的差集，偏离时记一条 `checklog:scope-drift`（deterministic）并提醒。**advisory 不阻塞**——变更影响分析召回率仅 ~44%，scope 是 prediction 非 contract，偏差是常态信号；它的价值是让"我打算改 A/B/C"从口号变成可回溯的契约，实改了 D/E 时留痕供 review（而非拦死）。Plan 里的文件清单直接对应 `--scope` 的 glob：上方"改 internal/auth/login.rs 和 session.rs" → `--scope "internal/auth/login.rs" --scope "internal/auth/session.rs"`（或目录前缀 `--scope "internal/auth/"`）。
