package taskpipeline

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/hooks"
	"github.com/MjxUpUp/Forge/internal/shellexec"
)

// CheckNameDocsConsistency 是 task-complete 的 docs-consistency advisory 的 checklog 名，
// 让 trace 即使 gate 通过（advisory，永不阻塞）也能照出 drift 信号。
const CheckNameDocsConsistency checklog.CheckName = "docs-consistency-gate"

// CheckNameReviewSnapshot 是 review-snapshot fail-open 场景的 checklog 名——
// fail-open case (审查基线 commit 不可达，amend/rebase 改写历史）。fail-open 是设计本意
// （amend 是正常工作流，强复审会死循环），但必须落盘留痕——让 score/dashboard 能反映
// 「该任务靠 fail-open 而非真复审通过」，事后可追溯，而非只 stderr 一闪而过。
const CheckNameReviewSnapshot checklog.CheckName = "review-snapshot-failopen"

// CheckNameTelemetryMissing 是 work-activity 遥测缺失降级的 checklog 名（host 的
// PostToolUse 分发未接，如 kimi）：两条遥测通道（toollog + hook 分发的 checklog 条目）
// 都为空，此时 work-activity 硬门禁是 100% 误报，降级为 advisory。落盘让
// score/dashboard/trace 能看到该 gate 是在遥测降级下放行的，而非有真实工作活动。
const CheckNameTelemetryMissing checklog.CheckName = "telemetry-missing"

// CheckNameBranchUnmerged 是 task-complete 分支归属 advisory 的 checklog 名：完成时
// 任务的 feature 分支尚未合入主干——「完成」不等于「交付」。仅 advisory，永不阻塞。
const CheckNameBranchUnmerged checklog.CheckName = "branch-unmerged"

// CheckNameGoalOutputMismatch 是 task-complete 目标↔产出粗匹配 advisory 的 checklog
// 名：任务标题与实改文件零关键词交集——交付内容可能有误的信号。仅 advisory，永不阻塞。
const CheckNameGoalOutputMismatch checklog.CheckName = "goal-output-mismatch"

// CheckNameUncommittedAtComplete 是 task-complete 提交顺序 advisory 的 checklog
// 名：complete 时工作区仍有未提交变更——协议规定的顺序是三门禁 → git commit →
// forge task complete（complete 会清空 active task ref，之后的源码写入脱离任务
// 追踪/保护）。2026-08-17 实证：某会话三门禁全过后先 complete、32 分钟后才
// commit——侥幸零实质损害（其间无写入），但窗口真实存在。仅 advisory，永不阻塞。
const CheckNameUncommittedAtComplete checklog.CheckName = "uncommitted-at-complete"

// CheckNameDependencyGate is the checklog name recorded when task-verify/task-complete BLOCKS on an
// undelivered upstream dependency. Matches the established "BLOCKED 必落盘" pattern (skill-decisions /
// test-coverage also record before blocking) so score / dashboard / forge trace can see a task
// repeatedly stalling at the gate waiting on an upstream — otherwise that repeated-stall signal is
// invisible and indistinguishable from "never attempted".
//
// CheckNameDependencyGate 是 task-verify/task-complete 因上游依赖未交付而 BLOCKED 时记的 checklog 名。
// 对齐既有的「BLOCKED 必落盘」模式（skill-decisions / test-coverage 阻断前也落盘），使 score /
// dashboard / forge trace 能照出任务反复卡在门禁等上游——否则该反复停滞信号不可见，与「从未尝试」无法区分。
const CheckNameDependencyGate checklog.CheckName = "dependency-gate"

// skillDecisionsEscapeAdvisoryFmt 是经逃生舱（per-task override 或 env）绕过
// skill-decisions guardrail 时打印的 ADVISORY。包级单一来源，供文案守卫测试断言
// （TestTaskVerify_SkillDecisionsGuardrail_EscapeHatchBypasses 断言它提及 2026-08
// 证据缩放豁免——遗漏它的提示会低估重证据任务的 Strength 状态）。
const skillDecisionsEscapeAdvisoryFmt = `skill %s 的 SKILL.md 改动已用 --skill-decisions disable 逃生舱绕过 guardrail——evidence 强度降级到 Weak（重证据任务按证据缩放豁免）。仍建议跑 'forge skills decide --skill <name> --outcome <accept|reject> --diagnosis <为何改> --revision <改了啥> --evidence <依据>' 记决策，避免下一轮 agent 重复探索已失败方向`

// recordAudit 落盘 checklog 条目并让落盘失败可听见：checklog.Record 的 error 极易
// 被丢掉（调用读起来像纯副作用），但落盘证据不可缺——score/dashboard/trace 都读
// 这些条目，静默写失败会让「task 为何卡在某门禁」无信号。按包内既有 warning 风格
// 打 stderr 后继续：审计自身绝不阻断门禁。
func recordAudit(root string, entry *checklog.Entry) {
	if err := checklog.Record(root, entry); err != nil {
		fmt.Fprintf(os.Stderr, "[forge] warning: checklog record failed: %v\n", err)
	}
}

// taskStartReadGraceWindow 限定 task 的 StartedAt 之前多久的 Read 仍计作
// read-before-edit，用于恢复 task-start/Read race（见
// toolusage.ReadEditCountsGraceWindow）。60s 覆盖并行工具调用窗口——与
// `forge task start` 同时发出的 Read 可能落到前一 task 的 ref 下，和/或时间戳
// 早于 StartedAt，从而被排除出 ReadEditCounts(taskRef)。
const taskStartReadGraceWindow = time.Minute

// ExecuteResult carries the result of a single task gate execution.
//
// ExecuteResult 承载一次 task gate 执行的结果。
type ExecuteResult struct {
	GateID  string
	Passed  bool
	Message string
}

// ExecuteTaskGate runs the checks for a single task gate. auto gates
// (task-implement) execute the corresponding hook script here. non-auto gates
// only verify that the gate has been marked passed (the actual work is done by
// the AI agent).
//
// ExecuteTaskGate 执行单个 task gate 的检查。
// auto gate（task-implement）由本函数跑对应 hook 脚本。
// 非 auto gate 只校验该 gate 是否已被标记为通过（实际工作由 AI agent 完成）。
func ExecuteTaskGate(root string, gateID string, state *TaskState) (*ExecuteResult, error) {
	gate := GateByID(gateID)
	if gate == nil {
		return nil, fmt.Errorf("unknown task gate: %s", gateID)
	}

	// generic kind（调研/设计/纯接续任务）不走门禁检查——这些任务没有代码变更可编译/测试/审查，
	// 强制 3 道门禁会把「开个调研/接续任务」变成过门禁负担，与接续真相源低摩擦持久化的定位相悖。
	// kind 默认空/"code"仍走完整门禁（向后兼容：老 task 无 Kind 字段）。直接标通过，跳过前置
	// gate / 审查快照 / 工作活动 / advisory 全部检查——generic task 的价值在持久化的 plan/决策，
	// 不在门禁。complete 评分也据此分流（见 cli/runTaskComplete）。
	if state.IsGeneric() {
		return &ExecuteResult{
			GateID:  gateID,
			Passed:  true,
			Message: fmt.Sprintf("%s - skipped (generic task: 走接续不走门禁)", gate.Name),
		}, nil
	}

	// 全 gate 共享前置（executor_check_common.go）：DependsOn 门禁（仅 verify/complete）+
	// 前置 gate 链。
	if err := checkDependsOnGate(root, gateID, state); err != nil {
		return nil, err
	}
	if err := checkPrerequisiteGates(gateID, state); err != nil {
		return nil, err
	}

	// task-complete 预检（executor_check_complete.go）：review 硬前置 + 快照一致性、
	// test-coverage 兜底，再到 complete 时的各 advisory。changedFiles 在兜底内算一次、
	// advisory 段复用（taskChangedFiles 起多个 git 子进程——不跑第二遍）。
	if gateID == GateComplete && state.CompletedAt == nil {
		if err := checkCompleteReviewPrereqs(root, state); err != nil {
			return nil, err
		}
		changedFiles, err := checkTestCoverageBackstop(root, state)
		if err != nil {
			return nil, err
		}
		adviseCompleteDelivery(root, state, changedFiles)
	}

	// auto gate：跑实际检查
	if gate.Auto {
		result, err := runAutoChecks(root, gateID, state)
		if err != nil {
			return nil, fmt.Errorf("auto-check failed: %w", err)
		}
		return result, nil
	}

	// 非 auto gate：仅标记为通过
	// 实际工作由 AI agent 经 SKILL.md 指引完成

	// 非 auto gate 的工作活动检查（executor_check_activity.go）：跳过已完成 task（复检）
	// 和最后一个 gate（之后无工作阶段）。
	if !gate.Auto && state.CompletedAt == nil && len(state.History) > 0 && !isLastGate(gateID) {
		if err := checkWorkActivity(root, gateID, state); err != nil {
			return nil, err
		}
	}

	// task-verify 检查（executor_check_verify*.go）。gitChanged 在 syncVerifyDesignPhases
	// 算一次后传给每个消费方；findingsDirty 自 cheat-scan 段流入 unused-scan 段，由后者
	// 在两段之后统一持久化一次。
	if gateID == GateVerify && state.CompletedAt == nil {
		gitChanged := syncVerifyDesignPhases(root, state)
		if err := checkVerifyTestCoverage(root, state, gitChanged); err != nil {
			return nil, err
		}

		// cross-repo-impact 声明（多仓 workspace，crossrepo.go）：本 repo 属于多仓
		// workspace 时，任务必须带显式影响声明——包括 "none"（声明强迫显式思考）。
		// 默认 advisory（fail-open 哲学）；protocol cross_repo_impact: required 把
		// 未声明升级为硬阻断。无多仓成员资格的 repo 静默跳过（不记日志）。
		// CheckCrossRepoImpact 在 BuildEvidenceChain 中排除——它是声明状态的观测，
		// 非验证证据。
		if err := checkCrossRepoImpact(root, state); err != nil {
			return nil, err
		}

		if err := checkVerifyScopeDrift(root, state, gitChanged); err != nil {
			return nil, err
		}
		findingsDirty := scanCheatFindings(root, state)
		adviseVerifyDocGate(root, state)
		scanUnusedFindings(root, state, findingsDirty)
		adviseConventionsLint(root, state)
		adviseTestCapability(root, state)
		adviseSkillEval(root, state, gitChanged)
		if err := checkSkillDecisions(root, state, gitChanged); err != nil {
			return nil, err
		}
		adviseAcceptance(state)
	}

	// 证据链 agent-claim（executor_check_common.go）：仅在 gate 实际通过且任务未完成时记录
	// （重检 completed 任务不重复声明）。
	if !gate.Auto && state.CompletedAt == nil {
		recordAgentClaimAudit(root, gateID, state)
	}

	return &ExecuteResult{
		GateID:  gateID,
		Passed:  true,
		Message: fmt.Sprintf("%s - passed (verified by AI agent)", gate.Name),
	}, nil
}

// PendingDependencies returns the subset of refs not yet delivered — the DependsOn gate's
// block list.
//
// PendingDependencies 返回尚未交付的 ref 子集——DependsOn 门禁的阻断清单。加载失败的 ref 计为
// pending：从未创建、已 abort、或拼错的依赖都非「已交付」，若放过会让 task 校验过一个断裂的依赖边。
// 返回的字符串原样保留入参 ref（在 BLOCKED 信息里拼接）；状态细节在 mine --blocked 处为人展开。
//
// 跨仓语义（多仓 workspace Option B，见 depref.go）：`<key>:<ref>` 条目经 LoadDepState 解析到
// forgedata.RootDir(key)/tasks——只读、无锁。保守规则延伸到跨仓：目标 task 缺失/不可读、或
// key 根本没有数据目录，一律计 PENDING（绝不静默放行）；阻断清单里落的是原样 key:ref，让用户
// 看清门禁在等哪个仓。
//
// API 分工（与 health.DeadlockedDependency 的双形态是有意为之，非 DRY 违反）：本函数接收 root + refs，
// 内部对每个 ref 跑 LoadDepState（per-ref 磁盘读、无缓存），返回裸 ref 列表——契合门禁侧的调用点
// （门禁已有 root，只需知道「卡住没」+阻断清单）。DeadlockedDependency 接收单个 TaskState + lookup 回调，
// 返回 (ref, bool)——契合 health 扫描侧（调用方可缓存/限定 scope/复用 state map）。门禁侧无须 state map
// （它只判本任务），故采 root 形态更直接；若未来门禁也需 deadlock 判定，可在调用点自建 lookup 复用，
// 不必在此统一两 API（强行统一会迫使门禁侧为「只判本任务」而构建无用 state map）。
func PendingDependencies(root string, refs []string) []string {
	var pending []string
	for _, ref := range refs {
		if ref == `` {
			continue
		}
		st, err := LoadDepState(root, ref)
		if err != nil || st == nil {
			pending = append(pending, ref)
			continue
		}
		if !st.IsDelivered() {
			pending = append(pending, ref)
		}
	}
	return pending
}

// runAutoChecks 跑 task gate 的自动化检查。
func runAutoChecks(root string, gateID string, state *TaskState) (*ExecuteResult, error) {
	switch gateID {
	case GateImplement:
		return checkImplement(root, state)
	default:
		return &ExecuteResult{
			GateID:  gateID,
			Passed:  true,
			Message: "no auto-checks defined",
		}, nil
	}
}

// hasCodeChanges 自 task 起算是否真有代码变更。
// 先查工作树变更，再查 untracked 新建文件，再查自 task 记录基准以来的新 commit。
// 基准优先用 state.HeadCommit（task start 时记录）——与分支无关，main/master 上的
// task 中途 commit（AGENTS.md 流程本身鼓励中段 commit）与 feature 分支走同一路径；
// HeadCommit 为空（legacy state）时才回落到 feature 分支的 base 分支探测。非 git
// 仓库优雅退化（返回 true 以免误判）。
func hasCodeChanges(root string, state *TaskState) bool {
	// 检查 1：工作树变更（含 staged 未 commit）
	cmd := exec.Command("git", "-C", root, "diff", "--stat", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return true // 非 git 仓库——放行
	}
	if len(strings.TrimSpace(string(out))) > 0 {
		return true
	}

	// 检查 1.5：untracked 文件——新建尚未 git add。`git diff HEAD` 看不到 untracked
	// 内容，故只新建了文件的任务会被硬拦成「no code changes」（2026-08-29 审查轮）。
	// --exclude-standard 排除被忽略的构建产物。任意 untracked 路径即算：本门禁问的是
	// 「任务动没动东西」而非「是不是源码」——刻意不复用 taskChangedFiles，其归属过滤
	// 可能丢掉非源码文件。探测失败不可判定——落到检查 2，不硬拦。
	out, err = exec.Command("git", "-C", root, "ls-files", "--others", "--exclude-standard").Output()
	if err == nil && len(strings.TrimSpace(string(out))) > 0 {
		return true
	}

	// 检查 2：自 task 记录基准以来的新 commit
	if state != nil {
		if state.HeadCommit != "" {
			// HeadCommit（task start 时记录）是精确基准——与 taskChangedFiles/scoring
			// 同源——且在 main/master 与 feature 分支上行为一致。此前此处按
			// Branch != main/master 设卡，main 分支 task 中途 commit 后会永远落入
			// 「no code changes」死锁。
			cmd = exec.Command("git", "-C", root, "diff", "--name-only", state.HeadCommit+"..HEAD")
			out, err = cmd.Output()
			if err != nil {
				// 基准不可达（amend/rebase 改写历史）——放行，不谎报「无代码改动」。
				return true
			}
			return len(strings.TrimSpace(string(out))) > 0
		}
		if state.Branch != "" && state.Branch != "main" && state.Branch != "master" {
			// 无 HeadCommit 的 legacy state：feature 分支上探测 base 分支
			for _, base := range []string{"main", "origin/main", "master", "origin/master"} {
				cmd = exec.Command("git", "-C", root, "rev-list", "--count", base+"..HEAD")
				out, err = cmd.Output()
				if err == nil {
					return strings.TrimSpace(string(out)) != "0"
				}
			}
			// 找不到任何 base 分支——放行
			return true
		}
	}

	// main/master 上、无 HeadCommit 且无未 commit 改动
	return false
}

// checkImplement 跑（v0.25 advisory）auto-compile + assertion-check hook，把结果
// 记到 checklog，并校验代码改动确实存在。两个 hook 现已非阻塞——只发 advisory
// 提醒、永不 FAIL——所以这里剩下的唯一硬失败是「task 起算无代码改动」（task
// 语义，而非 tech-stack 检查）。下面的 compilePassed/assertPassed 分支保留作
// defense-in-depth，防未来 hook 回归重新引入 FAIL。
func checkImplement(root string, state *TaskState) (*ExecuteResult, error) {
	taskRef := ""
	if state != nil {
		taskRef = state.TaskRef
	}

	// 1. 编译检查——经 EMBEDDED auto-compile 脚本，与 write-time PostToolUse hook
	//（cli.runHook）使用同一份源。改读磁盘上的 .forge/hooks/auto-compile.sh 会让
	// gate 检查可篡改副本、而 write-time hook 检查受信 embed，两者会漂移（被篡改
	// 的磁盘脚本可能让 gate 通过一个 write-time hook 仍会标记为坏的构建）。
	compilePassed, compileInfra, compileOutput := runEmbeddedHook(root, "auto-compile")

	compileDetail := fmt.Sprintf("auto-compile.sh: %s", compileOutput)
	if compileInfra {
		// 基建故障（bash spawn error / exit 126/127——WSL bash 看不到 Windows 临时
		// 路径的特征）：Detail 加 INFRA: 前缀（Passed=false 保留，便于统计基建故障率），
		// 但不判 gate fail——fail-open 对齐 hook 路径 isHookInfraFailure 哲学（环境
		// 问题不是质量失败）。真实编译错误（脚本正常执行、编译器报错）语义不变。
		compileDetail = "INFRA: " + compileDetail
	}
	compileEntry := &checklog.Entry{
		Check:   checklog.CheckAutoCompile,
		Passed:  compilePassed,
		Checked: true,
		TaskRef: taskRef,
		Detail:  compileDetail,
	}
	if compileInfra {
		// 基建故障 fail-open 不是质量失败——Level warn（trace 渲染 ⚠ 而非 ✗）；
		// scoring 侧按 INFRA: 前缀跳过（scoring.go），Passed=false 仅用于故障率统计。
		compileEntry.Level = checklog.LevelWarn
	}
	recordAudit(root, compileEntry)

	if !compilePassed && !compileInfra {
		return &ExecuteResult{
			GateID:  GateImplement,
			Passed:  false,
			Message: fmt.Sprintf("build failed: %s", compileOutput),
		}, nil
	}

	// 2. 断言弱化检查——与 write-time PreToolUse hook 同源 embed。无磁盘回退：
	// embed 即 canonical，故被篡改的 .forge/hooks/assertion-check.sh 无法削弱
	// gate 强制的内容。
	assertPassed, assertInfra, assertOutput := runEmbeddedHook(root, "assertion-check")

	assertDetail := fmt.Sprintf("assertion-check.sh: %s", assertOutput)
	if assertInfra {
		// 基建故障分级同 auto-compile（见上）：INFRA: 前缀记录，不判 gate fail。
		assertDetail = "INFRA: " + assertDetail
	}
	assertEntry := &checklog.Entry{
		Check:   checklog.CheckAssertion,
		Passed:  assertPassed,
		Checked: true,
		TaskRef: taskRef,
		Detail:  assertDetail,
	}
	if assertInfra {
		// 同 auto-compile：基建故障 Level warn，非质量失败。
		assertEntry.Level = checklog.LevelWarn
	}
	recordAudit(root, assertEntry)

	if !assertPassed && !assertInfra {
		return &ExecuteResult{
			GateID:  GateImplement,
			Passed:  false,
			Message: fmt.Sprintf("assertion check failed: %s", assertOutput),
		}, nil
	}

	// 3. 校验确有代码改动（不仅是预编译的 base）。
	if !hasCodeChanges(root, state) {
		return &ExecuteResult{
			GateID:  GateImplement,
			Passed:  false,
			Message: "no code changes detected - build passed but no files modified",
		}, nil
	}

	// 4. 方案前置记录（shift-left）：产出了真实改动但 Plan/Goal 皆空的代码任务 = 跳过
	// 方案阶段——方向错误会拖到审查环节才暴露，那是 实现→审查→返工 循环最贵的一端。
	// 放在代码改动校验之后：失败/空改的 implement 重试不会每轮刷日志。无方案仅
	// advisory，绝不阻塞（小修复合法地不写方案）。
	//
	// 每任务记录一次：实现首次坐实时落条目（有方案=Passed:true）并发提示，之后的
	// implement 重试两者都跳过。每任务一次（2026-08 噪音审计）：同一 advisory 在单
	// 任务上最多重复 3 次（8 个任务 30 条全同文案）——首次之后信息量不再增长。
	// PlanFirstAdvisoryFired 把已发标记持久化进 task state，重试（每次从磁盘重载
	// state）跳过条目与 stderr 提示。
	if state != nil && !state.PlanFirstAdvisoryFired {
		hasPlan := state.Plan != "" || state.Goal != ""
		entry := &checklog.Entry{
			Check:   checklog.CheckPlanFirst,
			Passed:  hasPlan,
			Checked: true,
			TaskRef: taskRef,
			Level:   checklog.LevelAdvisory,
		}
		if hasPlan {
			entry.Detail = "plan/goal recorded before implementation"
		} else {
			entry.Detail = GateAdvisory("[task-implement] 无方案记录（task start 未带 --plan-file/--goal）——方案先行能在方案阶段拦下方向错误，降低审查-返工轮次；小修复可忽略本提示")
			fmt.Fprintf(os.Stderr, "%s\n", entry.Detail)
		}
		recordAudit(root, entry)
		// 先置标记再持久化（best-effort）：即便落盘失败，本进程内的重试也不会重发；
		// 跨进程重试最坏重发一次，优于静默丢失「已记录」的语义。与 DesignPhases 持久化
		// 同款模式（executor.go task-verify 段）。
		// Flag first, persist second (best-effort): even if the write fails, retries
		// within this process stay silent; a cross-process retry re-fires at most once —
		// better than silently losing the "already recorded" semantics. Same pattern as
		// the DesignPhases persistence (executor.go task-verify section).
		state.PlanFirstAdvisoryFired = true
		if err := MergeOrPersistTaskState(root, state, func(s *TaskState) error {
			s.PlanFirstAdvisoryFired = true
			return nil
		}); err != nil {
			fmt.Fprintln(os.Stderr, "[task-implement] plan-first marker persist failed:", err)
		}
	}

	return &ExecuteResult{
		GateID:  GateImplement,
		Passed:  true,
		Message: "code changes present (compile/assertion advisory via hooks)",
	}, nil
}

// runEmbeddedHook 执行 embed 的 hook 脚本（hooks.EmbeddedContent）：写临时文件后
// 用 bash 跑——镜像 write-time 路径（cli.runHook）跑 hook 的方式。gate 层使用与
// write-time 检查同源的 embed 源；改读磁盘 .forge/hooks/*.sh 会让 gate 检查可
// 篡改副本、可能与受信 embed 漂移。root 作为 $1 与工作目录传入，对齐此前磁盘
// hook 调用方式，让脚本经 $1 或 $PWD 解析项目时表现一致。
//
// infra 返回值标记基础设施故障（bash spawn 错误 / exit 126/127——WSL bash 特征，
// 脚本文件不可见）：不是门禁结论。checkImplement 用 INFRA: Detail 前缀记录它们
// （Passed=false 保留，基建故障率可统计）但不判 gate fail——与 write-time hook
// 路径（cli.isHookInfraFailure）的 fail-open 哲学对齐。真实编译失败（脚本正常
// 执行、编译器报错）保持 passed=false, infra=false，gate 照旧 fail。
//
// bash 解析走 shellexec.FindBash——与 write-time 路径同源的 WSL 规避逻辑。
// shellexec 之前的裸 exec.Command("bash", ...) 在原生父进程下解析到 WSL bash，
// 45/53 次 gate auto-compile FAIL 都是 'forge-gate-*.sh: No such file or directory'。
func runEmbeddedHook(root, name string) (passed bool, infra bool, output string) {
	content, ok := hooks.EmbeddedContent(name)
	if !ok {
		// 未知 hook 名是编程错误而非基建故障——fail closed。
		return false, false, fmt.Sprintf("embedded hook %q not found", name)
	}
	tmp, err := os.CreateTemp("", "forge-gate-*.sh")
	if err != nil {
		return false, true, fmt.Sprintf("create temp hook file: %v", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return false, true, fmt.Sprintf("write temp hook file: %v", err)
	}
	tmp.Close()
	// bash 把文件作为参数读；无需 chmod（不直接 exec）。

	bashPath, err := findBashForHook()
	if err != nil {
		return false, true, fmt.Sprintf("resolve bash: %v", err)
	}

	// Windows: os.CreateTemp 返回反斜杠路径（C:\Users\...\forge-gate-*.sh），bash 把反斜杠当
	// 转义吃掉 → "No such file or directory"，task-implement 的 build 检查因此误判失败。
	// filepath.ToSlash 转正斜杠（Git Bash 可解析）。cmd.Dir 仍用原生 root——Go exec 在
	// Windows 启动 bash 子进程要原生路径做 cwd，bash 自身能处理 Windows cwd。
	cmd := exec.Command(bashPath, filepath.ToSlash(tmpPath), filepath.ToSlash(root))
	cmd.Dir = root
	// 剔除继承自父进程的 tool-input env（2026-08-25 review minor）：cmd.Env 未设时
	// 子进程继承 os.Environ()——父 shell 恰好 export 了 FORGE_FILE_PATH 会把
	// assertion-check 从 batch（门禁全量扫描，以 FILE_PATH 为空判别）静默降级成
	// per-edit 分析，门禁职责落空。正常注入路径（cli.runHook）是显式设置这些 env
	// 的，不经本函数，不受影响。FORGE_CONTENT/FORGE_COMMAND 同样会被继承但无消费
	// 者：前者只在 FILE_PATH 非空时读取，后者只被不经本路径的 bash-guard/
	// hazard-guard 读取。
	env := make([]string, 0, len(os.Environ()))
	for _, kv := range os.Environ() {
		k := kv
		if i := strings.IndexByte(kv, '='); i >= 0 {
			k = kv[:i]
		}
		switch k {
		case "FORGE_FILE_PATH", "FORGE_TOOL_NAME", "FORGE_OLD_STRING", "FORGE_NEW_STRING":
			continue
		}
		env = append(env, kv)
	}
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if shellexec.IsHookInfraFailure(err) {
		return false, true, strings.TrimSpace(fmt.Sprintf("%v: %s", err, string(out)))
	}
	return err == nil, false, strings.TrimSpace(string(out))
}

// findBashForHook 解析 embedded gate hook 的 bash 解释器。以包级变量留缝，
// 供测试模拟基建故障（spawn 错误 / exit 127）。生产值为 shellexec.FindBash——
// 与 write-time hook 路径（cli.runHook）同源的 WSL 规避解析；单一真相源，
// 一个修复落点。
var findBashForHook = shellexec.FindBash

// getDisableWorkActivity 返回本 task 是否禁用 work-activity / read-before-edit 检查。方案5: per-task Overrides (forge task override) take
// 优先于进程级 FORGE_WORK_ACTIVITY env——某 task 逃生不会泄漏到同 shell 的其他
// task。env 留作 CI/测试 fallback。
func getDisableWorkActivity(state *TaskState) bool {
	return escapeDisabled(state, escapeWorkActivity, envWorkActivity)
}

// isLastGate 返回给定 gate ID 是否是流水线的最后一个 gate。最后一个 gate
// （task-complete）之后无工作阶段，故跳过工作活动检查——没有可「花时间」的内容。
func isLastGate(gateID string) bool {
	gates := DefaultGates()
	return len(gates) > 0 && gates[len(gates)-1].ID == gateID
}

// behaviorSurfacePrefixes 是改变用户可见行为、而文档守卫看不到（README/
// homepage/插件文档里的行为描述）的仓库路径。按精确文件或目录前缀匹配。
var behaviorSurfacePrefixes = []string{
	"internal/cli/init.go",
	"internal/cli/sync.go",
	"internal/cli/uninstall.go",
	"internal/agentbridge/",
	"internal/skillgen/",
	"internal/hooks/settings.go",
	"internal/protocol/",
	"internal/registry/",
	"internal/forgedata/",
	"internal/projectroot/",
}

// behaviorSurfaceHits 返回触及用户可见行为面（behaviorSurfacePrefixes）的
// 改动文件，供 task-complete 文档 advisory 使用。无命中返回 nil。
func behaviorSurfaceHits(changed []string) []string {
	var out []string
	for _, f := range changed {
		for _, p := range behaviorSurfacePrefixes {
			if f == p || strings.HasPrefix(f, p) {
				out = append(out, f)
				break
			}
		}
	}
	return out
}
