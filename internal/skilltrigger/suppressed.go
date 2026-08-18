package skilltrigger

// suppressed.go — cooldown 抑制计数的 session 级短命存储（辩论 P1 / R8 修复）。
//
// 职责边界：本计数器只活在 session 生命周期内（$TMPDIR，与 noise marker 同目录同寿命），
// 职责是「攒着抑制次数，等下次真实触发时回填进 Entry.Meta（checklog.MetaKeySuppressedSinceLast）」。
// 持久化由 checklog 承担——回填落章后跨 session 聚合从不可变日志推导，运行时不再维护
// 任何长命计数状态（反目标：不在热路径养计数器）。
//
// 诚实缺口（G5，文档化并接受）：session 末段的抑制突发没有「下次触发」可回填，计数随
// $TMPDIR 清理消失——逐条抑制记录会淹日志，这是 R8 折中的已知代价。
//
// suppressed.go — session-scoped short-lived storage for cooldown suppression counts
// (debate P1 / R8 fix).
//
// Boundary: this counter lives only within a session's lifetime ($TMPDIR, same dir and
// lifespan as noise markers); its job is "hold the count until the next actual fire
// backfills it into Entry.Meta (checklog.MetaKeySuppressedSinceLast)". Durability is
// checklog's job — once backfilled, cross-session aggregation derives from the immutable
// log; the runtime keeps no long-lived counter state (anti-goal: no counters on the hot
// path).
//
// Honest gap (G5, documented and accepted): a suppression burst at session end has no
// "next fire" to backfill into — the count dies with $TMPDIR cleanup. Per-event
// suppression records would flood the log; this is the known cost of the R8 compromise.

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/MjxUpUp/Forge/internal/util"
)

// FileSuppressedCounter 是抑制计数的文件实现：BaseDir/<session>/<skill>.suppressed。
// 与 FileBasedNoiseController 同 BaseDir 即同寿命（marker 目录），互不干涉。
//
// FileSuppressedCounter is the file impl of the suppression counter:
// BaseDir/<session>/<skill>.suppressed. Sharing BaseDir with FileBasedNoiseController
// means sharing lifespan (the marker dir) without interference.
type FileSuppressedCounter struct {
	BaseDir string
}

// NewFileSuppressedCounter 构造一个文件态抑制计数器。
func NewFileSuppressedCounter(baseDir string) *FileSuppressedCounter {
	return &FileSuppressedCounter{BaseDir: baseDir}
}

func (c *FileSuppressedCounter) path(sessionID, skill string) string {
	return filepath.Join(c.BaseDir, sanitizePart(sessionID), sanitizePart(skill)+".suppressed")
}

// Incr 抑制计数 +1。失败静默（best-effort：计数是观测辅助，绝不能阻塞 hook 链）。
func (c *FileSuppressedCounter) Incr(sessionID, skill string) error {
	p := c.path(sessionID, skill)
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return err
	}
	cnt := 0
	if data, err := os.ReadFile(p); err == nil {
		cnt, _ = strconv.Atoi(strings.TrimSpace(string(data)))
	}
	return util.AtomicWrite(p, []byte(strconv.Itoa(cnt+1)), 0644)
}

// Take 读取并清零抑制计数（下次真实触发时调用）。无文件/读失败 = 0。读后清零而非
// 单独 Reset：回填与清零必须原子成对，否则一次失败会让同一批抑制被下次重复计入。
//
// Take reads and resets the suppression count (called at the next actual fire). Absent
// file / read failure = 0. Read-then-reset rather than a separate Reset: backfill and
// reset must be an atomic pair, or one failure double-counts the same batch next time.
func (c *FileSuppressedCounter) Take(sessionID, skill string) int {
	p := c.path(sessionID, skill)
	data, err := os.ReadFile(p)
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	_ = os.Remove(p)
	return n
}
