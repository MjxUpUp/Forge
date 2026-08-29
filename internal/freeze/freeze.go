// Package freeze implements the session/project-level write-scope freeze behind
// `forge freeze` and the freeze-guard PreToolUse hook — the forge-side landing of
// on-demand-guards' /freeze directory lock.
//
// Mechanism: `forge freeze <path>...` records the allowed write scope (one or more
// directories/files) in the project state store; the freeze-guard hook
// (PreToolUse Write|Edit, wired BEFORE task-guard) then hard-blocks every Write/Edit
// whose target lies outside all frozen paths. `forge freeze --off` lifts it.
// This replaces the skill-text form of /freeze, whose reliability was the agent
// remembering to self-check every turn — a prompt-level guard that drifts in long
// sessions and after compaction, exactly the scenario it exists for.
//
// This package only handles state persistence and path containment; the hook
// script (FreezeGuardHook in hooks/embed.go) is a thin exit-code forwarder around
// `forge freeze check`, so all path normalization (relative paths, multi-path,
// Windows case-insensitivity) lives here in Go, tested, not in bash.
//
// Package freeze 实现 `forge freeze` 与 freeze-guard PreToolUse hook 背后的
// 会话/项目级写入范围冻结——on-demand-guards /freeze 目录锁定的 forge 侧落地。
//
// 机制：`forge freeze <路径>...` 把允许写入的范围（一个或多个目录/文件）记入
// 项目状态存储；freeze-guard hook（PreToolUse Write|Edit，接线在 task-guard
// 之前）随后硬阻断所有目标不在任一冻结路径内的 Write/Edit。`forge freeze --off`
// 解除。它替代 /freeze 的 skill 文本形态——后者的可靠性等于 agent 每回合记得
// 自检，prompt 型护栏在长会话/压缩后必漂移，恰是它防的场景。
//
// 本包只管状态持久化与路径包含判定；hook 脚本（hooks/embed.go 的
// FreezeGuardHook）是 `forge freeze check` 的薄退出码转发器，故全部路径归一化
// （相对路径、多路径、Windows 大小写不敏感）都在此 Go 包内、有测试，不在 bash。
package freeze

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/util"
)

// State is the persisted freeze record at DataDir/freeze/state.json. Paths are
// canonical absolute paths (see Canonical) — storing canonical form at activation
// keeps the check path a plain prefix comparison.
//
// State 是持久化的 freeze 记录，存 DataDir/freeze/state.json。Paths 为
// canonical 绝对路径（见 Canonical）——激活时即归一化，判定时只做前缀比较。
type State struct {
	Paths     []string  `json:"paths"` // canonical 绝对路径（允许写入的范围）
	UpdatedAt time.Time `json:"updated_at"`
}

// Canonical returns the normalized absolute form of path: relative paths resolve
// against base, the result is Cleaned, and the longest existing ancestor is
// symlink-resolved (macOS /var symlinks; Windows 8.3 short names expand via
// EvalSymlinks too) so activation-time and check-time canonicalization of the same
// directory converge to the same string. Case is NOT folded here — folding is a
// comparison-time concern (contains), so the stored form stays displayable.
//
// Canonical 返回 path 的归一化绝对形式：相对路径相对 base 解析，结果经 Clean，
// 并把最长已存在祖先做 symlink 解析（macOS /var symlink；Windows 8.3 短名也经
// EvalSymlinks 展开），使激活时与判定时的同目录归一化收敛到同一字符串。此处
// 不折叠大小写——折叠是比较时（contains）的事，存储形式保持可展示。
func Canonical(base, path string) string {
	if !filepath.IsAbs(path) {
		path = filepath.Join(base, path)
	}
	path = filepath.Clean(path)
	return resolveExistingAncestor(path)
}

// resolveExistingAncestor resolves symlinks on the longest existing ancestor of
// path and joins the not-yet-existing tail back — a frozen directory (or the Write
// target) may not exist yet, and a one-level climb is not enough for two-segment
// tails. Mirrors cli's resolveSymlinks (kept separate: freeze must not import cli).
// When no segment resolves, path is returned unchanged.
//
// resolveExistingAncestor 对 path 的最长已存在祖先求值 symlink，再把未存在的
// 尾部拼回——冻结目录（或 Write 目标）可能尚不存在，且爬一级够不到两段尾部。
// 镜像 cli 的 resolveSymlinks（独立实现：freeze 不能 import cli）。没有任何
// 段可解析时原样返回。
func resolveExistingAncestor(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	dir := filepath.Dir(path)
	tail := filepath.Base(path)
	for dir != filepath.Dir(dir) { // 未到根
		if resolved, err := filepath.EvalSymlinks(dir); err == nil {
			return filepath.Join(resolved, tail)
		}
		tail = filepath.Join(filepath.Base(dir), tail)
		dir = filepath.Dir(dir)
	}
	return path
}

// contains reports whether target is the frozen path itself or lives underneath
// it. Both inputs must already be canonical (Canonical output). foldCase selects
// case-insensitive comparison (Windows); kept a parameter rather than reading
// runtime.GOOS internally so the case-folding behavior is testable on any host.
// The separator-anchored prefix check rejects sibling-prefix false positives
// (frozen src/foo must not allow src/foobar).
//
// contains 报告 target 是否就是冻结路径本身或位于其下。两个入参须已 canonical
// （Canonical 输出）。foldCase 选择大小写不敏感比较（Windows）；做成参数而非
// 内部读 runtime.GOOS，使大小写折叠行为在任意宿主机上可测。分隔符锚定的前缀
// 检查拒绝兄弟前缀误命中（冻结 src/foo 不得放行 src/foobar）。
func contains(frozen, target string, foldCase bool) bool {
	if foldCase {
		frozen, target = strings.ToLower(frozen), strings.ToLower(target)
	}
	if frozen == target {
		return true
	}
	return strings.HasPrefix(target, frozen+string(filepath.Separator))
}

// foldCaseForHost reports whether path comparison on this host folds case.
// Windows path comparison is case-insensitive (E:\Forge vs e:\forge) — the same
// convention adoptPayloadCwd uses.
//
// foldCaseForHost 报告本宿主机路径比较是否折叠大小写。Windows 路径比较大小写
// 不敏感（E:\Forge vs e:\forge）——与 adoptPayloadCwd 同款约定。
func foldCaseForHost() bool { return runtime.GOOS == "windows" }

// Activate records paths as the freeze scope, replacing any existing state
// (re-activation adjusts the scope). Each path is canonicalized against base
// (the caller's cwd); duplicates collapse. paths must be non-empty.
//
// Activate 把 paths 记为 freeze 范围，替换已有状态（再次激活即调整范围）。
// 每个路径相对 base（调用方 cwd）canonical 化；重复项收敛。paths 必须非空。
func Activate(p *forgedata.Project, base string, paths []string) (*State, error) {
	if len(paths) == 0 {
		return nil, fmt.Errorf("freeze: at least one path is required")
	}
	st := &State{UpdatedAt: time.Now()}
	for _, raw := range paths {
		if strings.TrimSpace(raw) == "" {
			return nil, fmt.Errorf("freeze: empty path")
		}
		c := Canonical(base, raw)
		if !slices.Contains(st.Paths, c) {
			st.Paths = append(st.Paths, c)
		}
	}
	if err := os.MkdirAll(p.FreezeDir(), 0755); err != nil {
		return nil, fmt.Errorf("freeze: create state dir: %w", err)
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("freeze: marshal state: %w", err)
	}
	// AtomicWrite (temp+fsync+rename): freeze's failure direction is fail-open by design
	// (Check allows on parse errors), so a half-written state.json would silently lift
	// the guard while the user believes it is active. An interrupted write must land as
	// "not activated" (explicit), never as "corrupted → always allow".
	//
	// AtomicWrite（临时文件+fsync+rename）：freeze 的失败方向设计为 fail-open
	//（Check 对解析错误放行），半写的 state.json 会在用户以为护栏生效时静默解除。
	// 中断的写入必须落成「未激活」（显式状态），绝不能落成「损坏 → 永远放行」。
	if err := util.AtomicWrite(p.FreezeStatePath(), data, 0644); err != nil {
		return nil, fmt.Errorf("freeze: write state: %w", err)
	}
	return st, nil
}

// Deactivate lifts the freeze by removing the state file. A missing state file
// is a clean no-op (idempotent --off).
//
// Deactivate 删除状态文件解除 freeze。状态文件不存在是干净 no-op（幂等 --off）。
func Deactivate(p *forgedata.Project) error {
	if err := os.Remove(p.FreezeStatePath()); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("freeze: remove state: %w", err)
	}
	return nil
}

// Load reads the freeze state. Returns (nil, nil) when no freeze is active —
// the common case, not an error. A corrupt state file IS an error; the caller
// decides the fail direction (the hook fails open so a broken guard never
// hard-stops every edit).
//
// Load 读取 freeze 状态。无激活 freeze 时返回 (nil, nil)——常态，不是错误。
// 状态文件损坏才是错误；fail 方向由调用方决定（hook fail-open，护栏故障不
// 硬停每次编辑）。
func Load(p *forgedata.Project) (*State, error) {
	data, err := os.ReadFile(p.FreezeStatePath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("freeze: read state: %w", err)
	}
	var st State
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("freeze: parse state %s: %w", p.FreezeStatePath(), err)
	}
	return &st, nil
}

// Check reports whether a Write/Edit of target is allowed under the current
// freeze state. target may be absolute or relative (relative resolves against
// the project root — the hook runs with Dir=root and receives repo-relative
// FORGE_FILE_PATH). No active freeze (nil state or empty paths) allows
// everything. On a state error Check fails open (allowed) and returns the error
// for the caller to surface as a warning.
//
// Check 判定当前 freeze 状态下是否允许写入 target。target 可为绝对或相对路径
// （相对路径相对项目根解析——hook 以 Dir=root 运行且收到 repo-relative 的
// FORGE_FILE_PATH）。无激活 freeze（nil 状态或空 paths）放行一切。状态出错时
// Check fail-open（放行）并返回 error 供调用方作警告展示。
func Check(p *forgedata.Project, target string) (allowed bool, st *State, err error) {
	st, err = Load(p)
	if err != nil {
		return true, nil, err
	}
	if st == nil || len(st.Paths) == 0 {
		return true, st, nil
	}
	ct := Canonical(p.Root, target)
	for _, f := range st.Paths {
		if contains(f, ct, foldCaseForHost()) {
			return true, st, nil
		}
	}
	return false, st, nil
}
