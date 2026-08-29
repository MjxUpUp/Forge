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
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func init() {
	updateCmd.Flags().BoolVar(&updatePluginFlag, "plugin", false, "更新 binary 后打印 plugin marketplace 重装指引（不脚本化，agent CLI 内交互运行）")
	rootCmd.AddCommand(updateCmd)
}

// updatePluginFlag: the --plugin flag. Reinstalling the plugin marketplace requires interactive runs inside the agent CLI
// (cannot be scripted). After the binary is updated, print one-click uninstall/reinstall commands.
//
// updatePluginFlag --plugin flag：因 plugin marketplace 重装需在 agent CLI 内交互
// 跑（不可脚本化），更新 binary 后打印一键卸载/重装命令。
var updatePluginFlag bool

// printPluginReinstallGuidance writes the plugin marketplace reinstall guidance to w.
// It is intended to be run manually inside the agent CLI (Claude Code/Codex/Cursor/Copilot CLI each use different commands,
// so it is not auto-executed by update). The guidance is three-step (uninstall -> reinstall); the generator is not scripted (marketplace
// reinstall must be run interactively inside the agent CLI).
//
// printPluginReinstallGuidance 输出 plugin marketplace 重装指引到 w。
// 在 agent CLI 内手工跑（Claude Code/Codex/Cursor/Copilot CLI 各有不同命令，
// 故不在 update 自动执行）。三步式（卸载→重装）指引，不脚本化生成器（marketplace
// 重装须在 agent CLI 内交互跑）。
func printPluginReinstallGuidance(w io.Writer) {
	fmt.Fprintln(w, ``)
	fmt.Fprintln(w, `提示：plugin marketplace 中 plugin.json 镜像 Forge Go 变更，建议重新安装以同步：`)
	fmt.Fprintln(w, `  Claude Code / Cursor:  /plugin uninstall forge@forge && /plugin install forge@forge`)
	fmt.Fprintln(w, `  Codex:                 codex plugin uninstall forge@forge && codex plugin install forge@forge`)
	fmt.Fprintln(w, `  GitHub Copilot CLI:    copilot plugin uninstall forge@forge && copilot plugin install forge@forge`)
}

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "自更新 Forge 到最新版本",
	Long: `检查并更新 Forge 到最新版本（安装通道自动检测）。

- npm 安装（二进制位于 node_modules/@agent_forge 下）：查 npm registry 最新版本，
  打印对应包管理器（npm/pnpm/yarn/bun 自动检测）的更新命令。npm 包不可变，
  原地替换会被下次 npm install 还原，故不代下载（可用 FORGE_NPM_REGISTRY 覆盖
  registry）。
- 其他（GitHub Release / 手动放置）：从 GitHub Releases 下载并原地替换，
  支持 SHA-256 校验和验证。Windows 上更新前先把旧二进制重命名为 .old、成功后
  删除；若替换与回滚都失败，需按错误提示手动 move .old 还原（forge 不会在下次
  启动时自动恢复）。

可加 --plugin 触发后打印 plugin marketplace 重装指引（marketplace 含的 plugin.json
镜像 Go 变更时建议重装以同步）。`,
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
	// Channel records which install channel wrote this entry ("github" |
	// "npm"; "" = legacy entry written before the field existed). The two
	// channels can disagree during the two-stage release window (GitHub tag
	// before npm publish), so a fresh entry from the other channel is
	// re-queried rather than trusted.
	//
	// Channel 记录写入本条目的安装通道（"github" | "npm"；"" = 字段存在前
	// 的旧条目）。两段式发版窗口内两通道可能不一致（GitHub tag 先于
	// npm publish），另一通道的新鲜条目须重查而非信任。
	Channel string `json:"channel,omitempty"`
}

func runUpdate(cmd *cobra.Command, args []string) error {
	current := getCurrentVersion(cmd.Root().Version)
	fmt.Fprintf(os.Stderr, "当前版本: %s\n", current)

	channel := detectInstallChannelFn()

	// 1. Fetch the latest version from the channel-appropriate source
	//    (npm registry for npm installs, GitHub API otherwise).
	//
	// 1. 按安装通道取最新版本（npm 安装查 npm registry，其余查 GitHub API）
	fmt.Fprintf(os.Stderr, "正在检查更新...\n")
	var latest string
	var release *githubRelease
	if channel.kind == channelNPM {
		v, err := getLatestVersionFromNPM()
		if err != nil {
			return fmt.Errorf("检查更新失败（npm registry）: %w", err)
		}
		latest = v
	} else {
		r, err := getLatestRelease()
		if err != nil {
			return fmt.Errorf("检查更新失败: %w", err)
		}
		release = r
		latest = strings.TrimPrefix(release.TagName, "v")
	}
	fmt.Fprintf(os.Stderr, "最新版本: %s\n", latest)

	if latest == current {
		fmt.Fprintf(os.Stderr, "已是最新版本\n")
		_ = saveUpdateCache(latest, channel.kind)
		return nil
	}

	// Refuse downgrade: only update to a newer version.
	//
	// 拒绝降级——只更新到更新版本
	if compareVersions(latest, current) <= 0 {
		fmt.Fprintf(os.Stderr, "当前 %s 已是最新或更新版本（远端: %s）\n", current, latest)
		_ = saveUpdateCache(current, channel.kind)
		return nil
	}

	// npm channel: redirect to the package manager instead of downloading.
	// See update_channel.go for why an in-place replace is wrong under npm.
	//
	// npm 通道：重定向到包管理器而非下载。为何 npm 下原地替换是错的
	// 见 update_channel.go。
	if channel.kind == channelNPM {
		printNpmUpdateGuidance(os.Stderr, latest, channel.pm)
		// Same --plugin contract as the GitHub path: the marketplace mirror
		// needs a reinstall after the (user-run) update either way.
		//
		// 与 GitHub 路径同 --plugin 契约：无论哪条路更新后 marketplace
		// 镜像都可能需要重装。
		if updatePluginFlag {
			printPluginReinstallGuidance(os.Stderr)
		}
		_ = saveUpdateCache(latest, channel.kind)
		return nil
	}

	// 2. Find the platform asset.
	//
	// 2. 找 platform asset
	asset := findPlatformAsset(release.Assets)
	if asset == nil {
		return fmt.Errorf("找不到 %s/%s 的发行包", runtime.GOOS, runtime.GOARCH)
	}
	fmt.Fprintf(os.Stderr, "下载: %s (%.1f MB)\n", asset.Name, float64(asset.Size)/1024/1024)

	// 3. Download to a temp dir.
	//
	// 3. 下载到 temp dir
	tmpDir, err := os.MkdirTemp("", "forge-update-*")
	if err != nil {
		return fmt.Errorf("创建临时目录失败: %w", err)
	}
	defer os.RemoveAll(tmpDir)
	// Restrict access to owner only (mitigate TOCTOU).
	os.Chmod(tmpDir, 0700) // 仅 owner 可访问（防 TOCTOU）

	// #4: Validate the asset name: reject path traversal and drive letters.
	//
	// #4：校验 asset name——拒绝路径穿越与盘符
	archivePath := filepath.Join(tmpDir, asset.Name)
	if !strings.HasPrefix(filepath.Clean(archivePath), filepath.Clean(tmpDir)+string(os.PathSeparator)) {
		return fmt.Errorf("invalid asset name %q: path traversal detected", asset.Name)
	}
	if err := downloadFile(asset.BrowserDownloadURL, archivePath); err != nil {
		return fmt.Errorf("下载失败: %w", err)
	}
	fmt.Fprintf(os.Stderr, "下载完成\n")

	// 4. Verify the checksum.
	//
	// 4. 校验 checksum
	fmt.Fprintf(os.Stderr, "校验 SHA-256...\n")
	if err := verifyChecksum(release.Assets, asset.Name, archivePath); err != nil {
		return fmt.Errorf("校验失败: %w", err)
	}
	fmt.Fprintf(os.Stderr, "校验通过\n")

	// 5. Extract the binary.
	//
	// 5. 解压 binary
	fmt.Fprintf(os.Stderr, "解压...\n")
	extractedPath, err := extractBinary(archivePath, tmpDir)
	if err != nil {
		return fmt.Errorf("解压失败: %w", err)
	}

	// 6. Self-test.
	//
	// 6. 自检（self-test）
	fmt.Fprintf(os.Stderr, "验证新版本...\n")
	if err := selfTest(extractedPath); err != nil {
		return fmt.Errorf("新版本验证失败: %w", err)
	}

	// 7. Replace the binary.
	//
	// 7. 替换 binary
	exePath, err := getExecutablePath()
	if err != nil {
		return fmt.Errorf("获取当前路径失败: %w", err)
	}
	fmt.Fprintf(os.Stderr, "替换: %s\n", exePath)

	if err := replaceBinary(exePath, extractedPath); err != nil {
		return fmt.Errorf("替换失败: %w", err)
	}

	fmt.Fprintf(os.Stderr, "✓ 已更新到 %s\n", latest)

	// Update the cache.
	//
	// 更新缓存
	_ = saveUpdateCache(latest, channel.kind)

	// --plugin flag: prompt plugin reinstallation (marketplace mirror).
	//
	// --plugin flag: 提示 plugin 重新安装（marketplace 镜像）
	if updatePluginFlag {
		printPluginReinstallGuidance(os.Stderr)
	}

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
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	return &release, nil
}

func getGitHubAPIURL() string {
	if host := os.Getenv("FORGE_BINARY_HOST"); host != "" {
		return strings.TrimSuffix(host, "/") + "/repos/MjxUpUp/Forge/releases/latest"
	}
	return "https://api.github.com/repos/MjxUpUp/Forge/releases/latest"
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

	// goreleaser archive naming: forge_{version}_{os}_{arch}.tar.gz
	//
	// goreleaser archive 名：forge_{version}_{os}_{arch}.tar.gz
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

	// Progress: write to stderr every 1MB.
	//
	// 进度：每 1MB 写 stderr
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
	// Find checksums.txt.
	//
	// 找 checksums.txt
	var checksumURL string
	for _, a := range assets {
		if a.Name == "checksums.txt" {
			checksumURL = a.BrowserDownloadURL
			// When using a mirror, force-fetch checksums.txt from the official GitHub (#2, #3).
			//
			// 使用 mirror 时强制从官方 GitHub 取 checksums.txt（#2、#3）
			if os.Getenv("FORGE_BINARY_HOST") != "" {
				checksumURL = "https://github.com/MjxUpUp/Forge/releases/latest/download/checksums.txt"
			}
			break
		}
	}
	if checksumURL == "" {
		return fmt.Errorf("release 中没有 checksums.txt")
	}

	// Download checksums.txt.
	//
	// 下载 checksums.txt
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

	// Parse checksums.txt: the format is hash followed by two spaces and then the filename.
	//
	// 解析 checksums.txt——格式为 hash 后接两个空格再接 filename
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

	// Compute the actual hash.
	//
	// 算实际 hash
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

		// Reject symlinks and hard links (security: prevent path escape).
		//
		// 拒绝 symlink 与 hard link（安全：防路径逃逸）
		if hdr.Typeflag == tar.TypeSymlink || hdr.Typeflag == tar.TypeLink {
			continue
		}

		// Find the forge binary inside the archive.
		//
		// 在 archive 中找 forge binary
		base := filepath.Base(hdr.Name)
		if base == binaryName && !hdr.FileInfo().IsDir() {
			outPath := filepath.Join(destDir, "new-"+binaryName)
			// Strip everything but the permission bits. archive/tar maps
			// setuid/setgid/sticky onto the high os.FileMode bits
			// (os.ModeSetuid/...), not the low 12 bits — masking with 0o6000
			// stripped nothing. Perm() keeps only the rwx bits.
			//
			// 只保留权限位。archive/tar 把 setuid/setgid/sticky 映射到
			// os.FileMode 高位（os.ModeSetuid/...），不在低 12 位——用
			// 0o6000 掩码剥不掉任何东西。Perm() 只留 rwx 位。
			safeMode := hdr.FileInfo().Mode().Perm()
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
	// Resolve symlinks (on Unix, the npm wrapper may use a symlink).
	//
	// 解析 symlink（Unix 上 npm wrapper 可能用 symlink）
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return "", err
	}
	return exe, nil
}

func replaceBinary(exePath, newBinaryPath string) error {
	// Read the new binary data.
	//
	// 读新 binary 数据
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

	// Remove any leftover .old file.
	//
	// 移除残留的 .old
	os.Remove(oldPath)

	// Step 1: rename the current exe to .old.
	//
	// 步骤 1：把当前 exe 重命名为 .old
	if err := os.Rename(exePath, oldPath); err != nil {
		return fmt.Errorf("重命名当前二进制失败: %w", err)
	}

	// Step 2: write the new binary.
	//
	// 步骤 2：写新 binary
	if err := os.WriteFile(exePath, newData, 0755); err != nil {
		// Rollback: attempt to restore .old.
		//
		// 回滚：尝试还原 .old
		if rerr := os.Rename(oldPath, exePath); rerr != nil {
			return fmt.Errorf("写入新二进制失败且回滚也失败: %w (rollback: %v)。无法自动恢复——请手动执行 move %q %q（或等价重命名）恢复旧版后重试更新", err, rerr, oldPath, exePath)
		}
		return fmt.Errorf("写入新二进制失败（已回滚）: %w", err)
	}

	// Step 3: run the self-test on the new binary.
	//
	// 步骤 3：对新 binary 跑 self-test
	if err := selfTest(exePath); err != nil {
		// Rollback: restore .old.
		//
		// 回滚：还原 .old
		if rerr := os.Rename(oldPath, exePath); rerr != nil {
			return fmt.Errorf("新版本验证失败且回滚也失败: %w (rollback: %v)。无法自动恢复——请手动执行 move %q %q（或等价重命名）恢复旧版后重试更新", err, rerr, oldPath, exePath)
		}
		return fmt.Errorf("新版本验证失败（已回滚）: %w", err)
	}

	// Step 4: delete .old (success).
	//
	// 步骤 4：删除 .old（成功）
	os.Remove(oldPath)

	return nil
}

func replaceBinaryUnix(exePath string, newData []byte) error {
	// Write to a temp file in the same directory, then atomically rename.
	//
	// 写到同目录的 temp file，再 atomic rename
	dir := filepath.Dir(exePath)
	tmpPath := filepath.Join(dir, ".forge-update-tmp")

	if err := os.WriteFile(tmpPath, newData, 0755); err != nil {
		return fmt.Errorf("写入临时文件失败: %w", err)
	}

	// Self-test before replacing.
	//
	// 替换前先 self-test
	if err := selfTest(tmpPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("新版本验证失败: %w", err)
	}

	// Atomic replacement.
	//
	// 原子替换
	if err := os.Rename(tmpPath, exePath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("替换二进制失败: %w", err)
	}

	return nil
}

// getCurrentVersion extracts the bare version number (e.g. 0.11.1) from the full version string
// set by SetVersion (e.g. X.Y.Z (commit: ..., built: ...)).
//
// getCurrentVersion 从 SetVersion 设置的完整 version 串（如 X.Y.Z (commit: ..., built: ...)）
// 中提取裸版本号（如 0.11.1）。
func getCurrentVersion(fullVersion string) string {
	if fullVersion == "dev" {
		return "dev"
	}
	// Extract the version before the first space or parenthesis.
	//
	// 在第一个空格/括号前提取 version
	idx := strings.IndexByte(fullVersion, ' ')
	if idx > 0 {
		return fullVersion[:idx]
	}
	return fullVersion
}

// compareVersions compares two semver-style version strings (e.g. 0.11.1 vs 0.12.0).
// Returns: 1 if a > b, 0 if a == b, -1 if a < b.
// When the numeric cores are equal, pre-release is tie-broken per semver §11:
// a release outranks its pre-releases (0.12.0 > 0.12.0-beta.1 — otherwise beta
// users would never see the GA release), and two pre-releases compare
// dot-segment-wise (numeric segments numerically, numeric < alphanumeric,
// alphanumeric ASCII-wise; a shorter set is smaller when all preceding match).
//
// compareVersions 比较两个 semver 风格的 version 串（如 0.11.1 对 0.12.0）。
// 返回：a > b 返 1，a == b 返 0，a < b 返 -1。
// 数字核心相等时按 semver §11 tie-break pre-release：正式版高于 pre-release
// （0.12.0 > 0.12.0-beta.1——否则 beta 用户永远收不到正式版）；两个 pre-release
// 按 . 分段比较（数字段按数值、数字段 < 字母段、字母段按 ASCII；前缀全同时
// 段数少者小）。
func compareVersions(a, b string) int {
	aCore, aPre := splitPreRelease(a)
	bCore, bPre := splitPreRelease(b)

	if c := compareVersionCores(aCore, bCore); c != 0 {
		return c
	}
	if aPre == bPre {
		return 0
	}
	if aPre == "" {
		return 1 // release > pre-release (semver §11)
	}
	if bPre == "" {
		return -1
	}
	return comparePreReleases(aPre, bPre)
}

// splitPreRelease splits a version string into its numeric core and the
// pre-release suffix ("" when absent).
//
// splitPreRelease 把 version 串拆成数字核心与 pre-release 后缀（无则 ""）。
func splitPreRelease(v string) (core, pre string) {
	if idx := strings.IndexByte(v, '-'); idx > 0 {
		return v[:idx], v[idx+1:]
	}
	return v, ""
}

// compareVersionCores compares the numeric dot-separated cores.
//
// compareVersionCores 比较点分数字核心。
func compareVersionCores(aCore, bCore string) int {
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

// comparePreReleases compares two pre-release suffixes per semver §11.4:
// dot-separated identifiers, numeric identifiers compare numerically and are
// lower than alphanumeric ones, alphanumeric compare ASCII-wise, and a smaller
// set of identifiers is lower when all preceding identifiers are equal.
//
// comparePreReleases 按 semver §11.4 比较两个 pre-release 后缀：. 分段，
// 数字段按数值且低于字母段，字母段按 ASCII，前缀段全等时段数少者低。
func comparePreReleases(aPre, bPre string) int {
	aSegs := strings.Split(aPre, ".")
	bSegs := strings.Split(bPre, ".")

	maxLen := len(aSegs)
	if len(bSegs) > maxLen {
		maxLen = len(bSegs)
	}

	for i := 0; i < maxLen; i++ {
		if i >= len(aSegs) {
			return -1 // a ran out of identifiers: a < b
		}
		if i >= len(bSegs) {
			return 1
		}
		aNum, aIsNum := numericIdentifier(aSegs[i])
		bNum, bIsNum := numericIdentifier(bSegs[i])
		switch {
		case aIsNum && bIsNum:
			if aNum != bNum {
				if aNum > bNum {
					return 1
				}
				return -1
			}
		case aIsNum: // numeric < alphanumeric
			return -1
		case bIsNum:
			return 1
		default:
			if c := strings.Compare(aSegs[i], bSegs[i]); c != 0 {
				if c > 0 {
					return 1
				}
				return -1
			}
		}
	}
	return 0
}

// numericIdentifier reports whether a pre-release identifier is purely numeric,
// returning its value when it is.
//
// numericIdentifier 判断 pre-release 分段是否纯数字，是则返回其数值。
func numericIdentifier(s string) (int, bool) {
	if s == "" || s[0] == '+' || s[0] == '-' {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, false
	}
	return n, true
}

func parseVersionPart(s string) int {
	// Strip non-numeric prefixes/suffixes (e.g. rc1 must not be counted as 1; return 0 instead).
	//
	// 剥离非数字前缀/后缀（如 rc1 错误地算成 1，直接返 0）
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
