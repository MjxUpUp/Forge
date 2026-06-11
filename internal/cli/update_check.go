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

// checkForUpdateInBackground checks if a newer version is available.
// It uses a 24h cache to avoid hitting GitHub API on every command.
// Results are printed to stderr as a notification.
// Errors are silently ignored — this is a best-effort check.
func checkForUpdateInBackground(fullVersion string, cmd *cobra.Command) {
	if shouldSkipUpdateCheck(fullVersion, cmd) {
		return
	}

	current := getCurrentVersion(fullVersion)
	if current == "dev" {
		return
	}

	// Check cache
	cache, err := loadUpdateCache()
	if err == nil && !cache.isExpired() {
		// Cache hit — just notify if newer version exists
		if cache.LatestVersion != "" && cache.LatestVersion != current {
			fmt.Fprintf(os.Stderr, "\n💡 Forge %s 可用（当前 %s）。运行 `forge update` 更新。\n\n", cache.LatestVersion, current)
		}
		return
	}

	// Cache expired or missing — query GitHub API
	release, err := getLatestRelease()
	if err != nil {
		// Network failure — silently skip, don't update cache
		return
	}

	latest := strings.TrimPrefix(release.TagName, "v")

	// Save to cache regardless of whether it's newer
	_ = saveUpdateCache(latest)

	// Notify if newer
	if latest != current {
		fmt.Fprintf(os.Stderr, "\n💡 Forge %s 可用（当前 %s）。运行 `forge update` 更新。\n\n", latest, current)
	}
}

// shouldSkipUpdateCheck returns true when the update check should be skipped.
func shouldSkipUpdateCheck(fullVersion string, cmd *cobra.Command) bool {
	// Skip if FORGE_SKIP_UPDATE_CHECK is set
	if os.Getenv("FORGE_SKIP_UPDATE_CHECK") != "" {
		return true
	}

	// Skip dev builds
	if fullVersion == "dev" {
		return true
	}

	// Skip in hook mode (hooks run on every file edit, must be fast)
	if cmd.Name() == "hook" {
		return true
	}

	// Skip gate in silent mode
	if cmd.Name() == "gate" {
		if flag, err := cmd.Flags().GetBool("silent"); err == nil && flag {
			return true
		}
	}

	// Skip update command itself (avoid recursion)
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
