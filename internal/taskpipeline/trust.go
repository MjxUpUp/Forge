package taskpipeline

import "reflect"

// trust.go — the single source of truth for stripping FOREIGN gate/trust signals off a TaskState that entered the local DataDir from an untrusted source.
//
// trust.go —— 从不可信源进入本地 DataDir 的 TaskState 上剥离「外来」门禁/信任信号的单一真相源。
// 两个入口共用：
//
//   - task import（cli/task_port.go）：手改/恶意的 bundle 绝不能不经本机重验就满足本机门禁；
//   - .forge migrate（cli/sync.go migrateRuntimeResidue + cli/migrate.go）：repo 提交的
//     .forge/tasks/*.json 是攻击者可书写的内容（clone 恶意仓库即得），否则会被逐字提升为
//     DataDir 信任状态。
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
// StripForeignGateSignals 因此同时清两类字段。History 剔除所有「已通过」条目、只保留失败的作
// 溯源：executor.go 的门禁前置链会把外来的 `task-verify: Passed` 当作已满足，让导入方直接跳到
// `gate task-complete`，跳过 task-verify 内部的全部硬检查（work-activity、skill-decisions
// guardrail、cheat-scan、test-coverage）。门禁通过必须本机挣得；失败条目不满足任何前置，留作
// 真实历史。Assignment 同样剥离——IsDelivered() 把 Assignment.Status==delivered 当 DependsOn
// 的放行信号，外来「已交付」声明不得在没有本机交付的情况下放行下游本机任务。CurrentGate 从
// 幸存的 History 重推，使状态展示与现实一致。
// GhostForeignSessions marks every session link as imported (a "ghost"): it records
// who participated on the SOURCE machine, never a local anchor — HasSession/AddSession
// ignore ghost links, so a foreign session id must not anchor this machine's session
// to the task.
//
// GhostForeignSessions 把每条 session 链接标为已导入（「幽灵」）：它记录的是源机器
// 上谁参与过，永非本机锚点——HasSession/AddSession 忽略幽灵链接，外来 session id
// 不得把本机 session 锚到任务上。
//
// 从 StripForeignGateSignals 拆出（project-sync）的原因：幽灵化不是信任降级而是
// 事实——跨机器同步后，源机器的 session 在本机不存在，与信任姿态无关。所有导入
// 路径都要应用它——包括刻意保留结果字段（评分/完成）的 lineage 受信
// `forge project import`，它同样绝不能把外来 session id 当本机锚点。
func GhostForeignSessions(s *TaskState) {
	for i := range s.SessionLinks {
		s.SessionLinks[i].Imported = true
	}
}

func StripForeignGateSignals(s *TaskState) {
	// 结果类信号（原有剥离）。
	s.ReviewPassed = false
	s.ReviewedHeadCommit = ``
	s.ReviewedChangeHash = ``
	// ReviewRounds 是 review-pass 历史——与快照字段同信任类：外来 review 打戳不得
	// 虚增本机返工度量（ScoreTask 会把 len(ReviewRounds) 折进 Evidence.ReviewPasses）。
	s.ReviewRounds = nil
	s.Score = nil
	for i := range s.Acceptance {
		s.Acceptance[i].Passed = false
		s.Acceptance[i].AcceptedHeadCommit = ``
		s.Acceptance[i].AcceptedBaseCommit = ``
		s.Acceptance[i].AcceptedChangeHash = ``
		s.Acceptance[i].Output = ``
	}
	// 控制流信号（2026-08-15 修复）。CompletedAt 是关掉所有 CompletedAt==nil 守卫硬检查的总开关
	// ——而「CompletedAt 已设 ⟹ 门禁本机通过过」的本机不变量恰是被不可信源打破的东西。
	// Overrides 是削弱门禁的开关，必须由本机 `forge task override` 决定重建，不能继承。
	s.CompletedAt = nil
	s.Overrides = TaskOverrides{}
	// Kind=="generic" 是所有门禁削弱开关里最强的——比上面刚剥的 Overrides 还强：IsGeneric()
	// 直接短路 ExecuteTaskGate 本体（三道门禁内的全部硬检查），且 runTaskComplete 的 generic
	// 分支位于 IsComplete 检查与 acceptance pre-flight 之前，直入 completeGenericTask——在本地
	// HEAD 伪造三道门禁 Passed + MarkComplete，IsDelivered() 随即放行下游 DependsOn。一个外来
	// 字符串不得携带这种权力。与 Overrides 同规：generic 是本机决策——重建途径是
	// `forge task abort`（丢导入的接续数据）后 `forge task start --kind generic`，或直接接受
	// 导入任务作为 code 任务走全量门禁。注意对同一 ref 直接 `task start --kind generic` 不是
	// 途径：导入的任务文件已存在，start 会以 "task already exists" 拒绝（2026-08-16 二轮复审）。
	s.Kind = ``
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
	// 从幸存的 History 重推当前门禁——伪造的 CurrentGate（如 "task-complete"）不得在状态展示中
	// 残留。所有已通过条目被剥后，NextGate 落在 DefaultGates 的第一个门禁（任务从头重走）。
	//
	// 默认拒绝清扫（T5 重构，2026-08-29）：上方的显式剥离记录每个具名字段不可信
	// 的【原因】，但清单已漏过两次（DocReview/ReportedFindings，2026-08-29 轮；
	// 更早的 Assignment）——每次泄漏都是"新增字段没人记得加"。反射清扫把不在
	// continuity 白名单上的全部导出字段置零：未来字段默认被剥、要随 import 存活
	// 必须显式加白——失败模式从「忘了剥」（门禁绕过）反转为「忘了加白」（丢失
	// 便利数据、立即可见）。在本次重推【之前】跑：CurrentGate 是派生态，清扫清零、
	// 重推回填。
	stripFieldsNotWhitelisted(s)
	s.CurrentGate = s.NextGate()
	// 分派控制流：Assignment.Status==delivered 是下游本机任务 task-verify/task-complete 检查的
	// DependsOn 放行信号——外来「已交付」声明不得在没有本机交付的情况下放行本机依赖门禁。
	// 整体剥掉 Assignment；工作若真在本机继续，本机侧重新分派（forge task assign）。
	s.Assignment = nil
	// 外来源的每条 session 链接都成为幽灵（Imported=true）：它记录源机器谁参与过，永非本机
	// 锚点——HasSession/AddSession 忽略幽灵链接，外来 session id 不得把本机 session 锚到任务上。
	// 幽灵化在 GhostForeignSessions（单一真相源）完成，import 与 migrate 两条路都拿到——
	// migrate 路径此前漏了（2026-08-16 复审）。
	GhostForeignSessions(s)
	// Run 命令作为 spec 保留（交接需要带上「验什么」），但它们是外来作者的可执行字符串——打上
	// 标记，使 verify-acceptance 在首次本机执行前要求显式的、基于审阅的受信（--trust-foreign）。
	s.AcceptanceForeign = len(s.Acceptance) > 0
	// Doc-review 证据是 doc 门禁 L2 硬前置的唯一输入（CheckDocGate 读
	// state.DocReview 的新鲜度/Passed/分数）。跨机同步携带相同 commit hash，外来
	// DocReview 会在零本机重验的情况下满足本机 doc 门禁——与顶部剥离的
	// ReviewPassed 快照同信任类。DocReviewHistory（轮次审计）随之剥离。
	//（2026-08-29 审查轮：功能探针实证 attacker 署名的 DocReview 经 --untrusted
	// 导入直接通过 task-complete 的 doc 门禁。）
	s.DocReview = nil
	s.DocReviewHistory = nil
	// ReportedFindings 按指纹预压制本机 advisory（advisory_dedup 的
	// filterUnreported 丢弃集合中已存在的指纹）——外来预填集合会让本机的
	// cheat-scan/unused-scan 报告静音。
	s.ReportedFindings = nil
}

// trustContinuityWhitelist 是不可信 import 允许携带的 TaskState 字段集——交接
// 存在的意义就是搬运这些 continuity/spec 数据。其余字段被 stripFieldsNot-
// Whitelisted 置零。History/Acceptance/SessionLinks 作为【字段】在白名单里，
// 因为它们的信任边界在条目级（History 只留 FAILED 条目；Acceptance 留 spec 但
// 结果被清洗；SessionLinks 被幽灵化）——这些清洗在 StripForeignGateSignals 里。
var trustContinuityWhitelist = map[string]bool{
	// identity & provenance（身份与出处）
	"TaskRef": true, "Branch": true, "Source": true, "Summary": true,
	"StartedAt": true, "HeadCommit": true, "SessionID": true,
	"ExternalOrigin": true, "OriginTool": true, "ParentTaskRef": true,
	// continuity payload（接续载荷）
	"Goal": true, "Plan": true, "Decisions": true, "NextSteps": true,
	"Blockers": true, "Findings": true, "Artifacts": true, "ResumeStale": true,
	"SessionLinks": true,
	// spec（规格——声明而非结果）
	"DependsOn": true, "Acceptance": true, "PlanScope": true,
	"SpecArtifacts": true, "CrossRepoImpact": true,
	// advisory-neutral inference cache（重算安全的推断缓存）
	"DesignPhases": true,
	// per-entry/custom-handled（条目级清洗的字段级壳）
	"History": true, "AcceptanceForeign": true,
}

// stripFieldsNotWhitelisted 把不在 trustContinuityWhitelist 上的全部可设置导出
// 字段置零。非导出字段（integrityBroken）跳过——那是攻击者无法通过 JSON 伪造的
// 运行时标记。
func stripFieldsNotWhitelisted(s *TaskState) {
	v := reflect.ValueOf(s).Elem()
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() || trustContinuityWhitelist[f.Name] {
			continue
		}
		fv := v.Field(i)
		if fv.CanSet() {
			fv.Set(reflect.Zero(f.Type))
		}
	}
}
