package cli

import (
	"encoding/json"
	"fmt"
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
// cache to avoid hitting the GitHub API on every command. Results are printed
// to stderr as a notification. Errors are silently ignored—this is a
// best-effort check.
//
// checkForUpdate 检查是否有更新版本可用。
// 用 24h 缓存避免每次命令都打 GitHub API。
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

	// Check the cache
	//
	// 检查缓存
	cache, err := loadUpdateCache()
	if err == nil && !cache.isExpired() {
		// Cache hit—notify only when a newer version exists
		//
		// 缓存命中——仅在存在更新版本时通知
		if cache.LatestVersion != "" && compareVersions(cache.LatestVersion, current) > 0 {
			fmt.Fprintf(os.Stderr, "\n💡 Forge %s 可用（当前 %s）。运行 `forge update` 更新。\n\n", cache.LatestVersion, current)
		}
		return
	}

	// Cache expired or missing—query the GitHub API
	//
	// 缓存过期或缺失——查询 GitHub API
	release, err := getLatestRelease()
	if err != nil {
		// Network failure—skip silently, do not update the cache
		//
		// 网络失败——静默跳过，不更新缓存
		return
	}

	latest := strings.TrimPrefix(release.TagName, "v")

	// Save to cache regardless of whether an update is available
	//
	// 无论是否更新都保存到缓存
	_ = saveUpdateCache(latest)

	// Notify when an update is available
	//
	// 更新则通知
	if compareVersions(latest, current) > 0 {
		fmt.Fprintf(os.Stderr, "\n💡 Forge %s 可用（当前 %s）。运行 `forge update` 更新。\n\n", latest, current)
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

func saveUpdateCache(version string) error {
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
	}

	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}

	path := filepath.Join(forgeDir, updateCacheFile)
	return os.WriteFile(path, data, 0644)
}
