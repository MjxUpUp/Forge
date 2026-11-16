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

	"github.com/MjxUpUp/Forge/internal/agentsignals"
	"github.com/MjxUpUp/Forge/internal/hostcap"
	"github.com/MjxUpUp/Forge/internal/nodestamp"
	"github.com/MjxUpUp/Forge/internal/util"
)

// sessionMaxIdle 是 session 轮换前的最长空闲时间。当前 session 起算若超过此时长，
// 下次 task start 时创建新 session。
const sessionMaxIdle = 2 * time.Hour

// SessionRecord represents a single agent development session.
//
// SessionRecord 表示一次 agent 开发 session。
// session 把同一 agent 交互里创建的 task 归为一组。
type SessionRecord struct {
	SessionID string    `json:"session_id"`
	StartedAt time.Time `json:"started_at"`
	// StartedEpoch is the Unix-second form of StartedAt, persisted alongside it.
	//
	// StartedEpoch 是 StartedAt 的 Unix 秒形式，与其一起落盘，让需要 epoch 的
	// 消费者（典型如 session-health bash hook）能读到一个精确整数，而不必用
	// 跨平台脆弱的 date 命令（GNU vs BSD）解析 RFC3339Nano 串。这让 Go→bash 的
	// 契约是一个显式整数字段，而非一个 bash 必须反向解析的格式。
	StartedEpoch int64  `json:"started_epoch,omitempty"`
	AgentType    string `json:"agent_type,omitempty"`
	// Stamp 携带机器归因（node_id/seq/ts_hlc/sig），由 appendSessionLog 经
	// nodestamp.Next 落章——存量行与 fail-open 时为零值。
	nodestamp.Stamp
}

// sessionFilePath 返回当前 session 跟踪文件路径。
func sessionFilePath(root string) string {
	return filepath.Join(dataHome(root), "session.json")
}

// sessionsLogPath 返回历史 sessions 日志路径。
func sessionsLogPath(root string) string {
	return filepath.Join(dataHome(root), "sessions.jsonl")
}

// EnsureSession returns the currently active session, creating one when needed.
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

	// legacy 路径：加载/轮换全局 session.json。
	existing, err := loadSession(root)
	if err != nil {
		return nil, err
	}

	if existing != nil && time.Since(existing.StartedAt) < sessionMaxIdle {
		return existing, nil
	}

	// 创建新 session 前先归档旧的
	if existing != nil {
		if err := appendSessionLog(root, existing); err != nil {
			return nil, err
		}
	}

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

	// 同时写入历史日志
	if err := appendSessionLog(root, session); err != nil {
		return nil, err
	}

	return session, nil
}

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

	// 追加到历史日志。一行重复无害：LoadSessions 不在日志行间去重（保留每个可解析行），
	// 故同一 SessionID 的重复行原样容忍；jsonl agent-upsert（upsertSessionAgentInLog）
	// 更新每一条匹配行，使重复行保持一致，而非留下空 agent_type 的副本。切勿把该 upsert
	// 「简化」为只改首条命中——那会重新引入空 agent 的指标缺口。
	if err := appendSessionLog(root, session); err != nil {
		return nil, err
	}

	return session, nil
}

// sessionScopedFilePath 返回按 session 隔离的记录路径。
func sessionScopedFilePath(root, sessionID string) string {
	return filepath.Join(dataHome(root), "sessions", util.SanitizeSessionID(sessionID)+".json")
}

// saveScopedSession 把 session 记录写到它的 scoped 路径。
func saveScopedSession(root string, s *SessionRecord) error {
	path := sessionScopedFilePath(root, s.SessionID)
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	// AtomicWrite（temp+rename）：半写会损坏所有 session 加载方解析的 JSON
	// （论证同 state_atomic_test.go 之 task state）。
	return util.AtomicWrite(path, data, 0644)
}

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
		// JSON 损坏有意视为「无 session」——与 ensureScopedSession 的契约一致（见其
		// 注释）：调用方直接轮换出新 session，而非为一个不可读的簿记文件让
		// task start 失败。
		return nil, nil
	}
	return &s, nil
}

// saveSession 写入当前 session 文件。
func saveSession(root string, s *SessionRecord) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	// AtomicWrite（temp+rename）——理由见 saveScopedSession。
	return util.AtomicWrite(sessionFilePath(root), data, 0644)
}

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

// LoadSessions reads all historical session records from DataDir/sessions.jsonl. A currently active session, if any, is included as well.
//
// LoadSessions 从 DataDir/sessions.jsonl 读所有历史 session 记录。
// 若存在当前活跃 session 也一并包含。
func LoadSessions(root string) ([]SessionRecord, error) {
	var sessions []SessionRecord

	// 先读当前 session（最新）
	current, err := loadSession(root)
	if err != nil {
		return nil, err
	}

	// 读历史日志
	f, err := os.Open(sessionsLogPath(root))
	if err != nil {
		if os.IsNotExist(err) {
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

// detectAgentType 检查已知的 agent 配置目录，按固定优先级顺序返回首个命中。委托给共享
// agentsignals 表，使会话归因与 agent 接线对"哪些项目标记算数"永不分歧，且覆盖
// 全部受支持 agent。优先级确定（有序切片，非 map）：项目含多个 agent 目录
// （如 .claude + .cursor）时 OriginTool 不随启动次序漂移。
func detectAgentType(root string) string {
	return agentsignals.ProjectAgentMarker(root)
}

// newSessionID 生成唯一 session 标识符，含时间戳与随机后缀。
func newSessionID() string {
	var buf [2]byte
	rand.Read(buf[:])
	return fmt.Sprintf("session-%s-%s", time.Now().Format("20060102150405"), hex.EncodeToString(buf[:]))
}

// CurrentSessionID returns Claude Code's session id from the env.
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
// 历史注记：旧宿主接线经 `forge hook <name>` 运行，把各 host stdin 的 session_id
// normalize 后以 FORGE_SESSION_ID 注入脚本 env（thin wrapper 继承它）。阶段 D 已删除
// 宿主层，现在只剩「宿主环境变量优先、FORGE_SESSION_ID 兜底」这一条读路径。故在
// Claude 专属 env 之后回落 FORGE_SESSION_ID，让 kimi/windsurf/codex session 也获得
// session-scoped 状态（active-task-ref-<sid>、ResumeStale 标记），而非全挤到
// legacy 全局文件。"default" 是脚本侧的空占位符——按空处理。
func CurrentSessionID() string {
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

// lastSessionFreshWindow 限定 last-session 指针可被采纳的时长：除 claude-code
// 外任何宿主的 Bash 工具里跑 `forge task start` 都不带身份 env，故 CLI 侧采纳
// 最近一次 hook 观察到的会话——但仅限此窗口内。残留的误归属风险（agent 活动
// 后几分钟内人在裸终端跑 forge）被接受且范围明确：采纳的 sid 会把新任务绑进
// agent 会话的 scoped 键（SetActiveTaskRef/EnsureSession），agent 侧 hook 会把
// 人类的任务当作自己的活动任务，OriginTool 也会被错标——是归因错误，绝非门
// 禁/完整性错误（review M2：爆炸半径大于一个标签，这是有意设计——主场景正
// 是 agent 的 Bash 工具；窗口限制了暴露面）。
const lastSessionFreshWindow = 15 * time.Minute

// lastSessionWriteThrottle 限制指针重写频率：hook 每次工具调用都触发，而指针
// 的消费方只需要分钟级新鲜度。节流是会话无关的：两个非 claude 宿主交错
// （kimi + codex）时，宿主切换最长需要一个节流窗口才反映到指针——期间新宿
// 主的 `forge task start` 会采纳前一个宿主的会话（review M1；接受——单宿主
// 使用是主流，且错误仅限归因）。
const lastSessionWriteThrottle = 30 * time.Second

// LastSessionPointer records the most recent hook-observed session, bridging the hook-path/CLI-path identity gap.
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

// lastSessionPath 返回项目 DataDir 内指针文件的路径。
func lastSessionPath(root string) string {
	return filepath.Join(dataHome(root), "last-session.json")
}

// RecentHookSession returns the pointer's session id and agent when the pointer exists and is younger than lastSessionFreshWindow; otherwise ok=false.
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
