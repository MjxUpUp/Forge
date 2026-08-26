package workspace

import (
	"fmt"
	"os"
	"strings"

	"github.com/MjxUpUp/Forge/internal/forgedata"
)

// Drift kinds reported by Doctor. All are ADVISORY observations — a global
// manifest cannot hard-refuse drift (repos move, get re-registered, or serve
// two workspaces legitimately); it can only make the drift visible.
//
// Doctor 报告的 drift 类别。全部是 ADVISORY 观测——全局清单不能硬性拒绝
// drift（仓库会移动、重新登记、或合法地服务两个 workspace）；只能让漂移
// 可见。
const (
	// DriftNotRegistered: the member key matches no live registry entry (the
	// repo was never forge-init'd here, was re-keyed by adopt, or faded out).
	//
	// DriftNotRegistered：成员 key 匹配不到任何存活 registry 条目（本机未
	// forge init、被 adopt 换过 key、或已淡出）。
	DriftNotRegistered = `not-registered`
	// DriftPathMissing: the cached display path no longer exists on disk.
	//
	// DriftPathMissing：缓存的展示路径在磁盘上已不存在。
	DriftPathMissing = `path-missing`
	// DriftPathMismatch: the cached path and the registry's current path for
	// the same key differ (the repo moved since add).
	//
	// DriftPathMismatch：同 key 的缓存路径与 registry 现路径不一致（add 之后
	// 仓库搬过家）。
	DriftPathMismatch = `path-mismatch`
	// DriftMultiWorkspace: one key belongs to more than one workspace — legal
	// by design, surfaced so the user can confirm the overlap is intended.
	//
	// DriftMultiWorkspace：一个 key 属于多个 workspace——设计上合法，亮出来
	// 让用户确认重叠是刻意的。
	DriftMultiWorkspace = `multi-workspace`
	// DriftEmpty: the workspace has no members (a create without any add, or
	// all members removed).
	//
	// DriftEmpty：workspace 没有任何成员（create 后没 add，或成员被删光）。
	DriftEmpty = `empty`
	// DriftDepCycle: a task-dependency cycle spanning member repos (a task in
	// repo A DependsOn one in repo B whose chain leads back) — every task on
	// the ring blocks forever at verify/complete. DETECTED AT THE CLI LAYER
	// (internal/cli/workspace_depcycle.go): the workspace package must not
	// import taskpipeline (taskpipeline already imports workspace — the import
	// would cycle), so Doctor() itself never emits this kind; runWorkspaceDoctor
	// appends these findings to Doctor's output.
	//
	// DriftDepCycle：跨成员仓的 task 依赖环（A 仓 task DependsOn B 仓 task，而 B
	// 的依赖链又指回 A）——环上每个 task 在 verify/complete 永久阻塞。检出在
	// CLI 层（internal/cli/workspace_depcycle.go）：workspace 包不能 import
	// taskpipeline（taskpipeline 已 import workspace，import 会成环），故
	// Doctor() 本身绝不产生本类 finding；runWorkspaceDoctor 把这类 finding
	// 追加到 Doctor 的输出之后。
	DriftDepCycle = `dep-cycle`
)

// Finding is one Doctor observation.
//
// Finding 是一条 Doctor 观测。
type Finding struct {
	Workspace    string `json:"workspace"`
	Kind         string `json:"kind"` // Drift* 常量
	Key          string `json:"key,omitempty"`
	Path         string `json:"path,omitempty"`          // 缓存路径（path-missing/path-mismatch 时）
	RegistryPath string `json:"registry_path,omitempty"` // registry 现路径（path-mismatch 时）
	Detail       string `json:"detail"`
}

// registryKeys derives the key→current-path map of live registry entries.
// Live paths come from registry.List() (already pruned of dead entries); each
// key is re-derived from its path — forgedata.Key for git repos, PathKey
// fallback for non-git registrations (same fallback registry.Add uses).
//
// registryKeys 推导存活 registry 条目的 key→现路径映射。存活路径来自
// registry.List()（已精简掉死条目）；每个 key 从路径重新推导——git 仓走
// forgedata.Key，非 git 登记回落 PathKey（registry.Add 同款回落）。
func registryKeys(regPaths []string) map[string]string {
	m := make(map[string]string, len(regPaths))
	for _, p := range regPaths {
		key, err := forgedata.Key(p)
		if err != nil {
			key = forgedata.PathKey(p)
		}
		m[key] = p
	}
	return m
}

// Doctor checks the manifest against reality (the live registry + the
// filesystem) and returns every drift finding, grouped in manifest order.
// regPaths is registry.List()'s output — the caller passes it so Doctor stays
// a pure, testable function with no hidden global reads.
//
// Doctor 把清单与现实（存活 registry + 文件系统）比对，按清单顺序返回全部
// drift finding。regPaths 是 registry.List() 的输出——由调用方传入，让
// Doctor 保持纯函数、可测、无隐藏全局读。
func (f *File) Doctor(regPaths []string) []Finding {
	byKey := registryKeys(regPaths)
	var out []Finding
	seen := make(map[string][]string) // key → 所属 workspace 名（multi-workspace 检出）
	for _, w := range f.Workspaces {
		if len(w.Repos) == 0 {
			out = append(out, Finding{
				Workspace: w.Name,
				Kind:      DriftEmpty,
				Detail:    fmt.Sprintf(`workspace %q 没有任何成员 repo（forge workspace add %s 添加）`, w.Name, w.Name),
			})
			continue
		}
		for _, r := range w.Repos {
			seen[r.Key] = append(seen[r.Key], w.Name)
			regPath, registered := byKey[r.Key]
			if !registered {
				out = append(out, Finding{
					Workspace: w.Name,
					Kind:      DriftNotRegistered,
					Key:       r.Key,
					Path:      r.Path,
					Detail:    fmt.Sprintf("成员 key %s 不在全局注册表（该 repo 未在本机 forge init，或 key 已漂移）；到该 repo 跑 forge init，或 forge workspace remove %s 移除", r.Key, w.Name),
				})
				continue
			}
			if r.Path != `` {
				if _, err := os.Stat(r.Path); os.IsNotExist(err) {
					out = append(out, Finding{
						Workspace:    w.Name,
						Kind:         DriftPathMissing,
						Key:          r.Key,
						Path:         r.Path,
						RegistryPath: regPath,
						Detail:       fmt.Sprintf("缓存路径 %s 已不存在（registry 现路径 %s）；重新 forge workspace add %s 刷新展示缓存", r.Path, regPath, w.Name),
					})
					continue
				}
				if r.Path != regPath {
					out = append(out, Finding{
						Workspace:    w.Name,
						Kind:         DriftPathMismatch,
						Key:          r.Key,
						Path:         r.Path,
						RegistryPath: regPath,
						Detail:       fmt.Sprintf("缓存路径 %s 与 registry 现路径 %s 不一致（仓库已移动）；重新 forge workspace add %s 刷新", r.Path, regPath, w.Name),
					})
				}
			}
		}
	}
	for key, names := range seen {
		if len(names) > 1 {
			out = append(out, Finding{
				Workspace: names[0],
				Kind:      DriftMultiWorkspace,
				Key:       key,
				Detail:    fmt.Sprintf("key %s 同时属于多个 workspace（%s）——设计上允许，确认重叠是刻意的", key, strings.Join(names, `, `)),
			})
		}
	}
	return out
}
