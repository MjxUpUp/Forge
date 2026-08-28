// Package cli hook_conventions.go — conventions-profile 层 2（注入）的两个
// Go 内 advisory hook：
//
//	conventions-context → SessionStart + PostCompact（会话摘要：≤15 行 always-on 层）
//	conventions-write   → PreToolUse Write|Edit（写入时刻：规范文件指针 + 同目录范例）
//
// 两者永不阻断（advisory 层，fail-open）；仅在真的发射输出时落 checklog 观察
// （CheckConventionsInject）并盖 Delivered/Channel 章——静默路径不落章（与
// failure-track/subagent-track 的 no-stamp 契约一致）。会话级 marker 全部放
// $TMPDIR（短命、OS 清理，F6——与 skill-trigger/test-nudge 同寿命选择）。
//
// 分层对齐业界共识：SessionStart 摘要是 always-on 层，必须极小（≤15 行）；
// 细节按需注入在写入时刻（glob 层的等价物：按目标文件路径命中的指针 + 范例）。
// PostCompact 重注入不设 marker——压缩刚把上下文清空，此时摘要恰是恢复定向
// 最便宜的手段，而 SessionStart 的 marker 防的是 resume 场景的重复注入。
//
// Package cli hook_conventions.go — the two Go-internal advisory hooks of
// conventions-profile layer 2 (injection):
//
//	conventions-context → SessionStart + PostCompact (session digest: ≤15-line always-on layer)
//	conventions-write   → PreToolUse Write|Edit (write-time: instruction pointers + sibling exemplars)
//
// Both never block (advisory, fail-open); both record a checklog observation
// (CheckConventionsInject) with a Delivered/Channel stamp ONLY when they
// actually emit — silent paths stamp nothing (same no-stamp contract as
// failure-track/subagent-track). Session markers live under $TMPDIR
// (short-lived, OS-cleaned, F6 — same lifespan choice as skill-trigger/test-nudge).
package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/conventions"
	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/MjxUpUp/Forge/internal/util"
)

// conventionsMarkerDir is the session-marker home under $TMPDIR. Same lifespan
// class as skill-trigger's markers: session-scoped, OS-cleaned.
//
// conventionsMarkerDir 是 $TMPDIR 下的会话 marker 目录。与 skill-trigger 的
// marker 同寿命类：会话级、OS 定期清理。
func conventionsMarkerDir() string { return filepath.Join(os.TempDir(), "forge-conventions") }

// runConventionsContextHook handles SessionStart + PostCompact: injects the
// conventions digest (profile present) or a one-time init suggestion (profile
// absent but the repo declares conventions). The SessionStart injection is
// marker-gated to once per session id (resume re-fires the event with the same
// id); PostCompact always injects — compaction just wiped the context the
// digest was part of.
//
// runConventionsContextHook 处理 SessionStart + PostCompact：注入规范摘要
// （有档案），或一次性建档建议（无档案但仓库已声明规范）。SessionStart 注入
// 按 session id marker 每会话一次（resume 会以同一 id 重发事件）；PostCompact
// 恒注入——压缩刚把摘要所在的上下文清掉。
func runConventionsContextHook(hookInput HookInput, root, version, agent string) error {
	if root == "" {
		return nil
	}
	dataDir := forgedata.DataDirFor(root)
	profile, err := conventions.LoadProfile(dataDir)
	if err != nil {
		// Corrupt profile: fail-open with a stderr hint; the advisory layer must
		// never take a session down, and `forge conventions show` surfaces the
		// rebuild path for interactive repair.
		//
		// 档案损坏：fail-open + stderr 提示；advisory 层绝不拖垮会话，
		// `forge conventions show` 给交互修复的重建路径。
		fmt.Fprintf(os.Stderr, "[conventions] warning: profile unreadable (%v) — rebuild with `forge conventions init`\n", err)
		return nil
	}

	var inject string
	var extra map[string]string
	var markKind string // 非空 = 发射成功后落的 marker（先发射后落标：发射失败不吞掉本会话该有的注入）
	if profile != nil {
		if hookInput.HookEventName == "SessionStart" && conventionsMarkerExists(hookInput.SessionID, "ctx") {
			return nil
		}
		stale := conventions.Stale(root, profile)
		inject = conventions.SessionInject(profile, conventions.LoadSummary(dataDir), stale)
		extra = map[string]string{"stale": fmt.Sprintf("%t", stale)}
		if hookInput.HookEventName == "SessionStart" {
			markKind = "ctx"
		}
	} else {
		// No profile: offer adoption once per session when the repo declares
		// conventions — a scan tells whether anything is declared.
		//
		// 无档案：仓库已声明规范时每会话提供一次建档建议——扫一下才知道
		// 有没有可建档的东西。
		if conventionsMarkerExists(hookInput.SessionID, "suggest") {
			return nil
		}
		scan, err := conventions.Scan(root)
		if err != nil || len(scan.Instructions) == 0 {
			return nil
		}
		inject = conventions.SuggestInit(conventions.Paths(scan.Instructions))
		extra = map[string]string{"suggestion": "init"}
		markKind = "suggest"
	}
	if inject == "" {
		return nil
	}
	recordConventionsInject(hookInput, root, version, agent,
		"session digest injected", extra)
	emitErr := emitAdvisoryRouted(agent, hookInput.HookEventName, "conventions-context", root, hookInput.SessionID, true, inject)
	if emitErr == nil && markKind != "" {
		conventionsMarkSession(hookInput.SessionID, markKind)
	}
	return emitErr
}

// runConventionsWriteHook handles PreToolUse Write|Edit: for each source/test
// file being written in a directory NOT yet nudged this session, inject the
// instruction-file pointers plus sibling exemplars. Silent when the profile is
// absent (feature unadopted), the target is outside the project root (an
// absolute user path must never ride an injection), or there is nothing to say
// (no instructions and no exemplars — the dir is still marked only when
// something was said).
//
// Known gap (adversarial review 2026-08-28): codex reports file edits as the
// apply_patch tool with the patch in tool_input.command and NO file_path; the
// in-process dispatch (runHook) runs BEFORE step 2a's patch-header synthesis,
// so this hook never sees a file path on codex and stays silent there — the
// session digest (SessionStart tier) carries codex's conventions layer alone.
// Extracting the shared synthesis into a helper would close it when codex
// write-time coverage is wanted.
//
// runConventionsWriteHook 处理 PreToolUse Write|Edit：对每目录首个被写的
// 源/测试文件注入规范文件指针 + 同目录范例。以下情况静默：无档案（未采纳）、
// 目标在项目根外（用户绝对路径绝不能搭注入的便车）、无可奉告（无规范声明
// 且无范例——只有真说了话才标记该目录）。
//
// 已知缺口（2026-08-28 对抗审查）：codex 的文件编辑以 apply_patch 工具上报、
// patch 在 tool_input.command 且无 file_path；进程内分发（runHook）先于步骤
// 2a 的 patch 头合成执行，本 hook 在 codex 上永远拿不到文件路径而静默——
// codex 的 conventions 层由会话摘要（SessionStart 档）独立承载。需要 codex
// 写入时刻覆盖时，把共享合成抽成 helper 即可补上。
func runConventionsWriteHook(hookInput HookInput, root, version, agent string) error {
	if root == "" {
		return nil
	}
	var fields toolInputFields
	if len(hookInput.ToolInput) > 0 {
		if err := json.Unmarshal(hookInput.ToolInput, &fields); err != nil {
			// 与 runHook 主解析路径同纪律：静默吞掉解析错误会让本 hook 在
			// 方言异常的宿主上无声空转——stderr 一行，advisory 层不 fail。
			//
			// Same discipline as runHook's main parse path: silently swallowing
			// parse errors would leave this hook silently no-opping on hosts
			// with dialect quirks — one stderr line, the advisory layer never
			// fails on it.
			fmt.Fprintf(os.Stderr, "[conventions] warning: tool_input parse failed: %v\n", err)
		}
	}
	if fields.FilePath == "" {
		return nil
	}
	source, test := taskpipeline.ClassifyChangedPath(fields.FilePath)
	if !source && !test {
		return nil // 配置/文档/资产：非代码，不注入
	}
	// 根外目标（如 $HOME、/tmp 下的写入）：不属于本项目，无「本仓库规范」
	// 可言——静默跳过，也避免绝对路径进注入文本。
	//
	// Out-of-root targets (writes into $HOME, /tmp, ...): not this project's
	// files, no "this repo's conventions" apply — silent skip, and no absolute
	// path ever rides the injection.
	if rel, err := filepath.Rel(root, fields.FilePath); err != nil || strings.HasPrefix(rel, "..") {
		return nil
	}
	dataDir := forgedata.DataDirFor(root)
	profile, err := conventions.LoadProfile(dataDir)
	if err != nil || profile == nil {
		return nil // 未采纳（无档案）或损坏：静默，fail-open
	}
	relPath := conventions.RelPath(root, fields.FilePath)
	dirKey := filepath.ToSlash(filepath.Dir(relPath))
	if conventionsDirNudged(hookInput.SessionID, dirKey) {
		return nil
	}
	inject := conventions.WriteInject(relPath, profile, conventions.Stale(root, profile),
		conventions.Exemplars(root, fields.FilePath))
	if inject == "" {
		return nil
	}
	// 先发射后标记：发射失败时该目录下一次写入仍能拿到注入（与上方 ctx/suggest
	// marker 的顺序同理——marker 记「已送达」而非「已尝试」）。
	//
	// Emit before marking: on emission failure the next write to this dir can
	// still get the injection (same ordering as the ctx/suggest markers above —
	// a marker records "delivered", not "attempted").
	recordConventionsInject(hookInput, root, version, agent,
		fmt.Sprintf("write-time pointers injected for %s", relPath), map[string]string{"dir": dirKey})
	if err := emitAdvisoryRouted(agent, hookInput.HookEventName, "conventions-write", root, hookInput.SessionID, true, inject); err != nil {
		return err
	}
	conventionsMarkDir(hookInput.SessionID, dirKey)
	return nil
}

// recordConventionsInject writes the observation entry with the delivery stamp
// of the channel the emission actually uses (advisoryEmissionChannel covers
// kimi's queue path — same contract as test-nudge).
//
// recordConventionsInject 落观察条目并盖输出实际使用通道的送达章
// （advisoryEmissionChannel 覆盖 kimi 队列路径——与 test-nudge 同契约）。
func recordConventionsInject(hookInput HookInput, root, version, agent, detail string, extra map[string]string) {
	taskRef := taskRefForSession(root, hookInput.SessionID)
	delivered, channel := advisoryEmissionChannel(agent, hookInput.HookEventName)
	meta := map[string]string{"event": hookInput.HookEventName}
	for k, v := range extra {
		meta[k] = v
	}
	if err := checklog.Record(root, &checklog.Entry{
		Check:        checklog.CheckConventionsInject,
		Passed:       true,
		Checked:      true,
		ToolName:     hookInput.ToolName,
		TaskRef:      taskRef,
		SessionID:    hookInput.SessionID,
		Detail:       detail,
		Source:       checklog.EvidenceDeterministic,
		Level:        checklog.LevelAdvisory,
		Delivered:    &delivered,
		Channel:      channel,
		ForgeVersion: version,
		Meta:         meta,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "[conventions] warning: checklog record failed: %v\n", err)
	}
}

// ---- session marker helpers ($TMPDIR/forge-conventions/<sess>/*) ----

// conventionsMarkerPath builds the marker path for session id + kind.
//
// conventionsMarkerPath 构造 session id + kind 的 marker 路径。
func conventionsMarkerPath(sessionID, kind string) string {
	return filepath.Join(conventionsMarkerDir(), util.SanitizeSessionID(sessionID), kind+".marker")
}

// conventionsMarkerExists reports whether the (session, kind) marker landed.
//
// conventionsMarkerExists 报告 (session, kind) marker 是否已落盘。
func conventionsMarkerExists(sessionID, kind string) bool {
	_, err := os.Stat(conventionsMarkerPath(sessionID, kind))
	return err == nil
}

// conventionsMarkSession lands the (session, kind) marker (best-effort).
//
// conventionsMarkSession 落 (session, kind) marker（尽力而为）。
func conventionsMarkSession(sessionID, kind string) {
	path := conventionsMarkerPath(sessionID, kind)
	_ = os.MkdirAll(filepath.Dir(path), 0755)
	_ = util.AtomicWrite(path, []byte("1"), 0644)
}

// conventionsDirState is the write-hook's per-session nudged-directory set,
// persisted at $TMPDIR/forge-conventions/<sess>/dirs.json.
//
// conventionsDirState 是写 hook 的会话级已提示目录集合，持久化在
// $TMPDIR/forge-conventions/<sess>/dirs.json。
type conventionsDirState struct {
	Dirs map[string]bool `json:"dirs"`
}

// conventionsDirNudged reports whether this session already injected for dir.
//
// conventionsDirNudged 报告本会话是否已对 dir 注入过。
func conventionsDirNudged(sessionID, dir string) bool {
	state := loadConventionsDirState(sessionID)
	return state.Dirs[dir]
}

// conventionsMarkDir records dir as nudged for this session (best-effort).
//
// conventionsMarkDir 把 dir 记为本会话已提示（尽力而为）。
func conventionsMarkDir(sessionID, dir string) {
	path := filepath.Join(conventionsMarkerDir(), util.SanitizeSessionID(sessionID), "dirs.json")
	state := loadConventionsDirState(sessionID)
	if state.Dirs == nil {
		state.Dirs = map[string]bool{}
	}
	state.Dirs[dir] = true
	_ = os.MkdirAll(filepath.Dir(path), 0755)
	_ = util.AtomicWrite(path, mustJSONState(state), 0644)
}

// loadConventionsDirState reads the session's dir state (zero value on any
// error — a lost state file degrades to at most one extra injection, never to
// a lost one).
//
// loadConventionsDirState 读会话的目录状态（任何错误都取零值——状态文件丢失
// 最多多注入一次，绝不会丢一次该有的注入）。
func loadConventionsDirState(sessionID string) conventionsDirState {
	var state conventionsDirState
	path := filepath.Join(conventionsMarkerDir(), util.SanitizeSessionID(sessionID), "dirs.json")
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &state)
	}
	return state
}
