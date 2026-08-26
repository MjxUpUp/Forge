package cli

// Cross-repo dependency-cycle detection for `forge workspace doctor`
// (multi-repo workspace Option B): AddDependency refuses same-repo cycles at
// write time but deliberately never DFS-walks other repos' graphs (a real-time
// cross-repo check would need a global graph lock across DataDirs), so a cycle
// spanning member repos — A's task DependsOn b:B, and b:B's chain leads back
// to a:A — can only be caught here, periodically and advisory, by scanning
// every member's tasks (by KEY: forgedata.RootDir(key)/tasks) and DFS-ing the
// assembled graph. Every task on a ring deadlocks at verify/complete (each
// waits on an upstream that can never deliver), so the finding names the full
// key:ref sequence for manual edge removal.
//
// 跨仓依赖环检测，服务于 `forge workspace doctor`（多仓 workspace Option B）：
// AddDependency 在写入时拒绝本仓环，但刻意不 DFS 遍历他仓图（实时跨仓检查
// 需要跨 DataDir 的全局图锁），故跨成员仓的环——A 的 task DependsOn b:B，
// 而 b:B 的依赖链又指回 a:A——只能在这里周期性地、advisory 地检出：扫描每个
// 成员仓的 task（按 KEY 寻址：forgedata.RootDir(key)/tasks），拼出图后 DFS。
// 环上每个 task 都在 verify/complete 死锁（各等一个永不能交付的上游），故
// finding 给出完整的 key:ref 序列供人工摘边。

import (
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/MjxUpUp/Forge/internal/workspace"
)

// depNodeID is the graph node identity: key + NUL + taskRef (NUL can appear in
// neither, so cross-repo "k:a:b" and a hypothetical colon-carrying ref never
// merge two distinct nodes). The display label is the familiar key:ref.
//
// depNodeID 是图节点身份：key + NUL + taskRef（两者都不可能含 NUL，故跨仓
// "k:a:b" 与假想的含冒号 ref 绝不会把两个不同节点并成一个）。展示用回
// key:ref 形式。
func depNodeID(key, ref string) string { return key + "\x00" + ref }

func depNodeLabel(id string) string {
	key, ref, _ := strings.Cut(id, "\x00")
	return key + `:` + ref
}

// detectWorkspaceDepCycles builds the dependency graph over all member repos'
// tasks and returns every elementary cycle reachable by DFS, each as an
// ordered node-ID sequence (use depNodeLabel for display). Pure and
// deterministic: DFS roots and adjacency are sorted, and a cycle found from
// multiple starting points is reported once (canonical rotation dedup).
//
// Edges: a bare DependsOn entry points at (owner key, ref); a key:ref entry at
// (key, taskRef) — including keys that are NOT workspace members, which become
// dead-end nodes (their tasks are never scanned, so no cycle can pass through
// them, but the edge target still resolves).
//
// detectWorkspaceDepCycles 在全部成员仓的 task 上建依赖图，返回 DFS 可达的每个
// 基本环，每个环是有序节点 ID 序列（展示用 depNodeLabel）。纯函数且确定性：
// DFS 起点与邻接表都排序；同一环从多个起点发现只报一次（规范旋转去重）。
//
// 边规则：裸 DependsOn 条目指向（所属 key，ref）；key:ref 条目指向（key,
// taskRef）——包括非 workspace 成员的 key（成为死端节点：它们的 task 从不被
// 扫描，环不可能穿过，但边目标仍可解析）。
func detectWorkspaceDepCycles(tasksByKey map[string][]*taskpipeline.TaskState) [][]string {
	adj := map[string][]string{}
	for key, states := range tasksByKey {
		for _, s := range states {
			if s == nil {
				continue
			}
			from := depNodeID(key, s.TaskRef)
			for _, dep := range s.DependsOn {
				dk, dr := taskpipeline.SplitDepRef(dep)
				if dk == `` {
					dk = key
				}
				if dr == `` {
					continue
				}
				adj[from] = append(adj[from], depNodeID(dk, dr))
			}
		}
	}
	for n := range adj {
		slices.Sort(adj[n])
	}
	roots := make([]string, 0, len(adj))
	for n := range adj {
		roots = append(roots, n)
	}
	slices.Sort(roots)

	const (
		white = 0 // 未访问
		gray  = 1 // 在当前 DFS 路径上
		black = 2 // 已完结
	)
	color := map[string]int{}
	seen := map[string]bool{} // 规范旋转后的环 → 已报
	var cycles [][]string
	var stack []string
	var visit func(n string)
	visit = func(n string) {
		color[n] = gray
		stack = append(stack, n)
		for _, m := range adj[n] {
			switch color[m] {
			case gray:
				idx := slices.Index(stack, m)
				cyc := slices.Clone(stack[idx:])
				if canon := canonicalDepCycle(cyc); !seen[canon] {
					seen[canon] = true
					cycles = append(cycles, cyc)
				}
			case white:
				visit(m)
			}
		}
		stack = stack[:len(stack)-1]
		color[n] = black
	}
	for _, n := range roots {
		if color[n] == white {
			visit(n)
		}
	}
	return cycles
}

// canonicalDepCycle rotates a cycle so its smallest node ID leads, giving one
// canonical form for dedup regardless of the DFS entry point.
//
// canonicalDepCycle 把环旋转到最小节点 ID 开头，得到与 DFS 入口无关的规范形式
// 供去重。
func canonicalDepCycle(cyc []string) string {
	best := 0
	for i := range cyc {
		if cyc[i] < cyc[best] {
			best = i
		}
	}
	rot := append(slices.Clone(cyc[best:]), cyc[:best]...)
	return strings.Join(rot, "\x00")
}

// depCycleFindings formats detected cycles as Doctor findings (pure: the
// manifest supplies workspace attribution, tasksByKey the graph). A cycle is
// attributed to every workspace (manifest order) holding at least one of its
// member keys — a cross-workspace ring names them all.
//
// depCycleFindings 把检出的环格式化为 Doctor finding（纯函数：清单提供
// workspace 归属，tasksByKey 提供图）。环归属到所有（按清单顺序）含有环上
// 任一成员 key 的 workspace——跨 workspace 的环会点名全部。
func depCycleFindings(f *workspace.File, tasksByKey map[string][]*taskpipeline.TaskState) []workspace.Finding {
	cycles := detectWorkspaceDepCycles(tasksByKey)
	var out []workspace.Finding
	for _, cyc := range cycles {
		onRing := map[string]bool{}
		for _, id := range cyc {
			key, _, _ := strings.Cut(id, "\x00")
			onRing[key] = true
		}
		var names []string
		for _, w := range f.Workspaces {
			for _, r := range w.Repos {
				if onRing[r.Key] {
					names = append(names, w.Name)
					break
				}
			}
		}
		labels := make([]string, 0, len(cyc)+1)
		for _, id := range cyc {
			labels = append(labels, depNodeLabel(id))
		}
		labels = append(labels, depNodeLabel(cyc[0])) // 闭合成环
		out = append(out, workspace.Finding{
			Workspace: strings.Join(names, `, `),
			Kind:      workspace.DriftDepCycle,
			Detail: fmt.Sprintf("跨仓依赖环（环上 task 互相等待，verify/complete 永不放行）：%s；到任一依赖方所在 repo 摘掉一条边（重开任务调整 --depends-on，或编辑对应 task state 的 depends_on）",
				strings.Join(labels, ` → `)),
		})
	}
	return out
}

// collectWorkspaceTasks scans every member repo's tasks by KEY for the cycle
// detection (deduped — a key in two workspaces is read once). Best-effort per
// member: an unreadable member warns on stderr and contributes an empty task
// set (its edges out are invisible, so a ring through it goes unreported this
// run) — never fails the whole doctor.
//
// collectWorkspaceTasks 按 KEY 扫描每个成员仓的 task 供环检测（去重——同属两个
// workspace 的 key 只读一次）。成员级 best-effort：不可读的成员 stderr 警告并
// 贡献空 task 集（它向外的边本次不可见，经它的环本轮漏报）——绝不让整个
// doctor 失败。
func collectWorkspaceTasks(f *workspace.File, stderr io.Writer) map[string][]*taskpipeline.TaskState {
	tasksByKey := map[string][]*taskpipeline.TaskState{}
	for _, w := range f.Workspaces {
		for _, r := range w.Repos {
			if _, ok := tasksByKey[r.Key]; ok {
				continue
			}
			dir := forgedata.RootDir(r.Key)
			if dir == `` {
				continue // GlobalHome 故障——fail-open 跳过
			}
			states, err := taskpipeline.ListTaskStatesInDir(filepath.Join(dir, `tasks`))
			if err != nil {
				fmt.Fprintf(stderr, "Warning: 扫描成员 %s 的任务失败（跨仓环检测跳过该仓）: %v\n", r.Key, err)
				states = nil
			}
			tasksByKey[r.Key] = states
		}
	}
	return tasksByKey
}
