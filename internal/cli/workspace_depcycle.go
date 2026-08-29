package cli

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

// depNodeID 是图节点身份：key + NUL + taskRef（两者都不可能含 NUL，故跨仓
// "k:a:b" 与假想的含冒号 ref 绝不会把两个不同节点并成一个）。展示用回
// key:ref 形式。
func depNodeID(key, ref string) string { return key + "\x00" + ref }

func depNodeLabel(id string) string {
	key, ref, _ := strings.Cut(id, "\x00")
	return key + `:` + ref
}

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
			if !forgedata.ValidKeyFormat(r.Key) {
				continue // 畸形 key 绝不拼进文件系统路径（同 LoadDepState/status 的守卫）
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
