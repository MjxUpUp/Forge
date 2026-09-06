package cli

// npm_versions_test.go — npm 版本对齐守卫（mechanism-hardening P0-2）：
// 主包与五个平台子包的版本必须相等。发布 workflow 用 jq 注入正确版本
//（release.yml L104/132——发布态是对齐的），但仓内提交态曾漂移 22 个 minor
//（1.50.0 vs 1.28.2）——审计误导 + 绕过 workflow 手工发布即真漂移。守卫把
// "提交态漂移"变成 CI 红灯。esbuild 式逐版本互锁惯例的机械执法点。

import (
	"encoding/json"
	"fmt"
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
	// 发布窗口语义（v1.51.0 发版实证）：release-please 只 bump 主包
	// package.json，平台子包的提交态版本由发布 workflow 在 publish 时用 jq 注入
	// ——所以「主包=下一版、平台包=上一版」是每个发布窗口的**合法中间态**。
	// 守卫抓的是**多版本漂移**（1.50.0 vs 1.28.2 那种 22 个 minor 的遗忘）
	// 与平台包间互不一致；单版本滞后放行。
	prev, ok := previousMinor(main.Version)
	if !ok {
		t.Fatalf("主包版本 %q 非语义化 X.Y.Z", main.Version)
	}
	for pkg, pinned := range main.Optional {
		if pinned != main.Version && pinned != prev {
			t.Errorf("optionalDependencies[%s] 钉在 %s，主包 %s（合法态：相等或前一版 %s——更大差距为提交态漂移）", pkg, pinned, main.Version, prev)
		}
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
		if plat.Version != pinned {
			t.Errorf("平台包 %s 提交态版本 %s != optionalDependencies 钉的 %s——两处清单本身不一致（发布时会注入，但清单互斥漂移说明有人绕过单一真相源手改）", short, plat.Version, pinned)
		}
		if plat.Version != main.Version && plat.Version != prev {
			t.Errorf("平台包 %s 提交态版本 %s 相对主包 %s 超出单版本发布窗口（多版本漂移——上一轮 1.50.0 vs 1.28.2 的形态）", short, plat.Version, main.Version)
		}
	}
}

// previousMinor 返回 X.Y.Z 的前一 minor 版本串（X.Y-1.0；Y=0 时回退
// "X-1.<任意>" 不可表达——返回 ok=false 由调用方按非法处理；主包 major=0 或
// Y=0 的场景本仓不存在，不值得为此建全 semver 库）。
func previousMinor(v string) (string, bool) {
	var maj, min int
	if _, err := fmt.Sscanf(v, "%d.%d.0", &maj, &min); err == nil && min > 0 {
		return fmt.Sprintf("%d.%d.0", maj, min-1), true
	}
	if _, err := fmt.Sscanf(v, "%d.%d.%*s", &maj, &min); err == nil && min > 0 {
		return fmt.Sprintf("%d.%d.0", maj, min-1), true
	}
	return "", false
}
