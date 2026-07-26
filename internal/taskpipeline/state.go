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

// dataHome 返回 root 的 runtime-state DataDir：git 项目用 user-level
// ~/.forge/projects/<key>/（task state + active-task-ref 已从项目级
// <root>/.forge/ 迁来）；非 git 回落到 <root>/.forge/ 以保证 task state 仍记录。
// Key 仅 git 项目有——跨 MkdirAll 稳定（见 forgedata.DataDirFor）。
func dataHome(root string) string { return forgedata.DataDirFor(root) }

// LoadTaskState 从 DataDir/tasks/ 读 task state 文件。
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
	return &s, nil
}

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
	// 优先级 1：显式 active task ref 文件
	if ref := ReadActiveTaskRef(root, sessionID); ref != "" {
		state, err := LoadTaskState(root, ref)
		if err == nil && state != nil && state.CompletedAt == nil {
			return state, nil
		}
		// ref 文件过期——fall through
	}

	// 优先级 2：基于 branch 探测
	ctx := taskcontext.Detect(root)
	if ctx.IsSet() {
		state, err := LoadTaskState(root, ctx.TaskRef)
		if err != nil {
			return nil, err
		}
		if state.CompletedAt == nil {
			return state, nil
		}
		// 此 branch 上 task 已完成——fall through 到兜底
	}

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
		// 用作文件名前先 Sanitize session ID 以保文件系统安全
		safeID := util.SanitizeSessionID(sessionID)
		return filepath.Join(dataHome(root), "active-task-ref-"+safeID)
	}
	return filepath.Join(dataHome(root), activeTaskRefFile)
}

// otherSessionActiveTTL 限定 active-task-ref-<sid> 文件多老之后
// HasActiveTaskFromOtherSession 不再视其为活跃 session。该文件在 forge task
// start 时写一次、task 期间不更新，故 mtime ≈ task start。ClearActiveTaskRef
// 只在 task complete/abort 时跑——session 中途崩溃或被 kill 会留下 orphan，
// 无此限制会累积并使本守卫对所有未来非 task session 永久 auto-PASS
// （静默关掉并发 session 检查）。Bias 倾向不计老文件：假阴性（守卫不 auto-PASS）
// 只是让调研 session 手动跑 forge review pass——安全；假阳性（计入死亡 orphan）
// 会让未提交改动逃过审查——不安全。task 本身不删，只是本便利守卫忽略其活跃度。
const otherSessionActiveTTL = 7 * 24 * time.Hour

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
			continue // 非 session-scoped active-task-ref
		}
		if name == currentFile {
			continue // 自己的
		}
		info, err := e.Info()
		if err != nil || info.Size() == 0 {
			continue
		}
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

// SetActiveTaskRef 把 task ref 写入（session-scoped）active-task-ref。
// 由 forge task start 调用，无论有多少未完成 task 都让 active task 无歧义。
func SetActiveTaskRef(root, sessionID, taskRef string) error {
	return util.AtomicWrite(activeTaskRefPath(root, sessionID), []byte(taskRef), 0644)
}

// ClearActiveTaskRef 移除（session-scoped）active-task-ref 文件。
// 由 forge task complete 调用以清空 active task。
func ClearActiveTaskRef(root, sessionID string) error {
	err := os.Remove(activeTaskRefPath(root, sessionID))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// completeGraceFile 是 MarkCompleteGrace 在 DataDir 内写的 sentinel 名（带前缀）。
// Per-session；通过 timestamp check 过期，从不显式删除——stale stamp 无害，因
// file-sentinel 比对文件内 epoch 与 NOW。dogfood 2.3。
const completeGraceFile = ".task-complete-grace"

// completeGraceWindow 限定 forge task complete 之后 file-sentinel 容忍自然顺带的
// git commit 多久，而不是把它 quarantine 为无 active task + 源码写入。5min 覆盖
// 现实序列（commit + 可能 push）。更长窗口诱发滥用：complete 后持续 30+ 分钟
// 写源码的 session 已非 complete——应新开 task。
const completeGraceWindow = 5 * time.Minute

// CompleteGracePath 返回 DataDir 下的 per-session sentinel 文件路径。
// 导出供 file-sentinel（embed.go 中）镜像路径并读文件内 timestamp，避免依赖
// mtime stat（GNU 与 BSD stat 行为不同）。
func CompleteGracePath(root, sessionID string) string {
	if sessionID != "" {
		safeID := util.SanitizeSessionID(sessionID)
		return filepath.Join(dataHome(root), completeGraceFile+"-"+safeID)
	}
	return filepath.Join(dataHome(root), completeGraceFile)
}

// MarkCompleteGrace 在 CompleteGracePath 记录当前 epoch timestamp。
// 由 forge task complete 在 ClearActiveTaskRef 之后立即调用。文件内容为
// epoch-seconds 整数（以 newline 结尾），使 file-sentinel 无须 stat 即可比对
// NOW - stamp < completeGraceWindow。sessionID 为空时静默返回 nil（无 session
// 上下文 → 无 grace；此种罕见情形只发生有界写入，故不大声失败）。
func MarkCompleteGrace(root, sessionID string) error {
	if sessionID == "" {
		return nil
	}
	stamp := strconv.FormatInt(time.Now().Unix(), 10) + "\n"
	return util.AtomicWrite(CompleteGracePath(root, sessionID), []byte(stamp), 0644)
}

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

// NewTaskState 从探测到的 context 创建新 task state。
func NewTaskState(ctx *taskcontext.Context) *TaskState {
	gates := DefaultGates()
	return &TaskState{
		TaskRef:     ctx.TaskRef,
		Branch:      ctx.Branch,
		Source:      ctx.Source,
		Summary:     ctx.Summary,
		CurrentGate: gates[0].ID, // 从首道门禁开始
		History:     nil,
		StartedAt:   ctx.DetectedAt,
	}
}

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

// DeleteTaskState 移除 task state 文件。
func DeleteTaskState(root, taskRef string) error {
	filename := taskcontext.SanitizeRef(taskRef) + ".json"
	path := filepath.Join(dataHome(root), "tasks", filename)
	return os.Remove(path)
}

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
