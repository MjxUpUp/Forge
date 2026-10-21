package clitask

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/projectroot"
	"github.com/MjxUpUp/Forge/internal/registry"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/MjxUpUp/Forge/internal/util"
	"github.com/MjxUpUp/Forge/internal/workspace"
	"github.com/spf13/cobra"
)

// task_scope.go：按需拉取接续的共享层（docs/plans/task-resume-pull-model-2026-10-proposal.md）。
// `task list --open` 与 `task resume --match` 都在同一个范围里收集未完成任务、渲染同一种条目——
// 范围解析、跨项目遍历、条目字段只在这里定义一次，两个命令永不分歧。只读：不取锁、不写状态；
// 单个项目扫描失败 stderr 警告并跳过（runTaskMineAllProjects / workspace status 同款）。

// taskScope selects which projects an open-task scan covers.
//
// taskScope 选定未完成任务扫描覆盖哪些项目。
type taskScope int

const (
	scopeProject     taskScope = iota // 当前项目（默认）
	scopeWorkspace                    // 当前项目所属 forge workspace 的全部成员
	scopeAllProjects                  // 全部已接管（managed）项目
)

// addScopeFlags registers the shared --workspace/--all-projects flags on cmd.
//
// addScopeFlags 在 cmd 上注册共享的 --workspace/--all-projects 范围 flag。
func addScopeFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("workspace", false, "范围扩到当前项目所属 forge workspace 的全部成员仓")
	cmd.Flags().Bool("all-projects", false, "范围扩到本机全部已接管（managed）项目")
}

// scopeFromFlags reads the scope flags; giving both is an error.
//
// scopeFromFlags 读取范围 flag；两者同时给出报错。
func scopeFromFlags(cmd *cobra.Command) (taskScope, error) {
	ws, _ := cmd.Flags().GetBool("workspace")
	all, _ := cmd.Flags().GetBool("all-projects")
	switch {
	case ws && all:
		return scopeProject, fmt.Errorf("--workspace 与 --all-projects 二选一")
	case ws:
		return scopeWorkspace, nil
	case all:
		return scopeAllProjects, nil
	default:
		return scopeProject, nil
	}
}

// projectTasks is one project's task states plus how to label the project.
//
// projectTasks 是一个项目的任务状态，外加该项目的展示标签。
type projectTasks struct {
	Project string // 项目 key（forgedata.Key，失败回退目录名）
	Root    string // 展示路径（workspace 成员为 add 时刻的缓存路径）
	States  []*taskpipeline.TaskState
}

// collectScopedTasks scans every project in scope, resolving the current project from cwd.
//
// collectScopedTasks 扫描范围内每个项目，当前项目从 cwd 解析。
func collectScopedTasks(scope taskScope, warn io.Writer) ([]projectTasks, error) {
	if scope == scopeAllProjects {
		roots := registry.ListManaged()
		if len(roots) == 0 {
			return nil, fmt.Errorf(`全局视图无已接管（managed）项目——在项目目录跑 forge init 登记后重试；forge off 退出的项目不参与`)
		}
		return collectRoots(roots, warn), nil
	}
	root, err := projectroot.Find()
	if err != nil {
		return nil, err
	}
	if scope == scopeWorkspace {
		if groups, ok := collectWorkspace(root, warn); ok {
			return groups, nil
		}
		fmt.Fprintln(warn, `[forge] 当前项目不属于任何多仓 forge workspace，按当前项目列出`)
	}
	return collectRoots([]string{root}, warn), nil
}

// collectRoots scans live project roots.
//
// collectRoots 扫描存活的项目根。
func collectRoots(roots []string, warn io.Writer) []projectTasks {
	var out []projectTasks
	for _, r := range roots {
		states, err := taskpipeline.ListTaskStates(r)
		if err != nil {
			fmt.Fprintf(warn, "Warning: 扫描 project %s 失败（跳过）: %v\n", r, err)
			continue
		}
		key, keyErr := forgedata.Key(r)
		if keyErr != nil {
			key = filepath.Base(r)
		}
		out = append(out, projectTasks{Project: key, Root: r, States: states})
	}
	return out
}

// collectWorkspace scans every member of every multi-repo workspace containing root, addressed by key (member paths drift, keys do not); ok=false when root belongs to none.
//
// collectWorkspace 扫描包含 root 的每个多仓 workspace 的全部成员，按 KEY 寻址（成员路径
// add 后可能漂移，key 不会）；root 不属于任何多仓 workspace 时 ok=false。成员按 key 去重
// （一个仓可同属多个 workspace）。key 拼路径前先过 ValidKeyFormat，畸形 key 不得把扫描
// 引出数据 home。
func collectWorkspace(root string, warn io.Writer) ([]projectTasks, bool) {
	f, err := workspace.Load()
	if err != nil {
		fmt.Fprintf(warn, "Warning: 读取 workspace 清单失败: %v\n", err)
		return nil, false
	}
	seen := map[string]bool{}
	var members []workspace.RepoRef
	for _, w := range f.WorkspacesFor(ownRepoKey(root)) {
		if len(w.Repos) < 2 {
			continue
		}
		for _, r := range w.Repos {
			if !seen[r.Key] {
				seen[r.Key] = true
				members = append(members, r)
			}
		}
	}
	if len(members) == 0 {
		return nil, false
	}
	var out []projectTasks
	for _, r := range members {
		if !forgedata.ValidKeyFormat(r.Key) {
			fmt.Fprintf(warn, "Warning: 成员 key %s 格式非法（跳过）\n", r.Key)
			continue
		}
		dir := forgedata.RootDir(r.Key)
		if dir == `` {
			fmt.Fprintf(warn, "Warning: 解析 %s 的数据目录失败（跳过）\n", r.Key)
			continue
		}
		states, err := taskpipeline.ListTaskStatesInDir(filepath.Join(dir, `tasks`))
		if err != nil {
			fmt.Fprintf(warn, "Warning: 扫描 %s 的任务失败（跳过）: %v\n", r.Key, err)
			continue
		}
		out = append(out, projectTasks{Project: r.Key, Root: r.Path, States: states})
	}
	return out, true
}

// openTaskEntry is one incomplete task as `task list --open` and `task resume --match` report it.
//
// openTaskEntry 是 `task list --open` 与 `task resume --match` 共用的未完成任务条目。
// 用户来源字段（Ref/Summary/Branch/NextStep）在构造时已过 stripUnsafeControl。
type openTaskEntry struct {
	Project      string    `json:"project"`
	Root         string    `json:"root"`
	Ref          string    `json:"ref"`
	Summary      string    `json:"summary,omitempty"`
	Branch       string    `json:"branch,omitempty"`
	Gate         string    `json:"gate"`
	NextStep     string    `json:"next_step,omitempty"`
	OpenBlockers int       `json:"open_blockers"`
	Assignment   string    `json:"assignment,omitempty"` // offered | claimed | 其他派活状态；未派活为空
	LastActivity time.Time `json:"last_activity"`
	MatchedOn    string    `json:"matched_on,omitempty"` // 仅 --match：ref|summary|branch|goal
	Score        int       `json:"score,omitempty"`      // 仅 --match：0–100
}

// newOpenTaskEntry builds the entry for s within project p.
//
// newOpenTaskEntry 为项目 p 中的任务 s 构造条目。
func newOpenTaskEntry(p projectTasks, s *taskpipeline.TaskState) openTaskEntry {
	e := openTaskEntry{
		Project:      p.Project,
		Root:         p.Root,
		Ref:          stripUnsafeControl(s.TaskRef),
		Summary:      stripUnsafeControl(s.Summary),
		Branch:       stripUnsafeControl(s.Branch),
		Gate:         renderGateProgress(s),
		OpenBlockers: len(s.OpenBlockers()),
		LastActivity: lastActivity(s),
	}
	if len(s.NextSteps) > 0 {
		e.NextStep = stripUnsafeControl(s.NextSteps[0])
	}
	if s.Assignment != nil {
		e.Assignment = s.Assignment.Status
	}
	return e
}

// lastActivity is the latest of the task's start and its gate results.
//
// lastActivity 取任务开始时间与各门禁结果时间中最晚者。
func lastActivity(s *taskpipeline.TaskState) time.Time {
	t := s.StartedAt
	for _, h := range s.History {
		if h.CompletedAt.After(t) {
			t = h.CompletedAt
		}
	}
	return t
}

// openEntries lists every incomplete task in groups, most recently active first (ties by project, then ref).
//
// openEntries 列出 groups 里全部未完成任务，按最后活动时间倒序（并列时按项目、再按 ref）。
func openEntries(groups []projectTasks) []openTaskEntry {
	var out []openTaskEntry
	for _, g := range groups {
		for _, s := range g.States {
			if s != nil && s.CompletedAt == nil {
				out = append(out, newOpenTaskEntry(g, s))
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].LastActivity.Equal(out[j].LastActivity) {
			return out[i].LastActivity.After(out[j].LastActivity)
		}
		if out[i].Project != out[j].Project {
			return out[i].Project < out[j].Project
		}
		return out[i].Ref < out[j].Ref
	})
	return out
}

// renderOpenEntryLine renders one entry as a single scannable line; withProject prefixes the project key.
//
// renderOpenEntryLine 把条目渲染成一行；withProject 时前缀项目 key（跨项目视图）。
func renderOpenEntryLine(i int, e openTaskEntry, withProject bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "  %d. ", i)
	if withProject {
		b.WriteString("[" + e.Project + "] ")
	}
	b.WriteString(e.Ref)
	if e.Branch != `` && e.Branch != e.Ref {
		b.WriteString(" [分支 " + e.Branch + "]")
	}
	if e.Summary != `` {
		b.WriteString(" — " + util.TruncateRunes(e.Summary, inventoryFieldCap))
	}
	b.WriteString(" — 门禁 " + e.Gate)
	if e.NextStep != `` {
		b.WriteString(" — 下一步: " + util.TruncateRunes(e.NextStep, inventoryFieldCap))
	}
	if e.OpenBlockers > 0 {
		fmt.Fprintf(&b, " — 阻塞 %d", e.OpenBlockers)
	}
	if e.Assignment != `` {
		b.WriteString(" — 派活 " + e.Assignment)
	}
	b.WriteString(" — 最后活动 " + e.LastActivity.Local().Format("2006-01-02 15:04"))
	return b.String()
}

// stderrWriter is where scope scans send per-project warnings (a var so tests can capture it).
//
// stderrWriter 是范围扫描输出单项目警告的去处（变量化以便测试捕获）。
var stderrWriter io.Writer = os.Stderr
