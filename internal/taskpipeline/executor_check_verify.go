package taskpipeline

// executor_check_verify.go — ExecuteTaskGate 拆分（refactor/executor-pipeline 第一步）：
// task-verify 的设计阶段同步与 test-coverage / scope-drift 检查。代码体自 executor.go 的
// ExecuteTaskGate 原样提取，行为等价——仅变量引用改为参数名（gitChanged 由
// syncVerifyDesignPhases 算一次后线程式传递，避免多个 git 子进程双跑）。
//
// executor_check_verify.go — ExecuteTaskGate decomposition (refactor/executor-pipeline
// step 1): task-verify's design-phase sync plus the test-coverage / scope-drift checks.
// Bodies were extracted verbatim from ExecuteTaskGate in executor.go — behavior-equivalent;
// only variable references became parameter names (gitChanged is computed once in
// syncVerifyDesignPhases and threaded through, avoiding a double multi-subprocess run).

import (
	"fmt"
	"os"

	"github.com/MjxUpUp/Forge/internal/checklog"
)

// syncVerifyDesignPhases runs task-verify's phase-aware step (design 3.2/3.6) and returns
// the task's git-changed files for the downstream checks.
//
// phase-aware (design 3.2/3.6): infer the design phase from changed files, write it back
// to state.DesignPhases and persist. This is the loop completion point — downstream
// phaseKeys→Conclusion.DesignPhases→health.PhasePassRate is populated from it; the review
// sub-agent reads state.DesignPhases to load the matching design-artifact-standards/references/phase-X.md checklist.
// Zero friction (path inference, no declaration required). Persist only when phases change
// to avoid per-verify IO churn. inferDesignPhases previously had zero production callers,
// leaving the entire loop as dead code (review BUG-1); wiring it here makes it real.
// taskChangedFiles spawns multiple git subprocesses (testcoverage.go); compute once here.
// gitignore blind spot: all three git sources use --exclude-standard, so ignored design
// artifacts like docs/ are invisible → PhaseRequirement cannot be inferred (loop broken at
// the first link). scanDesignArtifacts reads the filesystem directly to fill the gap, but
// feeds phase inference only — ScopeDrift still uses a pure git view, preventing historical
// design files from being mis-counted as 'this-time actual changes' and triggering drift
// false positives.
//
// phase-aware（设计 3.2/3.6）：按改动文件推断设计阶段，写回 state.DesignPhases 并持久化。
// 回路接通点——下游 phaseKeys→Conclusion.DesignPhases→health.PhasePassRate 据此填充；
// review 子 agent 读 state.DesignPhases 加载对应 design-artifact-standards 的 references/phase-X.md checklist。零摩擦
// （路径推断，不要求声明）。仅 phases 变化时写盘，避免每次 verify 无谓 IO。inferDesignPhases
// 此前零生产调用致整条回路死代码（review BUG-1），此处接通让它名副其实。
// taskChangedFiles 跑多个 git 子进程（testcoverage.go），在此块算一次。
// gitignore 盲区：git 三源都 --exclude-standard，docs/ 等被忽略的设计产物
// 看不到→PhaseRequirement 推不出（回路断第一环）。scanDesignArtifacts 直接读
// 文件系统补上，但只喂 phase 推断——ScopeDrift 仍用纯 git 视角，避免历史设计
// 文件被误算进「本次实改」触发 drift 误报。
func syncVerifyDesignPhases(root string, state *TaskState) (gitChanged []string) {
	gitChanged = taskChangedFiles(root, state)
	scanned := scanDesignArtifacts(root)
	combined := make([]string, 0, len(gitChanged)+len(scanned))
	combined = append(combined, gitChanged...)
	combined = append(combined, scanned...)
	inferred := inferDesignPhases(combined)
	if !designPhasesEqual(state.DesignPhases, inferred) {
		state.DesignPhases = inferred
		// Locked field-merge persist (mutate-under-lock): a bare SaveTaskState over
		// this pre-lock snapshot rolls back concurrent writers (session links,
		// review stamps) — the lock.go contract load→mutate→save must hold.
		//
		// 锁内字段合并持久化：对锁前快照裸 SaveTaskState 会回滚并发写者
		//（session 链接、盖章）——lock.go 的 load→mutate→save 契约必须成立。
		if err := MergeOrPersistTaskState(root, state, func(s *TaskState) error {
			s.DesignPhases = state.DesignPhases
			return nil
		}); err != nil {
			fmt.Fprintln(os.Stderr, "[task-verify] DesignPhases persist failed:", err)
		}
	}
	return gitChanged
}

// checkVerifyTestCoverage is task-verify's test-coverage gate (v0.25 advisory): detects
// 'tests accompany changes' (CLAUDE.md rule 4); when tests are missing it only emits a
// stderr reminder + checklog record and no longer blocks the gate — adapting to loop
// engineering, where adding unit tests is the agent's self-check. CheckTestCoverage is
// still invoked: scoreTask's fallback reuses its verdict and the reminder content comes
// from `missing`. The checklog Passed field truthfully reflects the detection result
// (Passed=false when tests are missing), so forge trace retains the signal; it just is no
// longer used to block the session.
//
// Precomputed-list variant: gitChanged was already computed in syncVerifyDesignPhases for
// phase inference — the coverage gate must not spawn a second taskChangedFiles run
// (2026-08-29 review round: double computation eliminated).
//
// checkVerifyTestCoverage 是 task-verify 的 test-coverage gate（v0.25 advisory）：检测
// 「测试伴随变更」（CLAUDE.md rule 4），缺测试时只 stderr 提醒 + checklog 记录，不再阻塞
// gate——适配 loop engineering，补单测由 agent 自检。CheckTestCoverage 仍调用：scoreTask 的
// fallback 复用其判定，且提醒内容来自 missing。checklog 的 Passed 字段如实反映检测结果
// （缺测试时 Passed=false），让 forge trace 保留信号，只是不再用它阻断会话。
// 预计算列表变体：gitChanged 已在 syncVerifyDesignPhases 为 phase 推断算过——覆盖门禁
// 不得再跑一次 taskChangedFiles（2026-08-29 审查轮：双算消除）。
func checkVerifyTestCoverage(root string, state *TaskState, gitChanged []string) error {
	ok, missing, _ := checkTestCoverageChanged(root, state, gitChanged)
	recordAudit(root, &checklog.Entry{
		Check:   CheckNameTestCoverage,
		Passed:  ok,
		Checked: true,
		TaskRef: state.TaskRef,
		Detail:  testCoverageDetail(ok, missing),
	})
	if !ok {
		// Recurrence-driven hardening (recurrent.go): advisory→hard promotion fires only when
		// BOTH axes hold — the project's testing dimension has gone low ≥threshold times in
		// history (advisory self-discipline proven to fail here) AND this task still has untested
		// source. test-coverage's own escape (FORGE_TEST_COVERAGE / per-task override) is handled
		// inside CheckTestCoverage which returns ok=true, so an escaped task never reaches here;
		// the only ways out of a hardened block are to add the tests (intended) or lower the
		// recurrence floor (FORGE_RECURRENT_HARDEN=disable, no Strength penalty).
		//
		// 复发驱动升硬（recurrent.go）：advisory→hard 仅当两轴皆真才触发——项目 testing 维度历史
		// 低分 ≥阈值次（advisory 自律在此已被证明失效）且本任务仍有未测源码。test-coverage 自身
		// 逃生（FORGE_TEST_COVERAGE / per-task override）由 CheckTestCoverage 内部返回 ok=true 处理，
		// 逃生任务永不进此分支；升硬后唯一出路是真补测试（预期）或下调复发门槛
		// （FORGE_RECURRENT_HARDEN=disable，无 Strength 惩罚）。
		if recurrentHardenEnabled() {
			if cs := loadConclusions(root); dimRecurrent(cs, dimTesting, recurrentThreshold()) && len(missing) > 0 {
				return GateBlocked(`task-verify 拒绝（复发升 HARD stop）：项目 testing 维度已 %d 次低分（达到阈值 %d）——advisory 靠自律在此项目已被证明失效，本次 %d 个源文件仍无配对测试。%s出路：补测试后重跑；或 FORGE_TEST_COVERAGE=disable（降 evidence Weak；重证据任务按证据缩放豁免）；或 FORGE_RECURRENT_HARDEN=disable 回退纯 advisory`, lowDimCounts(cs)[dimTesting], recurrentThreshold(), len(missing), formatMissing(missing))
			}
		}
		fmt.Fprintf(os.Stderr, "%s%s\n", GateAdvisory("[task-verify] "), formatMissing(missing))
	}
	return nil
}

// checkVerifyScopeDrift is task-verify's scope-drift advisory (PlanScope whitelist): when a
// task declares a planned-change whitelist, detect whether actually-changed source exceeds
// the declaration. drift = taskChangedFiles (actual state) vs PlanScope (declared state)
// set difference — analogous to Terraform drift detection (desired vs actual). Purely
// advisory: change-impact analysis recall is only ~44%, scope is a prediction not a
// contract, and drift is a normal signal; here we merely make it measurable and reviewable
// (forge trace / task scope show) — never blocking.
// deterministic (the gate computes ScopeDrift, the agent cannot fake it). CheckScopeDrift
// is excluded from BuildEvidenceChain — it is an advisory observation, not 'verification
// evidence'; counting it would inflate Strength.
//
// checkVerifyScopeDrift 是 task-verify 的 scope-drift advisory（PlanScope whitelist）：任务
// 声明了计划改动白名单时，检测实改源码是否超出声明。drift = taskChangedFiles(实改态) vs
// PlanScope(声明态) 的差集——对应 Terraform drift detection（desired vs actual）。纯
// advisory：变更影响分析召回率仅 ~44%，scope 是 prediction 非 contract，drift 是常态信号；
// 这里只把它从隐性变可度量、可回顾（forge trace / task scope show），绝不阻塞。
// deterministic（gate 实算 ScopeDrift，agent 无法伪造）。CheckScopeDrift 在
// BuildEvidenceChain 中被排除——它是 advisory 观测非「验证证据」，计入会虚高 Strength。
func checkVerifyScopeDrift(root string, state *TaskState, gitChanged []string) error {
	if len(state.PlanScope) > 0 {
		drift := ScopeDrift(gitChanged, state.PlanScope)
		recordAudit(root, &checklog.Entry{
			Check:   checklog.CheckScopeDrift,
			Passed:  len(drift) == 0,
			Checked: true,
			TaskRef: state.TaskRef,
			Detail:  scopeDriftDetail(drift),
		})
		if len(drift) > 0 {
			// Recurrence-driven hardening (recurrent.go): scope-drift is advisory by design
			// (impact-prediction recall ~44%, hardening would reject half of legitimate changes).
			// Promote to BLOCKED only when BOTH the project is recurrent on scope AND this drift is
			// material (≥severe threshold files) — single-file drift stays advisory even on
			// recurrent projects (a normal prediction miss).
			//
			// 复发驱动升硬（recurrent.go）：scope-drift 设计上 advisory（影响预测召回率 ~44%，硬拦会
			// 拒一半合法改动）。仅当项目 scope 复发 且 本次 drift 实质（≥严重阈值文件）两者皆真时升
			// BLOCKED——单文件 drift 即便在复发项目也保持 advisory（正常预测失误）。
			if recurrentHardenEnabled() && scopeDriftSevere(drift) {
				if cs := loadConclusions(root); dimRecurrent(cs, dimScope, recurrentThreshold()) {
					return GateBlocked(`task-verify 拒绝（复发升 HARD stop）：项目 scope 维度已 %d 次低分（达到阈值 %d）——计划漂移已成系统性问题，本次 %d 个源文件超出 PlanScope 声明。出路：forge task scope add <glob> 收编实改；或 FORGE_RECURRENT_HARDEN=disable 回退纯 advisory`, lowDimCounts(cs)[dimScope], recurrentThreshold(), len(drift))
				}
			}
			fmt.Fprintf(os.Stderr, "%sscope-drift——%d 个源码文件超出 PlanScope 声明（advisory 不阻塞；收编: forge task scope add <glob>）\n", GateAdvisory("[task-verify] "), len(drift))
			for _, f := range drift {
				fmt.Fprintf(os.Stderr, "  ⚠ %s\n", f)
			}
		}
	}
	return nil
}
