package taskpipeline

// 跨仓影响门禁（多仓 workspace，docs/design/multi-repo-workspace.md）：任务所属
// repo 属于多仓 workspace（~/.forge/workspaces.json）时，task-verify 要求任务
// 上有显式影响声明——纯单仓改动也必须声明 "none"（声明动作本身强迫显式思考，
// 规则即代码模式）。哲学上 fail-open：workspace 清单是全局
// 用户级 store，任何机器上都可能缺失/损坏，故一切基建失败降级为 warn 级
// advisory，绝不阻断。

import (

	"github.com/MjxUpUp/Forge/internal/workspace"
)

// crossRepoVerdict 把任务的声明对照其 workspace 成员资格分类——
// checkCrossRepoImpact 的纯判定核心，刻意无 IO，让分类表无需 git/protocol
// fixture 即可单测。
type crossRepoVerdict int

const (
	// crossRepoSkip：无多仓成员资格——门禁整体不适用（不记 checklog；对占
	// 绝大多数的单仓场景保持静默）。
	crossRepoSkip crossRepoVerdict = iota
	// crossRepoOK：已声明（none，或 repos 合法的 multi）。
	crossRepoOK
	// crossRepoUndeclared：多仓成员但完全未声明。
	crossRepoUndeclared
	// crossRepoMultiEmptyRepos：level=multi 但没列受影响 repo。
	crossRepoMultiEmptyRepos
	// crossRepoMultiForeignRepos：level=multi 列了本 workspace 之外的 key。
	crossRepoMultiForeignRepos
	// crossRepoBadLevel：未知 level 值（手改 state 文件）。
	crossRepoBadLevel
)

// assessCrossRepoImpact 是纯判定：给定当前 repo 所属的 workspace 列表与任务
// 声明，返回 verdict（crossRepoMultiForeignRepos 时附带越界 key 列表）。
// 单仓 workspace 永不触发任何判定——单仓 workspace 只是个标签。
func assessCrossRepoImpact(memberships []workspace.Workspace, impact *CrossRepoImpact) (crossRepoVerdict, []string) {
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

// crossRepoHowTo 是四段式消息里共享的 HOW 段（advisory 与 blocked 措辞的修法
// 完全相同）。
const crossRepoHowTo = `HOW: forge task impact --level none（改动纯本仓）或 forge task impact --level multi --repo <key> [--note <说明>]`
