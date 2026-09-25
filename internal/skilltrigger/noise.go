package skilltrigger

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/util"
)

// MaxStopRounds Stop 事件每 session 最多注入次数（防 Stop→注入→响应→Stop 死循环，
// 参考 review-stop 的 MaxReviewRounds=3）。
const MaxStopRounds = 3

// MaxSessionSkillFires is the hard per-session per-skill injection ceiling.
//
// MaxSessionSkillFires 同一 session 内同一 skill 的注入硬封顶。cooldown 只限频不限量
// （2026-08 usage 证据：implementation-discipline 单 session 注入 79 次、test-discipline
// 20 次——第二次起 agent 一律不重读 skill，纯 context 浪费），故在 cooldown 之上加总量
// 硬顶：第 1 次完整注入、第 2 次短提醒（Hit.Reminder）、之后一律抑制
// （SuppressSessionCap）。封顶计数与 cooldown marker 同目录同寿命（$TMPDIR session 态）。
const MaxSessionSkillFires = 2

// NoiseController 抽象噪音控制：per-session per-skill cooldown/dedup + session 硬封顶 +
// Stop max-rounds + 全局/per-skill 禁用。生产用 FileBasedNoiseController（marker 文件），
// 测试用 InMemoryNoiseController。Eval 只调只读判定（ShouldFire / FireCount /
// StopRoundAllowed），落盘（Mark / IncrStopRound）由 CLI 层在确认注入后调用。
type NoiseController interface {
	ShouldFire(sessionID, skill string, cooldown time.Duration, now time.Time) bool
	Mark(sessionID, skill string, now time.Time) error
	// FireCount returns how many times this skill was injected in this session (cumulative Mark count).
	//
	// FireCount 返回本 session 内该 skill 已注入次数（Mark 的累计）。
	FireCount(sessionID, skill string) int
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

// firesPath 是 per-session per-skill 注入计数文件（与 .marker 同目录同寿命）。
func (n *FileBasedNoiseController) firesPath(sessionID, skill string) string {
	return filepath.Join(n.sessionDir(sessionID), sanitizePart(skill)+".fires")
}

// ShouldFire：全局禁用 / per-skill 禁用 / 在 cooldown 内已注入过 → false。
func (n *FileBasedNoiseController) ShouldFire(sessionID, skill string, cooldown time.Duration, now time.Time) bool {
	if Disabled() {
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

// Mark writes the marker file (mtime = last injection time) and increments the injection count.
//
// Mark 写 marker 文件（mtime 即上次注入时间）并把注入计数 +1。计数 RMW 与 IncrStopRound
// 同样存在跨进程并发丢更新的已知竞态（同 session 并行 hook 各读同写）——计数只服务
// 封顶判定，丢一次更新最坏多放一次提醒注入，可接受，不引入文件锁。
//
// TOCTOU 说明：Eval 的 session-cap 判定（FireCount 只读）与本 Mark 之间无锁——两个并行
// hook 进程可同见 count=1 同判放行，双发第 2 次注入。与 cooldown 的 ShouldFire→Mark
// 窗口同容忍度（最坏多一次注入，不丢不错），不引入跨进程锁。
func (n *FileBasedNoiseController) Mark(sessionID, skill string, now time.Time) error {
	p := n.markerPath(sessionID, skill)
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return err
	}
	if err := util.AtomicWrite(p, []byte(now.UTC().Format(time.RFC3339)), 0644); err != nil {
		return err
	}
	fp := n.firesPath(sessionID, skill)
	cnt := 0
	if data, err := os.ReadFile(fp); err == nil {
		cnt, _ = strconv.Atoi(strings.TrimSpace(string(data)))
	}
	return util.AtomicWrite(fp, []byte(strconv.Itoa(cnt+1)), 0644)
}

// FireCount reads this session's injection count for the skill (absent/unreadable = 0).
//
// FireCount 读本 session 该 skill 的注入计数（无文件/读失败 = 0）。
func (n *FileBasedNoiseController) FireCount(sessionID, skill string) int {
	data, err := os.ReadFile(n.firesPath(sessionID, skill))
	if err != nil {
		return 0
	}
	cnt, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	return cnt
}

// StopRoundAllowed：本 session 已注入 Stop 次数 < MaxStopRounds。
func (n *FileBasedNoiseController) StopRoundAllowed(sessionID string, now time.Time) bool {
	if Disabled() {
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
	// atomic write（tmp+rename+fsync，见 util.AtomicWrite）防部分写；session-scoped sid 使跨 host RMW 竞态不触发
	// （各 host 独立 session_id → 不同 sid 落不同文件，无共享计数器碰撞）。
	return util.AtomicWrite(p, []byte(strconv.Itoa(cnt+1)), 0644)
}

// Disabled reports whether the global skill-trigger kill switch is active
// (FORGE_SKILL_TRIGGER=0).
//
// Disabled 报告全局 skill-trigger 关闭开关是否生效（FORGE_SKILL_TRIGGER=0）。
// 判定的单一真相源：本包各 controller 与 cli 的早返路径都消费它，禁止散写
// env 字面量比较（2026-09 代码普查 P3：曾在 2 个包重复 5 处）。
func Disabled() bool {
	return os.Getenv("FORGE_SKILL_TRIGGER") == "0"
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

// InMemoryNoiseController 测试用（map-backed，不碰文件系统）。
type InMemoryNoiseController struct {
	FiredAt   map[string]time.Time // key = sessionID|skill
	FireCnt   map[string]int       // key = sessionID|skill
	StopCount map[string]int       // key = sessionID
}

// NewInMemoryNoiseController 构造一个内存态噪音控制器。
func NewInMemoryNoiseController() *InMemoryNoiseController {
	return &InMemoryNoiseController{
		FiredAt:   map[string]time.Time{},
		FireCnt:   map[string]int{},
		StopCount: map[string]int{},
	}
}

func noiseKey(sessionID, skill string) string { return sessionID + "|" + skill }

func (m *InMemoryNoiseController) ShouldFire(sessionID, skill string, cooldown time.Duration, now time.Time) bool {
	if Disabled() {
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
	m.FireCnt[noiseKey(sessionID, skill)]++
	return nil
}

// FireCount returns the cumulative Mark count.
//
// FireCount 返回 Mark 的累计次数。
func (m *InMemoryNoiseController) FireCount(sessionID, skill string) int {
	return m.FireCnt[noiseKey(sessionID, skill)]
}

func (m *InMemoryNoiseController) StopRoundAllowed(sessionID string, now time.Time) bool {
	if Disabled() {
		return false
	}
	return m.StopCount[sessionID] < MaxStopRounds
}

func (m *InMemoryNoiseController) IncrStopRound(sessionID string) error {
	m.StopCount[sessionID]++
	return nil
}
