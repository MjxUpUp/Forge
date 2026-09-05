package cli

// npm_versions_test.go — npm 版本对齐守卫（mechanism-hardening P0-2）：
// 主包与五个平台子包的版本必须相等。发布 workflow 用 jq 注入正确版本
//（release.yml L104/132——发布态是对齐的），但仓内提交态曾漂移 22 个 minor
//（1.50.0 vs 1.28.2）——审计误导 + 绕过 workflow 手工发布即真漂移。守卫把
// "提交态漂移"变成 CI 红灯。esbuild 式逐版本互锁惯例的机械执法点。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNpmPlatformVersionsAligned(t *testing.T) {
	mainPath := filepath.Join(repoRoot, "npm", "package.json")
	mainBody, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatalf("读 npm/package.json: %v", err)
	}
	var main struct {
		Version  string            `json:"version"`
		Optional map[string]string `json:"optionalDependencies"`
	}
	if err := json.Unmarshal(mainBody, &main); err != nil {
		t.Fatalf("解析: %v", err)
	}
	if main.Version == "" {
		t.Fatal("主包无版本号")
	}
	for pkg, want := range main.Optional {
		if want != main.Version {
			t.Errorf("optionalDependencies[%s] 钉在 %s，主包为 %s——须对齐（发布 workflow 会注入正确值，提交态不得漂移）", pkg, want, main.Version)
		}
		// @agent_forge/forge-win32-x64 → platforms/win32-x64（剥 @scope/forge- 前缀）。
		short := strings.TrimPrefix(pkg, "@agent_forge/forge-")
		if short == pkg {
			t.Errorf("unexpected optionalDependency %q（非 @agent_forge/forge- 命名）", pkg)
			continue
		}
		platPath := filepath.Join(repoRoot, "npm", "platforms", short, "package.json")
		platBody, err := os.ReadFile(platPath)
		if err != nil {
			t.Errorf("读 %s: %v", platPath, err)
			continue
		}
		var plat struct {
			Version string `json:"version"`
		}
		if err := json.Unmarshal(platBody, &plat); err != nil {
			t.Errorf("解析 %s: %v", platPath, err)
			continue
		}
		if plat.Version != main.Version {
			t.Errorf("平台包 %s 提交态版本 %s != 主包 %s（发布时会注入，但提交态漂移会误导审计且绕过 workflow 手工发布即真漂移）", short, plat.Version, main.Version)
		}
	}
}
