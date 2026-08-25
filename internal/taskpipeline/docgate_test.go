package taskpipeline

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// docgate_test.go: covers the doc pre-flight (output→re-check loop,
// docs/design/output-readability-gates.md 方案二). The gate gives the re-check
// a process node (complete refuses until docs pass), falsifiable criteria
// (L1 lint + L2 evidence) and a cost (BLOCKED + escape-hatch audit).
//
// docgate_test.go：覆盖 doc pre-flight（输出→回检循环，
// docs/design/output-readability-gates.md 方案二）。门禁给回检补流程节点
// （complete 前拦截）、可证伪判据（L1 lint + L2 证据）与代价（BLOCKED +
// 逃生舱审计）。

// newDocGateRepo creates a temp git repo with one base commit and returns the
// repo root plus the base HEAD (to anchor state.HeadCommit).
//
// newDocGateRepo 建临时 git 仓库（含一个基线提交），返回仓库根与基线 HEAD
// （用作 state.HeadCommit 锚点）。
func newDocGateRepo(t *testing.T) (root, baseHead string) {
	t.Helper()
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	root = t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", root, "-c", "user.email=t@t", "-c", "user.name=t"}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	git("init", "-q", ".")
	if err := os.WriteFile(root+"/base.txt", []byte("base\n"), 0644); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-q", "-m", "base")
	baseHead = strings.TrimSpace(git("rev-parse", "HEAD"))
	return root, baseHead
}

func TestCheckDocGateNoDocsPasses(t *testing.T) {
	root, base := newDocGateRepo(t)
	state := &TaskState{TaskRef: "feat/x", HeadCommit: base}
	ok, reasons := CheckDocGate(root, state)
	if !ok {
		t.Fatalf("无文档产物应放行, got reasons=%v", reasons)
	}
	// HeadCommit 缺失（非 git 起步任务）同样短路放行。
	ok, reasons = CheckDocGate(root, &TaskState{TaskRef: "feat/x"})
	if !ok {
		t.Fatalf("HeadCommit 缺失应放行, got reasons=%v", reasons)
	}
}

func TestCheckDocGateL1HardBlocks(t *testing.T) {
	root, base := newDocGateRepo(t)
	// Untracked markdown with a banned phrase — caught by the untracked sweep.
	//
	// 含禁令短语的未跟踪 markdown——未跟踪扫描应捕获。
	if err := os.WriteFile(root+"/summary.md", []byte("本方案基本可以。\n"), 0644); err != nil {
		t.Fatal(err)
	}
	state := &TaskState{TaskRef: "feat/x", HeadCommit: base}
	state.DocReview = &DocReview{Passed: true, RubricScore: 90, Round: 1, ReviewedAt: time.Now(), HeadCommit: GetHeadCommit(root)}
	ok, reasons := CheckDocGate(root, state)
	if ok {
		t.Fatal("L1 硬失败应阻断")
	}
	joined := strings.Join(reasons, "; ")
	if !strings.Contains(joined, "D1") || !strings.Contains(joined, "summary.md") {
		t.Fatalf("reason 应含 file 与规则号, got %s", joined)
	}
}

func TestCheckDocGateL2States(t *testing.T) {
	root, base := newDocGateRepo(t)
	if err := os.WriteFile(root+"/notes.md", []byte("# 笔记\n干净内容，`forge docs lint` 通过。\n"), 0644); err != nil {
		t.Fatal(err)
	}
	head := GetHeadCommit(root)

	t.Run("未记录回检 → 阻断", func(t *testing.T) {
		state := &TaskState{TaskRef: "feat/x", HeadCommit: base}
		ok, reasons := CheckDocGate(root, state)
		if ok || !strings.Contains(strings.Join(reasons, "; "), "L2 文档回检未记录") {
			t.Fatalf("未记录应阻断并指引 doc-review, got ok=%v reasons=%v", ok, reasons)
		}
	})

	t.Run("过期回检 → 阻断", func(t *testing.T) {
		state := &TaskState{TaskRef: "feat/x", HeadCommit: base}
		state.DocReview = &DocReview{Passed: true, RubricScore: 90, Round: 1, ReviewedAt: time.Now(), HeadCommit: "deadbeefdeadbeef"}
		ok, reasons := CheckDocGate(root, state)
		if ok || !strings.Contains(strings.Join(reasons, "; "), "旧代码") {
			t.Fatalf("过期回检应阻断, got ok=%v reasons=%v", ok, reasons)
		}
	})

	t.Run("得分低于阈值 → 阻断", func(t *testing.T) {
		state := &TaskState{TaskRef: "feat/x", HeadCommit: base}
		state.DocReview = &DocReview{Passed: true, RubricScore: DocRubricThreshold - 1, Round: 1, ReviewedAt: time.Now(), HeadCommit: head}
		ok, reasons := CheckDocGate(root, state)
		if ok || !strings.Contains(strings.Join(reasons, "; "), "低于阈值") {
			t.Fatalf("低分应阻断, got ok=%v reasons=%v", ok, reasons)
		}
	})

	t.Run("轮次上限 → 升级人工确认文案", func(t *testing.T) {
		state := &TaskState{TaskRef: "feat/x", HeadCommit: base}
		state.DocReview = &DocReview{Passed: false, RubricScore: 60, Round: DocReviewMaxRounds, ReviewedAt: time.Now(), HeadCommit: head}
		ok, reasons := CheckDocGate(root, state)
		if ok {
			t.Fatal("轮次打满仍未过不得放行")
		}
		if !strings.Contains(strings.Join(reasons, "; "), "人工确认") {
			t.Fatalf("打满轮次应升级人工确认, got %v", reasons)
		}
	})

	t.Run("fresh 通过 → 放行", func(t *testing.T) {
		state := &TaskState{TaskRef: "feat/x", HeadCommit: base}
		state.DocReview = &DocReview{Passed: true, RubricScore: DocRubricThreshold, Round: 1, ReviewedAt: time.Now(), HeadCommit: head}
		ok, reasons := CheckDocGate(root, state)
		if !ok {
			t.Fatalf("fresh 通过应放行, got reasons=%v", reasons)
		}
	})
}

func TestCheckDocGateCriticalFindings(t *testing.T) {
	root, base := newDocGateRepo(t)
	if err := os.WriteFile(root+"/notes.md", []byte("# 笔记\n干净内容，`forge docs lint` 通过。\n"), 0644); err != nil {
		t.Fatal(err)
	}
	head := GetHeadCommit(root)
	state := &TaskState{TaskRef: "feat/x", HeadCommit: base}
	state.DocReview = &DocReview{Passed: true, RubricScore: 90, Round: 1, ReviewedAt: time.Now(), HeadCommit: head}
	state.AddFinding(Finding{
		Content:  "PR 描述无验证段",
		Source:   DocReviewSource,
		Severity: FindingSeverityCritical,
	})
	ok, reasons := CheckDocGate(root, state)
	if ok {
		t.Fatal("未决 Critical 应阻断")
	}
	if len(state.Findings) == 0 || state.Findings[0].ID == "" {
		t.Fatal("AddFinding 应生成 ID")
	}
	state.ResolveFinding(state.Findings[0].ID)
	ok, reasons = CheckDocGate(root, state)
	if !ok {
		t.Fatalf("Critical 解决后应放行, got reasons=%v", reasons)
	}

	// Legacy findings (empty severity) never block — additive field contract.
	//
	// 旧版 findings（空 severity）永不阻断——增量字段契约。
	state2 := &TaskState{TaskRef: "feat/y", HeadCommit: base}
	state2.DocReview = state.DocReview
	state2.AddFinding(Finding{Content: "旧格式发现", Source: DocReviewSource})
	state2.Findings[0].RaisedAt = time.Now()
	if ok, _ := CheckDocGate(root, state2); !ok {
		t.Fatal("空 severity 的旧版 finding 不应阻断")
	}
}

func TestCheckDocGateEscapeHatch(t *testing.T) {
	root, base := newDocGateRepo(t)
	if err := os.WriteFile(root+"/bad.md", []byte("基本可以。\n"), 0644); err != nil {
		t.Fatal(err)
	}
	state := &TaskState{TaskRef: "feat/x", HeadCommit: base}
	if ok, _ := CheckDocGate(root, state); ok {
		t.Fatal("前置：无逃生舱时应阻断")
	}
	t.Setenv("FORGE_DOC_GATE", "disable")
	ok, reasons := CheckDocGate(root, state)
	if !ok {
		t.Fatalf("逃生舱应放行, got reasons=%v", reasons)
	}
}

func TestChangedMarkdownDocsExemptions(t *testing.T) {
	root, base := newDocGateRepo(t)
	// NOTE: use a ROOT-level real.md — some machines carry a global gitignore
	// ignoring docs/ (untracked listing then drops it, by git's design);
	// exemption assertions here are about doclint.PathExempt, not git politics.
	//
	// 注意：real.md 放仓库根——部分机器的全局 gitignore 忽略 docs/
	// （未跟踪清单会按 git 设计丢弃它）；本测试断言的是 doclint.PathExempt，
	// 不是 git 的忽略策略。
	for _, p := range []string{"real.md", "internal/x/testdata/fixture.md", "vendor/lib/dep.md"} {
		full := root + "/" + p
		if idx := strings.LastIndex(full, "/"); idx > 0 {
			if err := os.MkdirAll(full[:idx], 0755); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(full, []byte("基本可以。\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	docs := changedMarkdownDocs(root, &TaskState{TaskRef: "feat/x", HeadCommit: base})
	if len(docs) != 1 || docs[0] != "real.md" {
		t.Fatalf("豁免路径应被排除，只留 real.md, got %v", docs)
	}
}
