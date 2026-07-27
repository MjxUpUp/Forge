package skilltrigger

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// MaxStopRounds Stop 事件每 session 最多注入次数（防 Stop→注入→响应→Stop 死循环，
// 参考 review-stop 的 MaxReviewRounds=3）。
const MaxStopRounds = 3

// NoiseController 抽象噪音控制：per-session per-skill cooldown/dedup + Stop max-rounds
// + 全局/per-skill 禁用。生产用 FileBasedNoiseController（marker 文件），测试用
// InMemoryNoiseController。Eval 只调只读判定（ShouldFire / StopRoundAllowed），
// 落盘（Mark / IncrStopRound）由 CLI 层在确认注入后调用。
type NoiseController interface {
	ShouldFire(sessionID, skill string, cooldown time.Duration, now time.Time) bool
	Mark(sessionID, skill string, now time.Time) error
	StopRoundAllowed(sessionID string, now time.Time) bool
	IncrStopRound(sessionID string) error
}

// FileBasedNoiseController 生产实现。marker 落 BaseDir/<sessionID>/<skill>.marker。
type FileBasedNoiseController struct {
	BaseDir string // 通常 <GlobalHome>/skill-trigger
}

// NewFileNoiseController 构造一个文件态噪音控制器。
func NewFileNoiseController(baseDir string) *FileBasedNoiseController {
	return &FileBasedNoiseController{BaseDir: baseDir}
}

func (n *FileBasedNoiseController) sessionDir(sessionID string) string {
	return filepath.Join(n.BaseDir, sanitizePart(sessionID))
}

func (n *FileBasedNoiseController) markerPath(sessionID, skill string) string {
	return filepath.Join(n.sessionDir(sessionID), sanitizePart(skill)+".marker")
}

func (n *FileBasedNoiseController) stopRoundsPath(sessionID string) string {
	return filepath.Join(n.sessionDir(sessionID), "stop-rounds")
}

// ShouldFire：全局禁用 / per-skill 禁用 / 在 cooldown 内已注入过 → false。
func (n *FileBasedNoiseController) ShouldFire(sessionID, skill string, cooldown time.Duration, now time.Time) bool {
	if os.Getenv("FORGE_SKILL_TRIGGER") == "0" {
		return false
	}
	if isSkillDisabled(skill) {
		return false
	}
	if info, err := os.Stat(n.markerPath(sessionID, skill)); err == nil && !info.ModTime().After(now) && now.Sub(info.ModTime()) < cooldown {
		return false
	}
	return true
}

// Mark 写 marker 文件（mtime 即上次注入时间）。
func (n *FileBasedNoiseController) Mark(sessionID, skill string, now time.Time) error {
	p := n.markerPath(sessionID, skill)
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return err
	}
	return atomicWriteFile(p, []byte(now.UTC().Format(time.RFC3339)))
}

// StopRoundAllowed：本 session 已注入 Stop 次数 < MaxStopRounds。
func (n *FileBasedNoiseController) StopRoundAllowed(sessionID string, now time.Time) bool {
	if os.Getenv("FORGE_SKILL_TRIGGER") == "0" {
		return false
	}
	data, err := os.ReadFile(n.stopRoundsPath(sessionID))
	if err != nil {
		return true // 无计数 = 允许
	}
	cnt, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	return cnt < MaxStopRounds
}

// IncrStopRound Stop 注入计数 +1。
func (n *FileBasedNoiseController) IncrStopRound(sessionID string) error {
	p := n.stopRoundsPath(sessionID)
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return err
	}
	cnt := 0
	if data, err := os.ReadFile(p); err == nil {
		cnt, _ = strconv.Atoi(strings.TrimSpace(string(data)))
	}
	// atomic write（tmp+rename）防部分写；session-scoped sid 使跨 host RMW 竞态不触发
	// （各 host 独立 session_id → 不同 sid 落不同文件，无共享计数器碰撞）。
	return atomicWriteFile(p, []byte(strconv.Itoa(cnt+1)))
}

// isSkillDisabled 检查 skill 是否在 FORGE_SKILL_TRIGGER_DISABLE 逗号列表中。
func isSkillDisabled(skill string) bool {
	list := os.Getenv("FORGE_SKILL_TRIGGER_DISABLE")
	if list == "" {
		return false
	}
	for _, s := range strings.Split(list, ",") {
		if strings.TrimSpace(s) == skill {
			return true
		}
	}
	return false
}

// sanitizePart 把 sessionID/skill 名规整为文件系统安全字符（非法字符→'_'，空→"default"）。
func sanitizePart(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "default"
	}
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	return b.String()
}

// atomicWriteFile 原子写：tmp 文件 + 同目录 rename（rename 同目录原子）。防 hook 进程
// 中途崩溃留半写 marker/stop-rounds。session-scoped sid 隔离下不解决跨 host RMW 竞态
// （那需 flock），但各 host session_id 不同 → 无共享计数器碰撞。
//
// atomicWriteFile writes atomically via tmp + same-dir rename. Prevents half-written
// marker/stop-rounds on hook-process crash.
func atomicWriteFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}

// InMemoryNoiseController 测试用（map-backed，不碰文件系统）。
type InMemoryNoiseController struct {
	FiredAt   map[string]time.Time // key = sessionID|skill
	StopCount map[string]int       // key = sessionID
}

// NewInMemoryNoiseController 构造一个内存态噪音控制器。
func NewInMemoryNoiseController() *InMemoryNoiseController {
	return &InMemoryNoiseController{
		FiredAt:   map[string]time.Time{},
		StopCount: map[string]int{},
	}
}

func noiseKey(sessionID, skill string) string { return sessionID + "|" + skill }

func (m *InMemoryNoiseController) ShouldFire(sessionID, skill string, cooldown time.Duration, now time.Time) bool {
	if os.Getenv("FORGE_SKILL_TRIGGER") == "0" {
		return false
	}
	if isSkillDisabled(skill) {
		return false
	}
	if t, ok := m.FiredAt[noiseKey(sessionID, skill)]; ok && !t.After(now) && now.Sub(t) < cooldown {
		return false
	}
	return true
}

func (m *InMemoryNoiseController) Mark(sessionID, skill string, now time.Time) error {
	m.FiredAt[noiseKey(sessionID, skill)] = now
	return nil
}

func (m *InMemoryNoiseController) StopRoundAllowed(sessionID string, now time.Time) bool {
	if os.Getenv("FORGE_SKILL_TRIGGER") == "0" {
		return false
	}
	return m.StopCount[sessionID] < MaxStopRounds
}

func (m *InMemoryNoiseController) IncrStopRound(sessionID string) error {
	m.StopCount[sessionID]++
	return nil
}
