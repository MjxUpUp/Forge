---
status: review
owner: MjxUpUp
ask: 审逃生舱审计补齐、无逃生裁决与文档卫生的口径
tier: mini
---

# 修复方案 | 逃生舱审计与裁决 + 文档卫生批（2026-10）

## TL;DR
#129 在 `docs/design/compat-commitments.md` 第二节第 5 条登记了 7 条已知例外。本批处理其中 6 条（scope 分支并入复发升硬的裁决）：两处逃生补审计行、两个逃生 env 入册、宿主提升文案标明身份、四处无逃生位点记裁决；同时新登记 1 条（checklist `drop` 与 `abort --detach-deps` 不写审计行）。另外清理冲突清单里的纯文档问题：命令面记账、方案状态字段、23 份文档缺状态行、过期路径与取代标注、hook 计数、forge-dsh README 自相矛盾。不新增阻断位点，不改任何门禁的放行条件；新增的只有审计行和 roster 条目。

## 问题与目标
- task-verify Stop 有界阻断的逃生 `FORGE_TASK_VERIFY_STOP=0` 生效时整段跳过，不留 checklog（`internal/hooks/embed_quality.go`），拒绝文案也没有指向兼容契约。
- 复发升硬的逃生 `FORGE_RECURRENT_HARDEN=disable` 实际解除 BLOCKED，但 `internal/compat/compat_test.go` 把它白名单为「非 gate-bypass」，生效时不留审计行；而 `internal/checklog/types.go` 的 `CheckEscapeHatch` 注释又把它列为逃生舱。
- 宿主提升逃生 `FORGE_ADVISORY_PROMOTION=soft`（`internal/hookdispatch/hook_emitters.go`）不在 `compat.EscapeEnvs`；提升后的 task-guard 拒绝文案（`internal/hooks/embed_task.go`）没标明是宿主提升，也没给逃生。
- 审查前置、checklist、上游依赖、bundle 验签四处没有逃生舱，也没有裁决记录。
- 冲突清单（2026-10-22 全景盘点的产物，只在本机、未入库）里的纯文档问题：B6 hook 计数、B7 命令面记账、B8 forge-dsh 零依赖、D1/D4 取代标注；废弃清单里的状态字段与过期路径。

## Non-goals
- 不处理 freeze-guard 与项目级自保护的真人终端校验（已知例外第 4 条），那需要改出口命令的行为。
- 宿主提升是否适用预告期，本批记裁决而不改代码。
- 不改 `skills/CONVENTIONS.md` 以外与 #128 重叠的文件；`skills/CONVENTIONS.md` 的计数在 #128 合并后再改。
- 历史依据行（引用当时口径的段落）保持原样，只加批注。

## 需求与验收
- REQ-1 task-verify Stop 的逃生生效且本来会阻断时，每会话写一行 escape-hatch 审计；拒绝文案指向兼容契约
  accept[REQ-1]: go test ./internal/e2e/ -run ^TestTaskVerifyStopHook_BoundedBlockOnUncommittedTaskWork$ -count=1 :: --- PASS: TestTaskVerifyStopHook_BoundedBlockOnUncommittedTaskWork (
  accept[REQ-1]: go test ./internal/hooks/ -run ^TestTaskVerifyHookCarriesBoundedBlockContract$ -count=1 :: --- PASS: TestTaskVerifyHookCarriesBoundedBlockContract (
- REQ-2 `FORGE_RECURRENT_HARDEN` 入 `compat.EscapeEnvs`；它生效且本来会升硬时写 escape-hatch 审计行
  accept[REQ-2]: go test ./internal/taskpipeline/ -run ^TestRecurrent_TestCoverage_EscapeDisables$ -count=1 :: --- PASS: TestRecurrent_TestCoverage_EscapeDisables (
  accept[REQ-2]: go test ./internal/compat/ -run ^TestEscapeRosterComplete$ -count=1 :: --- PASS: TestEscapeRosterComplete (
- REQ-3 `FORGE_ADVISORY_PROMOTION` 入 roster；宿主提升后的 task-guard 拒绝文案标明是宿主提升并给出逃生 env
  accept[REQ-3]: go test ./internal/hookdispatch/ -run ^TestHook_DshTaskGuardNoTaskBlocks$ -count=1 :: --- PASS: TestHook_DshTaskGuardNoTaskBlocks (
- REQ-4 compat-commitments §三 记录审查前置、checklist、上游依赖、bundle 验签的无逃生裁决与宿主提升预告期裁决，已知例外清单同步收缩；compat 快照重钉
  accept[REQ-4]: grep -n 逃生舱审计与裁决批 docs/design/compat-commitments.md :: 逃生舱审计与裁决批
  accept[REQ-4]: go test ./internal/cli/ -run ^TestCompatSnapshotMatchesGolden$ -count=1 :: --- PASS: TestCompatSnapshotMatchesGolden (
- REQ-5 compat-commitments 第五节补记 v1.57、v1.63、v1.79 三次命令面超预算
  accept[REQ-5]: grep -n eval-rule-ledger docs/design/compat-commitments.md :: eval-rule-ledger
- REQ-6 文档卫生：23 份设计和方案文档补状态行；方案状态字段与取代标注更新；过期路径改正；hook 计数与 README 表对齐；forge-dsh README 依赖说法自洽
  accept[REQ-6]: go test ./internal/docsconsistency/ -count=1 :: ok
  accept[REQ-6]: grep -n 状态：已落地 docs/design/node-identity.md :: 状态：已落地
  accept[REQ-6]: grep -n 无第三方运行时依赖 plugins/forge-dsh/README.md :: 无第三方运行时依赖

## 设计
### 触点
（每行一个路径：PlanScope 按行取第一个路径解析。）
- `internal/hooks/embed_quality.go`
- `internal/hooks/embed_quality_stopblock_test.go`
- `internal/e2e/taskverify_stopblock_test.go`
- `internal/taskpipeline/executor_check_verify.go`
- `internal/taskpipeline/recurrent.go`
- `internal/taskpipeline/executor_recurrent_test.go`
- `internal/skillgen/claudemd.go`
- `internal/skillgen/claudemd_test.go`
- `internal/compat/compat.go`
- `internal/compat/compat_test.go`
- `internal/hooks/embed_task.go`
- `internal/hooks/profile.go`
- `internal/hooks/profile_test.go`
- `internal/hookdispatch/hook_dsh_test.go`
- `internal/checklog/types.go`
- `internal/checklog/evidence.go`
- `internal/checklog/evidence_test.go`
- `compat.snapshot.json`
- `README.md`
- `plugins/forge-dsh/README.md`
- `skills/CONVENTIONS.md`
- `docs/design/compat-commitments.md`
- `docs/design/compat-bridge-face.md`
- `docs/design/evolution-discipline-norms.md`
- `docs/design/focus-batches.md`
- `docs/design/forge-dsh-provider-roadmap.md`
- `docs/design/guardfall-semantic-w1-proposal.md`
- `docs/design/guardfall-semantic-w1.md`
- `docs/design/harness-fixes-a-g-2026-09.md`
- `docs/design/hostcap-behavior-registry.md`
- `docs/design/mechanism-hardening.md`
- `docs/design/multi-repo-workspace.md`
- `docs/design/multi-task-concurrency.md`
- `docs/design/node-identity.md`
- `docs/design/plugin-philosophy.md`
- `docs/design/project-sync.md`
- `docs/design/project-policy-layer.md`
- `docs/design/slim-w0-proposal.md`
- `docs/design/slim-w0-spec.md`
- `docs/design/sync-convergence.md`
- `docs/design/w0-batch-dispatch-spec.md`
- `docs/design/output-readability-gates.md`
- `docs/plans/discipline-first-gates-2026-09-proposal.md`
- `docs/plans/discipline-first-gates-2026-09.md`
- `docs/plans/escape-hatch-hardening-2026-09.md`
- `docs/plans/feature-focus-2026-09.md`
- `docs/plans/release-flow-hardening-2026-09.md`
- `docs/plans/eval-validity-r3-r5-2026-10.md`
- `docs/plans/task-resume-pull-model-2026-10-proposal.md`
- `docs/plans/acceptance-gate-and-wedge-2026-10-proposal.md`
- `docs/plans/loop-contract-2026-10-proposal.md`
- `docs/plans/escape-rulings-doc-hygiene-2026-10.md`

### 关键取舍
- 不做（只登记例外）：否决。例外清单写明「修复前不得被援引为先例」，长期挂着会让第 5 条的约束形同虚设。
- 复发升硬按逃生舱处理（入 roster、留审计），而不是改写成「强度偏好」：它确实解除了 BLOCKED，按第 5 条判别顺序属于 ① 类的逃生。
- 四处无逃生位点记裁决，而不是补 env：审查前置的出口是 `review pass`（留审查 stamp，基线后变更另记 self-refresh 审计）；checklist 的 `drop` 与上游依赖的 `abort --detach-deps` 是显式出口命令，但目前不写 checklog，登记为已知例外待补；bundle 验签的逃生等于接受伪造。
- 审计行每会话只写一次（marker 去重），与 gate-cmd-form 逃生同款，避免逃生期间每次 Stop 都刷一行。

## 验证与回滚
- `go vet ./...` 无输出；`go test ./... -count=1 -timeout 30m` 退出码 0（64 个包 ok，合并 #128 前跑）；合并 #128 后重跑了 skillsqa、agentbridge、docsconsistency 与 compat 快照守卫。
- 复发升硬的逃生现在按逃生计价：本来会升硬时，关掉开关的任务分数封顶 89、证据 Weak。这是对原设计「不加 Strength 惩罚」的有意改变，协议文案已同步。
- task-verify Stop 的逃生按 ④ 类计价：分数封顶，证据强度不降（`internal/checklog/evidence.go` 把它和 work-activity 一样识别为节奏类逃生）。
- 回滚即 revert 本分支的提交。新增的审计行只追加 checklog，不改既有键；compat 快照逃生面只增两项。
