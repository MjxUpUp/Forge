package taskpipeline

// Cross-repo impact gate (multi-repo workspace, docs/design/multi-repo-workspace.md):
// when the task's repo belongs to a multi-repo workspace (~/.forge/workspaces.json),
// task-verify requires an explicit impact declaration on the task — even a
// single-repo change must declare "none" (the declaration itself forces explicit
// thought — the rule-as-code pattern). fail-open by philosophy: the
// workspace manifest is a global user-level store that can be absent/corrupt on any
// machine, so every infra failure degrades to a warn-level advisory, never a block.
//
// 跨仓影响门禁（多仓 workspace，docs/design/multi-repo-workspace.md）：任务所属
// repo 属于多仓 workspace（~/.forge/workspaces.json）时，task-verify 要求任务
// 上有显式影响声明——纯单仓改动也必须声明 "none"（声明动作本身强迫显式思考，
// 规则即代码模式）。哲学上 fail-open：workspace 清单是全局
// 用户级 store，任何机器上都可能缺失/损坏，故一切基建失败降级为 warn 级
// advisory，绝不阻断。

import (
	"fmt"
	"os"
	"strings"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/protocol"
	"github.com/MjxUpUp/Forge/internal/workspace"
)

// crossRepoVerdict classifies one task's declaration against its workspace
// memberships — the pure decision core of checkCrossRepoImpact, kept IO-free so
// the classification table is unit-testable without git/protocol fixtures.
//
// crossRepoVerdict 把任务的声明对照其 workspace 成员资格分类——
// checkCrossRepoImpact 的纯判定核心，刻意无 IO，让分类表无需 git/protocol
// fixture 即可单测。
type crossRepoVerdict int

const (
	// crossRepoSkip: no multi-repo membership — the gate does not apply at all
	// (no checklog entry; silence for the overwhelming single-repo majority).
	//
	// crossRepoSkip：无多仓成员资格——门禁整体不适用（不记 checklog；对占
	// 绝大多数的单仓场景保持静默）。
	crossRepoSkip crossRepoVerdict = iota
	// crossRepoOK: declared (none, or multi with valid repos).
	//
	// crossRepoOK：已声明（none，或 repos 合法的 multi）。
	crossRepoOK
	// crossRepoUndeclared: multi-repo member with no declaration at all.
	//
	// crossRepoUndeclared：多仓成员但完全未声明。
	crossRepoUndeclared
	// crossRepoMultiEmptyRepos: level=multi but no affected repo listed.
	//
	// crossRepoMultiEmptyRepos：level=multi 但没列受影响 repo。
	crossRepoMultiEmptyRepos
	// crossRepoMultiForeignRepos: level=multi listing keys outside the
	// workspace(s) this repo belongs to.
	//
	// crossRepoMultiForeignRepos：level=multi 列了本 workspace 之外的 key。
	crossRepoMultiForeignRepos
	// crossRepoBadLevel: an unknown level string (hand-edited state file).
	//
	// crossRepoBadLevel：未知 level 值（手改 state 文件）。
	crossRepoBadLevel
)

// assessCrossRepoImpact is the pure decision: given the workspaces the current
// repo belongs to and the task's declaration, return the verdict (+ the
// offending foreign keys for crossRepoMultiForeignRepos). Single-repo
// memberships never trigger anything — a one-repo workspace is just a label.
//
// assessCrossRepoImpact 是纯判定：给定当前 repo 所属的 workspace 列表与任务
// 声明，返回 verdict（crossRepoMultiForeignRepos 时附带越界 key 列表）。
// 单仓 workspace 永不触发任何判定——单仓 workspace 只是个标签。
func assessCrossRepoImpact(memberships []workspace.Workspace, impact *CrossRepoImpact) (crossRepoVerdict, []string) {
	// Only multi-repo workspaces create the obligation; collect their member
	// keys as the valid impact target set.
	//
	// 只有多仓 workspace 产生声明义务；其成员 key 的并集即合法影响目标集。
	valid := make(map[string]bool)
	multi := false
	for _, w := range memberships {
		if len(w.Repos) < 2 {
			continue
		}
		multi = true
		for _, r := range w.Repos {
			valid[r.Key] = true
		}
	}
	if !multi {
		return crossRepoSkip, nil
	}
	if impact == nil {
		return crossRepoUndeclared, nil
	}
	switch impact.Level {
	case CrossRepoNone:
		return crossRepoOK, nil
	case CrossRepoMulti:
		if len(impact.Repos) == 0 {
			return crossRepoMultiEmptyRepos, nil
		}
		var foreign []string
		for _, k := range impact.Repos {
			if !valid[k] {
				foreign = append(foreign, k)
			}
		}
		if len(foreign) > 0 {
			return crossRepoMultiForeignRepos, foreign
		}
		return crossRepoOK, nil
	default:
		return crossRepoBadLevel, nil
	}
}

// crossRepoRequired reads the protocol knob (cross_repo_impact: required).
// Any load failure falls back to advisory — protocol.yml is user-editable and
// must never turn a config typo into a gate block.
//
// crossRepoRequired 读 protocol 配置项（cross_repo_impact: required）。任何
// 加载失败回落 advisory——protocol.yml 是用户可编辑文件，绝不能因配置笔误
// 变成门禁阻断。
func crossRepoRequired(root string) bool {
	proto, err := protocol.Load(root)
	if err != nil || proto == nil {
		return false
	}
	return proto.CrossRepoImpact == `required`
}

// crossRepoHowTo is the shared HOW segment of the four-part message (the fix is
// identical for the advisory and the blocked phrasing).
//
// crossRepoHowTo 是四段式消息里共享的 HOW 段（advisory 与 blocked 措辞的修法
// 完全相同）。
const crossRepoHowTo = `HOW: forge task impact --level none（改动纯本仓）或 forge task impact --level multi --repo <key> [--note <说明>]`

// checkCrossRepoImpact is the task-verify wiring of the cross-repo impact gate:
// resolve the current repo key → load the workspace manifest → assess →
// advise/block + recordAudit. Returns a non-nil (GateBlocked) error only in the
// required+undeclared case; every other path is advisory or silent.
//
// checkCrossRepoImpact 是跨仓影响门禁在 task-verify 的接线：解析当前 repo
// key → 加载 workspace 清单 → 判定 → advise/阻断 + recordAudit。仅
// required+未声明 一种情况返回非 nil（GateBlocked）；其余路径全是 advisory
// 或静默。
func checkCrossRepoImpact(root string, state *TaskState) error {
	// Key derivation mirrors DataDirFor: git projects get Key, anything else
	// falls back to PathKey — the same identity the workspace add command
	// stored, so the lookup cannot split.
	//
	// key 推导与 DataDirFor 同款：git 项目用 Key，其余回落 PathKey——与
	// workspace add 命令存入的身份同源，查找不可能分裂。
	key, kerr := forgedata.Key(root)
	if kerr != nil {
		key = forgedata.PathKey(root)
	}

	ws, err := workspace.Load()
	if err != nil {
		// fail-open (INFRA): the manifest is a global store outside the project's
		// control — a corrupt file must degrade to a visible advisory, never block
		// verification. Checked=false marks the check as not-run (vs. run-and-passed).
		//
		// fail-open（INFRA）：清单是项目掌控之外的全局 store——文件损坏必须降级为
		// 可见 advisory，绝不阻断验证。Checked=false 标记检查未执行（区别于执行且通过）。
		recordAudit(root, &checklog.Entry{
			Check:   checklog.CheckCrossRepoImpact,
			Passed:  true,
			Checked: false,
			Level:   checklog.LevelWarn,
			TaskRef: state.TaskRef,
			Detail:  fmt.Sprintf("INFRA: workspace 清单不可读（%v）——cross-repo-impact 检查 fail-open 跳过", err),
		})
		fmt.Fprintf(os.Stderr, "%sworkspace 清单不可读（%v）——cross-repo-impact 检查跳过（fail-open，不阻断）\n", GateAdvisory("[task-verify] "), err)
		return nil
	}

	verdict, foreign := assessCrossRepoImpact(ws.WorkspacesFor(key), state.CrossRepoImpact)
	switch verdict {
	case crossRepoSkip:
		// No multi-repo membership: silence, no log — the check is invisible for
		// the single-repo majority.
		//
		// 无多仓成员资格：静默、不记日志——对单仓多数派本检查不可见。
		return nil
	case crossRepoOK:
		detail := `cross-repo-impact 已声明：none（改动限定本仓）`
		if state.CrossRepoImpact.Level == CrossRepoMulti {
			detail = fmt.Sprintf("cross-repo-impact 已声明：multi（影响 %s）", strings.Join(state.CrossRepoImpact.Repos, `, `))
		}
		recordAudit(root, &checklog.Entry{
			Check:   checklog.CheckCrossRepoImpact,
			Passed:  true,
			Checked: true,
			TaskRef: state.TaskRef,
			Detail:  detail,
		})
		return nil
	case crossRepoUndeclared:
		// Four-part message (WHAT/WHY/HOW/REF), the rule-as-code error contract.
		//
		// 四段式消息（WHAT/WHY/HOW/REF），规则即代码的报错契约。
		what := fmt.Sprintf(`WHAT: 本 repo（key %s）属于多仓 workspace，但任务 %s 未声明跨仓影响`, key, state.TaskRef)
		why := `WHY: 多仓 workspace 的改动可能波及其他 repo——显式声明（none 也是声明）强迫开工前想清楚影响面，避免「改了 A 仓忘了 B 仓接口」的协调事故`
		ref := `REF: docs/design/multi-repo-workspace.md`
		if crossRepoRequired(root) {
			// required mode: BLOCKED must hit disk before returning (the established
			// "BLOCKED 必落盘" pattern — score/dashboard/trace must see the stall).
			// Level explicit: Detail does not start with "BLOCKED: ", DeriveLevel
			// would misclassify the hard block as a plain fail.
			//
			// required 模式：BLOCKED 必先落盘再返回（既有「BLOCKED 必落盘」模式——
			// score/dashboard/trace 必须能看到停滞）。显式标 Level：Detail 不以
			// "BLOCKED: " 起头，DeriveLevel 会把硬阻断误判成普通 fail。
			recordAudit(root, &checklog.Entry{
				Check:   checklog.CheckCrossRepoImpact,
				Passed:  false,
				Checked: true,
				Level:   checklog.LevelBlocked,
				TaskRef: state.TaskRef,
				Detail:  fmt.Sprintf(`cross_repo_impact: required——任务未声明跨仓影响，task-verify 阻断`),
			})
			return GateBlocked("task-verify 拒绝（HARD stop，protocol cross_repo_impact: required）：\n%s\n%s\n%s\n%s", what, why, crossRepoHowTo, ref)
		}
		recordAudit(root, &checklog.Entry{
			Check:   checklog.CheckCrossRepoImpact,
			Passed:  false,
			Checked: true,
			Level:   checklog.LevelAdvisory,
			TaskRef: state.TaskRef,
			Detail:  `ADVISORY: 多仓 workspace 成员任务未声明跨仓影响（默认 advisory；protocol 设 cross_repo_impact: required 可升级为阻断）`,
		})
		fmt.Fprintf(os.Stderr, "%s\n%s\n%s\n%s\n%s\n", GateAdvisory("[task-verify] 未声明跨仓影响"), what, why, crossRepoHowTo, ref)
		return nil
	default:
		// Malformed declaration (empty repos / foreign keys / unknown level):
		// advisory correction hint — the intent to declare is there, the content
		// needs fixing; blocking on typos would punish the cooperative agent.
		//
		// 声明畸形（repos 空 / 越界 key / 未知 level）：advisory 修正提示——
		// 声明的意图在，内容要修；因笔误阻断会惩罚配合的 agent。
		var what string
		switch verdict {
		case crossRepoMultiEmptyRepos:
			what = `WHAT: 声明了 level=multi 但未列任何受影响 repo`
		case crossRepoMultiForeignRepos:
			what = fmt.Sprintf("WHAT: 声明的受影响 repo（%s）不在本 workspace 成员内", strings.Join(foreign, `, `))
		default:
			what = fmt.Sprintf("WHAT: 未知 level %q（合法值 none|multi）", state.CrossRepoImpact.Level)
		}
		recordAudit(root, &checklog.Entry{
			Check:   checklog.CheckCrossRepoImpact,
			Passed:  false,
			Checked: true,
			Level:   checklog.LevelAdvisory,
			TaskRef: state.TaskRef,
			Detail:  `ADVISORY: ` + what,
		})
		fmt.Fprintf(os.Stderr, "%s\n%s\n%s\n", GateAdvisory("[task-verify] 跨仓影响声明需修正"), what, crossRepoHowTo)
		return nil
	}
}
