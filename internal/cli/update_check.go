package cli

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/util"
	"github.com/spf13/cobra"
)

const updateCacheFile = "update-cache.json"
const updateCheckInterval = 24 * time.Hour

// updateCheckNegativeTTL 是负缓存条目（LatestVersion 为空，网络失败时写入）的短
// 生存期。失败的检查原先不落缓存，后续每条命令都要重付一次完整网络停顿——离线
// （或被防火墙挡住）的机器上等于每次 forge 调用挂 15 秒。30 分钟长到能覆盖一整
// 段工作会话的命令批，又短到瞬断不可能长期掩盖真实发版。
const updateCheckNegativeTTL = 30 * time.Minute

// updateCheckQueryDeadline 约束**启动路径**的版本查询（2.5 秒）。底层 15 秒
// client 超时不动（见 update_channel.go / update.go），作为最后兜底；这层短
// deadline 只包每条交互命令前跑的 best-effort 检查——后台更新通知绝不能让 CLI
// 被慢网络扣住。`forge update`（显式、用户主动）保持完整 15 秒。
//
// 用 var 而非 const 只为测试能缩短它；生产值 2.5 秒。
var updateCheckQueryDeadline = 2500 * time.Millisecond

// checkForUpdate 检查是否有更新版本可用。
// 用 24h 缓存避免每次命令都打版本源。
// 结果作为通知打印到 stderr；同一版本 updateNoticeInterval 内最多提示一次
// （见 maybePrintUpdateNotice）。
// 错误静默忽略——这是 best-effort 检查。
func checkForUpdate(fullVersion string, cmd *cobra.Command) {
	if shouldSkipUpdateCheck(fullVersion, cmd) {
		return
	}

	current := getCurrentVersion(fullVersion)
	if current == "dev" {
		return
	}

	// 版本源与通知里的更新命令都随安装通道走（npm 安装查 npm registry，
	// 其余查 GitHub API）。
	channel := detectInstallChannelFn()

	// 检查缓存
	cache, err := loadUpdateCache()
	if err == nil && !cache.isExpired() && cache.matchesChannel(channel.kind) {
		// 缓存命中——仅在存在更新版本时通知（同一版本受 24h 节流）。负条目（LatestVersion 为空，
		// 网络失败后写入）也在此命中：updateCheckNegativeTTL 内视为「已检查
		// 过」，不再发网络请求、不打印——与失败检查本就产生的静默一致。
		if cache.LatestVersion != "" && compareVersions(cache.LatestVersion, current) > 0 {
			maybePrintUpdateNotice(os.Stderr, cache.LatestVersion, current, channel, time.Now())
		}
		return
	}

	// 缓存过期、缺失、或由另一通道写入（两段式发版里 GitHub tag 先于
	// npm publish 落地；跨通道命中会通知一个 npm 还拿不到的版本）——
	// 在启动路径短 deadline 下按通道查询版本源。
	latest, err := queryLatestWithDeadline(channel)
	if err != nil {
		// 查询失败（网络错误，或超出 updateCheckQueryDeadline——对启动路径
		// 而言「慢而未死」的网络与死网络同样糟糕）。写一条短 TTL 负条目，
		// 让 updateCheckNegativeTTL 内的后续命令完全跳过网络而不是重付停顿。
		// 输出侧维持原有静默：空 LatestVersion 永不通知。
		_ = saveUpdateCache("", channel.kind)
		return
	}

	// 无论是否更新都保存到缓存
	_ = saveUpdateCache(latest, channel.kind)

	// 更新则通知
	if compareVersions(latest, current) > 0 {
		maybePrintUpdateNotice(os.Stderr, latest, current, channel, time.Now())
	}
}

// queryLatestWithDeadline 在启动路径短 deadline 下执行按通道的版本查询。查询
// 函数本身不收 context（与 `forge update` 共用，后者保持完整 15 秒 client 超时
// ），故 deadline 在**外层**强制：查询跑在 goroutine 上，deadline 先到则本侧返
// 回 ctx.Err()。goroutine 的发送带缓冲，迟到的结果不会阻塞或泄漏它——落进
// channel 后 goroutine 即退出；最坏情况是进程多背一个后台 HTTP 请求，直到其
// 15 秒 client 超时或进程退出。
func queryLatestWithDeadline(channel installChannel) (string, error) {
	type queryResult struct {
		latest string
		err    error
	}
	done := make(chan queryResult, 1)
	go func() {
		if channel.kind == channelNPM {
			v, err := getLatestVersionFromNPM()
			done <- queryResult{v, err}
			return
		}
		release, err := getLatestRelease()
		if err != nil {
			done <- queryResult{"", err}
			return
		}
		done <- queryResult{strings.TrimPrefix(release.TagName, "v"), nil}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), updateCheckQueryDeadline)
	defer cancel()
	select {
	case r := <-done:
		return r.latest, r.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// shouldSkipUpdateCheck 在应跳过更新检查时返回 true。
func shouldSkipUpdateCheck(fullVersion string, cmd *cobra.Command) bool {
	// 设置了 FORGE_SKIP_UPDATE_CHECK 时跳过
	if os.Getenv("FORGE_SKIP_UPDATE_CHECK") != "" {
		return true
	}

	// dev 构建跳过
	if fullVersion == "dev" {
		return true
	}

	// hook 子树跳过（hook 每次文件编辑都跑，必须快；含 hook batch 等嵌套子命令）
	if isHookSubtree(cmd) {
		return true
	}

	// silent 模式下的 gate 跳过
	if cmd.Name() == "gate" {
		if flag, err := cmd.Flags().GetBool("silent"); err == nil && flag {
			return true
		}
	}

	// update 命令自身跳过（避免递归）
	if cmd.Name() == "update" {
		return true
	}

	return false
}

// isHookSubtree 判断 cmd 是否位于 hook 子树（自身或任一祖先名为 hook，含
// `forge hook batch` 等嵌套子命令）。升级检查与 npm 垫片自愈提示的「hook 静默」判断
// 共用此函数——只判 cmd.Name() 会漏掉嵌套子命令（batch 的 Name() 是 batch），
// 让提示的 stderr 混进 hook 输出、进入 agent 上下文。
//
// isHookSubtree reports whether cmd is in the hook subtree (itself or any ancestor is
// named hook, covering nested subcommands such as `forge hook batch`). The update
// check and the npm-shim-heal notice share this one predicate for "silent under
// hook"; testing only cmd.Name() misses nested subcommands (batch's Name() is
// "batch") and lets the notice's stderr leak into hook output and agent context.
func isHookSubtree(cmd *cobra.Command) bool {
	for c := cmd; c != nil; c = c.Parent() {
		if c.Name() == "hook" {
			return true
		}
	}
	return false
}

// updateNoticeThrottleEnv 设为 0 时关闭升级提示节流（每条命令都提示，回到旧行为）。
// updateNoticeThrottleEnv set to 0 disables the notice throttle (notify on every
// command, the old behavior).
const updateNoticeThrottleEnv = "FORGE_UPDATE_NOTICE_THROTTLE"

// updateNoticeInterval 同一版本两次提示的最短间隔。
// updateNoticeInterval is the minimum gap between two notices for the same version.
const updateNoticeInterval = 24 * time.Hour

// noticeThrottled 判断 latest 是否已在 updateNoticeInterval 内通知过。
// noticeThrottled reports whether latest was already notified within
// updateNoticeInterval.
func (c *updateCache) noticeThrottled(latest string, now time.Time) bool {
	if c == nil || c.NotifiedVersion != latest || c.NotifiedAt == "" {
		return false
	}
	at, err := time.Parse(time.RFC3339, c.NotifiedAt)
	if err != nil {
		return false
	}
	// 时钟回拨（at 在未来）时不节流，避免长期静音。
	// A clock moved backwards (at in the future) must not throttle, or the notice
	// would stay silent for a long time.
	return !now.Before(at) && now.Sub(at) < updateNoticeInterval
}

// maybePrintUpdateNotice 打印升级提示，同一版本 updateNoticeInterval 内最多一次
// （已通知的版本与时间记在 update 缓存里）。缓存读写失败 fail-open：照常提示，
// 只是不节流——提示是 best-effort，不得因记账失败而吞掉。读改写无锁：并发的
// 两个进程可能各打一次，节流是尽力而为。
//
// maybePrintUpdateNotice prints the update notice at most once per version per
// updateNoticeInterval (notified version and time live in the update cache). Cache
// I/O failure fails open: the notice still prints, just unthrottled. The
// read-modify-write is unlocked: two concurrent processes may each print once, so
// the throttle is best-effort.
func maybePrintUpdateNotice(w io.Writer, latest, current string, channel installChannel, now time.Time) {
	throttle := os.Getenv(updateNoticeThrottleEnv) != "0"
	cache, err := loadUpdateCache()
	if err != nil {
		cache = nil
	}
	if throttle && cache.noticeThrottled(latest, now) {
		return
	}
	printUpdateNotice(w, latest, current, channel)
	if throttle && cache != nil {
		cache.NotifiedVersion = latest
		cache.NotifiedAt = now.UTC().Format(time.RFC3339)
		_ = writeUpdateCache(cache)
	}
}

// updateCachePath 返回 update 缓存文件路径——随 forgedata.GlobalHome 单一真相源
// （FORGE_DATA_HOME 优先），2026-09 代码普查 R4：原先 UserHomeDir+".forge" 直拼
// 让设置了 FORGE_DATA_HOME 的用户读/写错目录。
func updateCachePath() (string, error) {
	forgeRoot, err := forgedata.GlobalHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(forgeRoot, updateCacheFile), nil
}

func loadUpdateCache() (*updateCache, error) {
	path, err := updateCachePath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cache updateCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, err
	}
	return &cache, nil
}

func (c *updateCache) isExpired() bool {
	if c.CheckedAt == "" {
		return true
	}
	checked, err := time.Parse(time.RFC3339, c.CheckedAt)
	if err != nil {
		return true
	}
	// 负条目（LatestVersion 为空——查询失败后由 checkForUpdate 写入）按短 TTL
	// 过期：它断言的是「已查过、源不可达」而非「已是最新」，不得让真实检查
	// 静音一整天。正条目维持 24h 间隔。
	ttl := updateCheckInterval
	if c.LatestVersion == "" {
		ttl = updateCheckNegativeTTL
	}
	return time.Since(checked) > ttl
}

// matchesChannel 判断缓存条目对给定通道是否可用。旧条目（Channel 为空）
// 早于该字段存在，保持可用。
func (c *updateCache) matchesChannel(kind channelKind) bool {
	return c.Channel == "" || c.Channel == string(kind)
}

func saveUpdateCache(version string, kind channelKind) error {
	cache := updateCache{
		LatestVersion: version,
		CheckedAt:     time.Now().UTC().Format(time.RFC3339),
		Channel:       string(kind),
	}
	// 沿用已通知的版本与时间：刷新缓存不得清空节流记录，否则同一版本每次重查后
	// 又提示一遍。读失败（无缓存/损坏）即无记录可沿用；换了安装通道则不沿用——
	// 另一通道的提示带的是另一条更新命令，不能让它静音本通道的首次提示。
	// Carry over the notified version/time: a refresh must not wipe the throttle
	// record. A read failure (no cache / corrupt) means nothing to carry; a changed
	// install channel carries nothing either — the other channel's notice named a
	// different update command and must not silence this channel's first notice.
	if prev, err := loadUpdateCache(); err == nil && prev.matchesChannel(kind) {
		cache.NotifiedVersion = prev.NotifiedVersion
		cache.NotifiedAt = prev.NotifiedAt
	}
	return writeUpdateCache(&cache)
}

// writeUpdateCache 把 cache 原子写入缓存文件。
// writeUpdateCache atomically writes cache to the cache file.
func writeUpdateCache(cache *updateCache) error {
	path, err := updateCachePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}

	// AtomicWrite：更新检查在每条命令的启动路径并发可达，写一半的缓存文件会让
	// 下一次 loadUpdateCache 解析失败白付一次网络查询（2026-09 代码普查 R4）。
	return util.AtomicWrite(path, data, 0644)
}
