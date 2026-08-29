package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const updateCacheDir = ".forge"
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
// 结果作为通知打印到 stderr。
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
		// 缓存命中——仅在存在更新版本时通知。负条目（LatestVersion 为空，
		// 网络失败后写入）也在此命中：updateCheckNegativeTTL 内视为「已检查
		// 过」，不再发网络请求、不打印——与失败检查本就产生的静默一致。
		if cache.LatestVersion != "" && compareVersions(cache.LatestVersion, current) > 0 {
			printUpdateNotice(os.Stderr, cache.LatestVersion, current, channel)
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
		printUpdateNotice(os.Stderr, latest, current, channel)
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

	// hook 模式跳过（hook 每次文件编辑都跑，必须快）
	if cmd.Name() == "hook" {
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

func loadUpdateCache() (*updateCache, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(home, updateCacheDir, updateCacheFile)

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
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	forgeDir := filepath.Join(home, updateCacheDir)
	if err := os.MkdirAll(forgeDir, 0755); err != nil {
		return err
	}

	cache := updateCache{
		LatestVersion: version,
		CheckedAt:     time.Now().UTC().Format(time.RFC3339),
		Channel:       string(kind),
	}

	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}

	path := filepath.Join(forgeDir, updateCacheFile)
	return os.WriteFile(path, data, 0644)
}
