package ci

// release-please 层守卫：release-please-config.json / .release-please-manifest.json /
// .github/workflows/release-please.yml。
//
// 发版两层管道：release-please 管「何时发版、发什么版本」（changelog、版本 bump、tag、
// GitHub Release 正文）；release.yml 管「怎么构建发布」（goreleaser + npm）。两层经
// workflow_dispatch 串联。本组守卫钉住交接形状，防配置漂移悄悄断链：
//   - tag 形状必须保持 v<semver>：release.yml 由 on.push.tags "v*" 触发，npm job 硬编码
//     资产 URL releases/download/v<ver>/...；
//   - extra-files 必须持续 bump npm/package.json、.kimi-plugin/plugin.json 与
//     plugins/forge-dsh/package.json——正是 tag 对账门禁、
//     TestKimiPluginManifestVersionTracksRelease 与 dsh 发布步读的三个文件；
//   - manifest 版本必须等于 npm/package.json 版本：绕过 release-please 的手动发版会让
//     它失同步而变红（刻意的设计——把人推回 release-please 路径；release-please 从旧
//     版本起算下一版会撞已存在 tag）；
//   - action 必须 SHA pin（与 release.yml 同一供应链姿势），dispatch 步必须指向 release.yml。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// readRepoFile 读仓库根相对路径的文件（go test cwd = internal/ci/）。
func readRepoFile(t *testing.T, elem ...string) []byte {
	t.Helper()
	paths := append([]string{"..", ".."}, elem...)
	data, err := os.ReadFile(filepath.Join(paths...))
	if err != nil {
		t.Fatalf("读 %s 失败: %v（cwd 是否在 internal/ci/?）", filepath.Join(elem...), err)
	}
	return data
}

// releasePleaseExtraFile 对应 release-please-config.json 的一条 extra-files 配置。
type releasePleaseExtraFile struct {
	Type     string `json:"type"`
	Path     string `json:"path"`
	JSONPath string `json:"jsonpath"`
}

// releasePleasePackage 是单包配置；只取本守卫检查的字段。
type releasePleasePackage struct {
	ExtraFiles []releasePleaseExtraFile `json:"extra-files"`
}

// releasePleaseConfig 持根配置 flag。布尔用指针：缺 key 与显式 false 必须可区分——
// 下方守卫断言 flag 存在且取要求值，而非恰好为假（不能靠 release-please 的默认值
// 撑住发版链）。
type releasePleaseConfig struct {
	ReleaseType           string                          `json:"release-type"`
	IncludeComponentInTag *bool                           `json:"include-component-in-tag"`
	IncludeVInTag         *bool                           `json:"include-v-in-tag"`
	BootstrapSHA          string                          `json:"bootstrap-sha"`
	Packages              map[string]releasePleasePackage `json:"packages"`
}

func loadReleasePleaseConfig(t *testing.T) *releasePleaseConfig {
	t.Helper()
	var cfg releasePleaseConfig
	if err := json.Unmarshal(readRepoFile(t, "release-please-config.json"), &cfg); err != nil {
		t.Fatalf("unmarshal release-please-config.json: %v", err)
	}
	return &cfg
}

// TestReleasePleaseConfig_TagShape pins the tag format to bare v<semver>.
//
// TestReleasePleaseConfig_TagShape：钉死 tag 格式为裸 v<semver>。
// release.yml 由 on.push.tags "v*" 触发，npm job 硬编码资产 URL
// releases/download/v${VER}/forge_${VER}_*.tar.gz——组件前缀 tag（forge-v1.2.3）或
// 无 v tag（1.2.3）都会让整个构建层与 release-please 失联。
func TestReleasePleaseConfig_TagShape(t *testing.T) {
	cfg := loadReleasePleaseConfig(t)

	if cfg.IncludeComponentInTag == nil || *cfg.IncludeComponentInTag {
		t.Fatal("include-component-in-tag 必须存在且为 false——" +
			"组件前缀 tag（如 forge-v1.2.3）不匹配 release.yml 的 on.push.tags \"v*\"，" +
			"npm 资产 URL 也对不上，整条构建链失联")
	}
	if cfg.IncludeVInTag == nil || !*cfg.IncludeVInTag {
		t.Fatal("include-v-in-tag 必须存在且为 true——" +
			"无 v 前缀 tag（如 1.2.3）不匹配 release.yml 的 on.push.tags \"v*\"" +
			"（显式钉住，防 go 策略默认值漂移）")
	}
}

// TestReleasePleaseConfig_StrategyAndBootstrap pins the go strategy and bootstrap-sha requirements.
//
// TestReleasePleaseConfig_StrategyAndBootstrap：go 策略无需树内版本文件（发版版本来自
// tag + manifest）；node/simple 策略会在根目录找 package.json / version.txt 而失败。
// bootstrap-sha 限定首个 Release PR 的 commit 收集范围（之后被忽略）——须为完整
// 40 位 SHA。
func TestReleasePleaseConfig_StrategyAndBootstrap(t *testing.T) {
	cfg := loadReleasePleaseConfig(t)
	if cfg.ReleaseType != "go" {
		t.Fatalf("release-type 须为 go（版本来自 tag+manifest，无需树内版本文件；"+
			"node/simple 会在根目录找 package.json/version.txt 而失败），got %q", cfg.ReleaseType)
	}
	if !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(cfg.BootstrapSHA) {
		t.Fatalf("bootstrap-sha 须为 40 位完整 commit SHA（限定首个 Release PR 的收集范围），got %q", cfg.BootstrapSHA)
	}
}

// TestReleasePleaseConfig_ExtraFilesBumpAllManifests: the Release PR must bump the same files the downstream gates read.
//
// TestReleasePleaseConfig_ExtraFilesBumpAllManifests：Release PR 必须 bump 下游门禁读的
// 同一批文件。丢 npm/package.json 会破 release.yml 的 tag 对账门禁；丢
// .kimi-plugin/plugin.json 会破 TestKimiPluginManifestVersionTracksRelease；丢
// plugins/forge-dsh/package.json 则 dsh 发布步永远看不到新版本（静默 skip——插件重新
// 游离出发版火车）。在此同时守卫，让失败在配置层就点名根因，而不是下游报噪声。
func TestReleasePleaseConfig_ExtraFilesBumpAllManifests(t *testing.T) {
	cfg := loadReleasePleaseConfig(t)
	pkg, ok := cfg.Packages["."]
	if !ok {
		t.Fatal(`release-please-config.json 缺根包 packages["."]（本仓单包发版，版本 bump 挂在根包）`)
	}
	bumped := map[string]string{}
	for _, f := range pkg.ExtraFiles {
		if f.Type == "json" {
			bumped[f.Path] = f.JSONPath
		}
	}
	for path, wantJSONPath := range map[string]string{
		"npm/package.json":               "$.version",
		".kimi-plugin/plugin.json":       "$.version",
		"plugins/forge-dsh/package.json": "$.version",
	} {
		if bumped[path] != wantJSONPath {
			t.Fatalf("extra-files 缺 {type:json, path:%s, jsonpath:%s}——Release PR 不再 bump 此文件，"+
				"下游 tag 对账门禁/TestKimiPluginManifestVersionTracksRelease/dsh 发布步会红; got extra-files %+v",
				path, wantJSONPath, pkg.ExtraFiles)
		}
	}
}

// TestReleasePleaseManifest_MatchesNpmVersion: .release-please-manifest.json is release-please's "last released version" ledger.
//
// TestReleasePleaseManifest_MatchesNpmVersion：.release-please-manifest.json 是 release-please
// 的「上次已发布版本」账本。每个 Release PR 都会把它与 npm/package.json 一起更新，
// 失同步只可能来自绕过 release-please 的手动发版（旧 release.js 路径——把 manifest
// 同步到实际发布版本）或手改文件。
func TestReleasePleaseManifest_MatchesNpmVersion(t *testing.T) {
	var manifest map[string]string
	if err := json.Unmarshal(readRepoFile(t, ".release-please-manifest.json"), &manifest); err != nil {
		t.Fatalf("unmarshal .release-please-manifest.json: %v", err)
	}
	var pkg struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(readRepoFile(t, "npm", "package.json"), &pkg); err != nil {
		t.Fatalf("unmarshal npm/package.json: %v", err)
	}
	got, ok := manifest["."]
	if !ok {
		t.Fatal(`.release-please-manifest.json 缺根包 "." 条目——release-please 拿不到「上次发布版本」，无法起算下一版本`)
	}
	if got != pkg.Version {
		t.Fatalf("manifest 版本 %q != npm/package.json 版本 %q——绕过 release-please 的手动发版/手改文件造成失同步；"+
			"release-please 会从旧版本起算下一版并撞已存在 tag。请把 manifest 同步到实际发布版本", got, pkg.Version)
	}
}

// TestReleasePleaseManifest_DshPluginTracksTrain: @agent_forge/forge-dsh rides the main release train — its package.json is in extra-files, so every Release PR bumps it in lockstep with the root version.
//
// TestReleasePleaseManifest_DshPluginTracksTrain：@agent_forge/forge-dsh 随主发布火车——
// 它的 package.json 在 extra-files 里，每个 Release PR 都会把它与根版本 lockstep bump。
// 此处失同步只可能来自手改插件版本号（插件游离于自动发版之外的旧手动 bump 路径）或
// extra-files 条目被丢；release.yml 的 dsh 发布步随后会发出与 tag 对不上的版本。
func TestReleasePleaseManifest_DshPluginTracksTrain(t *testing.T) {
	var manifest map[string]string
	if err := json.Unmarshal(readRepoFile(t, ".release-please-manifest.json"), &manifest); err != nil {
		t.Fatalf("unmarshal .release-please-manifest.json: %v", err)
	}
	var pkg struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(readRepoFile(t, "plugins", "forge-dsh", "package.json"), &pkg); err != nil {
		t.Fatalf("unmarshal plugins/forge-dsh/package.json: %v", err)
	}
	got, ok := manifest["."]
	if !ok {
		t.Fatal(`.release-please-manifest.json 缺根包 "." 条目——release-please 拿不到「上次发布版本」，无法起算下一版本`)
	}
	if got != pkg.Version {
		t.Fatalf("plugins/forge-dsh/package.json 版本 %q != manifest 版本 %q——dsh 插件随主火车 lockstep 发版，"+
			"版本号由 Release PR 经 extra-files 统一 bump，不要手改；请把它同步回根版本", pkg.Version, got)
	}
}

// TestReleasePleaseWorkflow_PinnedAndDispatchesRelease: the workflow must (a) pin the action by full SHA.
//
// TestReleasePleaseWorkflow_PinnedAndDispatchesRelease：workflow 必须 (a) pin 完整 SHA——
// 浮动 @v5 tag 可被上游重指向（与 release.yml 的 action pin 同一供应链姿势）；
// (b) 按文件名 dispatch release.yml 到新 tag——GITHUB_TOKEN 路径上 release-please 层到
// 构建层的唯一连接；(c) dispatch 以 release_created 为前提，普通 PR 刷新不调度构建层。
func TestReleasePleaseWorkflow_PinnedAndDispatchesRelease(t *testing.T) {
	raw := string(readRepoFile(t, ".github", "workflows", "release-please.yml"))
	if !regexp.MustCompile(`googleapis/release-please-action@[0-9a-f]{40}`).MatchString(raw) {
		t.Fatal("release-please-action 必须 pin 完整 40 位 commit SHA" +
			"（浮动 @v5 可被上游重指向；与 release.yml 的 SHA pin 同姿势）")
	}
	if !strings.Contains(raw, "gh workflow run release.yml") {
		t.Fatal("release-please.yml 必须 gh workflow run release.yml --ref <tag> 串联构建层——" +
			"GITHUB_TOKEN 产生的 tag push 不触发 workflow，缺这步则发版止步于 GitHub Release，无二进制无 npm 包")
	}
	// dispatch 命令必须显式 --repo：release-please workflow 不 checkout（workspace 无 .git），
	// gh 无 --repo 时从当前 git 仓库推断会 fatal: not a git repository（v1.38.0 首发事故，
	// tag/Release 已建、构建层没被调度）。锚定行首防注释满足。
	if !regexp.MustCompile(`(?m)^\s*gh workflow run release\.yml\b[^\n]*--repo\b`).MatchString(raw) {
		t.Fatal("dispatch 命令必须显式 --repo \"$GITHUB_REPOSITORY\"——本 workflow 不 checkout（无 .git），" +
			"gh 缺 --repo 时从 git 上下文推断仓库直接 fatal（v1.38.0 首发：tag 建了、构建层没跑）")
	}
	if !strings.Contains(raw, "release_created") {
		t.Fatal("dispatch 步必须以 release_created 输出为前提——否则每次普通 PR 刷新也调度构建层")
	}
}

// TestReleasePleaseWorkflow_NoSecretsInIf pins that no `if:` expression references the secrets context.
//
// TestReleasePleaseWorkflow_NoSecretsInIf：GitHub 静态拒绝任何引用 secrets 上下文的
// if: 表达式——secrets 不在 steps.if/job.if 的上下文白名单，整个 workflow 文件校验
// 失败：run 记录 0s 死、0 jobs、无日志，且不匹配分支的推送也产生 failure 记录
// （2026-08-19 首跑事故，scratch 分支推送二分定位）。双 token 路径的 PAT 检测必须
// 留在 run 脚本内：env 可引用 secrets，if: 不行。
func TestReleasePleaseWorkflow_NoSecretsInIf(t *testing.T) {
	raw := string(readRepoFile(t, ".github", "workflows", "release-please.yml"))
	if regexp.MustCompile(`(?m)^\s*if:.*secrets\.`).MatchString(raw) {
		t.Fatal("release-please.yml 的 if: 表达式引用了 secrets. 上下文——GitHub 静态校验会拒绝整个" +
			" workflow 文件（run 0s 失败、0 jobs、无日志）。PAT 检测须放 run 脚本读 env（env 可引用 secrets）")
	}
}
