package taskpipeline

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/taskcontext"
	"github.com/MjxUpUp/Forge/internal/tasktypes"
	"github.com/MjxUpUp/Forge/internal/util"
	"github.com/MjxUpUp/Forge/internal/worktree"
)

// dataHome 返回 root 的 runtime-state DataDir：git 项目用 user-level
// ~/.forge/projects/<key>/（task state + active-task-ref 已从项目级
// <root>/.forge/ 迁来）；非 git 回落到 <root>/.forge/ 以保证 task state 仍记录。
// Key 仅 git 项目有——跨 MkdirAll 稳定（见 forgedata.DataDirFor）。
func dataHome(root string) string { return forgedata.DataDirFor(root) }

// LoadTaskState reads the task state file from DataDir/tasks/.
//
// LoadTaskState 从 DataDir/tasks/ 读 task state 文件。
//
// ref 串号防护：SanitizeRef 把 '/'、'\\'、':'、空格全压成 '-'，feat/foo bar、
// feat/foo:bar、feat/foo/bar 这类 ref 共用同一状态文件。加载后校验文件内的
// TaskRef 与请求的 ref 一致——否则 B 任务会静默读到 A 的
// History/ReviewPassed/Acceptance，review 硬前置被串号绕过。SanitizeRef 本身不动
// （与既有文件名兼容）。
func LoadTaskState(root, taskRef string) (*TaskState, error) {
	return LoadTaskStateInDir(filepath.Join(dataHome(root), "tasks"), taskRef)
}

// LoadTaskStateInDir is LoadTaskState's read core over an explicit tasks dir.
//
// LoadTaskStateInDir 是 LoadTaskState 针对显式 tasks 目录的读取核心——导出给
// 跨仓 DependsOn 解析（LoadDepState）：成员仓按 KEY 寻址
// （forgedata.RootDir(key)/tasks）而非按存活项目根（成员路径入组后可能漂移，
// key 不会）。契约与 LoadTaskState 一致，含下方 ref 串号防护。
func LoadTaskStateInDir(tasksDir, taskRef string) (*TaskState, error) {
	filename := taskcontext.SanitizeRef(taskRef) + ".json"
	path := filepath.Join(tasksDir, filename)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// 包装哨兵错误，调用方可用 errors.Is(err, fs.ErrNotExist) 区分「任务不
			// 存在」与串号/解析失败——把串号守卫的报错当「不存在」会让 task start
			// 直接覆盖同文件的任务状态（2026-08-29 审查轮功能探针实证）。
			return nil, fmt.Errorf("task %q not found: run 'forge task start' first: %w", taskRef, err)
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
	// 完整性验签（state-integrity-signing）：签名【存在】且验不过 = 状态被手改——
	// 消费方按 IntegrityBroken() 拒采信。无签名（nil）= 签名前的存量数据，放行
	//（首次保存自动补签）。
	if s.Integrity != nil {
		if ok, err := tasktypes.VerifyTaskState(&s); err != nil {
			fmt.Fprintf(os.Stderr, "[forge] warning: task %s integrity check errored: %v\n", taskRef, err)
			s.MarkIntegrityBroken()
		} else if !ok {
			fmt.Fprintf(os.Stderr, "[forge] warning: task %s integrity check FAILED — state file was modified outside forge; gate-satisfying fields will not be trusted\n", taskRef)
			s.MarkIntegrityBroken()
		}
	}
	return &s, nil
}

// SaveTaskState writes the task state file to DataDir/tasks/.
//
// SaveTaskState 把 task state 文件写到 DataDir/tasks/。每次写入都签名（对 integrity
// 字段置零后的 canonical JSON 做 HMAC）——唯一写入漏斗使「forge 写的」与「手改的
// 文件」在读取时变得可判定（state-integrity-signing 设计，见 docs/design/state-integrity-signing.md）。
func SaveTaskState(root string, state *TaskState) error {
	tasksDir := filepath.Join(dataHome(root), "tasks")
	if err := os.MkdirAll(tasksDir, 0755); err != nil {
		return fmt.Errorf("failed to create tasks directory: %w", err)
	}

	if sig, keyID, err := tasktypes.SignTaskState(state); err != nil {
		// Missing identity → quiet unsigned write (legacy-shaped; first forge init
		// creates the identity and the next save signs). Only UNEXPECTED signing
		// failures warn — otherwise test/CI envs without an identity spam stderr.
		if !errors.Is(err, fs.ErrNotExist) {
			fmt.Fprintf(os.Stderr, "[forge] warning: task state signing unavailable: %v\n", err)
		}
		state.Integrity = nil
	} else {
		state.Integrity = &StateIntegrity{KeyID: keyID, Alg: "hmac-sha256", Sig: sig}
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
	// 优先级 1：显式 active task ref 文件
	if ref := ReadActiveTaskRef(root, sessionID); ref != "" {
		state, err := LoadTaskState(root, ref)
		if err == nil && state != nil && state.CompletedAt == nil {
			return state, nil
		}
		// ref 文件过期——fall through
	}

	// 优先级 1.5：cwd workspace 绑定（multi-task-concurrency §4，L1）。绑定按 workspace
	// 路径键控而非 session id——同一目录/worktree 里的【新】窗口解析到同一任务。这
	// 是「退出重进」的锚点：目录是稳定的，session id 永远不是。设计上无新鲜度门；
	// 存在即锚（设新鲜度门会把崩溃会话变成丢任务）。
	if b := worktree.Load(root); b != nil && b.TaskRef != "" {
		if state, err := LoadTaskState(root, b.TaskRef); err == nil && state != nil && state.CompletedAt == nil {
			return state, nil
		}
		// 绑定过期（任务已完成/中止）——fall through；task start/finish/abort 维护
		// 绑定，这里是崩溃窗口的兜底。
	}

	// 优先级 2：基于 branch 探测，带跨 worktree 误挂守卫（multi-task-concurrency
	// §4）：分支映射的任务只有在【没有】被其他活跃会话锚定时才算数——两个窗口共
	// 享分支名时，分支本身已说明不了任务是谁的。守卫跳过命中（fall through），
	// 绝不猜。
	ctx := taskcontext.Detect(root)
	if ctx.IsSet() {
		if state, err := LoadTaskState(root, ctx.TaskRef); err == nil && state != nil {
			if state.CompletedAt == nil && !taskAnchoredByOtherActiveSession(root, state, sessionID) {
				return state, nil
			}
			// 已完成或正被其他会话持有——fall through
		}
		// 加载失败（state 文件缺失/损坏，或 ref 串号不匹配）同样 fall through——
		// 此处中断会跳过 legacy 桥，丢掉本应无歧义的 active task。
	}

	// 优先级 3：legacy 全局指针桥（multi-task-concurrency §4 用它替换了旧的「唯一未
	// 完成任务」环境猜测——多任务世界里仅剩的那个任务至少同样可能是别人的）。
	// legacy 全局 active-task-ref（session-scoped 之前版本写入、无 sid 的 CLI 运行
	// 仍会写）对任意会话保持可读，作为一代桥接；命中时顺手物化 workspace 绑定，
	// 让下次解析走 binding 优先。
	if ref := ReadActiveTaskRef(root, ""); ref != "" {
		if state, err := LoadTaskState(root, ref); err == nil && state != nil && state.CompletedAt == nil {
			// M1（review MEDIUM）：桥接绑定只在【主检出】物化——设计意图是「主
			// workspace 的一次性转换」；任何穿落进来的目录都绑会使每个未绑定
			// worktree 误挂到该任务并粘住（混合宿主：无 sid CLI 起任务 + 新
			// worktree 窗口）。判定：本目录的 git common dir 与主检出一致（linked
			// worktree 的 common dir 是主 repo 的 .git 文件位置不同——以 rev-parse
			// 的工作树根对比为准）。转换成功后一次性清除 legacy 指针——自此
			// binding 是权威。
			if IsMainCheckout(root) {
				_ = worktree.BindTask(root, state.TaskRef, state.Branch, sessionID)
				// 比较后删除：并发的无 sid task start 可能在我们读取与本次清除
				// 之间把 legacy 指针改指【新】任务——无条件删除会让那个任务失去
				// 唯一锚点（每个 hook 解析都在读的指针上的 TOCTOU）。
				if cur := ReadActiveTaskRef(root, ""); cur == state.TaskRef {
					_ = ClearActiveTaskRef(root, "")
				}
			}
			return state, nil
		}
	}
	return nil, nil
}

// IsMainCheckout reports whether root is the project's main checkout (vs a linked worktree).
//
// IsMainCheckout 报告 root 是否项目主检出（vs linked worktree）：主检出的
// `git rev-parse --git-dir` 解析为工作树内的【目录】，linked worktree 是指向主 repo
// .git/worktrees/<name> 的【文件】。尽力而为 false——桥只在确信的主检出命中时
// 物化绑定（M1）；task complete 的分形态解绑也复用同一判定。
func IsMainCheckout(root string) bool {
	out, err := exec.Command("git", "-C", root, "rev-parse", "--path-format=absolute", "--git-dir").Output()
	if err != nil {
		return false
	}
	gd := strings.TrimSpace(string(out))
	if info, err := os.Stat(gd); err != nil || !info.IsDir() {
		return false // worktree 的 .git 是文件
	}
	absRoot, _ := filepath.Abs(root)
	// git 在 Windows 上也输出正斜杠，而 filepath.Abs 保留平台分隔符，且 Windows 路径
	// 大小写不敏感——两侧先归一化再比较，否则该等式在 win32 上恒为 false（legacy
	// 桥与 complete 时解绑在那里是死路径）。
	sameDir := strings.EqualFold(filepath.Clean(filepath.ToSlash(filepath.Dir(gd))), filepath.Clean(filepath.ToSlash(absRoot)))
	return sameDir
}

// taskAnchoredByOtherActiveSession 报告任务的锚定会话里是否有【另一个】会话的指针
// 文件在活跃 TTL 内仍指向本任务——即此刻有别的人在干它（multi-task-concurrency
// §4 的 P5 守卫）。空 sessionID（无身份宿主）永不守卫：没有身份时大家共享 legacy
// 全局指针，守卫会把解析整个锁死。
func taskAnchoredByOtherActiveSession(root string, state *TaskState, currentSessionID string) bool {
	if currentSessionID == "" {
		return false
	}
	cutoff := time.Now().Add(-otherSessionActiveTTL)
	for _, l := range state.SessionLinks {
		if l.Imported || l.SessionID == "" || l.SessionID == currentSessionID {
			continue
		}
		p := activeTaskRefPath(root, l.SessionID)
		info, err := os.Stat(p)
		if err != nil || info.Size() == 0 || info.ModTime().Before(cutoff) {
			continue
		}
		if data, err := os.ReadFile(p); err == nil && strings.TrimSpace(string(data)) == state.TaskRef {
			return true
		}
	}
	return false
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

// HasActiveTaskFromOtherSession returns true when at least one other Claude Code session holds an active task (via an active-task-ref-<sid> file).
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
//
// SetActiveTaskRef 把 task ref 写入（session-scoped）active-task-ref。
// 由 forge task start 调用，无论有多少未完成 task 都让 active task 无歧义。
func SetActiveTaskRef(root, sessionID, taskRef string) error {
	return util.AtomicWrite(activeTaskRefPath(root, sessionID), []byte(taskRef), 0644)
}

// ClearActiveTaskRef removes the (session-scoped) active-task-ref file.
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

// completeGraceFile 是 MarkCompleteGrace 在 DataDir 内写的 sentinel 名（带前缀）。
// Per-session；通过 timestamp check 过期，从不显式删除——stale stamp 无害，因
// file-sentinel 比对文件内 epoch 与 NOW。dogfood 2.3。
const completeGraceFile = ".task-complete-grace"

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
//
// MarkCompleteGrace 在 completeGracePath 记录当前 epoch timestamp。
// 由 forge task complete 在 ClearActiveTaskRef 之后立即调用。文件内容为
// epoch-seconds 整数（以 newline 结尾）。窗口比对的执法点在 file-sentinel bash
// hook（internal/hooks/embed.go 硬编码 300s——bash 调不了 Go 常量，Go 侧不保留第二份无守卫
// 拷贝；2026-09 代码普查清扫：曾镜像此值的 completeGraceWindow 常量已删）。
// sessionID 为空时静默返回 nil（无 session
// 上下文 → 无 grace；此种罕见情形只发生有界写入，故不大声失败）。
func MarkCompleteGrace(root, sessionID string) error {
	if sessionID == "" {
		return nil
	}
	stamp := strconv.FormatInt(time.Now().Unix(), 10) + "\n"
	return util.AtomicWrite(completeGracePath(root, sessionID), []byte(stamp), 0644)
}

// resumeStaleFile 是标记「本 session 的 context 刚被压缩——下个 prompt 重注入任务
// handoff」的 sentinel 名（带前缀）。Per-session（.resume-stale-<sid>），取代
// task-scoped 的 TaskState.ResumeStale bool：用户级 DataDir 跨 worktree/多 host
// 共享后，N 个 session 共享一个 task，task-scoped 标志会让 session B 消费（并清掉）
// 本属于 A 的标记——漏注一次。per-session sentinel 让每个 session 只消费自己的标记；
// 且不需要改 task state，PostCompact hook 对共享 json 保持零写。仅 Go 侧使用
// （compact-resume/resume-reinject 都是进 forge task resume 的 thin wrapper）——
// 不像 completeGracePath 需要 bash 镜像。
const resumeStaleFile = ".resume-stale"

// resumeStalePath 返回 DataDir 下 per-session 的 resume-stale sentinel 路径。
func resumeStalePath(root, sessionID string) string {
	return filepath.Join(dataHome(root), resumeStaleFile+"-"+util.SanitizeSessionID(sessionID))
}

// MarkResumeStale records that this session's context was just compacted.
//
// MarkResumeStale 记录本 session 的 context 刚被压缩。由 PostCompact hook
// （compact-resume）在确认有活跃任务后调用。sessionID 必须非空（否则 caller 回落到
// legacy 的 task-scoped bool）。
func MarkResumeStale(root, sessionID string) error {
	stamp := strconv.FormatInt(time.Now().Unix(), 10) + "\n"
	return util.AtomicWrite(resumeStalePath(root, sessionID), []byte(stamp), 0644)
}

// ConsumeResumeStale reports whether a resume-stale mark exists for this session, and clears it if so — guaranteeing a single re-injection per compaction.
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

// coldStartInjectedPath 返回 DataDir 下 per-session 的 cold-start sentinel 路径，镜像
// resumeStalePath。
func coldStartInjectedPath(root, sessionID string) string {
	return filepath.Join(dataHome(root), coldStartInjectedFile+"-"+util.SanitizeSessionID(sessionID))
}

// MarkColdStartInjected records that this session has received its cold-start handoff.
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

// IsColdStartInjected reports whether this session has already received its cold-start handoff (the sentinel file exists).
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
	return ListTaskStatesInDir(filepath.Join(dataHome(root), "tasks"))
}

// ListTaskStatesInDir is ListTaskStates' scan core over an explicit tasks dir.
//
// ListTaskStatesInDir 是 ListTaskStates 针对显式 tasks 目录的扫描核心——
// 导出给多仓 workspace 的 status 聚合：成员仓按 KEY 寻址
// （forgedata.RootDir(key)/tasks）而非按存活项目根（成员路径入组后可能
// 漂移，key 不会）。只读、单文件级 best-effort，语义同 ListTaskStates。
func ListTaskStatesInDir(tasksDir string) ([]*TaskState, error) {
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

// PruneOldTasks deletes task state files in DataDir/tasks/ that are complete (IsComplete) with CompletedAt before cutoff.
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
		// 2026-08-18 死锁修复引入的新滞留类（review m2）：门禁全过（IsComplete）但
		// 从未 `forge task complete`（CompletedAt==nil——完成标记已归还 complete 命令）。
		// session 在 finalize 窗口死亡即产生此类；不回收会成为永久僵尸（SessionStart
		// 反复点名、单任务扫描歧义）。老化锚是**最后一道门的通过时间**而非 StartedAt
		// ——长命任务的门禁昨天刚过不该因启动早被误杀。真未完成的任务
		//（IsComplete==false）仍始终保留。
		if s.IsComplete() && s.CompletedAt == nil {
			if lastGateAt(s).Before(cutoff) {
				if delErr := DeleteTaskState(root, s.TaskRef); delErr != nil {
					if !os.IsNotExist(delErr) {
						errs = append(errs, delErr.Error())
					}
					continue
				}
				removed++
			}
			continue
		}
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

// lastGateAt 返回最后一道 gate 的通过时间（History 末条 CompletedAt）。History 空
// （理论不可达——IsComplete 要求全部 gate 有记录）时回落 StartedAt，保底不早于任务
// 启动。PruneOldTasks 用它作 gates-done-never-completed 僵尸类的老化锚。
func lastGateAt(s *TaskState) time.Time {
	if n := len(s.History); n > 0 {
		return s.History[n-1].CompletedAt
	}
	return s.StartedAt
}
