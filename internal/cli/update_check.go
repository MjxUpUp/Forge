package cli

import (
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

// checkForUpdate reports whether a newer version is available. It uses a 24h
// cache to avoid hitting the version source on every command. Results are
// printed to stderr as a notification. Errors are silently ignored—this is a
// best-effort check.
//
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

	// The version source and the update command in the notice both follow the
	// install channel (npm registry for npm installs, GitHub API otherwise).
	//
	// 版本源与通知里的更新命令都随安装通道走（npm 安装查 npm registry，
	// 其余查 GitHub API）。
	channel := detectInstallChannelFn()

	// Check the cache
	//
	// 检查缓存
	cache, err := loadUpdateCache()
	if err == nil && !cache.isExpired() && cache.matchesChannel(channel.kind) {
		// Cache hit—notify only when a newer version exists
		//
		// 缓存命中——仅在存在更新版本时通知
		if cache.LatestVersion != "" && compareVersions(cache.LatestVersion, current) > 0 {
			printUpdateNotice(os.Stderr, cache.LatestVersion, current, channel)
		}
		return
	}

	// Cache expired, missing, or written by the other channel (the GitHub tag
	// lands before the npm publish in the two-stage release; a cross-channel
	// hit would notify about a version npm cannot serve yet)—query the
	// channel-appropriate version source.
	//
	// 缓存过期、缺失、或由另一通道写入（两段式发版里 GitHub tag 先于
	// npm publish 落地；跨通道命中会通知一个 npm 还拿不到的版本）——
	// 按通道查询版本源。
	var latest string
	if channel.kind == channelNPM {
		v, err := getLatestVersionFromNPM()
		if err != nil {
			// Network failure—skip silently, do not update the cache
			//
			// 网络失败——静默跳过，不更新缓存
			return
		}
		latest = v
	} else {
		release, err := getLatestRelease()
		if err != nil {
			// Network failure—skip silently, do not update the cache
			//
			// 网络失败——静默跳过，不更新缓存
			return
		}
		latest = strings.TrimPrefix(release.TagName, "v")
	}

	// Save to cache regardless of whether an update is available
	//
	// 无论是否更新都保存到缓存
	_ = saveUpdateCache(latest, channel.kind)

	// Notify when an update is available
	//
	// 更新则通知
	if compareVersions(latest, current) > 0 {
		printUpdateNotice(os.Stderr, latest, current, channel)
	}
}

// shouldSkipUpdateCheck returns true when the update check should be skipped.
//
// shouldSkipUpdateCheck 在应跳过更新检查时返回 true。
func shouldSkipUpdateCheck(fullVersion string, cmd *cobra.Command) bool {
	// Skip when FORGE_SKIP_UPDATE_CHECK is set
	//
	// 设置了 FORGE_SKIP_UPDATE_CHECK 时跳过
	if os.Getenv("FORGE_SKIP_UPDATE_CHECK") != "" {
		return true
	}

	// Skip dev builds
	//
	// dev 构建跳过
	if fullVersion == "dev" {
		return true
	}

	// Skip hook mode (hooks run on every file edit, must be fast)
	//
	// hook 模式跳过（hook 每次文件编辑都跑，必须快）
	if cmd.Name() == "hook" {
		return true
	}

	// Skip gate in silent mode
	//
	// silent 模式下的 gate 跳过
	if cmd.Name() == "gate" {
		if flag, err := cmd.Flags().GetBool("silent"); err == nil && flag {
			return true
		}
	}

	// Skip the update command itself (avoid recursion)
	//
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
	return time.Since(checked) > updateCheckInterval
}

// matchesChannel reports whether a cache entry is usable by the given
// channel. Legacy entries (empty Channel) predate the field and stay usable.
//
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
