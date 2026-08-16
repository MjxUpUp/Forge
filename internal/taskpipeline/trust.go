package taskpipeline

// trust.go — the single source of truth for stripping FOREIGN gate/trust signals off a TaskState
// that entered the local DataDir from an untrusted source. Two entry points share it:
//
//   - task import (cli/task_port.go): a hand-edited or hostile bundle must never satisfy local
//     gates without local re-verification;
//   - .forge migrate (cli/sync.go migrateRuntimeResidue + cli/migrate.go): repo-committed
//     .forge/tasks/*.json is attacker-authorable content (a cloned malicious repo), promoted into
//     the trusted DataDir verbatim otherwise.
//
// trust.go —— 从不可信源进入本地 DataDir 的 TaskState 上剥离「外来」门禁/信任信号的单一真相源。
// 两个入口共用：
//
//   - task import（cli/task_port.go）：手改/恶意的 bundle 绝不能不经本机重验就满足本机门禁；
//   - .forge migrate（cli/sync.go migrateRuntimeResidue + cli/migrate.go）：repo 提交的
//     .forge/tasks/*.json 是攻击者可书写的内容（clone 恶意仓库即得），否则会被逐字提升为
//     DataDir 信任状态。
//
// The 2026-08-15 review found the original import-time strip cleared RESULT fields
// (ReviewPassed/Score/acceptance results) but let CONTROL-FLOW fields through:
//
//   - CompletedAt: every hard check in executor.go is guarded by `state.CompletedAt == nil`
//     (review prereq / snapshot consistency / test-coverage backstop / work-activity / DependsOn),
//     and `gate task-complete` AUTO-PASSES (not refuses) an already-completed task — so a bundle
//     carrying completed_at + two passed gate History entries disabled all five hard checks after
//     one gate run;
//   - Overrides: four hard gates (read-before-edit PreToolUse block included) silently disabled on
//     the importer, with no disclosure — escape hatches are supposed to be deliberate LOCAL
//     `forge task override` decisions;
//   - Acceptance Run: foreign command strings preserved verbatim, executed by verify-acceptance
//     with full env — arbitrary command execution steered by the tool's own BLOCKED guidance.
//
// 2026-08-15 审查发现：原 import 侧剥离只清「结果」字段（ReviewPassed/Score/验收结果），却放行
// 「控制流」字段：
//
//   - CompletedAt：executor.go 的每个硬检查都以 `state.CompletedAt == nil` 为守卫（review 前置/
//     快照一致性/test-coverage 兜底/work-activity/DependsOn），且 `gate task-complete` 对已完成
//     任务是自动通过（而非拒绝）——带 completed_at + 两条 Passed History 的 bundle 在跑一次门禁
//     后使五个硬检查全部失效；
//   - Overrides：四个硬门禁（含 read-before-edit PreToolUse 阻断）在导入机上静默关闭且零披露
//     ——逃生舱本应是本机 `forge task override` 的审慎决定；
//   - Acceptance Run：外来命令串原样保留，被 verify-acceptance 以完整环境执行——任意命令执行，
//     且触发路径正是工具自己的 BLOCKED 指引。
//
// StripForeignGateSignals therefore clears BOTH classes. History drops every PASSED entry and
// keeps only failed ones as provenance: executor.go's gate-prerequisite walk treats a foreign
// `task-verify: Passed` as satisfying the chain, which would let the importer jump straight to
// `gate task-complete` and skip every hard check that lives inside task-verify (work-activity,
// skill-decisions guardrail, cheat-scan, test-coverage). Gate passes must be earned locally;
// failed entries never satisfy anything and stay as real history. Assignment is dropped too —
// IsDelivered() reads Assignment.Status==delivered as the DependsOn release signal, so a foreign
// "delivered" claim must not unblock downstream LOCAL tasks without a local delivery. CurrentGate
// is re-derived from the surviving History so status display matches reality.
//
// StripForeignGateSignals 因此同时清两类字段。History 剔除所有「已通过」条目、只保留失败的作
// 溯源：executor.go 的门禁前置链会把外来的 `task-verify: Passed` 当作已满足，让导入方直接跳到
// `gate task-complete`，跳过 task-verify 内部的全部硬检查（work-activity、skill-decisions
// guardrail、cheat-scan、test-coverage）。门禁通过必须本机挣得；失败条目不满足任何前置，留作
// 真实历史。Assignment 同样剥离——IsDelivered() 把 Assignment.Status==delivered 当 DependsOn
// 的放行信号，外来「已交付」声明不得在没有本机交付的情况下放行下游本机任务。CurrentGate 从
// 幸存的 History 重推，使状态展示与现实一致。
func StripForeignGateSignals(s *TaskState) {
	// Result-flavored signals (original strip).
	//
	// 结果类信号（原有剥离）。
	s.ReviewPassed = false
	s.ReviewedHeadCommit = ``
	s.ReviewedChangeHash = ``
	s.Score = nil
	for i := range s.Acceptance {
		s.Acceptance[i].Passed = false
		s.Acceptance[i].AcceptedHeadCommit = ``
		s.Acceptance[i].Output = ``
	}
	// Control-flow signals (2026-08-15 fix). CompletedAt is the master switch that disables every
	// CompletedAt==nil-guarded hard check — and the local invariant "CompletedAt set ⟹ gates passed
	// locally" is exactly what an untrusted source breaks. Overrides are gate-weakening switches
	// that must be re-established by a local `forge task override` decision, not inherited.
	//
	// 控制流信号（2026-08-15 修复）。CompletedAt 是关掉所有 CompletedAt==nil 守卫硬检查的总开关
	// ——而「CompletedAt 已设 ⟹ 门禁本机通过过」的本机不变量恰是被不可信源打破的东西。
	// Overrides 是削弱门禁的开关，必须由本机 `forge task override` 决定重建，不能继承。
	s.CompletedAt = nil
	s.Overrides = TaskOverrides{}
	// Kind=="generic" is the STRONGEST gate-weakening switch of all — stronger than the Overrides
	// stripped right above: IsGeneric() short-circuits ExecuteTaskGate itself (every hard check
	// inside all three gates), and runTaskComplete's generic branch sits BEFORE the IsComplete
	// check and the acceptance pre-flight, going straight into completeGenericTask, which stamps
	// all three gates Passed at the local HEAD + MarkComplete — IsDelivered() then releases
	// downstream DependsOn. One foreign string must not carry that power. Same rule as Overrides:
	// generic is a LOCAL decision — re-establishing it means `forge task abort` (drops the
	// imported continuity data) then `forge task start --kind generic`, or simply accepting that
	// the imported task walks the full gates as a code-kind task. Note `task start --kind generic`
	// on the same ref alone is NOT a path: the imported task file already exists, so start refuses
	// with "task already exists" (review round 2, 2026-08-16).
	//
	// Kind=="generic" 是所有门禁削弱开关里最强的——比上面刚剥的 Overrides 还强：IsGeneric()
	// 直接短路 ExecuteTaskGate 本体（三道门禁内的全部硬检查），且 runTaskComplete 的 generic
	// 分支位于 IsComplete 检查与 acceptance pre-flight 之前，直入 completeGenericTask——在本地
	// HEAD 伪造三道门禁 Passed + MarkComplete，IsDelivered() 随即放行下游 DependsOn。一个外来
	// 字符串不得携带这种权力。与 Overrides 同规：generic 是本机决策——重建途径是
	// `forge task abort`（丢导入的接续数据）后 `forge task start --kind generic`，或直接接受
	// 导入任务作为 code 任务走全量门禁。注意对同一 ref 直接 `task start --kind generic` 不是
	// 途径：导入的任务文件已存在，start 会以 "task already exists" 拒绝（2026-08-16 二轮复审）。
	s.Kind = ``
	// A forged bundle can put passed gate entries in History, which the executor's
	// gate-prerequisite walk and IsComplete() read as local progress — dropping every PASSED entry
	// (task-complete included) closes both: the task re-walks all gates locally, and task-verify's
	// own hard checks re-run on this machine. FAILED entries stay: they never satisfy a
	// prerequisite and show the real gate progression from the source machine.
	//
	// 手改的 bundle 可在 History 塞已通过的门禁条目，executor 的门禁前置链与 IsComplete() 都会
	// 把它读本机进度——剔除所有「已通过」条目（含 task-complete）同时封掉两条路：任务在本机
	// 重走全部门禁，task-verify 自身的硬检查在本机重跑。失败条目保留：它们不满足任何前置，
	// 且显示源机器的真实门禁进度。
	kept := make([]TaskGateResult, 0, len(s.History))
	for _, h := range s.History {
		if h.Passed {
			continue
		}
		kept = append(kept, h)
	}
	s.History = kept
	// Re-derive the current gate from the surviving History — a forged CurrentGate (e.g.
	// "task-complete") must not linger in status display. With every passed entry stripped,
	// NextGate lands on the first gate of DefaultGates (task re-walks from the start).
	//
	// 从幸存的 History 重推当前门禁——伪造的 CurrentGate（如 "task-complete"）不得在状态展示中
	// 残留。所有已通过条目被剥后，NextGate 落在 DefaultGates 的第一个门禁（任务从头重走）。
	s.CurrentGate = s.NextGate()
	// Delegation control-flow: Assignment.Status==delivered is the DependsOn release signal checked
	// by task-verify/task-complete on DOWNSTREAM local tasks — a foreign "delivered" claim must not
	// release a local dependency gate without a local delivery. Drop the whole Assignment; the
	// local side re-assigns (forge task assign) if the work is actually continued here.
	//
	// 分派控制流：Assignment.Status==delivered 是下游本机任务 task-verify/task-complete 检查的
	// DependsOn 放行信号——外来「已交付」声明不得在没有本机交付的情况下放行本机依赖门禁。
	// 整体剥掉 Assignment；工作若真在本机继续，本机侧重新分派（forge task assign）。
	s.Assignment = nil
	// Every session link from a foreign source becomes a GHOST (Imported=true): it records who
	// participated on the source machine, never a local anchor — HasSession/AddSession ignore
	// ghost links, so a foreign session id must not anchor this machine's session to the task.
	// Ghosting lives here (single source) so both the import path and the migrate path get it —
	// the migrate path previously forgot it (review 2026-08-16).
	//
	// 外来源的每条 session 链接都成为幽灵（Imported=true）：它记录源机器谁参与过，永非本机
	// 锚点——HasSession/AddSession 忽略幽灵链接，外来 session id 不得把本机 session 锚到任务上。
	// 幽灵化放在这里（单一真相源），import 与 migrate 两条路都拿到——migrate 路径此前漏了
	// （2026-08-16 复审）。
	for i := range s.SessionLinks {
		s.SessionLinks[i].Imported = true
	}
	// The Run commands survive as spec (the handoff carries what to verify), but they are
	// foreign-authored executable strings — mark them so verify-acceptance demands explicit
	// review-based trust (--trust-foreign) before the first local execution.
	//
	// Run 命令作为 spec 保留（交接需要带上「验什么」），但它们是外来作者的可执行字符串——打上
	// 标记，使 verify-acceptance 在首次本机执行前要求显式的、基于审阅的受信（--trust-foreign）。
	s.AcceptanceForeign = len(s.Acceptance) > 0
}
