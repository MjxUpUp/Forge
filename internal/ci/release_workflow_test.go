package ci

// 本包守护 forge 自身发布链路（.github/workflows/release.yml）的结构不变质。
// 这是"CI 防绕过"的沙盒验证层：解析 release.yml 断言 needs 强依赖链和触发条件，
// 不触发真实 release——未来有人误删 needs: test / 改触发条件，本测试立刻红。
//
// 历史教训（2026-06，v0.27.0/v0.27.1）：release.yml 的 needs 链本身是对的，
// 但发版被手动 gh release + npm publish 整个绕过（没走 workflow）。本测试守护
// needs 链不被破坏；手动绕过 workflow 本身靠 根目录 RELEASE.md 的发布纪律约束
// （那层无法沙盒验证——手动行为不在 CI 内）。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// releaseJob 只取守护 needs 链所需字段。
type releaseJob struct {
	// needs 可能是标量（needs: test）或序列（needs: [a, b]），用 yaml.Node 原样接收，
	// 再由 needsList 归一化。GitHub Actions 两种写法都合法。
	Needs yaml.Node `yaml:"needs"`
	Steps []struct {
		Run string `yaml:"run"`
	} `yaml:"steps"`
}

// releaseWorkflow 只解析 jobs——顶层 on: 字段在 yaml.v3（YAML 1.1 bool 语义）下
// 会被 resolve 成 bool(true)，结构化解析 on 会失败。on key 被忽略不影响 jobs
// 解析（jobs 是普通字符串 key）；触发条件断言改走原始文本（见 TestReleaseWorkflow_TagTriggered）。
type releaseWorkflow struct {
	Jobs map[string]releaseJob `yaml:"jobs"`
}

// needsList 把 needs yaml.Node 归一化为字符串列表。
//   - ScalarNode（needs: test）→ ["test"]
//   - SequenceNode（needs: [a, b]）→ ["a", "b"]
//   - 无 needs（test job）→ nil
func needsList(n yaml.Node) []string {
	switch n.Kind {
	case yaml.ScalarNode:
		return []string{n.Value}
	case yaml.SequenceNode:
		out := make([]string, 0, len(n.Content))
		for _, c := range n.Content {
			out = append(out, c.Value)
		}
		return out
	default:
		return nil
	}
}

func readReleaseYAML(t *testing.T) []byte {
	t.Helper()
	// go test 运行时 cwd = internal/ci/，release.yml 在仓库根 .github/workflows/。
	path := filepath.Join("..", "..", ".github", "workflows", "release.yml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读 release.yml 失败: %v（cwd 是否在 internal/ci/?）", err)
	}
	return data
}

func loadReleaseWorkflow(t *testing.T) *releaseWorkflow {
	t.Helper()
	var wf releaseWorkflow
	if err := yaml.Unmarshal(readReleaseYAML(t), &wf); err != nil {
		t.Fatalf("unmarshal release.yml jobs: %v", err)
	}
	return &wf
}

// TestReleaseWorkflow_TagTriggered：发版只能由打 tag 触发。
// 防止有人改成 push 分支触发，导致每次 main 推送都发版——那会绕过"显式打 tag 才发版"
// 的纪律，且让 needs 链在每次推送时都跑一遍。
// on 字段走原始文本断言（见 releaseWorkflow 注释，yaml.v3 on→bool 坑）。
func TestReleaseWorkflow_TagTriggered(t *testing.T) {
	raw := string(readReleaseYAML(t))
	if !strings.Contains(raw, "tags:") {
		t.Fatal("release.yml 必须由 tag push 触发（on.push.tags），现未发现 tags: 触发条件——" +
			"改成分支触发会让每次 main 推送都发版，绕过显式发版纪律")
	}
	if !strings.Contains(raw, `"v*"`) {
		t.Fatalf(`release.yml on.push.tags 必须匹配 "v*" 模式（当前打 v* tag 才发版）`)
	}
}

// TestReleaseWorkflow_NeedsChain：守护 test→goreleaser→npm 强依赖链。
// 这是"CI 防绕过"机制核心——只要发版走 release.yml，test 失败则 goreleaser/npm 都不跑，
// 不会发出坏包。沙盒验证：本测试解析 yaml 断言 needs，无需触发真实 release。
func TestReleaseWorkflow_NeedsChain(t *testing.T) {
	wf := loadReleaseWorkflow(t)

	goreleaser, ok := wf.Jobs["goreleaser"]
	if !ok {
		t.Fatal("release.yml 缺 goreleaser job（发二进制）")
	}
	if got := needsList(goreleaser.Needs); len(got) != 1 || got[0] != "test" {
		t.Fatalf("goreleaser 必须 needs: [test]（test 失败则不发二进制），got %v——"+
			"删掉此 needs 会让 test 失败仍发版，破坏 CI 防绕过链", got)
	}

	npm, ok := wf.Jobs["npm"]
	if !ok {
		t.Fatal("release.yml 缺 npm job（发 @agent_forge/forge）")
	}
	if got := needsList(npm.Needs); len(got) != 1 || got[0] != "goreleaser" {
		t.Fatalf("npm 必须 needs: [goreleaser]（二进制先发，npm 包 install.js 才能下载到平台二进制），got %v", got)
	}
}

// TestReleaseWorkflow_TestJobIsGateSource：test job 是 needs 链的源头——
// 它若退化（去掉 go test、去掉 -race），整条防绕过链就名存实亡（goreleaser needs 一个空 test）。
// 故 test job 必须跑 go test 且带 -race（与 ci.yml 一致）。
func TestReleaseWorkflow_TestJobIsGateSource(t *testing.T) {
	wf := loadReleaseWorkflow(t)
	test, ok := wf.Jobs["test"]
	if !ok {
		t.Fatal("release.yml 缺 test job（needs 链源头）")
	}
	hasTest, hasRace := false, false
	for _, s := range test.Steps {
		if strings.Contains(s.Run, "go test") {
			hasTest = true
		}
		if strings.Contains(s.Run, "-race") {
			hasRace = true
		}
	}
	if !hasTest {
		t.Fatal("test job 必须跑 go test（needs 链源头）——现 steps 无 go test，" +
			"goreleaser needs 的就是一个空 test，防绕过链失效")
	}
	if !hasRace {
		t.Fatal("test job 必须带 -race（与 ci.yml 一致的竞态检测标准）——现未发现 -race")
	}
}
