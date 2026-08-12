package taskpipeline

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/taskcontext"
	"github.com/MjxUpUp/Forge/internal/util"
)

// dataHome returns the runtime-state DataDir for root: git projects use user-level
// ~/.forge/projects/<key>/ (task state + active-task-ref migrated from project-level
// <root>/.forge/); non-git falls back to <root>/.forge/ so task state is still recorded.
// Key exists only for git projects — stable across MkdirAll (see forgedata.DataDirFor).
//
// dataHome 返回 root 的 runtime-state DataDir：git 项目用 user-level
// ~/.forge/projects/<key>/（task state + active-task-ref 已从项目级
// <root>/.forge/ 迁来）；非 git 回落到 <root>/.forge/ 以保证 task state 仍记录。
// Key 仅 git 项目有——跨 MkdirAll 稳定（见 forgedata.DataDirFor）。
func dataHome(root string) string { return forgedata.DataDirFor(root) }

// LoadTaskState reads the task state file from DataDir/tasks/.
//
// Ref-collision guard: SanitizeRef collapses '/', '\\', ':' and spaces to '-', so refs
// like feat/foo bar / feat/foo:bar / feat/foo/bar share one state file. Loading then
// verifies the TaskRef INSIDE the file matches the requested ref — otherwise task B
// would silently read task A's History/ReviewPassed/Acceptance, letting the review
// hard-prerequisite be bypassed via collision. SanitizeRef itself is left alone
// (backward compatibility with existing filenames).
//
// LoadTaskState 从 DataDir/tasks/ 读 task state 文件。
//
// ref 串号防护：SanitizeRef 把 '/'、'\\'、':'、空格全压成 '-'，feat/foo bar、
// feat/foo:bar、feat/foo/bar 这类 ref 共用同一状态文件。加载后校验文件内的
// TaskRef 与请求的 ref 一致——否则 B 任务会静默读到 A 的
// History/ReviewPassed/Acceptance，review 硬前置被串号绕过。SanitizeRef 本身不动
// （与既有文件名兼容）。
func LoadTaskState(root, taskRef string) (*TaskState, error) {
	filename := taskcontext.SanitizeRef(taskRef) + ".json"
	path := filepath.Join(dataHome(root), "tasks", filename)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("task %q not found: run 'forge task start' first", taskRef)
		}
		return nil, fmt.Errorf("failed to read task state: %w", err)
	}
	var s TaskState
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("failed to parse task state: %w", err)
	}
	if s.TaskRef != taskRef {
		return nil, fmt.Errorf("state file belongs to different task ref %q (requested %q) — refs sanitize to the same filename; use a different ref (e.g. forge task start --ref <other>)", s.TaskRef, taskRef)
	}
	return &s, nil
}

// SaveTaskState writes the task state file to DataDir/tasks/.
//
// SaveTaskState 把 task state 文件写到 DataDir/tasks/。
func SaveTaskState(root string, state *TaskState) error {
	tasksDir := filepath.Join(dataHome(root), "tasks")
	if err := os.MkdirAll(tasksDir, 0755); err != nil {
		return fmt.Errorf("failed to create tasks directory: %w", err)
	}

	filename := taskcontext.SanitizeRef(state.TaskRef) + ".json"
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal task state: %w", err)
	}
	path := filepath.Join(tasksDir, filename)
	return util.AtomicWrite(path, data, 0644)
}

// ActiveTaskState probes the current task context and loads the corresponding state.
// Returns nil without error when no task context is detected.
//
// sessionID scopes the active-task-ref lookup so concurrent sessions on a shared checkout
// each resolve their own active task. Empty sessionID falls back to the legacy global file.
//
// Probe priority:
//  1. Explicit: DataDir/active-task-ref file (written by forge task start)
//  2. Branch-based: feature branch name maps to task ref
//  3. Fallback: scan DataDir/tasks/ for a single incomplete task
//     (multiple tasks are ambiguous — return nil to avoid mismatches)
//
// ActiveTaskState 探测当前 task 上下文并加载对应 state。
// 未探测到 task 上下文时返回 nil 不报错。
//
// sessionID scope active-task-ref 查找，使共享 checkout 上的并发 session 各自
// 解析自己的 active task。空 sessionID 回落到 legacy global 文件。
//
// 探测优先级：
//  1. 显式：DataDir/active-task-ref 文件（由 forge task start 写入）
//  2. 基于 branch：feature branch 名映射到 task ref
//  3. 兜底：扫 DataDir/tasks/ 找单个未完成 task
//     （多 task 时歧义——返回 nil 以免误匹配）
func ActiveTaskState(root, sessionID string) (*TaskState, error) {
	// Priority 1: explicit active task ref file.
	//
	// 优先级 1：显式 active task ref 文件
	if ref := ReadActiveTaskRef(root, sessionID); ref != "" {
		state, err := LoadTaskState(root, ref)
		if err == nil && state != nil && state.CompletedAt == nil {
			return state, nil
		}
		// The ref file is stale — fall through.
		//
		// ref 文件过期——fall through
	}

	// Priority 2: branch-based probe.
	//
	// 优先级 2：基于 branch 探测
	ctx := taskcontext.Detect(root)
	if ctx.IsSet() {
		if state, err := LoadTaskState(root, ctx.TaskRef); err == nil && state != nil {
			if state.CompletedAt == nil {
				return state, nil
			}
			// Task on this branch already completed — fall through to fallback.
			//
			// 此 branch 上 task 已完成——fall through 到兜底
		}
		// Load failure (missing/corrupt state file, or a ref-collision mismatch) also
		// falls through to the fallback scan — aborting the whole probe here would skip
		// priority 3 and lose an otherwise unambiguous active task.
		//
		// 加载失败（state 文件缺失/损坏，或 ref 串号不匹配）同样 fall through 到
		// 兜底扫描——此处中断会跳过优先级 3，丢掉本应无歧义的 active task。
	}

	// Priority 3: scan for a single incomplete task (unambiguous context).
	//
	// 优先级 3：扫单个未完成 task（无歧义上下文）
	all, err := ListTaskStates(root)
	if err != nil {
		return nil, nil
	}
	var incomplete []*TaskState
	for _, s := range all {
		if s.CompletedAt == nil {
			incomplete = append(incomplete, s)
		}
	}
	if len(incomplete) == 1 {
		return incomplete[0], nil
	}
	return nil, nil
}

const activeTaskRefFile = "active-task-ref"

// activeTaskRefPath returns the active-task-ref file path.
//
// When sessionID is non-empty the file is session-scoped (DataDir/active-task-ref-<sessionID>),
// letting concurrent Claude Code sessions on a shared checkout each resolve their own active task —
// eliminating the main concurrency race (two sessions stomping the same global file, hooks
// attributing work to the wrong task).
//
// Empty sessionID falls back to the legacy global file (DataDir/active-task-ref) for backward
// compatibility and non-Claude (manual terminal) usage.
//
// activeTaskRefPath 返回 active-task-ref 文件路径。
//
// sessionID 非空时文件为 session-scoped（DataDir/active-task-ref-<sessionID>），
// 使共享 checkout 上并发 Claude Code session 各自解析自己的 active task——
// 根除主要并发竞态（两个 session 互踩同一 global 文件、hooks 把工作归到错误
// task）。
//
// 空 sessionID 回落到 legacy global 文件（DataDir/active-task-ref）以保持向后
// 兼容及非 Claude（手动终端）使用。
func activeTaskRefPath(root, sessionID string) string {
	if sessionID != "" {
		// Sanitize the session ID before using it as a filename to keep the filesystem safe.
		//
		// 用作文件名前先 Sanitize session ID 以保文件系统安全
		safeID := util.SanitizeSessionID(sessionID)
		return filepath.Join(dataHome(root), "active-task-ref-"+safeID)
	}
	return filepath.Join(dataHome(root), activeTaskRefFile)
}

// otherSessionActiveTTL bounds how old an active-task-ref-<sid> file can be before
// HasActiveTaskFromOtherSession no longer treats it as an active session. The file is
// written once at forge task start and not updated during the task, so mtime ≈ task start.
// ClearActiveTaskRef only runs on task complete/abort — a session that crashes mid-task
// or is killed leaves an orphan; without this limit orphans accumulate and make this guard
// permanently auto-PASS for all future non-task sessions (silently disabling the concurrent-
// session check). Bias toward not counting old files: false negative (guard does not auto-PASS)
// only forces the research session to run forge review pass manually — safe; false positive
// (counting a dead orphan) lets uncommitted changes escape review — unsafe. The task itself
// is not deleted; only this convenience guard ignores its liveness.
//
// otherSessionActiveTTL 限定 active-task-ref-<sid> 文件多老之后
// HasActiveTaskFromOtherSession 不再视其为活跃 session。该文件在 forge task
// start 时写一次、task 期间不更新，故 mtime ≈ task start。ClearActiveTaskRef
// 只在 task complete/abort 时跑——session 中途崩溃或被 kill 会留下 orphan，
// 无此限制会累积并使本守卫对所有未来非 task session 永久 auto-PASS
// （静默关掉并发 session 检查）。Bias 倾向不计老文件：假阴性（守卫不 auto-PASS）
// 只是让调研 session 手动跑 forge review pass——安全；假阳性（计入死亡 orphan）
// 会让未提交改动逃过审查——不安全。task 本身不删，只是本便利守卫忽略其活跃度。
const otherSessionActiveTTL = 7 * 24 * time.Hour

// HasActiveTaskFromOtherSession returns true when at least one other Claude Code session
// holds an active task (via an active-task-ref-<sid> file). Used by the review-stop hook
// (non-task mode) to detect concurrent sessions: if another session is editing code, the
// global git diff belongs to its task — its task-complete gate will enforce review, so this
// session's Stop hook should PASS rather than block on changes it did not make.
//
// Returns false when currentSessionID is empty (legacy mode, cannot distinguish sessions).
// Only session-scoped files (active-task-ref-* prefix) are considered; the legacy global
// active-task-ref file is not counted. Files older than otherSessionActiveTTL are treated
// as dead-session orphans (crashed/abandoned) and skipped — see that const's comment.
//
// HasActiveTaskFromOtherSession 在至少一个其他 Claude Code session 持有 active
// task（经 active-task-ref-<sid> 文件）时返回 true。供 review-stop hook
// （非 task 模式）探测并发 session：若另一 session 正在改码，全局 git diff 归属
// 其 task——其 task-complete 门禁会强制 review，故本 session 的 Stop hook 应
// PASS 而非拦在它没做的改动上。
//
// currentSessionID 为空（legacy 模式，无法区分 session）时返回 false。只考虑
// session-scoped 文件（active-task-ref-* 前缀）；legacy global active-task-ref
// 文件不计。文件超过 otherSessionActiveTTL 视作死亡 session orphan（崩溃/弃用）
// 并跳过——理由见该 const 注释。
func HasActiveTaskFromOtherSession(root, currentSessionID string) bool {
	if currentSessionID == "" {
		return false
	}
	dir := dataHome(root)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	currentFile := activeTaskRefFile + "-" + util.SanitizeSessionID(currentSessionID)
	cutoff := time.Now().Add(-otherSessionActiveTTL)
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, activeTaskRefFile+"-") {
			// Not a session-scoped active-task-ref.
			continue // 非 session-scoped active-task-ref
		}
		if name == currentFile {
			// Mine.
			continue // 自己的
		}
		info, err := e.Info()
		if err != nil || info.Size() == 0 {
			continue
		}
		// Crash-orphan guard: skip files past the TTL — orphans left by sessions
		// that did not run complete/abort. Without this limit orphans accumulate
		// and disable the guard (see otherSessionActiveTTL comment).
		//
		// Crash-orphan 守卫：跳过超过 TTL 的文件——session 未跑 complete/abort
		// 留下的 orphan。无此限制 orphan 累积并关掉守卫（见 otherSessionActiveTTL
		// 注释）。
		if info.ModTime().Before(cutoff) {
			continue
		}
		return true
	}
	return false
}

// SetActiveTaskRef writes the task ref to the (session-scoped) active-task-ref.
// Called by forge task start to make the active task unambiguous regardless of
// how many incomplete tasks exist.
//
// SetActiveTaskRef 把 task ref 写入（session-scoped）active-task-ref。
// 由 forge task start 调用，无论有多少未完成 task 都让 active task 无歧义。
func SetActiveTaskRef(root, sessionID, taskRef string) error {
	return util.AtomicWrite(activeTaskRefPath(root, sessionID), []byte(taskRef), 0644)
}

// ClearActiveTaskRef removes the (session-scoped) active-task-ref file.
// Called by forge task complete to clear the active task.
//
// ClearActiveTaskRef 移除（session-scoped）active-task-ref 文件。
// 由 forge task complete 调用以清空 active task。
func ClearActiveTaskRef(root, sessionID string) error {
	err := os.Remove(activeTaskRefPath(root, sessionID))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// completeGraceFile is the sentinel name (with prefix) MarkCompleteGrace writes inside the DataDir.
// Per-session; expired via timestamp check, never explicitly deleted — a stale stamp is harmless
// because file-sentinel compares the epoch inside the file with NOW. dogfood 2.3.
//
// completeGraceFile 是 MarkCompleteGrace 在 DataDir 内写的 sentinel 名（带前缀）。
// Per-session；通过 timestamp check 过期，从不显式删除——stale stamp 无害，因
// file-sentinel 比对文件内 epoch 与 NOW。dogfood 2.3。
const completeGraceFile = ".task-complete-grace"

// completeGraceWindow bounds how long after forge task complete file-sentinel tolerates a
// naturally-following git commit, rather than quarantining it as no active task + source write.
// 5min covers the realistic sequence (commit + possible push). A longer window invites abuse:
// a session still writing source 30+ minutes after complete is no longer completing — it should
// start a new task.
//
// completeGraceWindow 限定 forge task complete 之后 file-sentinel 容忍自然顺带的
// git commit 多久，而不是把它 quarantine 为无 active task + 源码写入。5min 覆盖
// 现实序列（commit + 可能 push）。更长窗口诱发滥用：complete 后持续 30+ 分钟
// 写源码的 session 已非 complete——应新开 task。
const completeGraceWindow = 5 * time.Minute

// completeGracePath returns the per-session sentinel file path under the DataDir.
// The file-sentinel bash hook (embed.go) replicates this path scheme by convention —
// bash cannot call Go — and reads the timestamp inside the file, avoiding reliance on
// mtime stat (GNU and BSD stat behave differently). Keep the two in sync.
//
// completeGracePath 返回 DataDir 下的 per-session sentinel 文件路径。
// file-sentinel bash hook（embed.go 中）按约定镜像此路径方案——bash 调不了 Go——
// 并读文件内 timestamp，避免依赖 mtime stat（GNU 与 BSD stat 行为不同）。两处需保持同步。
func completeGracePath(root, sessionID string) string {
	if sessionID != "" {
		safeID := util.SanitizeSessionID(sessionID)
		return filepath.Join(dataHome(root), completeGraceFile+"-"+safeID)
	}
	return filepath.Join(dataHome(root), completeGraceFile)
}

// MarkCompleteGrace records the current epoch timestamp at completeGracePath.
// Called by forge task complete right after ClearActiveTaskRef. The file content is an
// epoch-seconds integer (newline-terminated) so file-sentinel can compare
// NOW - stamp < completeGraceWindow without a stat. Returns nil silently when sessionID
// is empty (no session context → no grace; this rare case only triggers a bounded write,
// so it does not fail loudly).
//
// MarkCompleteGrace 在 completeGracePath 记录当前 epoch timestamp。
// 由 forge task complete 在 ClearActiveTaskRef 之后立即调用。文件内容为
// epoch-seconds 整数（以 newline 结尾），使 file-sentinel 无须 stat 即可比对
// NOW - stamp < completeGraceWindow。sessionID 为空时静默返回 nil（无 session
// 上下文 → 无 grace；此种罕见情形只发生有界写入，故不大声失败）。
func MarkCompleteGrace(root, sessionID string) error {
	if sessionID == "" {
		return nil
	}
	stamp := strconv.FormatInt(time.Now().Unix(), 10) + "\n"
	return util.AtomicWrite(completeGracePath(root, sessionID), []byte(stamp), 0644)
}

// resumeStaleFile is the sentinel name (with prefix) marking "this session's context was
// just compacted — re-inject the task handoff on its next prompt". Per-session
// (.resume-stale-<sid>), replacing the task-scoped TaskState.ResumeStale bool: with the
// user-level DataDir shared across worktrees and multiple hosts, N sessions share one
// task, and a task-scoped flag let session B consume (and clear) the mark meant for
// session A — a missed re-injection. A per-session sentinel makes each session consume
// exactly its own mark; it also needs no task-state mutation, so the PostCompact hook
// stays write-free on the shared json. Go-only (compact-resume/resume-reinject are thin
// wrappers into forge task resume) — no bash mirror, unlike completeGracePath.
//
// resumeStaleFile 是标记「本 session 的 context 刚被压缩——下个 prompt 重注入任务
// handoff」的 sentinel 名（带前缀）。Per-session（.resume-stale-<sid>），取代
// task-scoped 的 TaskState.ResumeStale bool：用户级 DataDir 跨 worktree/多 host
// 共享后，N 个 session 共享一个 task，task-scoped 标志会让 session B 消费（并清掉）
// 本属于 A 的标记——漏注一次。per-session sentinel 让每个 session 只消费自己的标记；
// 且不需要改 task state，PostCompact hook 对共享 json 保持零写。仅 Go 侧使用
// （compact-resume/resume-reinject 都是进 forge task resume 的 thin wrapper）——
// 不像 completeGracePath 需要 bash 镜像。
const resumeStaleFile = ".resume-stale"

// resumeStalePath returns the per-session resume-stale sentinel path under the DataDir.
//
// resumeStalePath 返回 DataDir 下 per-session 的 resume-stale sentinel 路径。
func resumeStalePath(root, sessionID string) string {
	return filepath.Join(dataHome(root), resumeStaleFile+"-"+util.SanitizeSessionID(sessionID))
}

// MarkResumeStale records that this session's context was just compacted. Called by the
// PostCompact hook (compact-resume) after confirming an active task exists. sessionID
// must be non-empty (the caller falls back to the legacy task-scoped bool otherwise).
//
// MarkResumeStale 记录本 session 的 context 刚被压缩。由 PostCompact hook
// （compact-resume）在确认有活跃任务后调用。sessionID 必须非空（否则 caller 回落到
// legacy 的 task-scoped bool）。
func MarkResumeStale(root, sessionID string) error {
	stamp := strconv.FormatInt(time.Now().Unix(), 10) + "\n"
	return util.AtomicWrite(resumeStalePath(root, sessionID), []byte(stamp), 0644)
}

// ConsumeResumeStale reports whether a resume-stale mark exists for this session, and
// clears it if so — guaranteeing a single re-injection per compaction. Returns false
// when sessionID is empty (legacy sessions use the task-scoped bool instead).
//
// ConsumeResumeStale 报告本 session 是否存在 resume-stale 标记，存在则清除——保证
// 每次压缩只重注入一次。sessionID 为空时返回 false（legacy session 改用 task-scoped
// bool）。
func ConsumeResumeStale(root, sessionID string) bool {
	if sessionID == "" {
		return false
	}
	return os.Remove(resumeStalePath(root, sessionID)) == nil
}

// coldStartInjectedFile is the per-session sentinel marking that this session has already
// received its cold-start task handoff via the UserPromptSubmit backfill. It exists for one
// host class — kimi 0.35.0 — which drops SessionStart hook output from the model context
// (SessionStart is observation-only there, verified via wire.jsonl cross-check). The
// SessionStart task-resume hook still runs (its attach side-effect lands), but its rendered
// handoff never reaches the model. The UserPromptSubmit resume-reinject hook backfills the
// active-task handoff on the first prompt of such a session (the offered-tasks block is a
// pre-existing gap, see renderHookReinject); this sentinel dedupes so subsequent prompts
// stay silent.
//
// Per-session, for the same reason as resumeStaleFile: the user-level DataDir is shared
// across worktrees and hosts, so a task-scoped flag would let one session's backfill
// suppress another's. Unlike resume-stale it is NOT consumed on read — a session needs its
// cold-start handoff exactly once, so the sentinel persists for the session lifetime (and
// the compact-reinject path also sets it, since a full handoff just delivered satisfies
// cold-start too — prevents a double-inject on the next prompt). There is no TTL cleanup:
// like resumeStale orphans these are tiny per-session files with no existing periodic
// sweep; accepted as a bounded slow leak rather than adding cleanup infrastructure.
//
// coldStartInjectedFile 是 per-session sentinel，标记本 session 已通过 UserPromptSubmit 回填
// 拿到冷启动 task handoff。它只为一种 host 存在——kimi 0.35.0——该 host 把 SessionStart hook
// 输出丢弃出模型上下文（SessionStart 在那里是 observation-only，经 wire.jsonl 交叉验证核实）。
// SessionStart 的 task-resume hook 仍会跑（attach 副作用落地），但它渲染的 handoff 到不了模型。
// UserPromptSubmit 的 resume-reinject hook 在这类 session 的首个 prompt 回填活跃任务 handoff
// （offered-tasks 块是既有缺口，见 renderHookReinject）；本 sentinel 去重使后续 prompt 保持静默。
//
// Per-session，与 resumeStaleFile 同因：用户级 DataDir 跨 worktree/host 共享，task-scoped 标志
// 会让一个 session 的回填抑制另一个。与 resume-stale 不同，它读时不被消费——一个 session 恰需
// 一次冷启动 handoff，故 sentinel 存活整个 session 生命周期（compact-reinject 路径也设它，
// 因为刚交付的完整 handoff 也满足冷启动——防下个 prompt 双注）。无 TTL 清理：与 resumeStale
// 孤儿一样是 tiny per-session 文件、无既有定期清扫；接受为有界慢泄漏，不为此加清理基础设施。
const coldStartInjectedFile = ".cold-start-injected"

// coldStartInjectedPath returns the per-session cold-start sentinel path under the DataDir,
// mirroring resumeStalePath.
//
// coldStartInjectedPath 返回 DataDir 下 per-session 的 cold-start sentinel 路径，镜像
// resumeStalePath。
func coldStartInjectedPath(root, sessionID string) string {
	return filepath.Join(dataHome(root), coldStartInjectedFile+"-"+util.SanitizeSessionID(sessionID))
}

// MarkColdStartInjected records that this session has received its cold-start handoff.
// Idempotent (AtomicWrite overwrites the same path). A no-op when sessionID is empty — the
// backfill is gated on a non-empty sid upstream, but the guard keeps the sentinel file from
// being written under an empty id (which would collide across sessions and mis-suppress).
//
// MarkColdStartInjected 记录本 session 已拿到冷启动 handoff。幂等（AtomicWrite 覆写同路径）。
// sessionID 为空时 no-op——回填上游已门控在非空 sid，但此守卫避免在空 id 下写 sentinel 文件
// （那会跨 session 碰撞并误抑制）。
func MarkColdStartInjected(root, sessionID string) error {
	if sessionID == "" {
		return nil
	}
	stamp := strconv.FormatInt(time.Now().Unix(), 10) + "\n"
	return util.AtomicWrite(coldStartInjectedPath(root, sessionID), []byte(stamp), 0644)
}

// IsColdStartInjected reports whether this session has already received its cold-start
// handoff (the sentinel file exists). Returns false when sessionID is empty. Unlike
// ConsumeResumeStale it does NOT remove the sentinel — read-only check, the sentinel
// persists for the session lifetime.
//
// IsColdStartInjected 报告本 session 是否已拿到冷启动 handoff（sentinel 文件存在）。sessionID
// 为空时返回 false。与 ConsumeResumeStale 不同，它不移除 sentinel——只读检查，sentinel 存活
// 整个 session 生命周期。
func IsColdStartInjected(root, sessionID string) bool {
	if sessionID == "" {
		return false
	}
	if _, err := os.Stat(coldStartInjectedPath(root, sessionID)); err != nil {
		return false
	}
	return true
}

// ReadActiveTaskRef reads the active task ref from the (session-scoped) file.
// Returns an empty string when the file is missing or empty.
//
// Exported so forge task abort can decide whether the aborted task is the current active
// task (and thus whether to clear the active-task-ref).
//
// ReadActiveTaskRef 从（session-scoped）文件读 active task ref。
// 文件不存在或为空时返回空字符串。
//
// 导出供 forge task abort 判定被 abort 的 task 是否为当前 active task
// （进而决定是否清 active-task-ref）。
func ReadActiveTaskRef(root, sessionID string) string {
	data, err := os.ReadFile(activeTaskRefPath(root, sessionID))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// NewTaskState creates a new task state from the detected context.
//
// NewTaskState 从探测到的 context 创建新 task state。
func NewTaskState(ctx *taskcontext.Context) *TaskState {
	gates := DefaultGates()
	return &TaskState{
		TaskRef: ctx.TaskRef,
		Branch:  ctx.Branch,
		Source:  ctx.Source,
		Summary: ctx.Summary,
		// Start from the first gate.
		CurrentGate: gates[0].ID, // 从首道门禁开始
		History:     nil,
		StartedAt:   ctx.DetectedAt,
	}
}

// GetHeadCommit returns the current short HEAD commit hash.
// Returns an empty string silently when not in a git repo.
//
// GetHeadCommit 返回当前 short HEAD commit hash。
// 非 git 仓库时静默返回空字符串。
func GetHeadCommit(root string) string {
	cmd := exec.Command("git", "-C", root, "rev-parse", "--short", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// IsGitRepo reports whether root is inside a git working tree.
//
// The task pipeline degrades gracefully without git — gates still pass (hasCodeChanges
// returns true, CheckTestCoverage treats an empty changed-set as nothing to cover), and
// task complete still scores. But git-backed scoring dimensions go neutral: scope has no
// diff to measure (fixed 70, Diff stat unavailable). Not surfacing this means an agent
// starting a task in a bare directory gets no signal of degraded mode — exactly the blind
// spot that pins a session in a non-git project. Callers print this signal accordingly.
//
// IsGitRepo 报告 root 是否位于 git working tree 内。
//
// task pipeline 在无 git 时优雅降级——门禁仍通过（hasCodeChanges 返回 true、
// CheckTestCoverage 把空 changed-set 视为无可覆盖），task complete 仍评分。
// 但 git 支撑的评分维度变 neutral：scope 无 diff 可测（固定 70，Diff stat
// unavailable）。不暴露此点，agent 在裸目录里启动 task 就没有降级模式的信号
// ——正是把 session 卡在非 git 项目里的盲点。调用方据此打印该信号。
func IsGitRepo(root string) bool {
	cmd := exec.Command("git", "-C", root, "rev-parse", "--git-dir")
	return cmd.Run() == nil
}

// DeleteTaskState removes the task state file.
//
// DeleteTaskState 移除 task state 文件。
func DeleteTaskState(root, taskRef string) error {
	filename := taskcontext.SanitizeRef(taskRef) + ".json"
	path := filepath.Join(dataHome(root), "tasks", filename)
	return os.Remove(path)
}

// ListTaskStates returns all task state files under DataDir/tasks/.
//
// ListTaskStates 返回 DataDir/tasks/ 下所有 task state 文件。
func ListTaskStates(root string) ([]*TaskState, error) {
	tasksDir := filepath.Join(dataHome(root), "tasks")
	entries, err := os.ReadDir(tasksDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var states []*TaskState
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(tasksDir, e.Name()))
		if err != nil {
			continue
		}
		var s TaskState
		if err := json.Unmarshal(data, &s); err != nil {
			continue
		}
		states = append(states, &s)
	}
	return states, nil
}

// PruneOldTasks deletes task state files in DataDir/tasks/ that are complete (IsComplete)
// with CompletedAt before cutoff. In-progress tasks (IsComplete==false) are always kept —
// they may still be active or resumable. Aborted task files need no handling: forge task abort
// deletes the file outright, no file lingers in abort state.
//
// best-effort: per-file parse/delete failures are skipped and accumulated into err, never
// aborting the whole scan. Returns the removal count plus the accumulated non-fatal errors.
// Callers compute cutoff from the shared retention window (FORGE_LOG_RETENTION_DAYS) so task
// metadata, checklog archives, and toollog archives age out together.
//
// PruneOldTasks 删除 DataDir/tasks/ 中已完成（IsComplete）且 CompletedAt 早于
// cutoff 的 task state 文件。In-progress task（IsComplete==false）始终保留——
// 它们可能仍活跃或可 resume。Aborted task 文件无需处理：forge task abort 直接
// 删文件，无文件停留 abort 状态。
//
// best-effort：单文件的 parse/delete 失败被跳过并累积进 err，从不中止整次扫描。
// 返回移除计数 + 累积的非致命 error。调用方从共享 retention 窗口
// （FORGE_LOG_RETENTION_DAYS）算 cutoff，使 task 元数据、checklog 归档、
// toollog 归档一同老化。
func PruneOldTasks(root string, cutoff time.Time) (removed int, err error) {
	states, err := ListTaskStates(root)
	if err != nil {
		return 0, err
	}
	var errs []string
	for _, s := range states {
		if !s.IsComplete() || s.CompletedAt == nil {
			continue
		}
		if !s.CompletedAt.Before(cutoff) {
			continue
		}
		if delErr := DeleteTaskState(root, s.TaskRef); delErr != nil {
			if !os.IsNotExist(delErr) {
				errs = append(errs, delErr.Error())
			}
			continue
		}
		removed++
	}
	if len(errs) > 0 {
		return removed, fmt.Errorf("prune old tasks: %s", strings.Join(errs, "; "))
	}
	return removed, nil
}
