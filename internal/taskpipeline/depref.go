package taskpipeline

// Cross-repo DependsOn refs (multi-repo workspace Option B,
// docs/design/multi-repo-workspace.md): a DependsOn entry may carry a
// `<key>:` prefix addressing a task in ANOTHER member repo's DataDir
// (forgedata.RootDir(key)/tasks); a bare ref keeps the same-repo meaning with
// zero behavior change. This file owns the ref syntax (SplitDepRef) and the
// resolution read path (LoadDepState). All cross-repo reads are read-only and
// fail-CONSERVATIVE at the gate (an unresolvable target counts as pending,
// never as delivered — a broken edge must not silently unblock), while every
// manifest/infra failure on the write side (cli validation) is fail-OPEN
// advisory, matching the crossrepo.go gate philosophy.
//
// 跨仓 DependsOn ref（多仓 workspace Option B，
// docs/design/multi-repo-workspace.md）：DependsOn 条目可带 `<key>:` 前缀，
// 寻址另一个成员仓 DataDir（forgedata.RootDir(key)/tasks）里的 task；裸 ref
// 保持本仓语义，零行为变化。本文件承载 ref 语法（SplitDepRef）与解析读路径
// （LoadDepState）。跨仓读一律只读，门禁侧 fail-CONSERVATIVE（解析不出的目标
// 计 pending，绝不当已交付——断裂的依赖边不得静默放行）；写入侧（cli 校验）
// 的清单/基建失败则 fail-OPEN 降级 advisory，与 crossrepo.go 门禁哲学一致。

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/MjxUpUp/Forge/internal/forgedata"
)

// SplitDepRef splits a DependsOn entry into (key, taskRef). No colon — or a
// LEADING colon (":foo", a degenerate ref) — returns ("", ref): same-repo,
// the pre-workspace meaning. Otherwise the FIRST colon splits: "k:a:b" →
// ("k", "a:b"), so a taskRef may itself contain colons while the key never
// can. A trailing colon ("key:") yields an empty taskRef whose load always
// fails → the gate treats it as pending (conservative).
//
// Boundary basis — can a bare task ref itself contain ':'? Branch-derived
// refs never do (git check-ref-format forbids ':' in refnames, and
// taskcontext.ParseBranchName output inherits that), so the dominant ref
// source is unambiguous. An explicit `forge task start --ref` CAN carry ':'
// (SanitizeRef maps ':' → '-' for the filename; LoadTaskState's collision
// doc cites feat/foo:bar as a real sanitize-collision shape). Under this
// syntax such a ref is read as key:ref inside DependsOn — an accepted,
// documented trade-off: the CLI validation refuses a colon ref whose prefix
// is not a member key of this repo's workspaces, steering the user to drop
// the colon, instead of silently deadlocking the gate on a misread.
//
// SplitDepRef 把 DependsOn 条目拆成 (key, taskRef)。无冒号——或冒号在首位
// （":foo"，畸形 ref）——返回 (""，ref)：本仓语义，workspace 之前的含义。
// 否则按第一个冒号拆分："k:a:b" → ("k", "a:b")，故 taskRef 自身可含冒号而
// key 绝不。结尾冒号（"key:"）得到空 taskRef，加载必失败 → 门禁按 pending
// 处理（保守）。
//
// 边界依据——裸 task ref 自身可能含 ':' 吗？branch 推导的 ref 绝不会（git
// check-ref-format 禁止 refname 含 ':'，taskcontext.ParseBranchName 的输出
// 继承此约束），故占主导的 ref 来源无歧义。显式 `forge task start --ref`
// 可以带 ':'（SanitizeRef 把 ':' 压成 '-' 做文件名；LoadTaskState 的串号
// 注释以 feat/foo:bar 为真实碰撞形态）。在本语法下这种 ref 在 DependsOn 里
// 会被读成 key:ref——这是接受的、写明文档的权衡：CLI 校验会拒绝前缀不是本
// repo 所属 workspace 成员 key 的含冒号 ref，引导用户去掉冒号，而不是让
// 门禁在误读上静默死锁。
func SplitDepRef(ref string) (key, taskRef string) {
	i := strings.IndexByte(ref, ':')
	if i <= 0 {
		return ``, ref
	}
	return ref[:i], ref[i+1:]
}

// LoadDepState resolves a DependsOn entry to its task state, cross-repo aware:
// a bare ref loads from this repo's DataDir (identical to LoadTaskState); a
// key:ref loads from forgedata.RootDir(key)/tasks — by KEY, never by a cached
// path (the member's filesystem path may have drifted since it joined the
// workspace; the key never does). Read-only, no locking: the dependent only
// cares about the target's IsDelivered, a stale read just re-checks next gate
// run. Any resolution failure (unknown key dir, missing/unreadable/corrupt
// state, TaskRef mismatch) is an error — callers treat it as not-delivered.
//
// LoadDepState 把 DependsOn 条目解析成对应 task state，跨仓感知：裸 ref 从
// 本仓 DataDir 加载（与 LoadTaskState 完全相同）；key:ref 从
// forgedata.RootDir(key)/tasks 加载——按 KEY 寻址，绝不走缓存路径（成员仓
// 文件系统路径入组后可能漂移，key 不会）。只读、无锁：依赖方只关心目标的
// IsDelivered，读到旧值无非下次门禁重查。任何解析失败（key 无数据目录、
// state 缺失/不可读/损坏、TaskRef 不匹配）都返回错误——调用方按未交付处理。
func LoadDepState(root, ref string) (*TaskState, error) {
	key, taskRef := SplitDepRef(ref)
	if key == `` {
		return LoadTaskState(root, ref)
	}
	dir := forgedata.RootDir(key)
	if dir == `` {
		return nil, fmt.Errorf("dep %q: 项目 key %q 无数据目录（GlobalHome 不可解析）", ref, key)
	}
	// The key is attacker-controllable (DependsOn travels inside tasks/*.json
	// bundles): reject anything outside the two legitimate key shapes before it
	// is joined into a filesystem path, so a crafted key cannot steer the
	// read-only scan outside the data home.
	//
	// key 是攻击者可控输入（DependsOn 随 tasks/*.json bundle 旅行）：拼进文件
	// 系统路径前先拒绝两种合法形态之外的 key，防构造 key 把只读扫描引出数据
	// home。
	if !forgedata.ValidKeyFormat(key) {
		return nil, fmt.Errorf("dep %q: 项目 key %q 格式非法（合法形态 hash12 或 p+hash12）", ref, key)
	}
	st, err := LoadTaskStateInDir(filepath.Join(dir, `tasks`), taskRef)
	if err != nil {
		return nil, fmt.Errorf("dep %q: %w", ref, err)
	}
	return st, nil
}
