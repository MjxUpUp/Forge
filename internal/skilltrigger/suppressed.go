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

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/MjxUpUp/Forge/internal/util"
)

// FileSuppressedCounter is the file impl of the suppression counter: BaseDir/<session>/<skill>.suppressed.
//
// FileSuppressedCounter 是抑制计数的文件实现：BaseDir/<session>/<skill>.suppressed。
// 与 FileBasedNoiseController 同 BaseDir 即同寿命（marker 目录），互不干涉。
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

// Incr suppression count +1.
//
// Incr 抑制计数 +1。失败静默（best-effort：计数是观测辅助，绝不能阻塞 hook 链）。
// 已知竞态（review m3，文档化接受）：跨进程 read-modify-write——同 session 并行 hook
// 并发 Incr 会丢更新（两读 0 两写 1）。观测计数的轻量损失，不值得引入文件锁复杂度。
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

// Take reads and resets the suppression count (called at the next actual fire).
//
// Take 读取并清零抑制计数（下次真实触发时调用）。无文件/读失败 = 0。先用原子 rename
// 把文件挪走再读（review m3：Read→Remove 窗口内落进的 Incr 会随 Remove 整条丢失；
// rename 语义把窗口收窄到「本 Take 已看到的快照」，窗口后新落的 Incr 落在新文件、
// 由下次 Take 回收）。回填与清零成对发生，失败不重复计入。
func (c *FileSuppressedCounter) Take(sessionID, skill string) int {
	p := c.path(sessionID, skill)
	taken := p + ".taken"
	// rename 失败 = 文件不存在（或瞬时故障）→ 无计数可取。
	if err := os.Rename(p, taken); err != nil {
		return 0
	}
	data, err := os.ReadFile(taken)
	_ = os.Remove(taken)
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	return n
}
