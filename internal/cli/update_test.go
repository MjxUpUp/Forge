package cli

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
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
	if err := saveUpdateCache(version); err != nil {
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

func TestVerifyChecksumLogic(t *testing.T) {
	testContent := []byte("hello forge update test")
	hashArray := sha256.Sum256(testContent)
	expectedHash := hex.EncodeToString(hashArray[:])

	// Verify checksums.txt line parsing
	checksums := fmt.Sprintf("%s  test.tar.gz\nother_hash  other_file.tar.gz\n", expectedHash)
	lines := strings.Split(checksums, "\n")
	found := false
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "  ", 2)
		if len(parts) == 2 && parts[1] == "test.tar.gz" {
			if parts[0] == expectedHash {
				found = true
			}
			break
		}
	}
	if !found {
		t.Error("checksums.txt parsing failed to find matching entry")
	}
}

func TestExtractBinary(t *testing.T) {
	tmpDir := t.TempDir()
	archivePath := filepath.Join(tmpDir, "test.tar.gz")

	f, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}

	gzw := gzip.NewWriter(f)
	tw := tar.NewWriter(gzw)

	binaryName := "forge"
	if runtime.GOOS == "windows" {
		binaryName = "forge.exe"
	}

	content := []byte("#!/bin/sh\necho forge v99.0.0")
	hdr := &tar.Header{
		Name: binaryName,
		Mode: 0755,
		Size: int64(len(content)),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	tw.Close()
	gzw.Close()
	f.Close()

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

	if err := saveUpdateCache("test-version"); err != nil {
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
		{"0.11.0", "0.11.0-beta.1", 0},
		{"10.0.0", "9.99.99", 1},
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
