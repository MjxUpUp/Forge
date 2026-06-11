package cli

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(updateCmd)
}

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "自更新 Forge 到最新版本",
	Long: `从 GitHub Releases 下载最新版本的 Forge 二进制文件并原地替换。

支持 SHA-256 校验和验证、Windows 崩溃恢复（.old 安全模式）。`,
	RunE: runUpdate,
}

type githubRelease struct {
	TagName string        `json:"tag_name"`
	Assets  []githubAsset `json:"assets"`
}

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

type updateCache struct {
	LatestVersion string `json:"latest_version"`
	CheckedAt     string `json:"checked_at"`
}

func runUpdate(cmd *cobra.Command, args []string) error {
	current := getCurrentVersion(cmd.Root().Version)
	fmt.Fprintf(os.Stderr, "当前版本: %s\n", current)

	// 1. Get latest release
	fmt.Fprintf(os.Stderr, "正在检查更新...\n")
	release, err := getLatestRelease()
	if err != nil {
		return fmt.Errorf("检查更新失败: %w", err)
	}

	latest := strings.TrimPrefix(release.TagName, "v")
	fmt.Fprintf(os.Stderr, "最新版本: %s\n", latest)

	if latest == current {
		fmt.Fprintf(os.Stderr, "已是最新版本\n")
		_ = saveUpdateCache(latest)
		return nil
	}

	// Reject downgrades — only update to newer versions
	if compareVersions(latest, current) <= 0 {
		fmt.Fprintf(os.Stderr, "当前 %s 已是最新或更新版本（远端: %s）\n", current, latest)
		_ = saveUpdateCache(current)
		return nil
	}

	// 2. Find platform asset
	asset := findPlatformAsset(release.Assets)
	if asset == nil {
		return fmt.Errorf("找不到 %s/%s 的发行包", runtime.GOOS, runtime.GOARCH)
	}
	fmt.Fprintf(os.Stderr, "下载: %s (%.1f MB)\n", asset.Name, float64(asset.Size)/1024/1024)

	// 3. Download to temp dir
	tmpDir, err := os.MkdirTemp("", "forge-update-*")
	if err != nil {
		return fmt.Errorf("创建临时目录失败: %w", err)
	}
	defer os.RemoveAll(tmpDir)
	os.Chmod(tmpDir, 0700) // Restrict to owner only (prevent TOCTOU)

	// #4: Validate asset name — reject path traversal and drive letters
	archivePath := filepath.Join(tmpDir, asset.Name)
	if !strings.HasPrefix(filepath.Clean(archivePath), filepath.Clean(tmpDir)+string(os.PathSeparator)) {
		return fmt.Errorf("invalid asset name %q: path traversal detected", asset.Name)
	}
	if err := downloadFile(asset.BrowserDownloadURL, archivePath); err != nil {
		return fmt.Errorf("下载失败: %w", err)
	}
	fmt.Fprintf(os.Stderr, "下载完成\n")

	// 4. Verify checksum
	fmt.Fprintf(os.Stderr, "校验 SHA-256...\n")
	if err := verifyChecksum(release.Assets, asset.Name, archivePath); err != nil {
		return fmt.Errorf("校验失败: %w", err)
	}
	fmt.Fprintf(os.Stderr, "校验通过\n")

	// 5. Extract binary
	fmt.Fprintf(os.Stderr, "解压...\n")
	extractedPath, err := extractBinary(archivePath, tmpDir)
	if err != nil {
		return fmt.Errorf("解压失败: %w", err)
	}

	// 6. Self-test
	fmt.Fprintf(os.Stderr, "验证新版本...\n")
	if err := selfTest(extractedPath); err != nil {
		return fmt.Errorf("新版本验证失败: %w", err)
	}

	// 7. Replace binary
	exePath, err := getExecutablePath()
	if err != nil {
		return fmt.Errorf("获取当前路径失败: %w", err)
	}
	fmt.Fprintf(os.Stderr, "替换: %s\n", exePath)

	if err := replaceBinary(exePath, extractedPath); err != nil {
		return fmt.Errorf("替换失败: %w", err)
	}

	fmt.Fprintf(os.Stderr, "✓ 已更新到 %s\n", latest)

	// Update cache
	_ = saveUpdateCache(latest)

	return nil
}

func getLatestRelease() (*githubRelease, error) {
	apiURL := getGitHubAPIURL()

	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "forge-self-update")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GitHub API 请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("GitHub API 返回 %d: %s", resp.StatusCode, string(body))
	}

	var release githubRelease
	if err := jsonUnmarshal(resp.Body, &release); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	return &release, nil
}

func getGitHubAPIURL() string {
	if host := os.Getenv("FORGE_BINARY_HOST"); host != "" {
		return strings.TrimSuffix(host, "/") + "/repos/MjxUpUp/forge/releases/latest"
	}
	return "https://api.github.com/repos/MjxUpUp/forge/releases/latest"
}

func findPlatformAsset(assets []githubAsset) *githubAsset {
	var osName, archName string

	switch runtime.GOOS {
	case "linux":
		osName = "linux"
	case "darwin":
		osName = "darwin"
	case "windows":
		osName = "windows"
	default:
		return nil
	}

	switch runtime.GOARCH {
	case "amd64":
		archName = "x86_64"
	case "arm64":
		archName = "aarch64"
	default:
		return nil
	}

	// goreleaser archive name: forge_{version}_{os}_{arch}.tar.gz
	for i := range assets {
		name := assets[i].Name
		if strings.Contains(name, "_"+osName+"_") &&
			strings.Contains(name, "_"+archName+".") &&
			strings.HasSuffix(name, ".tar.gz") {
			return &assets[i]
		}
	}

	return nil
}

func downloadFile(url, dest string) error {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			wait := time.Duration(attempt) * 2 * time.Second
			fmt.Fprintf(os.Stderr, "  重试 (%d/3)...\n", attempt+1)
			time.Sleep(wait)
		}

		err := tryDownload(url, dest)
		if err == nil {
			return nil
		}
		lastErr = err
	}
	return fmt.Errorf("3 次重试后仍失败: %w", lastErr)
}

func tryDownload(url, dest string) error {
	client := &http.Client{Timeout: 5 * time.Minute}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "forge-self-update")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()

	// Progress: write to stderr every 1MB
	progress := &progressWriter{w: f, total: resp.ContentLength, lastReport: 0}
	_, err = io.Copy(progress, resp.Body)
	return err
}

type progressWriter struct {
	w          io.Writer
	total      int64
	written    int64
	lastReport int64
}

func (pw *progressWriter) Write(p []byte) (int, error) {
	n, err := pw.w.Write(p)
	pw.written += int64(n)
	if pw.total > 0 && pw.written-pw.lastReport >= 1024*1024 {
		pct := float64(pw.written) / float64(pw.total) * 100
		fmt.Fprintf(os.Stderr, "  %.0f%%\n", pct)
		pw.lastReport = pw.written
	}
	return n, err
}

func verifyChecksum(assets []githubAsset, assetName, archivePath string) error {
	// Find checksums.txt
	var checksumURL string
	for _, a := range assets {
		if a.Name == "checksums.txt" {
			checksumURL = a.BrowserDownloadURL
				// Force checksums.txt from official GitHub when using mirror (#2, #3)
				if os.Getenv("FORGE_BINARY_HOST") != "" {
					checksumURL = "https://github.com/MjxUpUp/forge/releases/latest/download/checksums.txt"
				}
			break
		}
	}
	if checksumURL == "" {
		return fmt.Errorf("release 中没有 checksums.txt")
	}

	// Download checksums.txt
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(checksumURL)
	if err != nil {
		return fmt.Errorf("下载 checksums.txt 失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载 checksums.txt 返回 HTTP %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("读取 checksums.txt 失败: %w", err)
	}

	// Parse checksums.txt — format: "hash  filename"
	expectedHash := ""
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "  ", 2)
		if len(parts) == 2 && parts[1] == assetName {
			expectedHash = parts[0]
			break
		}
	}
	if expectedHash == "" {
		return fmt.Errorf("checksums.txt 中没有 %s 的条目", assetName)
	}

	// Compute actual hash
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, f); err != nil {
		return err
	}
	actualHash := hex.EncodeToString(hasher.Sum(nil))

	if actualHash != expectedHash {
		return fmt.Errorf("SHA-256 不匹配:\n  期望: %s\n  实际: %s", expectedHash, actualHash)
	}

	return nil
}

func extractBinary(archivePath, destDir string) (string, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		return "", fmt.Errorf("gzip 解压失败: %w", err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)

	binaryName := "forge"
	if runtime.GOOS == "windows" {
		binaryName = "forge.exe"
	}

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("读取 tar 失败: %w", err)
		}

		// Reject symlinks and hard links (security: prevent path escape)
		if hdr.Typeflag == tar.TypeSymlink || hdr.Typeflag == tar.TypeLink {
			continue
		}

		// Look for the forge binary in the archive
		base := filepath.Base(hdr.Name)
		if base == binaryName && !hdr.FileInfo().IsDir() {
			outPath := filepath.Join(destDir, "new-"+binaryName)
			// Strip setuid/setgid bits from mode
			safeMode := hdr.FileInfo().Mode() &^ 0o6000
			out, err := os.OpenFile(outPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, safeMode)
			if err != nil {
				return "", err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return "", err
			}
			out.Close()
			return outPath, nil
		}
	}

	return "", fmt.Errorf("归档中没有找到 %s", binaryName)
}

func selfTest(binaryPath string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binaryPath, "--version")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("新二进制 --version 失败: %w\n%s", err, string(out))
	}

	version := strings.TrimSpace(string(out))
	if version == "" {
		return fmt.Errorf("新二进制 --version 返回空")
	}

	return nil
}

func getExecutablePath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	// Resolve symlinks (npm wrapper on Unix may use symlinks)
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return "", err
	}
	return exe, nil
}

func replaceBinary(exePath, newBinaryPath string) error {
	// Read new binary data
	newData, err := os.ReadFile(newBinaryPath)
	if err != nil {
		return fmt.Errorf("读取新二进制失败: %w", err)
	}

	if runtime.GOOS == "windows" {
		return replaceBinaryWindows(exePath, newData)
	}
	return replaceBinaryUnix(exePath, newData)
}

func replaceBinaryWindows(exePath string, newData []byte) error {
	oldPath := exePath + ".old"

	// Remove stale .old if present
	os.Remove(oldPath)

	// Step 1: rename current exe to .old
	if err := os.Rename(exePath, oldPath); err != nil {
		return fmt.Errorf("重命名当前二进制失败: %w", err)
	}

	// Step 2: write new binary
	if err := os.WriteFile(exePath, newData, 0755); err != nil {
		// Rollback: try to restore .old
		if rerr := os.Rename(oldPath, exePath); rerr != nil {
			return fmt.Errorf("写入新二进制失败且回滚也失败: %w (rollback: %v). 再次运行 forge 命令将从 .old 备份恢复", err, rerr)
		}
		return fmt.Errorf("写入新二进制失败（已回滚）: %w", err)
	}

	// Step 3: self-test the new binary
	if err := selfTest(exePath); err != nil {
		// Rollback: restore .old
		if rerr := os.Rename(oldPath, exePath); rerr != nil {
			return fmt.Errorf("新版本验证失败且回滚也失败: %w (rollback: %v). 再次运行 forge 命令将从 .old 备份恢复", err, rerr)
		}
		return fmt.Errorf("新版本验证失败（已回滚）: %w", err)
	}

	// Step 4: remove .old (success)
	os.Remove(oldPath)

	return nil
}

func replaceBinaryUnix(exePath string, newData []byte) error {
	// Write to temp file in same directory, then atomic rename
	dir := filepath.Dir(exePath)
	tmpPath := filepath.Join(dir, ".forge-update-tmp")

	if err := os.WriteFile(tmpPath, newData, 0755); err != nil {
		return fmt.Errorf("写入临时文件失败: %w", err)
	}

	// Self-test before replacing
	if err := selfTest(tmpPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("新版本验证失败: %w", err)
	}

	// Atomic replace
	if err := os.Rename(tmpPath, exePath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("替换二进制失败: %w", err)
	}

	return nil
}

// getCurrentVersion extracts the bare version number (e.g. "0.11.1") from
// the full version string set by SetVersion: "X.Y.Z (commit: ..., built: ...)"
func getCurrentVersion(fullVersion string) string {
	if fullVersion == "dev" {
		return "dev"
	}
	// Extract version before the first space/paren
	idx := strings.IndexByte(fullVersion, ' ')
	if idx > 0 {
		return fullVersion[:idx]
	}
	return fullVersion
}

func jsonUnmarshal(r io.Reader, v interface{}) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// compareVersions compares two semver-like version strings (e.g. "0.11.1" vs "0.12.0").
// Returns: 1 if a > b, 0 if a == b, -1 if a < b.
// Strips pre-release suffixes (-beta.1) and only compares numeric parts.
func compareVersions(a, b string) int {
	aCore := a
	bCore := b
	if idx := strings.IndexByte(a, '-'); idx > 0 {
		aCore = a[:idx]
	}
	if idx := strings.IndexByte(b, '-'); idx > 0 {
		bCore = b[:idx]
	}

	aParts := strings.Split(aCore, ".")
	bParts := strings.Split(bCore, ".")

	maxLen := len(aParts)
	if len(bParts) > maxLen {
		maxLen = len(bParts)
	}

	for i := 0; i < maxLen; i++ {
		av := 0
		bv := 0
		if i < len(aParts) {
			av = parseVersionPart(aParts[i])
		}
		if i < len(bParts) {
			bv = parseVersionPart(bParts[i])
		}
		if av != bv {
			if av > bv {
				return 1
			}
			return -1
		}
	}
	return 0
}

func parseVersionPart(s string) int {
	// Strip non-numeric prefix/suffix (e.g. "rc1" → 1 is wrong, just return 0)
	n := 0
	for _, c := range s {
		if c >= '0' && c <= '9' {
			n = n*10 + int(c-'0')
		} else {
			break
		}
	}
	return n
}
