package taskpipeline

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

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
		if err := archiveSession(root, existing); err != nil {
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

	// Append to the history log (idempotent enough: a duplicate line in an
	// append-only log is harmless, LoadSessions dedupes by SessionID).
	//
	// 追加到历史日志（足够幂等：append-only 日志里一行重复无害，LoadSessions
	// 会按 SessionID 去重）。
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

// archiveSession writes the completed session into the history log.
//
// archiveSession 把已完成的 session 写入历史日志。
func archiveSession(root string, s *SessionRecord) error {
	return appendSessionLog(root, s)
}

// appendSessionLog appends a session record to DataDir/sessions.jsonl.
//
// appendSessionLog 追加一条 session 记录到 DataDir/sessions.jsonl。
func appendSessionLog(root string, s *SessionRecord) error {
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

// detectAgentType checks for known agent configuration directories. The checks run
// in a FIXED priority order (first hit wins): an earlier version ranged over a map,
// and Go's random map iteration made OriginTool nondeterministic across process
// starts when a project has several agent dirs (e.g. .claude + .cursor) — wrong
// attribution is exactly what types.go's OriginTool comment warns about.
//
// detectAgentType 检查已知的 agent 配置目录。按固定优先级顺序检查（首个命中即
// 返回）：旧版本遍历 map，Go map 顺序随机，项目同时存在多个 agent 目录（如
// .claude + .cursor）时 OriginTool 每次进程启动随机取——归属错误正是
// types.go OriginTool 注释论证过的危害。
func detectAgentType(root string) string {
	checks := []struct {
		dir   string
		agent string
	}{
		{".claude", "claude-code"},
		{".cursor", "cursor"},
		{".github/instructions", "copilot"},
		{".windsurfrules", "windsurf"},
	}
	for _, c := range checks {
		path := filepath.Join(root, c.dir)
		if info, err := os.Stat(path); err == nil {
			if c.dir == ".windsurfrules" {
				// .windsurfrules is a file, not a directory.
				//
				// .windsurfrules 是文件而非目录
				if !info.IsDir() {
					return c.agent
				}
			} else if info.IsDir() {
				return c.agent
			}
		}
	}
	return ""
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
//（hookInput.SessionID / FORGE_SESSION_ID）送给 hooks。把它用作 per-session key
// 让共享 checkout 上并发的 session 各自隔离 .forge/ 状态。
//
// 这是本包唯一读取该 env 的位置——调用方把解析出的 id 显式下传，包内函数（及其
// 测试）绝不依赖环境 env（否则在 Claude Code 下测试会 flaky）。
//
// 不在 Claude Code 下（手动终端）返回空串。
func CurrentSessionID() string {
	return os.Getenv("CLAUDE_CODE_SESSION_ID")
}
