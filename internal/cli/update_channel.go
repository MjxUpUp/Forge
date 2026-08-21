package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Forge ships primarily as an npm package (@agent_forge/forge, binary in
// per-platform optionalDependencies subpackages). The npm channel changes how
// self-update must behave:
//
//   - npm packages are immutable: overwriting the binary inside node_modules
//     in place desyncs it from npm's metadata, and the next npm install /
//     update silently reverts it (version ping-pong).
//   - users installed from a package manager (npm/pnpm/yarn) that can reach
//     its registry by definition, while GitHub Releases may be unreachable.
//
// So update checks the npm registry for the latest version and redirects the
// user to their package manager instead of downloading from GitHub.
//
// Forge 主分发渠道是 npm 包（@agent_forge/forge，二进制在按平台的
// optionalDependencies 子包里）。npm 通道改变自更新行为：npm 包不可变，
// 原地改写 node_modules 里的二进制会与包管理器元数据脱钩，下次
// install/update 会静默还原（版本乒乓）；且用户按定义能达自己所用包管理器
// 的 registry（装的时候就用它），GitHub Releases 则未必可达。
// 因此更新检查改查 npm registry，并把用户重定向到包管理器命令。

// channelKind is the coarse install channel controlling the update flow.
//
// channelKind 是控制更新流程的粗粒度安装通道。
type channelKind string

const (
	channelGitHub channelKind = "github"
	channelNPM    channelKind = "npm"
)

// installChannel describes how the running forge binary was installed: the
// kind picks the version source / flow, pm (npm kind only) picks the update
// command so users never install a parallel copy under a different package
// manager.
//
// installChannel 描述运行中的 forge 二进制的安装方式：kind 选版本源/流程，
// pm（仅 npm kind）选更新命令——用户绝不会被指引用另一个包管理器装出
// 平行的第二份 forge。
type installChannel struct {
	kind channelKind
	pm   string // "npm" | "pnpm" | "yarn"; zero for github kind
}

// npmRegistryDefault is the npm registry used for version checks; override
// with FORGE_NPM_REGISTRY (mirror users whose .npmrc is invisible to forge).
//
// npmRegistryDefault 是版本检查用的 npm registry；可用 FORGE_NPM_REGISTRY
// 覆盖（镜像用户的 .npmrc 对 forge 不可见）。
const npmRegistryDefault = "https://registry.npmjs.org"

// semverPattern matches the version strings npm registries legitimately
// publish (the numeric core npm releases always carry; build metadata like
// 1.40.0+build is technically legal but release pipelines for this package
// never emit it, and rejecting it fails loudly rather than misbehaving).
// getLatestVersionFromNPM pastes the value into a copy-paste shell command,
// so a hostile/misbehaving mirror must not be able to smuggle
// metacharacters through the version field.
//
// semverPattern 匹配 npm registry 合法发布的 version 串（npm 发布版本恒有
// 的数字核心；形如 1.40.0+build 的 build metadata 理论合法但本包发布线
// 从不产出，拒绝它只会响亮报错而非行为异常）。getLatestVersionFromNPM
// 会把该值拼进可复制的 shell 命令，恶意/异常镜像不得借 version 字段
// 夹带元字符。
var semverPattern = regexp.MustCompile(`^\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$`)

// detectInstallChannelFn is the indirection tests use to force a channel
// (the real detection reads os.Executable(), which a test process cannot
// relocate into a node_modules layout).
//
// detectInstallChannelFn 是测试强制通道用的间接层（真实检测读
// os.Executable()，测试进程无法把自己挪进 node_modules 布局）。
var detectInstallChannelFn = detectInstallChannel

// detectInstallChannel infers the install channel from the resolved
// executable path. Best-effort: any failure falls back to the GitHub channel
// (the legacy behavior).
//
// detectInstallChannel 从解析后的可执行文件路径推断安装通道。best-effort：
// 任何失败回落 GitHub 通道（legacy 行为）。
func detectInstallChannel() installChannel {
	exe, err := os.Executable()
	if err != nil {
		return installChannel{kind: channelGitHub}
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	if !isNpmManagedPath(exe) {
		return installChannel{kind: channelGitHub}
	}
	return installChannel{kind: channelNPM, pm: packageManagerForPath(exe)}
}

// isNpmManagedPath reports whether exePath sits inside the @agent_forge scope
// of a node_modules layout. npm, yarn and pnpm global installs all place the
// platform subpackage binary at
// .../node_modules/@agent_forge/forge-<platform>-<arch>/bin/forge(.exe), so a
// single path-segment match covers every package manager.
//
// isNpmManagedPath 判断 exePath 是否位于 node_modules 布局的 @agent_forge
// scope 内。npm/yarn/pnpm 全局安装都把平台子包二进制放在
// .../node_modules/@agent_forge/forge-<platform>-<arch>/bin/forge(.exe)，
// 单一路径段匹配即可覆盖所有包管理器。
func isNpmManagedPath(exePath string) bool {
	// Normalize separators so Windows paths (which may mix \ and /) split
	// into clean segments.
	//
	// 归一化分隔符，让 Windows 路径（可能混用 \ 与 /）切成干净分段。
	norm := strings.ReplaceAll(exePath, "\\", "/")
	parts := strings.Split(norm, "/")
	for i := 0; i+1 < len(parts); i++ {
		if parts[i] != "@agent_forge" {
			continue
		}
		next := parts[i+1]
		// "forge" = main package (defensive; its bin is run.js), "forge-*" =
		// platform subpackages. Other @agent_forge packages (forge-dsh) are
		// separate CLIs, not the forge binary.
		//
		// "forge"=主包（防御性；其 bin 是 run.js），"forge-*"=平台子包。
		// 其余 @agent_forge 包（forge-dsh）是独立 CLI，不是 forge 二进制。
		if next == "forge" || strings.HasPrefix(next, "forge-") {
			return true
		}
	}
	return false
}

// packageManagerForPath infers which package manager owns an npm-layout
// path. pnpm globals live under .../.pnpm/ or .../pnpm/global/, yarn v1
// globals under ~/.yarn/global (unix) or ...\Yarn\global (Windows), bun
// globals under ~/.bun/install/global/; everything else — plain npm, and
// wrappers like Volta that manage npm itself — defaults to npm. Pointing a
// pnpm user at `npm install -g` would create a second parallel forge on
// PATH — exactly the stray-exe/version-ping-pong mess this channel split
// exists to prevent — so the command must match the manager that owns the
// install.
//
// packageManagerForPath 从 npm 布局路径推断所属包管理器。pnpm 全局在
// .../.pnpm/ 或 .../pnpm/global/ 下，yarn v1 全局在 ~/.yarn/global（unix）
// 或 ...\Yarn\global（Windows），bun 全局在 ~/.bun/install/global/ 下；
// 其余——裸 npm 及 Volta 这类包装 npm 本身的工具——默认 npm。给 pnpm
// 用户指 `npm install -g` 会在 PATH 上装出第二份平行 forge——恰是本次
// 通道分流要消灭的游离 exe/版本乒乓——命令必须对上持有该安装的包管理器。
func packageManagerForPath(exePath string) string {
	// Lowercase: yarn's global dir is ~/.yarn/global on unix but
	// %LOCALAPPDATA%\Yarn\global on Windows — case must not decide the
	// package manager.
	//
	// 小写化：yarn 全局目录 unix 上是 ~/.yarn/global，Windows 上是
	// %LOCALAPPDATA%\Yarn\global——大小写不应决定包管理器。
	lower := strings.ToLower(strings.ReplaceAll(exePath, "\\", "/"))
	switch {
	case strings.Contains(lower, "/.pnpm/") || strings.Contains(lower, "/pnpm/global/"):
		return "pnpm"
	case strings.Contains(lower, "/.yarn/global/") || strings.Contains(lower, "/yarn/global/"):
		return "yarn"
	case strings.Contains(lower, "/.bun/install/global/"):
		return "bun"
	default:
		return "npm"
	}
}

// npmUpdateCommand builds the update command for the given package manager.
// versionSpec is a concrete version ("1.40.0") or "latest".
//
// npmUpdateCommand 按包管理器构造更新命令。versionSpec 是具体版本
// （"1.40.0"）或 "latest"。
func npmUpdateCommand(pm, versionSpec string) string {
	switch pm {
	case "pnpm":
		return "pnpm add -g @agent_forge/forge@" + versionSpec
	case "yarn":
		return "yarn global add @agent_forge/forge@" + versionSpec
	case "bun":
		return "bun add -g @agent_forge/forge@" + versionSpec
	default:
		return "npm install -g @agent_forge/forge@" + versionSpec
	}
}

// getLatestVersionFromNPM fetches the latest published version of
// @agent_forge/forge from the npm registry (the /latest dist-tag document).
// The version must be plain semver — it gets pasted into a copy-paste
// command, so anything else is rejected outright.
//
// getLatestVersionFromNPM 从 npm registry 取 @agent_forge/forge 最新发布版本
// （/latest dist-tag 文档）。version 必须是纯 semver——它会被拼进可复制
// 命令，其余格式一律拒绝。
func getLatestVersionFromNPM() (string, error) {
	registry := os.Getenv("FORGE_NPM_REGISTRY")
	if registry == "" {
		registry = npmRegistryDefault
	}
	url := strings.TrimSuffix(registry, "/") + "/@agent_forge/forge/latest"

	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	// NB: the abbreviated-manifest accept type (vnd.npm.install-v1+json) is
	// only honored on the full packument — the /latest endpoint answers it
	// with 406. Verified out-of-band against registry.npmjs.org (2026-08-21);
	// the manual E2E lives outside the repo, so the Accept header is pinned
	// by TestGetLatestVersionFromNPM instead.
	//
	// 注意：缩略 manifest 的 accept 类型（vnd.npm.install-v1+json）只在完整
	// packument 上有效——/latest 端点对其回 406。已对 registry.npmjs.org
	// 带外实测（2026-08-21）；该手动 E2E 不在仓库内，故 Accept 头由
	// TestGetLatestVersionFromNPM 钉住。
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "forge-self-update")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("npm registry 返回 %d: %s", resp.StatusCode, string(body))
	}

	// Cap the success-path body too: a misbehaving mirror must not be able
	// to stream an unbounded document at us.
	//
	// 成功路径也限流：异常镜像不能对我们流式灌无界文档。
	var doc struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&doc); err != nil {
		return "", fmt.Errorf("解析 npm registry 响应失败: %w", err)
	}
	if doc.Version == "" {
		return "", fmt.Errorf("npm registry 响应缺少 version 字段")
	}
	if !semverPattern.MatchString(doc.Version) {
		return "", fmt.Errorf("npm registry 返回的 version 非法: %q", doc.Version)
	}
	return doc.Version, nil
}

// printNpmUpdateGuidance tells npm-channel users how to update, with the
// command matching their package manager. forge does not run the install
// itself: a global install may need elevation, and users may pin registries
// or versions — same philosophy as the --plugin flag (interactive steps are
// printed, not scripted).
//
// printNpmUpdateGuidance 告诉 npm 通道用户如何更新，命令随其包管理器
// 匹配。forge 不代跑安装：全局安装可能要提权，用户可能锁定 registry 或
// 版本——与 --plugin flag 同哲学（交互步骤只打印不脚本化）。
func printNpmUpdateGuidance(w io.Writer, latest string, pm string) {
	fmt.Fprintln(w, ``)
	fmt.Fprintln(w, `检测到 npm 通道安装（二进制位于 node_modules/@agent_forge 下）。`)
	fmt.Fprintln(w, `npm 包不可变，不做原地替换——请用对应的包管理器更新：`)
	fmt.Fprintf(w, "  %s\n", npmUpdateCommand(pm, latest))
	fmt.Fprintln(w, ``)
}

// printUpdateNotice writes the "new version available" notice with the
// update command matching the install channel.
//
// printUpdateNotice 写"有新版本"通知，更新命令随安装通道匹配。
func printUpdateNotice(w io.Writer, latest, current string, channel installChannel) {
	if channel.kind == channelNPM {
		fmt.Fprintf(w, "\n💡 Forge %s 可用（当前 %s）。运行 `%s` 更新。\n\n", latest, current, npmUpdateCommand(channel.pm, "latest"))
		return
	}
	fmt.Fprintf(w, "\n💡 Forge %s 可用（当前 %s）。运行 `forge update` 更新。\n\n", latest, current)
}
