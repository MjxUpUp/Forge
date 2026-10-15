package taskpipeline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/artifactchain"
	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/forgedata"
)

// writeChainSchema 把 schema.yaml 写进测试项目（FORGE_DATA_HOME 隔离后
// SchemaPath 才落在临时目录内——与 planfirst 测试同一隔离纪律）。
func writeChainSchema(t *testing.T, dir, schema string) {
	t.Helper()
	p := artifactchain.SchemaPath(dir)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("MkdirAll schema dir: %v", err)
	}
	if err := os.WriteFile(p, []byte(schema), 0o644); err != nil {
		t.Fatalf("写 schema.yaml: %v", err)
	}
}

func findArtifactChainEntry(t *testing.T, dir string) *checklog.Entry {
	t.Helper()
	entries, err := checklog.LoadAll(dir)
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	for i := range entries {
		if entries[i].Check == checklog.CheckArtifactChain {
			return &entries[i]
		}
	}
	return nil
}

// TestExecuteTaskGate_ArtifactChainAdvisoryDefault 钉住默认链零阻断契约：
// 无 schema.yaml（默认链全 advisory）时产物全缺 → gate 仍 PASS，落一条
// artifact-chain advisory（Passed=false），ArtifactAdvisoryFired 持久化后重试静默。
func TestExecuteTaskGate_ArtifactChainAdvisoryDefault(t *testing.T) {
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	writeCommitSource(t, dir, map[string]string{
		"foo.go": "package main\n\nfunc Foo() int { return 1 }\n",
	}, "add foo")

	state := &TaskState{TaskRef: "chain-advisory", Branch: "feat/chain"}
	stderr := captureStderr(t, func() {
		res, err := ExecuteTaskGate(dir, "task-implement", state)
		if err != nil || !res.Passed {
			t.Fatalf("默认链 advisory 绝不阻断: res=%+v err=%v", res, err)
		}
	})
	if !strings.Contains(stderr, "artifact") && !strings.Contains(stderr, "产物链") {
		t.Errorf("首次应打印产物缺失 advisory: %q", stderr)
	}
	if !state.ArtifactAdvisoryFired {
		t.Error("首次后 ArtifactAdvisoryFired 应置位")
	}
	if e := findArtifactChainEntry(t, dir); e == nil || e.Passed || e.Level != checklog.LevelAdvisory {
		t.Errorf("应落一条 advisory artifact-chain 条目: %+v", e)
	}

	// 从磁盘重载重试：advisory 不重发（每任务一次）。
	reloaded, err := LoadTaskState(dir, "chain-advisory")
	if err != nil {
		t.Fatalf("LoadTaskState: %v", err)
	}
	stderr2 := captureStderr(t, func() {
		if _, err := ExecuteTaskGate(dir, "task-implement", reloaded); err != nil {
			t.Fatalf("重试应 PASS: %v", err)
		}
	})
	if strings.Contains(stderr2, "产物链") {
		t.Errorf("重试不应重发产物链 advisory: %q", stderr2)
	}
}

// TestExecuteTaskGate_ArtifactChainHardBlock 钉住 hard 档只守事实：缺失阻断、
// 登记放行、漂移再阻断——三条都是存在/哈希事实，不涉及内容意见。
func TestExecuteTaskGate_ArtifactChainHardBlock(t *testing.T) {
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	writeCommitSource(t, dir, map[string]string{
		"foo.go": "package main\n\nfunc Foo() int { return 1 }\n",
	}, "add foo")
	writeChainSchema(t, dir, "version: 1\nstages:\n  - name: proposal\n  - name: spec\n    mode: hard\n    requires: [proposal]\n")

	state := &TaskState{TaskRef: "chain-hard", Branch: "feat/chain"}
	res, err := ExecuteTaskGate(dir, "task-implement", state)
	if err != nil {
		t.Fatalf("gate 返回错误形态: %v", err)
	}
	if res.Passed {
		t.Fatal("hard 档产物缺失应 BLOCKED")
	}
	if !strings.Contains(res.Message, "产物未登记") || !strings.Contains(res.Message, "--set spec") {
		t.Errorf("BLOCKED 文案应带原因与唯一下一步命令: %q", res.Message)
	}

	// 前置缺失（有 spec 无 proposal）也应阻断——链序语义。
	if aref, aerr := WriteArtifact(dir, state.TaskRef, "spec", "# 规格\n\naccept: true :: \n"); aerr != nil {
		t.Fatalf("WriteArtifact: %v", aerr)
	} else {
		state.SpecArtifacts = map[string]ArtifactRef{"spec": aref}
	}
	res2, err := ExecuteTaskGate(dir, "task-implement", state)
	if err != nil {
		t.Fatalf("gate 返回错误形态: %v", err)
	}
	if res2.Passed || !strings.Contains(res2.Message, "前置") {
		t.Fatalf("hard 档前置缺失应阻断: %+v", res2)
	}

	// 补齐 proposal → PASS。
	if aref, aerr := WriteArtifact(dir, state.TaskRef, "proposal", "# 提案\n\n解决一个真问题。\n"); aerr != nil {
		t.Fatalf("WriteArtifact: %v", aerr)
	} else {
		state.SpecArtifacts["proposal"] = aref
	}
	res3, err := ExecuteTaskGate(dir, "task-implement", state)
	if err != nil || !res3.Passed {
		t.Fatalf("产物齐全后应 PASS: %+v err=%v", res3, err)
	}

	// 登记后手改文件 → 漂移 → 再阻断（hard 档守哈希事实）。
	specPath := filepath.Join(forgedata.DataDirFor(dir), filepath.FromSlash(state.SpecArtifacts["spec"].Path))
	if err := os.WriteFile(specPath, []byte("# 规格\n\n内容被人改过。\n"), 0o644); err != nil {
		t.Fatalf("手改产物文件: %v", err)
	}
	res4, err := ExecuteTaskGate(dir, "task-implement", state)
	if err != nil {
		t.Fatalf("gate 返回错误形态: %v", err)
	}
	if res4.Passed || !strings.Contains(res4.Message, "漂移") {
		t.Fatalf("hard 档漂移应阻断: %+v", res4)
	}
}

// TestExecuteTaskGate_ArtifactChainHumanApproval 钉住 human 档：审批缺失阻断、
// 审批哈希匹配放行、文件改动后审批哈希失配再阻断——审批签的是内容。
func TestExecuteTaskGate_ArtifactChainHumanApproval(t *testing.T) {
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	writeCommitSource(t, dir, map[string]string{
		"foo.go": "package main\n\nfunc Foo() int { return 1 }\n",
	}, "add foo")
	writeChainSchema(t, dir, "version: 1\nstages:\n  - name: spec\n    mode: human\n")

	state := &TaskState{TaskRef: "chain-human", Branch: "feat/chain"}
	aref, aerr := WriteArtifact(dir, state.TaskRef, "spec", "# 规格\n\nv1 内容。\n")
	if aerr != nil {
		t.Fatalf("WriteArtifact: %v", aerr)
	}
	state.SpecArtifacts = map[string]ArtifactRef{"spec": aref}

	if res, err := ExecuteTaskGate(dir, "task-implement", state); err != nil || res.Passed {
		t.Fatalf("无审批应 BLOCKED: %+v err=%v", res, err)
	} else if !strings.Contains(res.Message, "--approve spec") {
		t.Errorf("BLOCKED 文案应指向审批命令: %q", res.Message)
	}

	// 审批哈希与引用不符 → 拒（审批的不是登记内容）。
	state.ArtifactApprovals = map[string]ArtifactApproval{"spec": {By: "human", Hash: "deadbeefdeadbeef"}}
	if res, err := ExecuteTaskGate(dir, "task-implement", state); err != nil || res.Passed {
		t.Fatalf("审批哈希失配应 BLOCKED: %+v err=%v", res, err)
	}

	// 正确审批 → PASS。
	state.ArtifactApprovals["spec"] = ArtifactApproval{By: "human", Hash: aref.Hash}
	if res, err := ExecuteTaskGate(dir, "task-implement", state); err != nil || !res.Passed {
		t.Fatalf("审批匹配应 PASS: %+v err=%v", res, err)
	}
}

// TestExecuteTaskGate_ArtifactChainRubricLint 钉住 rubric 档的机械 L1 lint：
// doclint Hard 级 issue（D1 空转措辞）阻断；干净产物放行。意见类判断不进 gate
// ——lint 只报机械事实。
func TestExecuteTaskGate_ArtifactChainRubricLint(t *testing.T) {
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	writeCommitSource(t, dir, map[string]string{
		"foo.go": "package main\n\nfunc Foo() int { return 1 }\n",
	}, "add foo")
	writeChainSchema(t, dir, "version: 1\nstages:\n  - name: spec\n    mode: rubric\n")

	state := &TaskState{TaskRef: "chain-rubric", Branch: "feat/chain"}
	bad, _ := WriteArtifact(dir, state.TaskRef, "spec", "# 规格\n\n综上所述基本可以收工。\n")
	state.SpecArtifacts = map[string]ArtifactRef{"spec": bad}
	if res, err := ExecuteTaskGate(dir, "task-implement", state); err != nil || res.Passed {
		t.Fatalf("L1 lint 未过应 BLOCKED: %+v err=%v", res, err)
	} else if !strings.Contains(res.Message, "L1 lint") {
		t.Errorf("BLOCKED 文案应说明 lint 未过: %q", res.Message)
	}

	good, _ := WriteArtifact(dir, state.TaskRef, "spec", "# 规格\n\n验收标准：accept: true ::\n")
	state.SpecArtifacts["spec"] = good
	if res, err := ExecuteTaskGate(dir, "task-implement", state); err != nil || !res.Passed {
		t.Fatalf("干净产物应 PASS: %+v err=%v", res, err)
	}
}

// TestExecuteTaskGate_ArtifactChainEscape 钉住逃生舱契约：FORGE_ARTIFACT_CHAIN=disable
// 整体跳过链执法（hard 档缺失也放行）并落 escape-hatch 审计行——逃生有痕。
func TestExecuteTaskGate_ArtifactChainEscape(t *testing.T) {
	t.Setenv("FORGE_ARTIFACT_CHAIN", "disable")
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	writeCommitSource(t, dir, map[string]string{
		"foo.go": "package main\n\nfunc Foo() int { return 1 }\n",
	}, "add foo")
	writeChainSchema(t, dir, "version: 1\nstages:\n  - name: spec\n    mode: hard\n")

	state := &TaskState{TaskRef: "chain-escape", Branch: "feat/chain"}
	res, err := ExecuteTaskGate(dir, "task-implement", state)
	if err != nil || !res.Passed {
		t.Fatalf("逃生舱下 hard 缺失应放行: %+v err=%v", res, err)
	}
	entries, lerr := checklog.LoadAll(dir)
	if lerr != nil {
		t.Fatalf("LoadAll: %v", lerr)
	}
	found := false
	for _, e := range entries {
		if e.Check == checklog.CheckEscapeHatch && strings.Contains(e.Detail, "artifact") {
			found = true
		}
	}
	if !found {
		t.Fatal("逃生必须落 escape-hatch 审计行")
	}
}

// TestCheckArtifactChainDrift 钉住 complete 侧漂移段：advisory 档漂移只留痕不拦
// 但作废审批；hard 档漂移阻断。
func TestCheckArtifactChainDrift(t *testing.T) {
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	dir := t.TempDir()
	initRepoWithMaster(t, dir)

	t.Run("advisory 档漂移不拦但作废审批", func(t *testing.T) {
		state := &TaskState{TaskRef: "drift-advisory", Branch: "feat/chain"}
		aref, err := WriteArtifact(dir, state.TaskRef, "spec", "# 规格 v1\n")
		if err != nil {
			t.Fatal(err)
		}
		state.SpecArtifacts = map[string]ArtifactRef{"spec": aref}
		state.ArtifactApprovals = map[string]ArtifactApproval{"spec": {By: "h", Hash: aref.Hash}}
		// 手改文件 → 漂移。
		p := filepath.Join(forgedata.DataDirFor(dir), filepath.FromSlash(aref.Path))
		if err := os.WriteFile(p, []byte("# 规格 v2（手改）\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if blocked := CheckArtifactChainDrift(dir, state); len(blocked) != 0 {
			t.Fatalf("advisory 档漂移不应阻断: %v", blocked)
		}
		if _, still := state.ArtifactApprovals["spec"]; still {
			t.Fatal("漂移必须作废审批")
		}
	})

	t.Run("hard 档漂移阻断", func(t *testing.T) {
		writeChainSchema(t, dir, "version: 1\nstages:\n  - name: spec\n    mode: hard\n")
		state := &TaskState{TaskRef: "drift-hard", Branch: "feat/chain"}
		aref, err := WriteArtifact(dir, state.TaskRef, "spec", "# 规格 v1\n")
		if err != nil {
			t.Fatal(err)
		}
		state.SpecArtifacts = map[string]ArtifactRef{"spec": aref}
		p := filepath.Join(forgedata.DataDirFor(dir), filepath.FromSlash(aref.Path))
		if err := os.WriteFile(p, []byte("# 规格 v2（手改）\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if blocked := CheckArtifactChainDrift(dir, state); len(blocked) == 0 {
			t.Fatal("hard 档漂移应阻断 complete")
		}
	})
}

// TestArtifactLintUsesSourceType pins that the rubric-tier L1 lint picks the document type from the registration source: a stored <stage>.md registered from a typed filename is linted under that type, while an empty or stdin source keeps the old untyped behaviour.
//
// TestArtifactLintUsesSourceType 钉住 rubric 档 L1 lint 按登记来源选文档类型：从带类型的文件名
// 登记的 <stage>.md 按该类型检查；来源为空或 stdin 时保持旧的无类型行为。
func TestArtifactLintUsesSourceType(t *testing.T) {
	abs := filepath.Join(t.TempDir(), "proposal.md")
	if err := os.WriteFile(abs, []byte("# 验收\n\n只有正文，没有结论章节。\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, src := range []string{"", "stdin"} {
		hard, err := lintArtifactHardIssues(abs, src)
		if err != nil || len(hard) != 0 {
			t.Fatalf("source %q must lint as untyped proposal.md, got %v %v", src, hard, err)
		}
	}
	hard, err := lintArtifactHardIssues(abs, "docs/qa/login-test-report.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(hard) == 0 || !strings.HasPrefix(hard[0], "D5:") {
		t.Fatalf("a test-report source must apply its required-heading rule, got %v", hard)
	}
	// 来源名匹配不到任何类型：回落按落盘名定型（不因来源名丢掉落盘名的类型）。
	designAbs := filepath.Join(t.TempDir(), "design.md")
	if err := os.WriteFile(designAbs, []byte("# 方案\n\n正文\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if hard, err := lintArtifactHardIssues(designAbs, "notes/C1.md"); err != nil || len(hard) != 0 {
		t.Fatalf("design type stays advisory after fallback, got %v %v", hard, err)
	}
	// survey 类型在观察窗口内全是 advisory：按 survey 定型也不产生 Hard。
	if hard, err := lintArtifactHardIssues(abs, "docs/surveys/agent-memory.md"); err != nil || len(hard) != 0 {
		t.Fatalf("survey rules are advisory and must not block, got %v %v", hard, err)
	}
}

// TestArtifactSource pins source normalization: repo-relative slash path inside root, absolute path outside, empty stays empty.
//
// TestArtifactSource 钉住来源规整：仓库内记斜杠相对路径，仓库外记绝对路径，空串保持空串。
func TestArtifactSource(t *testing.T) {
	root := t.TempDir()
	inside := filepath.Join(root, "docs", "surveys", "x.md")
	if got := ArtifactSource(root, inside); got != "docs/surveys/x.md" {
		t.Fatalf("inside root = %q, want docs/surveys/x.md", got)
	}
	outside := filepath.Join(t.TempDir(), "spec.md")
	if got := ArtifactSource(root, outside); !filepath.IsAbs(filepath.FromSlash(got)) || !strings.HasSuffix(got, "/spec.md") {
		t.Fatalf("outside root = %q, want an absolute path", got)
	}
	if got := ArtifactSource(root, ""); got != "" {
		t.Fatalf("empty source = %q", got)
	}
	// 目标是仓库根的上级目录：不在仓库内，必须记绝对路径而不是 ".."。
	if got := ArtifactSource(root, filepath.Dir(root)); got == ".." || !filepath.IsAbs(filepath.FromSlash(got)) {
		t.Fatalf("parent of root = %q, want an absolute path", got)
	}
}
