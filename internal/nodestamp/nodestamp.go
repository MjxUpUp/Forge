// Package nodestamp stamps every event line with its machine-attribution fields
// (docs/design/node-identity.md §4): node_id (pubkey fingerprint), seq (per-node
// monotonic counter), ts_hlc (HLC tie-break), sig (reserved, v1 always empty).
//
// Fail-open is the prime directive: stamping is observability, and the event it rides
// on is more important than the stamp. ANY failure (identity load, counter I/O, lock
// contention) yields a zero Stamp — the event is appended unstamped, exactly as
// pre-stamping versions behaved. The one failure mode we must NOT allow is silently
// reusing seqs: a corrupt counter therefore disables stamping rather than restarting
// at 1 (reuse would poison (node_id, seq) dedup across machines).
//
// Seq protocol: pre-increment + persist-before-use. A crash after persist leaves a
// gap (harmless); a crash before persist can never produce reuse. Gaps are fine —
// (node_id, seq) is a dedup key, not a log sequence.
//
// Package nodestamp 给每条事件行打机器归因字段（docs/design/node-identity.md §4）：
// node_id（公钥指纹）、seq（节点本地单调计数器）、ts_hlc（HLC 决胜戳）、sig
// （预留，v1 恒空）。
//
// fail-open 是第一原则：打戳是可观测性，戳依附的事件比戳重要。任何失败（身份
// 加载、计数器 IO、锁竞争）都产出零值 Stamp——事件按打戳前版本的原样追加。
// 唯一绝不允许的失败模式是静默复用 seq：损坏的计数器因此禁用打戳而非从 1 重启
// （复用会毒害跨机 (node_id, seq) 去重）。
package nodestamp

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/hlc"
	"github.com/MjxUpUp/Forge/internal/nodeid"
	"github.com/MjxUpUp/Forge/internal/util"
)

// Stamp is the machine-attribution block embedded (flattened) into every event
// struct. All fields omitempty: a zero Stamp adds ZERO bytes to the JSON line, so
// legacy-format events and old readers are untouched.
//
// Stamp 是内嵌（拍平）进每个事件 struct 的机器归因块。全字段 omitempty：零值
// Stamp 不给 JSON 行增一个字节，存量格式事件与老读者零感知。
type Stamp struct {
	NodeID string `json:"node_id,omitempty"` // fnode_<32hex>
	Seq    int64  `json:"seq,omitempty"`     // 节点本地单调计数器（1 起；洞允许，复用禁止）
	TsHLC  string `json:"ts_hlc,omitempty"`  // HLC 决胜戳（hlc.Timestamp.String）
	Sig    string `json:"sig,omitempty"`     // 预留：对事件字节的 ed25519 签名，v1 恒空
}

var (
	mu              sync.Mutex
	clock           *hlc.Clock
	identity        *nodeid.Identity
	idTried         bool
	identityFresh   bool // identity was CREATED in this process (true first run)
	cachedHome      string
	degradeNoted    bool
	seqRestartNoted bool
)

// warnDegrade surfaces a stamping-disable ONCE per process: fail-open without a
// trace is indistinguishable from "no attribution because nothing was stamped" —
// a machine with a broken node.json / node-seq silently loses machine attribution
// on every event (node show prints identity only, sync status never looks here),
// which is the one outcome this package must make discoverable.
//
// warnDegrade 每进程一次地暴露打戳禁用：无痕迹的 fail-open 与「没有打戳」无法
// 区分——node.json / node-seq 损坏的机器会在每条事件上静默丢失机器归因
// （node show 只打印身份，sync status 从不看这里），这是本包必须让人能发现的
// 唯一后果。
func warnDegrade(reason string) {
	if degradeNoted {
		return
	}
	degradeNoted = true
	fmt.Fprintf(os.Stderr, `⚠ forge: 事件打戳已禁用（fail-open——事件照常记录，但无机器归因）：%s`+"\n", reason)
}

// warnSeqRestart announces a counter restart-from-1 ONCE per process (see the
// guard in Next). Announced, not blocking: stamping must never block the event.
//
// warnSeqRestart 每进程一次告示计数器从 1 重启（见 Next 内守卫）。告示而不阻断：
// 打戳绝不阻塞事件。
func warnSeqRestart() {
	if seqRestartNoted {
		return
	}
	seqRestartNoted = true
	fmt.Fprintf(os.Stderr, `⚠ forge: node-seq 缺失而节点身份已存在——计数器将从 1 重新起算；若本机此前打过戳且 (node_id, seq) 已有跨机消费，存在复用风险（A 类 G-Set 启用前本告示将升级为硬禁用）`+"\n")
}

// resetForTest drops the process singletons (tests simulate process restart).
//
// resetForTest 丢弃进程单例（测试模拟进程重启）。
func resetForTest() {
	mu.Lock()
	defer mu.Unlock()
	clock = nil
	identity = nil
	idTried = false
	identityFresh = false
	cachedHome = ``
	degradeNoted = false
	seqRestartNoted = false
}

// Next returns the next stamp, or a zero Stamp on any failure (fail-open).
//
// Next 返回下一个戳；任何失败返回零值 Stamp（fail-open）。
func Next() Stamp {
	mu.Lock()
	defer mu.Unlock()
	// The singletons are resolved against ONE home. FORGE_DATA_HOME can switch
	// mid-process (tests do this constantly; production does not): keeping a cached
	// identity from the OLD home while the counter reads/writes the NEW home would
	// mix homes — an identity stamped with another home's seq counter. Drop the
	// cached identity on a home switch and re-resolve (freshness is re-derived for
	// the new home, so a fresh temp home is a fresh machine, not a "deleted counter").
	//
	// 单例针对同一个 home 解析。FORGE_DATA_HOME 可能进程中途切换（测试常态、生产
	// 不会）：若保留旧 home 的缓存身份而计数器读写新 home，就是跨 home 混用——
	// 身份配上了别人 home 的 seq 计数器。home 切换即丢弃缓存身份并重解析（新 home
	// 重新推导 fresh——全新的临时 home 是新机器，不是「计数器被删」）。
	if home, herr := forgedata.GlobalHome(); herr == nil && home != cachedHome {
		identity, idTried, identityFresh, cachedHome = nil, false, false, home
	}
	id, err := loadIdentity()
	if err != nil {
		warnDegrade(err.Error())
		return Stamp{}
	}
	// Identity exists but the counter is GONE. Since identities are born WITH a
	// seeded counter (nodeid.seedNodeSeqCounter), this is either a pre-seed machine
	// that never stamped (safe to start at 1) or a genuine counter loss (rm / partial
	// restore) on a machine that DID stamp — restarting at 1 reuses (node_id, seq)
	// pairs. We CANNOT tell the two apart here, so the prime directive decides:
	// stamping never blocks events. Announce the restart ONCE (the notice is the
	// observable surface; a hard disable is deferred until a (node_id, seq) consumer
	// actually exists — see the node-identity §4 实现校正) and continue from 1.
	//
	// 身份已存在而计数器没了。身份诞生即带播种的计数器
	//（nodeid.seedNodeSeqCounter），所以这要么是从未打过戳的 pre-seed 机器（从 1
	// 起安全），要么是打过戳机器的计数器真丢了（rm / 部分恢复）——从 1 重启会复用
	// (node_id, seq)。两者在此无法区分，按第一原则裁决：打戳绝不阻塞事件。每进程
	// 一次告示重启（告示即可观测面；硬禁用推迟到 (node_id, seq) 消费方真实出现时
	//——见 node-identity §4 实现校正），从 1 继续。
	if !identityFresh {
		if p, perr := seqPath(); perr == nil {
			if _, serr := os.Stat(p); os.IsNotExist(serr) {
				warnSeqRestart()
			}
		}
	}
	seq, err := bumpSeq()
	if err != nil {
		warnDegrade(err.Error())
		return Stamp{}
	}
	if clock == nil {
		clock = hlc.NewClock(nil)
	}
	return Stamp{
		NodeID: id.NodeID,
		Seq:    seq,
		TsHLC:  clock.Now().String(),
	}
}

// loadIdentity caches the node identity per process; a failure is remembered so a
// broken node.json does not cost a disk read per event on the hook hot path.
//
// loadIdentity 按进程缓存节点身份；失败被记住——损坏的 node.json 不会在 hook
// 热路径上每条事件付出一次磁盘读。
func loadIdentity() (*nodeid.Identity, error) {
	if identity != nil {
		return identity, nil
	}
	if idTried {
		return nil, fmt.Errorf(`node identity previously failed`)
	}
	idTried = true
	// Distinguish "identity already on disk" (this node stamped before — a missing
	// node-seq later means REUSE risk) from "created just now" (fresh machine — a
	// missing node-seq is simply the counter not existing yet).
	//
	// 区分「身份已在磁盘上」（本节点以前打过戳——之后 node-seq 缺失意味着复用
	// 风险）与「刚刚创建」（新机器——node-seq 缺失只是计数器尚未存在）。
	if id, err := nodeid.Load(); err == nil {
		identity = id
		identityFresh = false
		return id, nil
	}
	id, err := nodeid.LoadOrCreate()
	if err != nil {
		return nil, err
	}
	identity = id
	identityFresh = true
	return id, nil
}

// seqPath returns ~/.forge/node-seq (FORGE_DATA_HOME aware).
//
// seqPath 返回 ~/.forge/node-seq（FORGE_DATA_HOME 感知）。
func seqPath() (string, error) {
	home, err := forgedata.GlobalHome()
	if err != nil {
		return ``, err
	}
	return filepath.Join(home, `node-seq`), nil
}

// seqLockMaxWait bounds cross-process contention on the counter lock. Legitimate
// holds are sub-millisecond (read+increment+write); the full wait is only paid when
// a holder died between create and remove.
//
// seqLockMaxWait 限定计数器锁的跨进程竞争等待。合法持锁亚毫秒（读+增+写）；
// 等满只在持锁者死于 create 与 remove 之间时发生。
const seqLockMaxWait = 2 * time.Second

// seqLockStaleAfter bounds how long an uncontended counter lock may exist before a
// crashed holder is assumed and the lock is broken (same rationale as LockTask).
//
// seqLockStaleAfter 限定无竞争的计数器锁存在多久后视为持锁者崩溃并被打破（依据同
// LockTask）。
const seqLockStaleAfter = 30 * time.Second

// bumpSeq reads, increments and persists the node counter, returning the NEW value
// (seq starts at 1). Persist-before-use: the returned value is already durable, so a
// crash can only leave a gap, never a reuse. A missing file starts at 1 (fresh node);
// a CORRUPT file is an error — stamping fails open rather than risking seq reuse.
//
// bumpSeq 读、增、持久化节点计数器，返回新值（seq 从 1 起）。先持久化后使用：
// 返回值落盘后才被消费，崩溃只留洞、永不复用。文件缺失从 1 起（新节点）；文件
// 损坏是错误——打戳 fail-open，绝不冒 seq 复用风险。
func bumpSeq() (int64, error) {
	p, err := seqPath()
	if err != nil {
		return 0, err
	}
	unlock, err := lockSeq(p)
	if err != nil {
		return 0, err
	}
	defer unlock()

	var cur int64
	raw, err := os.ReadFile(p)
	switch {
	case err == nil:
		cur, err = strconv.ParseInt(strings.TrimSpace(string(raw)), 10, 64)
		if err != nil || cur < 0 {
			return 0, fmt.Errorf(`corrupt node-seq %q`, strings.TrimSpace(string(raw)))
		}
	case os.IsNotExist(err):
		cur = 0
	default:
		return 0, err
	}
	next := cur + 1
	if err := util.AtomicWrite(p, []byte(strconv.FormatInt(next, 10)+"\n"), 0600); err != nil {
		return 0, err
	}
	return next, nil
}

// lockSeq takes the cross-process counter lock (O_EXCL file, token-content checked
// unlock, stale break — same portable pattern as taskpipeline.LockTask; flock is not
// used because its syscall shape differs on Windows).
//
// lockSeq 获取跨进程计数器锁（O_EXCL 文件、令牌内容校验解锁、stale 打破——与
// taskpipeline.LockTask 同款可移植模式；不用 flock 因其 syscall 形态在 Windows
// 上不同）。
func lockSeq(p string) (func(), error) {
	lock := p + `.lock`
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(seqLockMaxWait)
	for {
		f, err := os.OpenFile(lock, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if err == nil {
			token := fmt.Sprintf("%d\n", time.Now().UnixNano())
			_, _ = f.WriteString(token)
			_ = f.Close()
			return func() {
				if data, err := os.ReadFile(lock); err == nil && string(data) == token {
					_ = os.Remove(lock)
				}
			}, nil
		}
		if !os.IsExist(err) {
			// Windows transient contention (holder between OpenFile and Close, AV
			// indexer scanning) returns access-denied / sharing-violation instead of
			// EEXIST — retry within the deadline, same shape as taskpipeline.LockTask's
			// isLockContentionErr (duplicated here: nodestamp sits BELOW taskpipeline in
			// the import graph and cannot import it).
			//
			// Windows 瞬态竞争（持锁者尚在 OpenFile 与 Close 之间、AV 索引器扫描）返回
			// access-denied / sharing-violation 而非 EEXIST——在 deadline 内重试，与
			// taskpipeline.LockTask 的 isLockContentionErr 同形（此处复制：
			// nodestamp 在导入图上位于 taskpipeline 之下，不能反向导入）。
			if !isLockContentionErr(err) {
				return nil, err
			}
		} else {
			// Break an abandoned lock (holder crashed between create and remove).
			// Double-check staleness immediately before removing: a plain stat-then-remove
			// lets a THIRD party's fresh lock be deleted in a rare interleave (breaker
			// stats stale lock → another breaker replaces it → first removes the fresh
			// one → two live holders → the seq reuse this design forbids). Re-stat and
			// require the SAME mtime still stale, narrowing the window to nothing a
			// 30s-stale threshold could plausibly produce.
			//
			// 打破被遗弃的锁（持锁者死于 create 与 remove 之间）。删除前立即二次确认
			// staleness：朴素的 stat-then-remove 在罕见交错下会删掉第三方的新锁
			// （打破者 stat 到旧锁 → 另一打破者已替换 → 前者删掉新锁 → 两个持锁者并存
			// → 本设计明令禁止的 seq 复用）。重 stat 并要求同一 mtime 仍 stale，把
			// 窗口收窄到 30s stale 阈值不可能产生的量级。
			if fi, statErr := os.Stat(lock); statErr == nil && time.Since(fi.ModTime()) > seqLockStaleAfter {
				mtime := fi.ModTime()
				time.Sleep(50 * time.Millisecond)
				if fi2, statErr2 := os.Stat(lock); statErr2 == nil && fi2.ModTime().Equal(mtime) && time.Since(fi2.ModTime()) > seqLockStaleAfter {
					_ = os.Remove(lock)
					continue
				}
			}
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf(`node-seq lock contention timeout`)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// isLockContentionErr reports whether an O_EXCL lock-open failure is Windows transient
// contention rather than a hard error (mirror of taskpipeline's helper; see lockSeq).
//
// isLockContentionErr 判断 O_EXCL 开锁失败是否为 Windows 瞬态竞争而非硬错误
// （taskpipeline 同款 helper 的镜像；见 lockSeq）。
func isLockContentionErr(err error) bool {
	if runtime.GOOS != "windows" {
		return false
	}
	return os.IsPermission(err) || // ERROR_ACCESS_DENIED
		errors.Is(err, syscall.Errno(32)) || // ERROR_SHARING_VIOLATION
		errors.Is(err, syscall.Errno(33)) // ERROR_LOCK_VIOLATION
}

// StringJSON is a test/debug helper: the JSON a Stamp contributes when flattened
// into an event struct.
//
// StringJSON 是测试/调试辅助：Stamp 拍平进事件 struct 后贡献的 JSON。
func (s Stamp) StringJSON() string {
	raw, _ := json.Marshal(s)
	return string(raw)
}
