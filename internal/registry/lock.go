// lock.go — 注册表变更方的互斥锁（Project Policy Layer 收尾）。
//
// writeEntries 的 read-modify-write 非并发安全（registry.go 注释自认：本地工具
// 并发概率低、丢失可重跑 init 补）。P2 起 projects.json 成为 12 宿主并发会话 +
// forge on/off 的热写目标，declined 条目在写竞态下丢失 = managed 静默复活——
// 违反"退出不可被重置"红线，故补文件锁。锁实现刻意简单：O_EXCL 创建 + 过期
// 破锁（持有者崩溃不永久死锁）+ 有限重试；进程内调用方串行由锁自身保证。
package registry

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// lockStaleAfter：锁文件超过该时长视为持有者已死，破锁重试。
const lockStaleAfter = 10 * time.Second

// inProcMu 同进程写者先互斥：并发竞争的大头是同进程 goroutine（测试的 24 并发
// Add、12 宿主多会话共享一个 forge 进程族），进程内互斥后文件锁只剩跨进程职责
// ——竞争超时的无锁退化窗口对同进程并发彻底关闭。2026-09-08 实证：windows -race
// CI 下 24 并发 Add + SetStatus 竞争越过 2s 文件锁竞争超时，走无锁退化丢 1 条
// （read-modify-write 互相覆盖），且是时序型 flake——本机两基线各 8 连跑全绿，
// 仅 CI 慢时序现形。
var inProcMu sync.Mutex

// withLock 以 projects.json.lock 互斥执行 fn；同进程调用先经 inProcMu 串行，
// 跨进程竞争按 25ms 间隔重试至多 ~10s，仍失败则破锁（过期）或告警后退化并执行
// （锁是防丢写优化；退化仅剩跨进程同机并发一种残余窗口，必须可观测——静默丢
// 条目就是 2026-09-08 事故的形态）。
func withLock(fn func() error) error {
	inProcMu.Lock()
	defer inProcMu.Unlock()

	p, err := globalPath()
	if err != nil {
		return fn()
	}
	lockPath := p + `.lock`
	deadline := time.Now().Add(10 * time.Second)
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
				deadline = time.Now().Add(10 * time.Second) // 破锁后再给一轮
				continue
			}
			fmt.Fprintf(os.Stderr, "warn: 注册表锁竞争超时，本次变更无锁执行（并发写可能丢失条目）: %s\n", lockPath)
			return fn()
		}
		time.Sleep(25 * time.Millisecond)
	}
}
