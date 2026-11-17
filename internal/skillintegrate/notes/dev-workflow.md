# dev-workflow · Forge 集成

仅 forge 项目适用。skills 零反向依赖契约（CONVENTIONS §13）下，本 skill 的 forge 集成知识由 forge 侧维护。

## 验收标准实跑门禁（spec-as-gate）

把 Plan（含 `Run:/Expected:` 块）写成 markdown 文件，`forge task start --plan-file <plan.md>` 时 forge 自动从里面提取每条 Run/Expected 持久化进任务——无需手抄 `--accept`（手抄靠 agent 自觉必漏，是 acceptance 维度空转的根因）。随后 `forge task verify-acceptance` 实跑每条 Run、比对 Expected、回填结果并记 `checklog:acceptance`（deterministic，不可伪造）。plan 文件位置灵活（临时文件或 `docs/`），forge 提取后全文已存 `task.Plan`（`forge task resume` 可拉回），用完可不留。需手动微调/覆盖某条时才用 `--accept "run :: expected"`（显式 `--accept` 优先于 plan 提取，按 Run 去重）。

## scope 声明白名单（可度量契约）

`--scope <glob>` 仍可登记（可重复，或中途 `forge task scope add <glob>`），但 task-verify 的差集比对已随 1.88 删除；scope 现在是给人和评审看的声明：scope 是 prediction 不是 contract，偏离由评审者对照 `forge impact` 判断。Plan 里的文件清单直接对应 `--scope` glob：如"改 internal/auth/login.rs 和 session.rs" → `--scope "internal/auth/login.rs"`。

## 方案档位（design tier）

阶段 1 的档位表在 forge 里对应 `forge task start --tier none|mini|full`。forge 用内置硬触发器（迁移/sql、proto/OpenAPI、鉴权目录、api/pkg/sdk、依赖文件、跨 3 个以上目录）推导档位，与声明取较高者；声明低于推导须 `--tier-reason "<理由>"`（落审计）。规则表可在 `.forge/protocol.yml` 的 `design_tiers`（`rules` 按 name 追加/覆盖、`standard_changes` 追加白名单、`replace_defaults`）调整。implement 首轮落 `design-tier` 行；complete 时用实际 diff 复核，超出档位且未登记 design 产物给 advisory。`forge task list --plan-conversion` 看各档位的跳过率/否决率——持续上升说明门禁太重。

## 触点 → PlanScope 与需求追溯

- 方案写在 change.md 的「改动 / 影响 / 验证」里（1.88 起不再有 artifact 命令）；影响面用 `forge impact` 随时查看，`--scope` 仍可显式声明 PlanScope。
- 验收写在 change 的 acceptance.md 或 `task start --accept`；`forge task verify-acceptance` 实跑并记确定性证据，`forge task report` 一并披露影响面。
