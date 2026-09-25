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
	// 键集完整性:严格相等循环只遍历**存在的**键——键被删则守卫静默缩圈。
	// 5 平台键与 config 侧 5 条 jsonpath、release.js 的 hits===5 三方互锁。
	if len(main.Optional) != 5 {
		t.Fatalf("optionalDependencies 键数 %d != 5（键被增删——与 release-please-config 的 5 条 jsonpath 及 scripts/release.js 的 replacePlatformPins 三方互锁,增删须同步三处）", len(main.Optional))
	}
	// 语义化 X.Y.Z 格式面保留(原 previousVersion 解析旁的检查):格式坏会让
	// release-please/manifest 同步守卫的对照全部失真,这里先红。
	var maj, min, patch int
	if _, err := fmt.Sscanf(main.Version, "%d.%d.%d", &maj, &min, &patch); err != nil {
		t.Fatalf("主包版本 %q 非语义化 X.Y.Z", main.Version)
	}
	// 严格相等契约（2026-09-18 收紧）:optionalDependencies 钉随 release 火车自动
	// bump（release-please-config.json 的 5 条 optionalDependencies jsonpath,
	// fix/npm-pins-automation)后,Release PR 内 version/钉/平台包三者同步移动——
	// 提交态任何不相等都是漂移。旧的「滞后一版合法」宽容是为手工对齐仪式留的
	// 瞬态窗口（#72/#74/#77 三轮 npm-align 的债务根源,曾致 v1.65.0 发布失败）
	// ——自动化落地后宽容口即漏洞口:钉停在前一版而该版未发布(如被跳过的
	// 1.65.0)会让安装时 optionalDependencies 解析不到平台包。
	for pkg, pinned := range main.Optional {
		if pinned != main.Version {
			t.Errorf("optionalDependencies[%s] 钉在 %s，主包 %s（严格相等契约——火车自动 bump 后不相等即漂移；若 Release PR 漏 bump 钉,查 release-please-config.json 的 optionalDependencies jsonpath 是否被删）", pkg, pinned, main.Version)
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
		if plat.Version != main.Version {
			t.Errorf("平台包 %s 提交态版本 %s != 主包 %s（严格相等契约——extra-files 同 PR bump,不相等即绕过单一真相源手改）", short, plat.Version, main.Version)
		}
	}
}

// previousVersion（「合法滞后一版」基线算术）随宽容口径一并退役:火车自动 bump
// 钉后严格相等是唯一合法态,滞后即红。历史:2026-09-09 修复其 Sscanf 双分支只认
// ".0" 结尾的 bug(1.55.1 误判非语义化,发版 test job 必红)——教训由上方的
// 格式面检查继续承载。
