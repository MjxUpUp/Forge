package taskpipeline

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/MjxUpUp/Forge/internal/agentsignals"
	"github.com/MjxUpUp/Forge/internal/hostcap"
	"github.com/MjxUpUp/Forge/internal/nodestamp"
	"github.com/MjxUpUp/Forge/internal/util"
)

// sessionMaxIdle is the maximum idle time before session rotation. If the
// current session has been alive longer than this duration, the next task start
// creates a new session.
//
// sessionMaxIdle 是 session 轮换前的最长空闲时间。当前 session 起算若超过此时长，
// 下次 task start 时创建新 session。
const sessionMaxIdle = 2 * time.Hour

// SessionRecord represents a single agent development session.
// A session groups tasks created within the same agent interaction.
//
// SessionRecord 表示一次 agent 开发 session。
// session 把同一 agent 交互里创建的 task 归为一组。
type SessionRecord struct {
	SessionID string    `json:"session_id"`
	StartedAt time.Time `json:"started_at"`
	// StartedEpoch is the Unix-second form of StartedAt, persisted alongside it
	// so epoch consumers (typically the session-health bash hook) can read a
	// precise integer without parsing RFC3339Nano strings via the
	// cross-platform fragile date command (GNU vs BSD). This makes the Go→bash
	// contract an explicit integer field rather than a format that bash must
	// reverse-parse.
	//
	// StartedEpoch 是 StartedAt 的 Unix 秒形式，与其一起落盘，让需要 epoch 的
	// 消费者（典型如 session-health bash hook）能读到一个精确整数，而不必用
	// 跨平台脆弱的 date 命令（GNU vs BSD）解析 RFC3339Nano 串。这让 Go→bash 的
	// 契约是一个显式整数字段，而非一个 bash 必须反向解析的格式。
	StartedEpoch int64  `json:"started_epoch,omitempty"`
	AgentType    string `json:"agent_type,omitempty"`
	// Stamp carries machine attribution (node_id/seq/ts_hlc/sig), filled by
	// appendSessionLog via nodestamp.Next — zero on legacy lines and on fail-open.
	//
	// Stamp 携带机器归因（node_id/seq/ts_hlc/sig），由 appendSessionLog 经
	// nodestamp.Next 落章——存量行与 fail-open 时为零值。
	nodestamp.Stamp
}

// sessionFilePath returns the path of the current session tracking file.
//
// sessionFilePath 返回当前 session 跟踪文件路径。
func sessionFilePath(root string) string {
	return filepath.Join(dataHome(root), "session.json")
}

// sessionsLogPath returns the path of the historical sessions log.
//
// sessionsLogPath 返回历史 sessions 日志路径。
func sessionsLogPath(root string) string {
	return filepath.Join(dataHome(root), "sessions.jsonl")
}

// EnsureSession returns the currently active session, creating one when needed.
//
// When sessionID is non-empty (Claude Code's session id), the session is stored
// in session-scoped fashion at DataDir/sessions/<sessionID>.json and identified
// by that id. This eliminates last-writer-wins clobbering of the global
// session.json when two sessions run concurrently on a shared checkout. Claude
// Code's session id is stable for the entire session lifetime, so this path
// needs no idle-rotation.
//
// When sessionID is empty (manual terminal use, no CLAUDE_CODE_SESSION_ID), the
// legacy global session.json path is used with idle-based rotation.
//
// EnsureSession 返回当前活跃 session，必要时创建一个。
//
// sessionID 非空（Claude Code 的 session id）时，session 以 session-scoped 方式
// 存到 DataDir/sessions/<sessionID>.json 并以该 id 标识。这样消除共享 checkout
// 上两个 session 并发时对全局 session.json 的 last-writer-wins clobber。Claude
// Code 的 session id 在整个 session 生命周期稳定，故此路径无需 idle-rotation。
//
// sessionID 为空（手动终端使用，无 CLAUDE_CODE_SESSION_ID）时，使用 legacy 全局
// session.json 路径并基于 idle 轮换。
func EnsureSession(root, sessionID string) (*SessionRecord, error) {
	if sessionID != "" {
		return ensureScopedSession(root, sessionID)
	}

	// Legacy path: load/rotate the global session.json.
	//
	// legacy 路径：加载/轮换全局 session.json。
	existing, err := loadSession(root)
	if err != nil {
		return nil, err
	}

	if existing != nil && time.Since(existing.StartedAt) < sessionMaxIdle {
		return existing, nil
	}

	// Archive the old session before creating a new one.
	//
	// 创建新 session 前先归档旧的
	if existing != nil {
		if err := appendSessionLog(root, existing); err != nil {
			return nil, err
		}
	}

	// Create a new session.
	//
	// 创建新 session
	now := time.Now()
	session := &SessionRecord{
		SessionID:    newSessionID(),
		StartedAt:    now,
		StartedEpoch: now.Unix(),
		AgentType:    detectAgentType(root),
	}

	if err := saveSession(root, session); err != nil {
		return nil, err
	}

	// Also append to the history log.
	//
	// 同时写入历史日志
	if err := appendSessionLog(root, session); err != nil {
		return nil, err
	}

	return session, nil
}

// ensureScopedSession loads or creates the session record for a specific (Claude
// Code) session id, stored at DataDir/sessions/<sessionID>.json. It also appends
// to the historical sessions.jsonl so LoadSessions / session-health can see it.
//
// ensureScopedSession 加载或创建特定（Claude Code）session id 的 session 记录，
// 存于 DataDir/sessions/<sessionID>.json。同时追加到历史 sessions.jsonl，让
// LoadSessions / session-health 能看到。
func ensureScopedSession(root, sessionID string) (*SessionRecord, error) {
	path := sessionScopedFilePath(root, sessionID)
	if data, err := os.ReadFile(path); err == nil {
		var s SessionRecord
		if err := json.Unmarshal(data, &s); err == nil && s.SessionID == sessionID {
			return &s, nil
		}
		// Corrupt/stale file — fall through to rebuild below.
		//
		// 损坏/陈旧文件——落入下方重建。
	}

	now := time.Now()
	session := &SessionRecord{
		SessionID:    sessionID,
		StartedAt:    now,
		StartedEpoch: now.Unix(),
		AgentType:    detectAgentType(root),
	}

	if err := saveScopedSession(root, session); err != nil {
		return nil, err
	}

	// Append to the history log. A duplicate line is harmless: LoadSessions does NOT
	// dedup across log lines (it preserves every parseable line), so duplicate lines for
	// the same SessionID are tolerated as-is; the jsonl agent-upsert
	// (upsertSessionAgentInLog) updates EVERY matching line, keeping duplicates consistent
	// rather than leaving copies with an empty agent_type. Do NOT "simplify" that upsert to
	// update only the first match — it would reintroduce the empty-agent metric gap.
	//
	// 追加到历史日志。一行重复无害：LoadSessions 不在日志行间去重（保留每个可解析行），
	// 故同一 SessionID 的重复行原样容忍；jsonl agent-upsert（upsertSessionAgentInLog）
	// 更新每一条匹配行，使重复行保持一致，而非留下空 agent_type 的副本。切勿把该 upsert
	// 「简化」为只改首条命中——那会重新引入空 agent 的指标缺口。
	if err := appendSessionLog(root, session); err != nil {
		return nil, err
	}

	return session, nil
}

// sessionScopedFilePath returns the session-isolated record path.
//
// sessionScopedFilePath 返回按 session 隔离的记录路径。
func sessionScopedFilePath(root, sessionID string) string {
	return filepath.Join(dataHome(root), "sessions", util.SanitizeSessionID(sessionID)+".json")
}

// saveScopedSession writes the session record to its scoped path.
//
// saveScopedSession 把 session 记录写到它的 scoped 路径。
func saveScopedSession(root string, s *SessionRecord) error {
	path := sessionScopedFilePath(root, s.SessionID)
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	// AtomicWrite (temp+rename): a torn write here would corrupt the JSON every
	// session loader parses (same argument as state_atomic_test.go for task state).
	//
	// AtomicWrite（temp+rename）：半写会损坏所有 session 加载方解析的 JSON
	// （论证同 state_atomic_test.go 之 task state）。
	return util.AtomicWrite(path, data, 0644)
}

// loadSession reads the current session file. Returns nil if it does not exist.
//
// loadSession 读取当前 session 文件。不存在返回 nil。
func loadSession(root string) (*SessionRecord, error) {
	path := sessionFilePath(root)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var s SessionRecord
	if err := json.Unmarshal(data, &s); err != nil {
		// Corrupt JSON is treated as "no session" on purpose — same contract as
		// ensureScopedSession (see its note): the caller simply rotates in a fresh
		// session rather than failing task start over an unreadable bookkeeping file.
		//
		// JSON 损坏有意视为「无 session」——与 ensureScopedSession 的契约一致（见其
		// 注释）：调用方直接轮换出新 session，而非为一个不可读的簿记文件让
		// task start 失败。
		return nil, nil
	}
	return &s, nil
}

// saveSession writes the current session file.
//
// saveSession 写入当前 session 文件。
func saveSession(root string, s *SessionRecord) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	// AtomicWrite (temp+rename) — see saveScopedSession.
	//
	// AtomicWrite（temp+rename）——理由见 saveScopedSession。
	return util.AtomicWrite(sessionFilePath(root), data, 0644)
}

// appendSessionLog appends a session record to DataDir/sessions.jsonl.
//
// appendSessionLog 追加一条 session 记录到 DataDir/sessions.jsonl。
func appendSessionLog(root string, s *SessionRecord) error {
	if s.Stamp == (nodestamp.Stamp{}) {
		s.Stamp = nodestamp.Next()
	}
	dir := filepath.Dir(sessionsLogPath(root))
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	f, err := os.OpenFile(sessionsLogPath(root), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	_, err = f.Write(append(data, '\n'))
	return err
}

// LoadSessions reads all historical session records from
// DataDir/sessions.jsonl. A currently active session, if any, is included as
// well.
//
// LoadSessions 从 DataDir/sessions.jsonl 读所有历史 session 记录。
// 若存在当前活跃 session 也一并包含。
func LoadSessions(root string) ([]SessionRecord, error) {
	var sessions []SessionRecord

	// Read the current session first (the latest).
	//
	// 先读当前 session（最新）
	current, err := loadSession(root)
	if err != nil {
		return nil, err
	}

	// Read the history log.
	//
	// 读历史日志
	f, err := os.Open(sessionsLogPath(root))
	if err != nil {
		if os.IsNotExist(err) {
			// Only the current session exists.
			//
			// 只有当前 session
			if current != nil {
				return []SessionRecord{*current}, nil
			}
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var s SessionRecord
		if err := json.Unmarshal(scanner.Bytes(), &s); err != nil {
			continue
		}
		sessions = append(sessions, s)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	// If the current session is not yet in the log, add it.
	//
	// 若当前 session 未在日志中则加入
	if current != nil {
		found := false
		for _, s := range sessions {
			if s.SessionID == current.SessionID {
				found = true
				break
			}
		}
		if !found {
			sessions = append(sessions, *current)
		}
	}

	return sessions, nil
}

// detectAgentType checks for known agent configuration directories, returning the
// first match in a FIXED priority order (first hit wins). Delegated to the shared
// agentsignals table — the SAME source agentbridge.DetectAgents uses for wiring — so
// session attribution and agent wiring can never disagree on which project markers
// count, and coverage is the full supported-agent set (not just the four legacy markers).
// First-match precedence is deterministic (an ordered slice, not a map): an earlier
// map-ranging version made OriginTool nondeterministic across process starts when a
// project has several agent dirs (e.g. .claude + .cursor) — wrong attribution is
// exactly what types.go's OriginTool comment warns about.
//
// detectAgentType 检查已知的 agent 配置目录，按固定优先级顺序返回首个命中。委托给共享
// agentsignals 表——与 agentbridge.DetectAgents 接线同源——使会话归因与 agent 接线对
// "哪些项目标记算数"永不分歧，且覆盖全部受支持 agent（不再只是四个遗留标记）。首次匹配
// 优先级确定（有序切片，非 map）：早期遍历 map 的版本在项目含多个 agent 目录（如
// .claude + .cursor）时使 OriginTool 跨进程启动不确定——归属错误正是 types.go
// OriginTool 注释论证过的危害。
func detectAgentType(root string) string {
	return agentsignals.ProjectAgentMarker(root)
}

// StampSessionAgent fills an EMPTY AgentType on the authoritative session record with
// the given agent, best-effort, and reflects the same value in the append-only
// sessions.jsonl so LoadSessions / attribution queries — which read the jsonl, NOT the
// scoped file — see the stamped agent.
//
// It serves the marker-ABSENT case: agents whose translators rewrite hook commands to
// carry `--agent <name>` (kimi, reasonix, windsurf) fire hooks with a resolved agent even
// when the project has NO marker detectAgentType recognizes, so the session is created
// with an empty AgentType. The first Pre/PostToolUse after session start stamps that
// authoritative agent onto the record.
//
// It does NOT help the Claude-compatible-stdin translators (codex, codebuddy, opencode):
// those fire hooks with agent=="" (no --agent on their commands), so the stamp is a no-op
// for them — they rely on Part 1's project markers (codex/opencode have markers; codebuddy
// has none by design and is a known attribution gap).
//
// Contract — intentionally narrow so a stamp can never make attribution WORSE:
//   - fills ONLY an empty AgentType on the live record AND in the jsonl (never
//     overwrites a non-empty value, which would clobber a correct marker-based
//     attribution);
//   - creates NO file if the session record is absent (a stamp before the session
//     exists is meaningless — EnsureSession creates it on the next task start);
//   - rotates nothing;
//   - touches the jsonl at most ONCE per session (only when a file-stamp actually fills
//     an empty slot);
//   - swallows all errors (best-effort bookkeeping — a stamp failure must never break
//     a tool call).
//
// The jsonl rewrite is best-effort under concurrency: two sessions stamping their first
// tool event at the same instant could race on the read-modify-write and one jsonl line
// stays empty. This is acceptable because (a) it happens at most once per session, (b) the
// scoped/legacy file remains the authoritative live record, and the jsonl is a
// best-effort history — the scoped value is never lost.
//
// StampSessionAgent 用给定 agent 填充权威 session 记录上空的 AgentType（尽力而为），并把同
// 一值反映到 append-only 的 sessions.jsonl，使读 jsonl（而非 scoped 文件）的 LoadSessions /
// 归因查询能看到盖戳的 agent。
//
// 它服务无标记场景：翻译器会把 hook 命令改写为携带 `--agent <name>` 的 agent
// （kimi、reasonix、windsurf），即便项目没有 detectAgentType 认识的标记，它们也会带解析出
// 的 agent 触发 hook，故 session 以空 AgentType 创建。session 起始后的首个 Pre/PostToolUse
// 把该权威 agent 盖到记录上。
//
// 它帮不到 Claude-兼容-stdin 翻译器（codex、codebuddy、opencode）：这些以 agent=="" 触发
// hook（命令上无 --agent），故盖戳对它们是 no-op——它们依赖 Part 1 的项目标记（codex/
// opencode 有标记；codebuddy 设计上无标记，是已知归因缺口）。
//
// 契约——刻意收窄，使盖戳绝不使归因更糟：
//   - 在 live 记录与 jsonl 上都只填空的 AgentType（绝不覆盖非空值，否则会冲掉正确的标记归因）；
//   - session 记录不存在时不创建文件（session 还不存在时盖戳无意义——下次 task start 时
//     EnsureSession 会创建）；
//   - 不轮换；
//   - 每 session 至多碰 jsonl 一次（仅当文件盖戳确实填了空）；
//   - 所有错误吞掉（尽力而为的簿记——盖戳失败绝不能打断工具调用）。
//
// jsonl 重写在并发下是尽力而为：两个 session 在首个工具事件同时盖戳可能在 read-modify-
// write 上竞争，其中一条 jsonl 行保持空。这可接受，因为（a）每 session 至多一次，
// （b）scoped/legacy 文件仍是权威 live 记录，jsonl 是尽力而为的历史——scoped 值不会丢。
func StampSessionAgent(root, sessionID, agent string) {
	if agent == "" {
		return
	}
	var changed bool
	var logSID string
	if sessionID != "" {
		changed = stampScopedSession(root, sessionID, agent)
		logSID = sessionID
	} else {
		changed, logSID = stampLegacySession(root, agent)
	}
	if changed && logSID != "" {
		upsertSessionAgentInLog(root, logSID, agent)
	}
}

// stampScopedSession stamps the session-scoped record at sessions/<sid>.json. It returns
// true only when it actually filled an empty AgentType (used to decide whether the jsonl
// needs the same update). It writes back to the exact path it READ — not saveScopedSession,
// which re-derives the path from s.SessionID and would diverge from the filename if the
// two ever drift.
//
// stampScopedSession 盖戳 session-scoped 记录 sessions/<sid>.json。仅当确实填了空 AgentType
// 时返回 true（用于判断 jsonl 是否需要同步更新）。写回的是它读取的那个确切路径——而非
// saveScopedSession（后者按 s.SessionID 重新推导路径，一旦 SessionID 与文件名漂移就会写偏）。
func stampScopedSession(root, sessionID, agent string) bool {
	path := sessionScopedFilePath(root, sessionID)
	data, err := os.ReadFile(path)
	if err != nil {
		return false // absent or unreadable — nothing to stamp (do NOT create).
	}
	var s SessionRecord
	if err := json.Unmarshal(data, &s); err != nil {
		return false // corrupt — leave it (EnsureSession rebuilds on the next start).
	}
	if s.AgentType != "" {
		return false // already attributed — never overwrite.
	}
	s.AgentType = agent
	out, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return false
	}
	if err := util.AtomicWrite(path, out, 0644); err != nil {
		return false
	}
	return true
}

// stampLegacySession stamps the legacy global session.json. Returns (changed, sid): sid is
// the record's own SessionID (passed to the jsonl upsert, since legacy callers pass an
// empty sessionID but the jsonl line is keyed by the record's real id).
//
// stampLegacySession 盖戳 legacy 全局 session.json。返回 (changed, sid)：sid 是记录自身的
// SessionID（传给 jsonl upsert，因 legacy 调用方传空 sessionID，而 jsonl 行以记录真实 id 为键）。
func stampLegacySession(root, agent string) (changed bool, sid string) {
	s, err := loadSession(root)
	if err != nil || s == nil {
		return false, ""
	}
	if s.AgentType != "" {
		return false, ""
	}
	s.AgentType = agent
	if err := saveSession(root, s); err != nil {
		return false, ""
	}
	return true, s.SessionID
}

// upsertSessionAgentInLog rewrites DataDir/sessions.jsonl so the line(s) matching sessionID
// carry the given agent where their AgentType is currently empty. This is what makes a
// stamped agent visible to LoadSessions and the attribution metric: appendSessionLog only
// writes a line at session creation (when AgentType is still empty), and LoadSessions reads
// the jsonl — NOT the scoped sessions/<sid>.json — so a stamp that only touches the scoped
// file would be invisible to every jsonl-based consumer. Without this, the stamp's
// contribution to the "53% missing agent_type" metric is exactly zero for scoped sessions.
//
// Best-effort + idempotent: only lines with an empty AgentType are touched (never
// overwrite); if no line matches, nothing is written (the session may not have been logged
// yet); AtomicWrite (temp+rename) keeps the rewrite tear-free. The line/byte structure is
// preserved on round-trip (split-on-newline rejoined with the same delimiter, including the
// trailing newline if present).
//
// upsertSessionAgentInLog 重写 DataDir/sessions.jsonl，使匹配 sessionID 的行在其 AgentType
// 当前为空时填上给定 agent。这是让盖戳的 agent 对 LoadSessions 与归因指标可见的关键：
// appendSessionLog 只在 session 创建时写一行（彼时 AgentType 仍为空），而 LoadSessions 读
// jsonl——不读 scoped 的 sessions/<sid>.json——故只动 scoped 文件的盖戳对一切 jsonl 消费方
// 不可见。没有这一步，盖戳对"53% agent_type 缺失"指标在 scoped session 上的贡献恰好为零。
//
// 尽力而为 + 幂等：只改 AgentType 为空的行（绝不覆盖）；无匹配行则不写（session 可能尚未记
// 日志）；AtomicWrite（temp+rename）使重写无撕裂。行/字节结构在往返中保持（按换行分割后以同
// 分隔符重新连接，含尾随换行时也保留）。
func upsertSessionAgentInLog(root, sessionID, agent string) {
	if sessionID == "" {
		return
	}
	path := sessionsLogPath(root)
	data, err := os.ReadFile(path)
	if err != nil {
		return // no log yet — nothing to update.
	}
	lines := bytes.Split(data, []byte("\n"))
	changed := false
	for i, ln := range lines {
		if len(bytes.TrimSpace(ln)) == 0 {
			continue
		}
		var s SessionRecord
		if err := json.Unmarshal(ln, &s); err != nil {
			continue // skip unparseable line, preserve it as-is.
		}
		if s.SessionID != sessionID || s.AgentType != "" {
			continue
		}
		s.AgentType = agent
		if rewritten, err := json.Marshal(s); err == nil {
			lines[i] = rewritten
			changed = true
		}
	}
	if !changed {
		return
	}
	_ = util.AtomicWrite(path, bytes.Join(lines, []byte("\n")), 0644)
}

// newSessionID generates a unique session identifier, with a timestamp and a
// random suffix.
//
// newSessionID 生成唯一 session 标识符，含时间戳与随机后缀。
func newSessionID() string {
	var buf [2]byte
	rand.Read(buf[:])
	return fmt.Sprintf("session-%s-%s", time.Now().Format("20060102150405"), hex.EncodeToString(buf[:]))
}

// CurrentSessionID returns Claude Code's session id from the env. Claude Code
// injects CLAUDE_CODE_SESSION_ID into every Bash command it runs, and the same
// value is fed to hooks via stdin (hookInput.SessionID / FORGE_SESSION_ID).
// Using it as a per-session key lets concurrent sessions on a shared checkout
// isolate their .forge/ state from each other.
//
// This is the only place in this package that reads the env — callers pass the
// resolved id down explicitly, and in-package functions (and their tests) must
// never depend on the env (otherwise tests under Claude Code would be flaky).
//
// Outside Claude Code (manual terminal), returns an empty string.
//
// CurrentSessionID 从 env 返回 Claude Code 的 session id。Claude Code 把
// CLAUDE_CODE_SESSION_ID 注入它跑的每条 Bash 命令，同一值也经 stdin
// （hookInput.SessionID / FORGE_SESSION_ID）送给 hooks。把它用作 per-session key
// 让共享 checkout 上并发的 session 各自隔离 .forge/ 状态。
//
// 这是本包唯一读取该 env 的位置——调用方把解析出的 id 显式下传，包内函数（及其
// 测试）绝不依赖环境 env（否则在 Claude Code 下测试会 flaky）。
//
// 不在 Claude Code 下（手动终端）返回空串。
//
// Multi-host (user-level management): hooks of every host run via `forge hook <name>`,
// which normalizes each host's stdin session_id and injects it as FORGE_SESSION_ID into
// the hook script env (cli/hook.go); thin wrappers (`exec forge task resume --hook`)
// inherit it. So after the Claude-specific env, fall back to FORGE_SESSION_ID so
// kimi/windsurf/codex sessions also get session-scoped state (active-task-ref-<sid>,
// resume-stale sentinel) instead of all collapsing onto the legacy global file.
// "default" is the scripts' own empty-placeholder — treat it as empty.
//
// 多 host（用户级管理）：各 host 的 hook 都经 `forge hook <name>` 运行，runHook 把
// 各 host stdin 的 session_id normalize 后以 FORGE_SESSION_ID 注入 hook 脚本 env
// （cli/hook.go），thin wrapper（`exec forge task resume --hook`）继承它。故在
// Claude 专属 env 之后回落 FORGE_SESSION_ID，让 kimi/windsurf/codex session 也获得
// session-scoped 状态（active-task-ref-<sid>、resume-stale sentinel），而非全挤到
// legacy 全局文件。"default" 是脚本侧的空占位符——按空处理。
func CurrentSessionID() string {
	// Host-injected shell env (today only claude-code's CLAUDE_CODE_SESSION_ID;
	// the registry loop keeps this host-agnostic — see hostcap.Host.ShellSessionEnv).
	//
	// 宿主注入的 shell env（目前仅 claude-code 的 CLAUDE_CODE_SESSION_ID；注册表
	// 循环使此处保持宿主无关——见 hostcap.Host.ShellSessionEnv）。
	if _, sid := hostcap.ProbeShellIdentity(); sid != "" {
		return sid
	}
	if sid := os.Getenv("FORGE_SESSION_ID"); sid != "" && sid != "default" {
		return sid
	}
	return ""
}

// EnsureHookSession registers a session observed on the hook path, best-effort.
// The hook dispatcher holds the AUTHORITATIVE double identity — the session id
// normalized from the host's stdin plus the resolved --agent — while the CLI path
// (EnsureSession via `forge task start`) is the only other registration point.
// Hosts whose agent drives forge through a Bash tool without any identity env
// (kimi/codex/cursor/...) never hit the CLI path with a real session id, so
// without this their sessions were NEVER registered: sessions.jsonl carried
// agent_type=claude-code only, fleet-wide (2026-08 attribution audit).
//
// Semantics:
//   - sessionID empty → no-op (the legacy global session.json keeps its
//     stamp-only path; a hook without a session id must not rotate legacy state).
//   - Record absent → create it with AgentType = agent (declarative truth from
//     --agent). agent empty → fall back to the project-marker weak signal
//     (detectAgentType), same as ensureScopedSession.
//   - Record present → reuse StampSessionAgent's fill-empty-only contract
//     (never overwrite, jsonl synced at most once per session).
//   - All errors swallowed: bookkeeping must never break a hook.
//
// EnsureHookSession 登记 hook 路径上观察到的会话，尽力而为。hook 分发器手持权威
// 双重身份——从宿主 stdin 归一化的 session id 加解析出的 --agent——而 CLI 路径
// （`forge task start` 的 EnsureSession）是此前唯一的登记点。agent 经无身份 env
// 的 Bash 工具驱动 forge 的宿主（kimi/codex/cursor/...）从不以真实 session id
// 走到 CLI 路径，故没有本函数它们的会话永远不被登记：sessions.jsonl 全机只有
// agent_type=claude-code（2026-08 归因审计）。
//
// 语义：
//   - sessionID 为空 → no-op（legacy 全局 session.json 保持 stamp-only 路径；
//     无 session id 的 hook 不得触发 legacy 轮换）。
//   - 记录不存在 → 以 AgentType = agent 建档（来自 --agent 的声明式真相）。
//     agent 为空 → 回落项目标记弱信号（detectAgentType），同 ensureScopedSession。
//   - 记录已存在 → 沿用 StampSessionAgent 的只填空契约（绝不覆盖，jsonl 每会话
//     至多同步一次）。
//   - 吞掉所有错误：簿记绝不能打断 hook。
func EnsureHookSession(root, sessionID, agent string) {
	if root == "" || sessionID == "" {
		return
	}
	if _, err := os.Stat(sessionScopedFilePath(root, sessionID)); err == nil {
		// Already registered — fill an empty AgentType only (idempotent).
		//
		// 已登记——仅填空的 AgentType（幂等）。
		StampSessionAgent(root, sessionID, agent)
		return
	}
	agentType := agent
	if agentType == "" {
		agentType = detectAgentType(root)
	}
	now := time.Now()
	s := &SessionRecord{
		SessionID:    sessionID,
		StartedAt:    now,
		StartedEpoch: now.Unix(),
		AgentType:    agentType,
	}
	if err := saveScopedSession(root, s); err != nil {
		return
	}
	// Duplicate jsonl lines for the same SessionID are tolerated (LoadSessions
	// keeps every parseable line; upsertSessionAgentInLog updates them all) —
	// two hooks racing the first registration may both append.
	//
	// 同一 SessionID 的重复 jsonl 行可容忍（LoadSessions 保留每个可解析行；
	// upsertSessionAgentInLog 全部更新）——两个 hook 竞争首次登记时可能都追加。
	_ = appendSessionLog(root, s)
}

// lastSessionFreshWindow bounds how long a last-session pointer stays
// adoptable: a `forge task start` run inside a host's Bash tool carries no
// identity env on any host except claude-code, so the CLI side adopts the most
// recent hook-observed session — but only within this window. The residual
// misattribution risk (a human running forge in a bare terminal minutes after
// agent activity) is accepted and scoped: the adopted sid binds the new task to
// the AGENT session's scoped key (SetActiveTaskRef/EnsureSession), so the
// agent's hooks would treat the human's task as their active one, and
// OriginTool is mislabeled — an attribution error, never a gate/integrity one
// (review M2: the blast radius is wider than a label, and that is deliberate —
// the dominant case IS an agent's Bash tool; the window caps the exposure).
//
// lastSessionFreshWindow 限定 last-session 指针可被采纳的时长：除 claude-code
// 外任何宿主的 Bash 工具里跑 `forge task start` 都不带身份 env，故 CLI 侧采纳
// 最近一次 hook 观察到的会话——但仅限此窗口内。残留的误归属风险（agent 活动
// 后几分钟内人在裸终端跑 forge）被接受且范围明确：采纳的 sid 会把新任务绑进
// agent 会话的 scoped 键（SetActiveTaskRef/EnsureSession），agent 侧 hook 会把
// 人类的任务当作自己的活动任务，OriginTool 也会被错标——是归因错误，绝非门
// 禁/完整性错误（review M2：爆炸半径大于一个标签，这是有意设计——主场景正
// 是 agent 的 Bash 工具；窗口限制了暴露面）。
const lastSessionFreshWindow = 15 * time.Minute

// lastSessionWriteThrottle caps pointer rewrites: hooks fire per tool call, and
// the pointer's consumers only need minute-scale freshness. The throttle is
// session-AGNOSTIC: with two non-claude hosts interleaving (kimi + codex), a
// host switch can take up to one throttle window to show up in the pointer —
// during it, the new host's `forge task start` adopts the previous host's
// session (review M1; accepted — single-host use dominates and the error is
// attribution-only).
//
// lastSessionWriteThrottle 限制指针重写频率：hook 每次工具调用都触发，而指针
// 的消费方只需要分钟级新鲜度。节流是会话无关的：两个非 claude 宿主交错
// （kimi + codex）时，宿主切换最长需要一个节流窗口才反映到指针——期间新宿
// 主的 `forge task start` 会采纳前一个宿主的会话（review M1；接受——单宿主
// 使用是主流，且错误仅限归因）。
const lastSessionWriteThrottle = 30 * time.Second

// LastSessionPointer records the most recent hook-observed session for a
// project, bridging the identity gap between the hook path (which knows
// sid+agent from stdin/--agent) and the CLI path (which on non-Claude hosts has
// neither).
//
// LastSessionPointer 记录项目最近一次 hook 观察到的会话，桥接 hook 路径（从
// stdin/--agent 得知 sid+agent）与 CLI 路径（非 Claude 宿主上两者皆无）之间
// 的身份缺口。
type LastSessionPointer struct {
	SessionID string `json:"session_id"`
	Agent     string `json:"agent,omitempty"`
	Epoch     int64  `json:"epoch"`
	Event     string `json:"event,omitempty"`
}

// lastSessionPath returns the pointer file path inside the project's DataDir.
//
// lastSessionPath 返回项目 DataDir 内指针文件的路径。
func lastSessionPath(root string) string {
	return filepath.Join(dataHome(root), "last-session.json")
}

// TouchLastSession refreshes the last-session pointer, throttled to one write
// per lastSessionWriteThrottle. Called by the hook dispatcher on every event
// where the host supplied a session id. Best-effort: all errors swallowed.
//
// TouchLastSession 刷新 last-session 指针，按 lastSessionWriteThrottle 节流。
// 由 hook 分发器在宿主提供了 session id 的每次事件上调用。尽力而为：吞掉所有
// 错误。
func TouchLastSession(root, sessionID, agent, event string) {
	if root == "" || sessionID == "" {
		return
	}
	path := lastSessionPath(root)
	if data, err := os.ReadFile(path); err == nil {
		var p LastSessionPointer
		if json.Unmarshal(data, &p) == nil && time.Since(time.Unix(p.Epoch, 0)) < lastSessionWriteThrottle {
			return
		}
	}
	out, err := json.Marshal(LastSessionPointer{
		SessionID: sessionID,
		Agent:     agent,
		Epoch:     time.Now().Unix(),
		Event:     event,
	})
	if err != nil {
		return
	}
	_ = util.AtomicWrite(path, out, 0644)
}

// RecentHookSession returns the pointer's session id and agent when the pointer
// exists and is younger than lastSessionFreshWindow; otherwise ok=false. CLI
// commands (task start, continuity anchors) use this as the LAST attribution
// fallback, after every env probe — adopting a stale pointer would mislabel a
// human's manual terminal work, so the freshness check is load-bearing.
//
// RecentHookSession 在指针存在且新于 lastSessionFreshWindow 时返回其 session id
// 与 agent；否则 ok=false。CLI 命令（task start、接续锚定）把它作为一切 env
// 探测之后的最终归因回落——采纳过期指针会错标人类的手动终端操作，故新鲜度
// 检查是承重的。
func RecentHookSession(root string) (sessionID, agent string, ok bool) {
	if root == "" {
		return "", "", false
	}
	data, err := os.ReadFile(lastSessionPath(root))
	if err != nil {
		return "", "", false
	}
	var p LastSessionPointer
	if err := json.Unmarshal(data, &p); err != nil || p.SessionID == "" {
		return "", "", false
	}
	if time.Since(time.Unix(p.Epoch, 0)) >= lastSessionFreshWindow {
		return "", "", false
	}
	return p.SessionID, p.Agent, true
}
