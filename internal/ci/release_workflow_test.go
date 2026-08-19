package ci

// This package guards the structural integrity of forge's own release pipeline (.github/workflows/release.yml).
// It is the "CI anti-bypass" sandbox-verification layer: parse release.yml to assert the needs hard-dependency chain and trigger conditions,
// without triggering a real release — if someone removes `needs: test` or changes trigger conditions, this test goes red immediately.
//
// Historical lesson (2026-06, v0.27.0/v0.27.1): the needs chain in release.yml was correct,
// but the release was entirely bypassed by manual gh release + npm publish (skipping the workflow). This test guards the
// needs chain from being broken; bypassing the workflow manually is constrained by the release discipline in RELEASE.md at the repo root
// (that layer cannot be sandbox-verified — manual behavior is outside CI).
//
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
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// releaseJob keeps only the fields needed to guard the needs chain.
//
// releaseJob 只取守护 needs 链所需字段。
type releaseJob struct {
	// needs can be a scalar (needs: test) or a sequence (needs: [a, b]); received as-is via yaml.Node,
	// then normalized by needsList. Both forms are valid GitHub Actions syntax.
	//
	// needs 可能是标量（needs: test）或序列（needs: [a, b]），用 yaml.Node 原样接收，
	// 再由 needsList 归一化。GitHub Actions 两种写法都合法。
	Needs yaml.Node `yaml:"needs"`
	Steps []struct {
		Run string `yaml:"run"`
	} `yaml:"steps"`
}

// releaseWorkflow parses only jobs — under yaml.v3 (YAML 1.1 bool semantics) the top-level on: field
// is resolved as bool(true), so structurally parsing on would fail. Ignoring the on key does not affect jobs
// parsing (jobs is a plain string key); trigger-condition assertions go through raw text instead (see TestReleaseWorkflow_TagTriggered).
//
// releaseWorkflow 只解析 jobs——顶层 on: 字段在 yaml.v3（YAML 1.1 bool 语义）下
// 会被 resolve 成 bool(true)，结构化解析 on 会失败。on key 被忽略不影响 jobs
// 解析（jobs 是普通字符串 key）；触发条件断言改走原始文本（见 TestReleaseWorkflow_TagTriggered）。
type releaseWorkflow struct {
	Jobs map[string]releaseJob `yaml:"jobs"`
}

// needsList normalizes the needs yaml.Node into a list of strings.
//   - ScalarNode (needs: test) → ["test"]
//   - SequenceNode (needs: [a, b]) → ["a", "b"]
//   - No needs (test job) → nil
//
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
	// go test runs with cwd = internal/ci/, and release.yml is at the repo root .github/workflows/.
	//
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

// TestReleaseWorkflow_TagTriggered: releases may only be triggered by pushing a tag.
// Prevents someone from switching to branch-push triggers, which would release on every main push — that would bypass the "release only on explicit tag"
// discipline and run the needs chain on every push.
// The on field is asserted via raw text (see releaseWorkflow comment; the yaml.v3 on→bool pitfall).
//
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

// TestReleaseWorkflow_NeedsChain: guard the test→goreleaser→npm hard-dependency chain.
// This is the core of the "CI anti-bypass" mechanism — as long as releases go through release.yml, a failing test means goreleaser/npm do not run,
// so no bad packages are published. Sandbox verification: this test parses the yaml to assert needs, without triggering a real release.
//
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
		t.Fatalf("npm 必须 needs: [goreleaser]（npm 平台子包的二进制来自 goreleaser 构建并上传的 GitHub Release 产物），got %v", got)
	}

	// npm-verify：发布后从 npmjs 装回并断言 forge --version == tag——
	// 「发布后装机无人验证」缺口的收口。必须在 npm 之后（装的是 npm 刚发的版本）。
	npmVerify, ok := wf.Jobs["npm-verify"]
	if !ok {
		t.Fatal("release.yml 缺 npm-verify job（发布后装机验证——npm 发出去不代表用户装得回、版本对得上）")
	}
	if got := needsList(npmVerify.Needs); len(got) != 1 || got[0] != "npm" {
		t.Fatalf("npm-verify 必须 needs: [npm]（验证的是 npm 刚发布的版本），got %v", got)
	}
	hasInstallAssert := false
	for _, s := range npmVerify.Steps {
		if strings.Contains(s.Run, "npm i -g") && strings.Contains(s.Run, "--version") {
			hasInstallAssert = true
		}
	}
	if !hasInstallAssert {
		t.Fatal("npm-verify 必须 npm i -g 装回并断言 forge --version（缺断言则装机验证名存实亡）")
	}
}

// TestReleaseWorkflow_TestJobIsGateSource: the test job is the source of the needs chain —
// if it degrades (drops go test, drops -race), the whole anti-bypass chain becomes nominal (goreleaser needs an empty test).
// So the test job must run go test with -race (matching ci.yml).
//
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

// readGoreleaserYAML loads .goreleaser.yml from the repo root (go test cwd = internal/ci/).
//
// readGoreleaserYAML 从仓库根读 .goreleaser.yml（go test cwd = internal/ci/）。
func readGoreleaserYAML(t *testing.T) []byte {
	t.Helper()
	path := filepath.Join("..", "..", ".goreleaser.yml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读 .goreleaser.yml 失败: %v（cwd 是否在 internal/ci/?）", err)
	}
	return data
}

// goreleaserSign captures only the signs: block fields this guard inspects (cmd + args);
// yaml.v3 ignores the other signs fields (signature, artifacts, output).
//
// goreleaserSign 只取守护 signs: 块所需字段（cmd + args）；
// yaml.v3 忽略 signs 的其它字段（signature/artifacts/output）。
type goreleaserSign struct {
	Cmd  string   `yaml:"cmd"`
	Args []string `yaml:"args"`
}

// goreleaserSignsConfig holds the top-level signs list.
//
// goreleaserSignsConfig 持有顶层 signs 列表。
type goreleaserSignsConfig struct {
	Signs []goreleaserSign `yaml:"signs"`
}

// TestGoreleaserSigns_CosignV3Bundle: the checksums signing step must use cosign v3's --bundle
// format (single .sigstore.json, cert+signature merged). The v2-era --output-signature/
// --output-certificate flags resolve to an empty path under cosign v3
// ("create bundle file: open : no such file or directory") and broke the v1.28.3 release.
// This guard prevents silently regressing back to the deprecated flags.
//
// Structural parse of the signs args list (not raw text) — so explanatory comments that merely
// name the deprecated flags don't false-trip the guard.
//
// TestGoreleaserSigns_CosignV3Bundle：checksums 签名步必须用 cosign v3 的 --bundle
// 格式（单个 .sigstore.json，证书+签名合一）。v2 旧 --output-signature/
// --output-certificate 在 cosign v3 下解析成空路径
// （"create bundle file: open : no such file or directory"），致 v1.28.3 发布失败。
// 本守护防 silently 退化回废弃 flags。
//
// 只结构化解析 signs 的 args 列表（非原始文本）——注释里提到废弃 flags 不会误触发守护。
func TestGoreleaserSigns_CosignV3Bundle(t *testing.T) {
	var cfg goreleaserSignsConfig
	if err := yaml.Unmarshal(readGoreleaserYAML(t), &cfg); err != nil {
		t.Fatalf("unmarshal .goreleaser.yml signs: %v", err)
	}
	if len(cfg.Signs) == 0 {
		t.Fatal(".goreleaser.yml 缺 signs: 块（checksums keyless 签名）——" +
			"删签名会让 release 资产可被静默替换（Sigstore 透明日志可验证性丢失）")
	}
	for i, s := range cfg.Signs {
		joined := strings.Join(s.Args, " ")
		if !strings.Contains(joined, "--bundle") {
			t.Fatalf("signs[%d] 必须用 cosign v3 的 --bundle（证书+签名合一），got args %v——"+
				"v2 旧 --output-signature/--output-certificate 在 cosign v3 下产空路径致发布失败", i, s.Args)
		}
		if strings.Contains(joined, "--output-signature") || strings.Contains(joined, "--output-certificate") {
			t.Fatalf("signs[%d] 含 cosign v2 废弃 flags（--output-signature/--output-certificate），got args %v——"+
				"v3 下被忽略产空路径，须改 --bundle", i, s.Args)
		}
	}
}

// TestReleaseWorkflow_DispatchTrigger: release.yml must keep the workflow_dispatch trigger —
// it is the entry point of the release-please chain on the GITHUB_TOKEN path (tag pushes created
// by GITHUB_TOKEN do not trigger workflows; dispatch is the documented exception). Removing it
// silently stops every release that comes through a merged Release PR while no PAT is configured.
//
// TestReleaseWorkflow_DispatchTrigger：release.yml 必须保留 workflow_dispatch 触发器——
// 它是 release-please 链在 GITHUB_TOKEN 路径上的入口（GITHUB_TOKEN 产生的 tag push 不
// 触发 workflow，dispatch 是文档化例外）。删掉它，未配 PAT 期间所有经 Release PR 的
// 发版都会静默止步。
func TestReleaseWorkflow_DispatchTrigger(t *testing.T) {
	raw := string(readReleaseYAML(t))
	// 必须锚定触发器键本身（缩进 + workflow_dispatch: 至行尾），不能裸 Contains：
	// release.yml 的注释里就写着 workflow_dispatch——删掉真触发器、只留注释时
	// 裸 Contains 依然绿（审查发现的守卫注水）。
	if !regexp.MustCompile(`(?m)^\s*workflow_dispatch:\s*$`).MatchString(raw) {
		t.Fatal("release.yml 缺 workflow_dispatch 触发器（on: 下的键，非注释）——release-please.yml 的 GITHUB_TOKEN 路径" +
			"靠 dispatch 在新 tag 上调度本 workflow，删掉则合并 Release PR 后不再发版")
	}
}

// TestGoreleaserReleaseMode_KeepsExisting: goreleaser must not replace the release body —
// release-please pre-creates the GitHub Release with the curated changelog (grouped sections +
// PR links); mode: replace would clobber it with a bare commit list. keep-existing uploads
// artifacts onto the existing release and keeps the body. On the manual-tag path (no
// pre-existing release) goreleaser creates the release itself, unchanged.
//
// TestGoreleaserReleaseMode_KeepsExisting：goreleaser 不得 replace release 正文——
// release-please 先建好带整理 changelog（分组 + PR 链接）的 GitHub Release；
// mode: replace 会把它覆盖成裸 commit 列表。keep-existing 往现存 release 上挂资产、
// 保留正文。手动打 tag 路径无预置 Release，goreleaser 照常自建（行为不变）。
func TestGoreleaserReleaseMode_KeepsExisting(t *testing.T) {
	var cfg struct {
		Release struct {
			Mode string `yaml:"mode"`
		} `yaml:"release"`
	}
	if err := yaml.Unmarshal(readGoreleaserYAML(t), &cfg); err != nil {
		t.Fatalf("unmarshal .goreleaser.yml release: %v", err)
	}
	if cfg.Release.Mode != "keep-existing" {
		t.Fatalf("release.mode 必须为 keep-existing（保留 release-please 的 changelog 正文，只挂资产），got %q——"+
			"replace 会把正文覆盖成裸 commit 列表", cfg.Release.Mode)
	}
}
