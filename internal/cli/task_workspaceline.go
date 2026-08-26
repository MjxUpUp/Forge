package cli

// Workspace context line for the continuity card / task status (multi-repo
// workspace Step 4, docs/design/multi-repo-workspace.md): when the task's repo
// belongs to a MULTI-repo workspace, the card gains ONE line —
// `Workspace: <name>（N repos）· 跨仓影响: 未声明/none/multi(...)` — so the
// handoff party sees the cross-repo obligation (and the declaration state the
// task-verify gate will judge) without running any command. Deliberately
// fail-OPEN: the manifest is a global user-level store that can be absent or
// corrupt on any machine, so any load trouble silently OMITS the line rather
// than polluting the card with an error. Single-repo memberships render
// nothing (a one-repo workspace is just a label — the cross-repo-impact gate
// in crossrepo.go is equally silent for them).
//
// 接续卡片 / task status 的 workspace 上下文行（多仓 workspace Step 4，
// docs/design/multi-repo-workspace.md）：任务所属 repo 属于多仓 workspace 时
// 卡片加一行 `Workspace: <name>（N repos）· 跨仓影响: 未声明/none/multi(...)`，
// 让接手方不跑命令即见跨仓义务（及 task-verify 门禁将判定的声明状态）。
// 刻意 fail-OPEN：清单是全局用户级 store，任何机器都可能缺失/损坏，加载故障
// 一律静默省略该行，绝不用错误污染卡片。单仓 workspace 不渲染（单仓只是标签
// ——crossrepo.go 的跨仓影响门禁对它们同样静默）。

import (
	"fmt"
	"strings"

	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/MjxUpUp/Forge/internal/workspace"
)

// workspaceContextLine returns the one-line workspace context for root's repo,
// or “ when the line should be omitted (no multi-repo membership, or any
// manifest trouble — fail-open). root == “ (unit tests without a fixture)
// omits the line without touching the store.
//
// workspaceContextLine 返回 root 所在 repo 的单行 workspace 上下文；应省略时
// 返回 “（无多仓成员资格，或任何清单故障——fail-open）。root == “（无
// fixture 的单测）不触碰 store 直接省略。
func workspaceContextLine(root string, impact *taskpipeline.CrossRepoImpact) string {
	if root == `` {
		return ``
	}
	f, err := workspace.Load()
	if err != nil {
		return `` // fail-open：清单损坏静默省略该行，不污染卡片
	}
	// One key may belong to several multi-repo workspaces (a shared library
	// serving two products) — name them all, still one line.
	//
	// 一个 key 可能属于多个多仓 workspace（服务两个产品的共享库仓）——全部
	// 点名，仍保持单行。
	var parts []string
	for _, w := range f.WorkspacesFor(ownRepoKey(root)) {
		if len(w.Repos) >= 2 {
			parts = append(parts, fmt.Sprintf(`%s（%d repos）`, w.Name, len(w.Repos)))
		}
	}
	if len(parts) == 0 {
		return ``
	}
	return fmt.Sprintf(`Workspace: %s· 跨仓影响: %s`, strings.Join(parts, `, `), crossRepoImpactLabel(impact))
}

// crossRepoImpactLabel renders the declaration state segment of the workspace
// line: 未声明 (nil — never declared) / none / multi(key, ...). An unknown
// level (hand-edited state file) is shown raw — the gate's advisory path
// already tells the user to fix it; the card just mirrors.
//
// crossRepoImpactLabel 渲染 workspace 行的声明状态段：未声明（nil——从未
// 声明）/ none / multi(key, ...)。未知 level（手改 state 文件）原样显示——
// 门禁的 advisory 路径已提示修正，卡片只镜像。
func crossRepoImpactLabel(impact *taskpipeline.CrossRepoImpact) string {
	if impact == nil {
		return `未声明`
	}
	switch impact.Level {
	case taskpipeline.CrossRepoNone:
		return `none`
	case taskpipeline.CrossRepoMulti:
		return fmt.Sprintf(`multi(%s)`, strings.Join(impact.Repos, `, `))
	default:
		return impact.Level
	}
}
