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
package hookdispatch

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/conventions"
	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/hostcap"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/MjxUpUp/Forge/internal/util"
)

// conventionsMarkerDir 是 $TMPDIR 下的会话 marker 目录。与 skill-trigger 的
// marker 同寿命类：会话级、OS 定期清理。
func conventionsMarkerDir() string { return filepath.Join(os.TempDir(), "forge-conventions") }

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
	emitErr := EmitAdvisoryRouted(agent, hookInput.HookEventName, "conventions-context", root, hookInput.SessionID, true, inject)
	if emitErr == nil && markKind != "" {
		conventionsMarkSession(hookInput.SessionID, markKind)
	}
	return emitErr
}

// runConventionsWriteHook 处理 PreToolUse Write|Edit：对每目录首个被写的
// 源/测试文件注入规范文件指针 + 同目录范例。以下情况静默：无档案（未采纳）、
// 目标在项目根外（用户绝对路径绝不能搭注入的便车）、无可奉告（无规范声明
// 且无范例——只有真说了话才标记该目录）。
//
// codex 的文件编辑以 apply_patch 工具上报（hostcap PatchToolName）、patch 在
// tool_input.command 且无 file_path——本 hook 用 runHook 路径门禁共享的
// applyPatchFilePath 从首个 patch 头合成目标（单一来源；多文件 patch 取
// 第一个目标，同一已文档化限制）。不做此合成时写入时刻层在 codex 上结构性
// 死码（2026-08-28 对抗审查发现；同日 conventions-followups 修复）。
func runConventionsWriteHook(hookInput HookInput, root, version, agent string) error {
	if root == "" {
		return nil
	}
	var fields toolInputFields
	if len(hookInput.ToolInput) > 0 {
		if err := json.Unmarshal(hookInput.ToolInput, &fields); err != nil {
			// 与 runHook 主解析路径同纪律：静默吞掉解析错误会让本 hook 在
			// 方言异常的宿主上无声空转——stderr 一行，advisory 层不 fail。
			fmt.Fprintf(os.Stderr, "[conventions] warning: tool_input parse failed: %v\n", err)
		}
	}
	if fields.FilePath == "" && hostcap.IsPatchTool(hookInput.ToolName) {
		fields.FilePath = applyPatchFilePath(fields.Command)
		// patch 头是仓库相对路径（codex apply_patch 约定）；下方根守卫与
		// Exemplars 需要绝对目标——按 root 拼接，否则根外守卫会静默丢掉合成
		// 出的相对路径（正是 TestConventionsWriteHook_ApplyPatchSynthesis
		// 抓到的坑）。
		if fields.FilePath != "" && !filepath.IsAbs(fields.FilePath) {
			fields.FilePath = filepath.Join(root, fields.FilePath)
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
	recordConventionsInject(hookInput, root, version, agent,
		fmt.Sprintf("write-time pointers injected for %s", relPath), map[string]string{"dir": dirKey})
	if err := EmitAdvisoryRouted(agent, hookInput.HookEventName, "conventions-write", root, hookInput.SessionID, true, inject); err != nil {
		return err
	}
	conventionsMarkDir(hookInput.SessionID, dirKey)
	return nil
}

// recordConventionsInject 落观察条目并盖输出实际使用通道的送达章
// （AdvisoryEmissionChannel 覆盖 kimi 队列路径——与 test-nudge 同契约）。
func recordConventionsInject(hookInput HookInput, root, version, agent, detail string, extra map[string]string) {
	taskRef := taskRefForSession(root, hookInput.SessionID)
	delivered, channel := AdvisoryEmissionChannel(agent, hookInput.HookEventName)
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

// conventionsMarkerPath 构造 session id + kind 的 marker 路径。
func conventionsMarkerPath(sessionID, kind string) string {
	return filepath.Join(conventionsMarkerDir(), util.SanitizeSessionID(sessionID), kind+".marker")
}

// conventionsMarkerExists 报告 (session, kind) marker 是否已落盘。
func conventionsMarkerExists(sessionID, kind string) bool {
	_, err := os.Stat(conventionsMarkerPath(sessionID, kind))
	return err == nil
}

// conventionsMarkSession 落 (session, kind) marker（尽力而为）。
func conventionsMarkSession(sessionID, kind string) {
	path := conventionsMarkerPath(sessionID, kind)
	_ = os.MkdirAll(filepath.Dir(path), 0755)
	_ = util.AtomicWrite(path, []byte("1"), 0644)
}

// conventionsDirState 是写 hook 的会话级已提示目录集合，持久化在
// $TMPDIR/forge-conventions/<sess>/dirs.json。
type conventionsDirState struct {
	Dirs map[string]bool `json:"dirs"`
}

// conventionsDirNudged 报告本会话是否已对 dir 注入过。
func conventionsDirNudged(sessionID, dir string) bool {
	state := loadConventionsDirState(sessionID)
	return state.Dirs[dir]
}

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

// taskRefForSession resolves the active task ref bound to the session — local
// copy of cli skill_trigger.go's helper (3 lines over taskpipeline.ActiveTaskState;
// helpers can't be shared across packages, comments cross-reference).
//
// taskRefForSession 解析 session 绑定的活跃 task ref——cli skill_trigger.go 同名
// 助手的本地副本（对 taskpipeline.ActiveTaskState 的 3 行封装；测试助手/小助手
// 无法跨包共享，注释互指防漂移）。
func taskRefForSession(root, sessionID string) string {
	if active, err := taskpipeline.ActiveTaskState(root, sessionID); err == nil && active != nil {
		return active.TaskRef
	}
	return ""
}
