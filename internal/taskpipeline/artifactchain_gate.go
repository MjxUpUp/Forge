package taskpipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/MjxUpUp/Forge/internal/artifactchain"
	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/doclint"
	"github.com/MjxUpUp/Forge/internal/forgedata"
)

// artifactchain_gate.go —— 产物链分档执法（artifact-chain-workflow.md §2/§5）：
// task-implement gate 的 rubric/human/hard 事实门禁 + advisory 汇总（每任务一次），
// 以及 task-complete pre-flight 的漂移段（hard/human 档阻断，advisory/rubric 档
// warn；漂移一律作废审批）。宪法：advisory fail-open（坏 schema 回落默认链），
// HARD 只守事实（存在+哈希+审批匹配），逃生舱 env-disable-able 落 checklog 审计行。
const artifactChainDisableEnv = "FORGE_ARTIFACT_CHAIN"

// chainIssue 是链检查的单项结论：什么 stage、什么档、为什么、唯一下一步命令。
type chainIssue struct {
	Stage  string
	Mode   artifactchain.Mode
	Reason string
	Next   string
}

// artifactAbsPath resolves an ArtifactRef to its absolute file path.
func artifactAbsPath(root string, ref ArtifactRef) string {
	return filepath.Join(forgedata.DataDirFor(root), filepath.FromSlash(ref.Path))
}

// ArtifactAbsPath is the exported form of artifactAbsPath for CLI consumers
// (`forge task artifact --extract` reads artifact files through it).
func ArtifactAbsPath(root string, ref ArtifactRef) string {
	return artifactAbsPath(root, ref)
}

// chainIssuesForStage checks one stage against its declared tier. missing 前置
// 与本体事实分开报——「进入下一节点先查上一节点产物」的链序语义落在 blocking
// 档的前置检查上（advisory 档的前置由汇总提示覆盖，不单独执法）。
func chainIssuesForStage(root string, chain *artifactchain.Chain, s artifactchain.Stage, state *TaskState) []chainIssue {
	ref, ok := state.SpecArtifacts[s.Name]
	if !ok {
		return []chainIssue{{
			Stage: s.Name, Mode: s.Mode,
			Reason: "产物未登记",
			Next:   fmt.Sprintf("forge task artifact --set %s --file <%s 文件>", s.Name, s.Produces),
		}}
	}
	var issues []chainIssue
	// 前置存在检查（链序事实）：任何 blocking 档都要求 requires 已登记——前置缺失
	// 意味着本产物建立时跳过了上游节点，链的「上一节点有人读」承诺失效。
	for _, r := range s.Requires {
		if _, has := state.SpecArtifacts[r]; !has {
			produces := r + ".md"
			if upstream, known := chain.StageByName(r); known {
				produces = upstream.Produces
			}
			issues = append(issues, chainIssue{
				Stage: s.Name, Mode: s.Mode,
				Reason: fmt.Sprintf("前置 stage %q 产物未登记（先产出上游节点）", r),
				Next:   fmt.Sprintf("forge task artifact --set %s --file <%s 文件>", r, produces),
			})
		}
	}
	curHash := ArtifactCurrentHash(root, ref)
	drifted := curHash == "" || curHash != ref.Hash
	switch s.Mode {
	case artifactchain.ModeHard:
		// hard 只守事实：引用失配 = 登记后文件被改，登记语义失效。
		if drifted {
			issues = append(issues, chainIssue{
				Stage: s.Name, Mode: s.Mode,
				Reason: "引用漂移（登记后文件已改，哈希失配）",
				Next:   fmt.Sprintf("forge task artifact --set %s --file <%s 文件> 后重跑 gate", s.Name, s.Produces),
			})
		}
	case artifactchain.ModeHuman:
		// human 档：审批签内容哈希——漂移/未审批/审批哈希失配都不是审批。
		apr, has := state.ArtifactApprovals[s.Name]
		switch {
		case drifted:
			issues = append(issues, chainIssue{
				Stage: s.Name, Mode: s.Mode,
				Reason: "引用漂移（文件已改，须重登记后重审批）",
				Next:   fmt.Sprintf("forge task artifact --set %s --file <%s 文件> && forge task artifact --approve %s", s.Name, s.Produces, s.Name),
			})
		case !has:
			issues = append(issues, chainIssue{
				Stage: s.Name, Mode: s.Mode,
				Reason: "人工审批缺失（human 档要求逐份拍板）",
				Next:   fmt.Sprintf("forge task artifact --approve %s", s.Name),
			})
		case apr.Hash != ref.Hash:
			issues = append(issues, chainIssue{
				Stage: s.Name, Mode: s.Mode,
				Reason: "审批哈希与引用失配（审批的不是当前登记内容）",
				Next:   fmt.Sprintf("forge task artifact --approve %s", s.Name),
			})
		}
	case artifactchain.ModeRubric:
		// rubric 档只加机械 L1 lint（意见类判断不进 gate）：对当前文件内容跑
		// doclint，Hard 级 issue 阻断。漂移不单独阻断（lint 的是当前内容——
		// 机械检查随文件走），但 complete 漂移段会留痕。
		hard, err := lintArtifactHardIssues(artifactAbsPath(root, ref))
		if err != nil {
			issues = append(issues, chainIssue{
				Stage: s.Name, Mode: s.Mode,
				Reason: fmt.Sprintf("产物不可读（%v）", err),
				Next:   fmt.Sprintf("forge task artifact --set %s --file <%s 文件>", s.Name, s.Produces),
			})
			break
		}
		if len(hard) > 0 {
			issues = append(issues, chainIssue{
				Stage: s.Name, Mode: s.Mode,
				Reason: fmt.Sprintf("L1 lint 未过（%s）", strings.Join(hard, "; ")),
				Next:   fmt.Sprintf("修复 %s 后 forge task artifact --set %s --file 同文件", s.Produces, s.Name),
			})
		}
	}
	return issues
}

// lintArtifactHardIssues runs doclint L1 on the artifact file and returns the
// Hard-level issue strings (mechanical only — the L2 rubric stays in the
// doc-review human loop).
func lintArtifactHardIssues(absPath string) ([]string, error) {
	issues, err := doclint.LintFile(absPath)
	if err != nil {
		return nil, err
	}
	var hard []string
	for _, is := range issues {
		if is.Hard() {
			hard = append(hard, fmt.Sprintf("%s:%d", is.Rule, is.Line))
		}
	}
	return hard, nil
}

// CheckArtifactChainGate is the task-implement gate's artifact-chain segment
// (artifact-chain-workflow.md §2). Returns a blocking ExecuteResult when any
// rubric/human/hard stage fails its tier; otherwise fires the one-shot
// advisory aggregate for missing advisory stages. nil = pass, no signal.
func CheckArtifactChainGate(root string, state *TaskState, taskRef string) *ExecuteResult {
	if state == nil {
		return nil
	}
	chain, warns := artifactchain.Load(root)
	for _, w := range warns {
		fmt.Fprintln(os.Stderr, w)
	}
	// 逃生舱 bypass 整段链检查（执法与 advisory 汇总都跳过——逃生意图即静默）。
	// 审计行只在有实质绕过时落（阻断档在场；纯 advisory 链本就零阻断，落行是
	// 夸大——审查 P2-6b/P2-D）。
	if escapeDisabled(state, escapeArtifactChain, artifactChainDisableEnv) {
		if chain.Enforcement() {
			recordAudit(root, &checklog.Entry{
				Check:   checklog.CheckEscapeHatch,
				Passed:  true,
				Checked: true,
				Level:   checklog.LevelWarn,
				TaskRef: taskRef,
				Detail:  "escape-hatch: artifact chain bypassed (per-task override or FORGE_ARTIFACT_CHAIN=disable)",
				Meta:    map[string]string{"escape.gate": "artifact-chain", "escape.reason": checklog.EscapeReasonOverride, "escape.owner": taskRef},
			})
		}
		return nil
	}

	// 1) blocking 档执法：任一事实缺失 → gate BLOCKED（每次 gate 都重查，无节流
	// ——阻断不是提醒，重复失败重复报）。纯 advisory 链（Enforcement=false）整段
	// 跳过——默认链零配置零阻断承诺。
	var issues []chainIssue
	if chain.Enforcement() {
		for _, s := range chain.Stages {
			if !s.Mode.Blocking() {
				continue
			}
			issues = append(issues, chainIssuesForStage(root, chain, s, state)...)
		}
	}
	entry := &checklog.Entry{
		Check:   checklog.CheckArtifactChain,
		Passed:  true,
		Checked: true,
		TaskRef: taskRef,
	}
	if len(issues) > 0 {
		var b strings.Builder
		fmt.Fprintf(&b, "[task-implement] 产物链阻断（%d 项）：", len(issues))
		for _, is := range issues {
			fmt.Fprintf(&b, "\n  - %s（%s）: %s —— 下一步: %s", is.Stage, is.Mode, is.Reason, is.Next)
		}
		b.WriteString("\n唯一逃生（落 checklog 审计，降 evidence 强度）: forge task override --artifact-chain disable 或 FORGE_ARTIFACT_CHAIN=disable")
		msg := b.String()
		entry.Passed = false
		entry.Detail = "artifact chain blocked: " + issues[0].Reason
		recordAudit(root, entry)
		return &ExecuteResult{
			GateID:  GateImplement,
			Passed:  false,
			Message: msg,
		}
	}

	// 2) advisory 汇总（无 blocking 配置或 blocking 全过时才有意义）：缺失的
	// advisory 节点一次性提醒（ArtifactAdvisoryFired 持久化，同 plan-first 噪音
	// 纪律——阻断档重查，advisory 只发一次）。
	var missing []string
	for _, s := range chain.Stages {
		if s.Mode == artifactchain.ModeAdvisory {
			if _, has := state.SpecArtifacts[s.Name]; !has {
				missing = append(missing, s.Name)
			}
		}
	}
	if len(missing) == 0 || state.ArtifactAdvisoryFired {
		if len(missing) == 0 {
			recordAudit(root, entry)
		}
		return nil
	}
	detail := GateAdvisory("%s", fmt.Sprintf("[task-implement] 产物链节点缺失: %s——按 schema.yaml 逐段产出（%s）；一次性提醒，不阻断", strings.Join(missing, "/"), nextArtifactHint(chain, missing)))
	fmt.Fprintln(os.Stderr, detail)
	recordAudit(root, &checklog.Entry{
		Check:   checklog.CheckArtifactChain,
		Passed:  false,
		Checked: true,
		Level:   checklog.LevelAdvisory,
		TaskRef: taskRef,
		Detail:  "artifact chain advisory: missing " + strings.Join(missing, "/"),
	})
	state.ArtifactAdvisoryFired = true
	if err := MergeOrPersistTaskState(root, state, func(s *TaskState) error {
		s.ArtifactAdvisoryFired = true
		return nil
	}); err != nil {
		fmt.Fprintln(os.Stderr, "[task-implement] artifact advisory marker persist failed:", err)
	}
	return nil
}

// nextArtifactHint renders the next-missing stage's instruction (§9 nextSteps
// 单命令纪律的 gate 语境收敛：给「写哪个产物、怎么写」的最短指引).
func nextArtifactHint(chain *artifactchain.Chain, missing []string) string {
	if len(missing) == 0 {
		return ""
	}
	if s, ok := chain.StageByName(missing[0]); ok && s.Instruction != "" {
		return s.Name + ": " + s.Instruction
	}
	return missing[0] + ".md"
}

// VerifyArtifactsReport re-verifies every SpecArtifacts ref for `forge task
// artifact --verify`: drifts get an artifact-drift warn row (auditable) and
// are returned as "stage: detail" strings. Non-mutating (approvals untouched
// — voiding is the complete pre-flight's job).
func VerifyArtifactsReport(root string, state *TaskState) []string {
	if state == nil {
		return nil
	}
	var drifted []string
	names := make([]string, 0, len(state.SpecArtifacts))
	for name := range state.SpecArtifacts {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		ref := state.SpecArtifacts[name]
		if ArtifactCurrentHash(root, ref) == ref.Hash {
			continue
		}
		drifted = append(drifted, fmt.Sprintf("%s: 引用漂移（ref %s ≠ 文件）", name, ref.Hash))
		recordAudit(root, &checklog.Entry{
			Check:   checklog.CheckArtifactDrift,
			Passed:  true,
			Checked: true,
			Level:   checklog.LevelWarn,
			TaskRef: state.TaskRef,
			Detail:  fmt.Sprintf("artifact %q drifted (manual --verify)", name),
		})
	}
	return drifted
}

// CheckArtifactChainDrift is the task-complete pre-flight's drift segment
// (artifact-chain-workflow.md §5). It verifies every SpecArtifacts ref:
// drift voids the stage's approval unconditionally and blocks complete for
// hard/human tiers; advisory/rubric tiers get a warn row. Returned strings are
// blocking reasons (empty = pass).
//
// 审查 P1-1 修正：整段扫描跑在 MergeOrPersistTaskState 锁内（重载盘上最新
// state），漂移判定与审批作废基于并发一致的引用——complete 前另一 session
// 重登记+重审批的场景不再被旧快照误判漂移、误删有效审批。
func CheckArtifactChainDrift(root string, state *TaskState) []string {
	if state == nil || len(state.SpecArtifacts) == 0 {
		return nil
	}
	if escapeDisabled(state, escapeArtifactChain, artifactChainDisableEnv) {
		recordAudit(root, &checklog.Entry{
			Check:   checklog.CheckEscapeHatch,
			Passed:  true,
			Checked: true,
			Level:   checklog.LevelWarn,
			TaskRef: state.TaskRef,
			Detail:  "escape-hatch: artifact drift pre-flight bypassed (per-task override or FORGE_ARTIFACT_CHAIN=disable)",
			Meta:    map[string]string{"escape.gate": "artifact-chain", "escape.reason": checklog.EscapeReasonOverride, "escape.owner": state.TaskRef},
		})
		return nil
	}
	chain, warns := artifactchain.Load(root)
	for _, w := range warns {
		fmt.Fprintln(os.Stderr, w)
	}

	var (
		blocked []string
		drifted []string
		rows    []checklog.Entry
	)
	// 锁内重载-扫描-作废：漂移判定、审批删除、blocked 结论共用同一份锁内快照
	//（锁外 state 形参只作入口判断，不作判定依据）。
	if err := MergeOrPersistTaskState(root, state, func(s *TaskState) error {
		blocked, drifted, rows = nil, nil, nil
		for stageName, ref := range s.SpecArtifacts {
			if ArtifactCurrentHash(root, ref) == ref.Hash {
				continue
			}
			mode := artifactchain.ModeAdvisory // 链外产物（legacy/手动注册）按 advisory 处理——漂移留痕不拦
			if st, ok := chain.StageByName(stageName); ok {
				mode = st.Mode
			}
			drifted = append(drifted, stageName)
			// 漂移一律作废审批：哈希不再匹配文件的审批不是审批（§5 事实语义）。
			// 双侧删除：锁内副本（持久化）+ 调用方快照（complete 尾部 ScoreTask
			// 整包回写以调用方快照为源——只删副本会被旧快照「复活」，审查 P2-A）。
			delete(s.ArtifactApprovals, stageName)
			if state.ArtifactApprovals != nil {
				delete(state.ArtifactApprovals, stageName)
			}
			if mode.Blocking() {
				blocked = append(blocked, fmt.Sprintf("%s（%s）: 引用漂移——重登记 %s 并重审批", stageName, mode, stageName))
				rows = append(rows, checklog.Entry{
					Check:   checklog.CheckArtifactDrift,
					Passed:  false,
					Checked: true,
					TaskRef: s.TaskRef,
					Detail:  fmt.Sprintf("artifact %q drifted (ref hash %s no longer matches file), tier %s blocks complete", stageName, ref.Hash, mode),
				})
			} else {
				rows = append(rows, checklog.Entry{
					Check:   checklog.CheckArtifactDrift,
					Passed:  true,
					Checked: true,
					Level:   checklog.LevelWarn,
					TaskRef: s.TaskRef,
					Detail:  fmt.Sprintf("artifact %q drifted (ref hash %s no longer matches file), tier %s: warn only", stageName, ref.Hash, mode),
				})
			}
		}
		return nil
	}); err != nil {
		// fail-open（基建错误不是事实结论，下次 complete 会重查）——但「留痕」
		// 要真留：warn 级审计行落 checklog（审查 P2-B）。
		recordAudit(root, &checklog.Entry{
			Check:   checklog.CheckArtifactDrift,
			Passed:  true,
			Checked: false,
			Level:   checklog.LevelWarn,
			TaskRef: state.TaskRef,
			Detail:  fmt.Sprintf("artifact drift scan failed (fail-open): %v", err),
		})
		fmt.Fprintln(os.Stderr, "[task-complete] artifact drift scan failed (fail-open 放行，已落审计行):", err)
		return nil
	}
	for i := range rows {
		recordAudit(root, &rows[i])
	}
	if len(drifted) > 0 {
		fmt.Fprintf(os.Stderr, "[task-complete] 漂移产物审批已作废: %s（重登记后 forge task artifact --approve <stage>）\n", strings.Join(drifted, ", "))
	}
	return blocked
}
