// lock.go — 注册表变更方的互斥锁（Project Policy Layer 收尾）。
//
// writeEntries 的 read-modify-write 非并发安全（registry.go 注释自认：本地工具
// 并发概率低、丢失可重跑 init 补）。P2 起 projects.json 成为 12 宿主并发会话 +
// forge on/off 的热写目标，declined 条目在写竞态下丢失 = managed 静默复活——
// 违反"退出不可被重置"红线，故补文件锁。锁实现刻意简单：O_EXCL 创建 + 过期
// 破锁（持有者崩溃不永久死锁）+ 有限重试；进程内调用方串行由锁自身保证。
package registry

import (
	"os"
	"path/filepath"
	"time"
)

// lockStaleAfter：锁文件超过该时长视为持有者已死，破锁重试。
const lockStaleAfter = 10 * time.Second

// withLock 以 projects.json.lock 互斥执行 fn；获取失败按 25ms 间隔重试至多 ~2s，
// 仍失败则破锁（过期）或放弃并执行（锁是防丢写优化，不是正确性前提之外的
// 硬门——竞态窗口退化到锁引入前的行为，而非让 forge 命令整体失败）。
func withLock(fn func() error) error {
	p, err := globalPath()
	if err != nil {
		return fn()
	}
	lockPath := p + `.lock`
	deadline := time.Now().Add(2 * time.Second)
	for {
		if err := os.MkdirAll(filepath.Dir(lockPath), 0755); err == nil {
			if f, cerr := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644); cerr == nil {
				_, _ = f.WriteString(time.Now().Format(time.RFC3339))
				_ = f.Close()
				defer func() { _ = os.Remove(lockPath) }()
				return fn()
			}
		}
		if time.Now().After(deadline) {
			// 过期破锁：持有者大概率已崩溃（正常持锁毫秒级）。
			if info, serr := os.Stat(lockPath); serr == nil && time.Since(info.ModTime()) > lockStaleAfter {
				_ = os.Remove(lockPath)
				deadline = time.Now().Add(2 * time.Second) // 破锁后再给一轮
				continue
			}
			return fn() // 放弃等待：退化到无锁行为（与锁引入前一致），不阻断命令
		}
		time.Sleep(25 * time.Millisecond)
	}
}
