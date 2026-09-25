package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

func TestGetCurrentVersion(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"0.11.1 (commit: abc123, built: 2026-01-01)", "0.11.1"},
		{"1.0.0 (commit: none, built: unknown)", "1.0.0"},
		{"dev", "dev"},
		{"0.9.0", "0.9.0"},
		{"2.0.0-beta.1 (commit: deadbeef, built: 2026-06-11)", "2.0.0-beta.1"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := getCurrentVersion(tt.input)
			if got != tt.expected {
				t.Errorf("getCurrentVersion(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestUpdateCacheRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	setTestHome(t, tmpDir)

	version := "1.2.3"
	if err := saveUpdateCache(version, channelGitHub); err != nil {
		t.Fatalf("saveUpdateCache failed: %v", err)
	}

	cache, err := loadUpdateCache()
	if err != nil {
		t.Fatalf("loadUpdateCache failed: %v", err)
	}

	if cache.LatestVersion != version {
		t.Errorf("LatestVersion = %q, want %q", cache.LatestVersion, version)
	}
	if cache.CheckedAt == "" {
		t.Error("CheckedAt is empty")
	}
	if _, err := time.Parse(time.RFC3339, cache.CheckedAt); err != nil {
		t.Errorf("CheckedAt is not valid RFC3339: %v", err)
	}
}

func TestCacheExpiry(t *testing.T) {
	tests := []struct {
		name    string
		cache   updateCache
		expired bool
	}{
		{
			name:    "empty checked_at",
			cache:   updateCache{LatestVersion: "1.0.0", CheckedAt: ""},
			expired: true,
		},
		{
			name:    "recent check",
			cache:   updateCache{LatestVersion: "1.0.0", CheckedAt: time.Now().UTC().Format(time.RFC3339)},
			expired: false,
		},
		{
			name:    "25 hours ago",
			cache:   updateCache{LatestVersion: "1.0.0", CheckedAt: time.Now().Add(-25 * time.Hour).UTC().Format(time.RFC3339)},
			expired: true,
		},
		{
			name:    "invalid format",
			cache:   updateCache{LatestVersion: "1.0.0", CheckedAt: "not-a-date"},
			expired: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.cache.isExpired()
			if got != tt.expired {
				t.Errorf("isExpired() = %v, want %v", got, tt.expired)
			}
		})
	}
}

func TestShouldSkipUpdateCheck(t *testing.T) {
	tests := []struct {
		name    string
		version string
		cmdUse  string
		env     string
		skip    bool
	}{
		{
			name:    "dev build",
			version: "dev",
			cmdUse:  "status",
			skip:    true,
		},
		{
			name:    "hook command",
			version: "0.11.1 (commit: abc)",
			cmdUse:  "hook",
			skip:    true,
		},
		{
			name:    "update command",
			version: "0.11.1 (commit: abc)",
			cmdUse:  "update",
			skip:    true,
		},
		{
			name:    "env override",
			version: "0.11.1 (commit: abc)",
			cmdUse:  "status",
			env:     "1",
			skip:    true,
		},
		{
			name:    "normal command",
			version: "0.11.1 (commit: abc)",
			cmdUse:  "status",
			skip:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &cobra.Command{Use: tt.cmdUse}

			if tt.env != "" {
				os.Setenv("FORGE_SKIP_UPDATE_CHECK", tt.env)
				defer os.Unsetenv("FORGE_SKIP_UPDATE_CHECK")
			} else {
				os.Unsetenv("FORGE_SKIP_UPDATE_CHECK")
			}

			got := shouldSkipUpdateCheck(tt.version, cmd)
			if got != tt.skip {
				t.Errorf("shouldSkipUpdateCheck() = %v, want %v", got, tt.skip)
			}
		})
	}
}

func TestFindPlatformAsset(t *testing.T) {
	assets := []githubAsset{
		{Name: "forge_0.11.1_linux_x86_64.tar.gz", BrowserDownloadURL: "http://example.com/1", Size: 1000},
		{Name: "forge_0.11.1_darwin_x86_64.tar.gz", BrowserDownloadURL: "http://example.com/2", Size: 1000},
		{Name: "forge_0.11.1_darwin_aarch64.tar.gz", BrowserDownloadURL: "http://example.com/3", Size: 1000},
		{Name: "forge_0.11.1_windows_x86_64.tar.gz", BrowserDownloadURL: "http://example.com/4", Size: 1000},
		{Name: "checksums.txt", BrowserDownloadURL: "http://example.com/5", Size: 100},
	}

	result := findPlatformAsset(assets)

	// windows/arm64 is excluded in goreleaser config
	if runtime.GOOS == "windows" && runtime.GOARCH == "arm64" {
		if result != nil {
			t.Fatalf("findPlatformAsset should return nil for windows/arm64, got %v", result)
		}
		return
	}

	if result == nil {
		t.Fatalf("findPlatformAsset returned nil for %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	var expectedSuffix string
	switch runtime.GOOS {
	case "linux":
		expectedSuffix = "linux_x86_64.tar.gz"
	case "darwin":
		if runtime.GOARCH == "arm64" {
			expectedSuffix = "darwin_aarch64.tar.gz"
		} else {
			expectedSuffix = "darwin_x86_64.tar.gz"
		}
	case "windows":
		expectedSuffix = "windows_x86_64.tar.gz"
	}

	if !strings.Contains(result.Name, expectedSuffix) {
		t.Errorf("findPlatformAsset() = %q, want name containing %q", result.Name, expectedSuffix)
	}
}

func TestFindPlatformAssetEmpty(t *testing.T) {
	if result := findPlatformAsset(nil); result != nil {
		t.Errorf("findPlatformAsset(nil) should return nil, got %v", result)
	}
	if result := findPlatformAsset([]githubAsset{}); result != nil {
		t.Errorf("findPlatformAsset(empty) should return nil, got %v", result)
	}
}

func TestExtractBinary(t *testing.T) {
	tmpDir := t.TempDir()
	archivePath := filepath.Join(tmpDir, "test.tar.gz")

	binaryName := "forge"
	if runtime.GOOS == "windows" {
		binaryName = "forge.exe"
	}

	content := []byte("#!/bin/sh\necho forge v99.0.0")
	createTestArchive(t, archivePath, content, 0755)

	extracted, err := extractBinary(archivePath, tmpDir)
	if err != nil {
		t.Fatalf("extractBinary failed: %v", err)
	}

	if !strings.HasSuffix(extracted, "new-"+binaryName) {
		t.Errorf("extracted path = %q, want ending with new-%s", extracted, binaryName)
	}

	data, err := os.ReadFile(extracted)
	if err != nil {
		t.Fatalf("read extracted file failed: %v", err)
	}
	if string(data) != string(content) {
		t.Errorf("extracted content mismatch")
	}
}

func TestGetGitHubAPIURL(t *testing.T) {
	os.Unsetenv("FORGE_BINARY_HOST")
	url := getGitHubAPIURL()
	if !strings.Contains(url, "api.github.com") {
		t.Errorf("default URL should use api.github.com, got %s", url)
	}

	os.Setenv("FORGE_BINARY_HOST", "https://mirror.example.com/api")
	defer os.Unsetenv("FORGE_BINARY_HOST")
	url = getGitHubAPIURL()
	if !strings.HasPrefix(url, "https://mirror.example.com/api/") {
		t.Errorf("override URL should use FORGE_BINARY_HOST, got %s", url)
	}
}

func TestUpdateCacheFileLocation(t *testing.T) {
	tmpDir := t.TempDir()
	setTestHome(t, tmpDir)

	if err := saveUpdateCache("test-version", channelNPM); err != nil {
		t.Fatal(err)
	}

	expectedPath := filepath.Join(tmpDir, ".forge", "update-cache.json")
	if _, err := os.Stat(expectedPath); err != nil {
		t.Errorf("cache file not found at %s: %v", expectedPath, err)
	}

	data, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatal(err)
	}

	var cache updateCache
	if err := json.Unmarshal(data, &cache); err != nil {
		t.Fatalf("cache is not valid JSON: %v", err)
	}
	if cache.LatestVersion != "test-version" {
		t.Errorf("LatestVersion = %q, want %q", cache.LatestVersion, "test-version")
	}
}

func TestReplaceBinaryWindowsRollback(t *testing.T) {
	if runtime.GOOS != "windows" {
		// Test the unix path instead
		tmpDir := t.TempDir()
		currentPath := filepath.Join(tmpDir, "forge")
		if err := os.WriteFile(currentPath, []byte("old"), 0755); err != nil {
			t.Fatal(err)
		}

		// replaceBinaryUnix writes new data to a temp file, self-tests, then renames.
		// Since we can't run selfTest on non-binary data, we expect an error.
		err := replaceBinaryUnix(currentPath, []byte("new-broken"))
		if err == nil {
			t.Fatal("expected error from replaceBinaryUnix with invalid binary")
		}

		// Original should still exist
		data, err := os.ReadFile(currentPath)
		if err != nil {
			t.Fatalf("original binary should still exist: %v", err)
		}
		if string(data) != "old" {
			t.Errorf("original binary content changed: got %q", string(data))
		}
		return
	}

	// Windows path
	tmpDir := t.TempDir()
	currentPath := filepath.Join(tmpDir, "forge.exe")
	if err := os.WriteFile(currentPath, []byte("old"), 0755); err != nil {
		t.Fatal(err)
	}

	err := replaceBinaryWindows(currentPath, []byte("new-broken"))
	if err == nil {
		t.Fatal("expected error from replaceBinaryWindows with invalid binary")
	}

	data, err := os.ReadFile(currentPath)
	if err != nil {
		t.Fatalf("original binary should still exist after rollback: %v", err)
	}
	if string(data) != "old" {
		t.Errorf("original binary content changed after rollback: got %q", string(data))
	}

	oldPath := currentPath + ".old"
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Error(".old file should not exist after successful rollback")
	}
}

// setTestHome sets the home directory for tests, handling Windows (USERPROFILE)
// and Unix (HOME) correctly.
func setTestHome(t *testing.T, dir string) {
	t.Helper()
	// 同步把 FORGE_DATA_HOME 指到 <dir>/.forge：TestMain 已把全局根重定向到共享
	// tmpDir，而 update 缓存走 forgedata.GlobalHome（2026-09 代码普查 R4 收敛后）
	// ——不同步重定向会让本组测试读写共享根（跨测试串扰），也与「HOME 的 .forge
	// 即数据根」的真实默认布局不符。
	t.Setenv("FORGE_DATA_HOME", filepath.Join(dir, ".forge"))
	if runtime.GOOS == "windows" {
		orig := os.Getenv("USERPROFILE")
		os.Setenv("USERPROFILE", dir)
		t.Cleanup(func() { os.Setenv("USERPROFILE", orig) })
	} else {
		orig := os.Getenv("HOME")
		os.Setenv("HOME", dir)
		t.Cleanup(func() { os.Setenv("HOME", orig) })
	}
}

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"0.11.0", "0.11.0", 0},
		{"0.12.0", "0.11.0", 1},
		{"0.11.0", "0.12.0", -1},
		{"1.0.0", "0.99.99", 1},
		{"0.11.1", "0.11.0", 1},
		{"0.11.0", "0.11.1", -1},
		{"2.0.0-beta.1", "1.99.0", 1},
		{"10.0.0", "9.99.99", 1},
		// 核心相等按 semver §11 tie-break：正式版 > pre-release（beta 用户必须
		// 能看到正式版更新），pre-release 之间按 . 分段排序。
		{"0.11.0", "0.11.0-beta.1", 1},
		{"0.11.0-beta.1", "0.11.0", -1},
		{"0.12.0-beta.1", "0.12.0-beta.1", 0},
		{"0.12.0-beta.1", "0.12.0-beta.2", -1},
		{"0.12.0-beta.2", "0.12.0-beta.1", 1},
		{"0.12.0-beta.2", "0.12.0-beta.10", -1},
		{"0.12.0-alpha", "0.12.0-beta", -1},
		{"0.12.0-1", "0.12.0-alpha", -1},
		{"0.12.0-alpha", "0.12.0-alpha.1", -1},
	}

	for _, tt := range tests {
		t.Run(tt.a+"_vs_"+tt.b, func(t *testing.T) {
			got := compareVersions(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("compareVersions(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestRunUpdateIntegration(t *testing.T) {
	// mock GitHub API 响应格式必须与 runUpdate 用的 githubRelease struct 匹配。
	// 完整下载/校验/解压链路覆盖在 TestExtractBinary / TestExtractBinary_StripsSetuid。
	apiResponse := `{"tag_name":"v99.0.0","assets":[` +
		`{"name":"forge_99.0.0_windows_x86_64.tar.gz","browser_download_url":"http://example.com/archive","size":1000},` +
		`{"name":"checksums.txt","browser_download_url":"http://example.com/checksums","size":100}` +
		`]}`

	var release githubRelease
	if err := json.Unmarshal([]byte(apiResponse), &release); err != nil {
		t.Fatalf("failed to parse mock API response: %v", err)
	}
	if release.TagName != "v99.0.0" {
		t.Errorf("tag_name = %q, want v99.0.0", release.TagName)
	}
	if len(release.Assets) != 2 {
		t.Errorf("assets count = %d, want 2", len(release.Assets))
	}
}

func createTestArchive(t *testing.T, archivePath string, binaryContent []byte, mode int64) {
	t.Helper()

	binaryName := "forge"
	if runtime.GOOS == "windows" {
		binaryName = "forge.exe"
	}

	f, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	gzw := gzip.NewWriter(f)
	defer gzw.Close()

	tw := tar.NewWriter(gzw)
	defer tw.Close()

	hdr := &tar.Header{
		Name: binaryName,
		Mode: mode,
		Size: int64(len(binaryContent)),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(binaryContent); err != nil {
		t.Fatal(err)
	}
}

// TestPrintPluginReinstallGuidance: the plugin reinstall guidance output triggered by the --plugin flag should contain all four agent-platform commands so users can copy them in one go.
//
// TestPrintPluginReinstallGuidance：--plugin flag 触发的 plugin 重装指引输出
// 应含全部四个 agent 平台命令，让用户可一键复制。钉死未来误删。
func TestPrintPluginReinstallGuidance(t *testing.T) {
	buf := &bytes.Buffer{}
	printPluginReinstallGuidance(buf)
	out := buf.String()
	wantSubstr := []string{
		`Claude Code`,
		`Cursor`,
		`Codex`,
		`Copilot CLI`,
		`plugin uninstall`,
		`plugin install`,
	}
	for _, w := range wantSubstr {
		if !strings.Contains(out, w) {
			t.Errorf(`指引输出缺 %q：`+"\n"+`%s`, w, out)
		}
	}
}

// TestExtractBinary_StripsSetuid pins that tar entries carrying setuid/setgid bits are stripped.
//
// TestExtractBinary_StripsSetuid 钉住被删 TestArchiveSafeMode 曾覆盖的安全属性：
// 带 setuid/setgid 位的 tar 条目落盘后必须只剩普通 rwx 权限位（Perm()，而非
// 原始 Mode()）。
func TestExtractBinary_StripsSetuid(t *testing.T) {
	tmpDir := t.TempDir()
	archivePath := filepath.Join(tmpDir, "setuid.tar.gz")

	content := []byte("#!/bin/sh\necho hi")
	// 0o4755 = setuid + rwxr-xr-x——恶意 archive 试图落 setuid 二进制。
	// archive/tar 把 setuid 位映射到 os.ModeSetuid。
	createTestArchive(t, archivePath, content, 0o4755)

	extracted, err := extractBinary(archivePath, tmpDir)
	if err != nil {
		t.Fatalf("extractBinary: %v", err)
	}
	fi, err := os.Stat(extracted)
	if err != nil {
		t.Fatal(err)
	}
	mode := fi.Mode()
	if mode&os.ModeSetuid != 0 || mode&os.ModeSetgid != 0 {
		t.Errorf("setuid/setgid survived extraction: mode = %v", mode)
	}
	// Windows 只认只读位——0o755 落盘为 0o666；rwx 位在 Unix 上才有意义。
	if runtime.GOOS != "windows" && mode.Perm() != 0o755 {
		t.Errorf("permission bits = %o, want 755", mode.Perm())
	}
}
