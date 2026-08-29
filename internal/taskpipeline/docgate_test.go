package taskpipeline

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
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

func TestCheckDocGateFingerprintStaleness(t *testing.T) {
	// I5：只绑 HEAD 有工作区盲区——评审通过后不提交地改文档，必须经内容
	// 指纹判评审过期。
	root, base := newDocGateRepo(t)
	if err := os.WriteFile(root+"/notes.md", []byte("# 笔记\n干净内容。\n"), 0644); err != nil {
		t.Fatal(err)
	}
	state := &TaskState{TaskRef: "feat/x", HeadCommit: base}
	state.DocReview = &DocReview{
		Passed: true, RubricScore: 90, Round: 1, ReviewedAt: time.Now(),
		HeadCommit:      GetHeadCommit(root),
		DocsFingerprint: DocContentFingerprint(root, state),
	}
	if ok, reasons := CheckDocGate(root, state); !ok {
		t.Fatalf("前置：内容未变应放行, got %v", reasons)
	}
	// 未提交修改：HEAD 不变、指纹变 → 过期。
	if err := os.WriteFile(root+"/notes.md", []byte("# 笔记\n改过的内容。\n"), 0644); err != nil {
		t.Fatal(err)
	}
	ok, reasons := CheckDocGate(root, state)
	if ok || !strings.Contains(strings.Join(reasons, "; "), "旧内容") {
		t.Fatalf("未提交修改应经指纹判过期, got ok=%v reasons=%v", ok, reasons)
	}
	// 无指纹的旧版评审：仅查 HEAD，照常放行。
	legacy := &TaskState{TaskRef: "feat/y", HeadCommit: base}
	legacy.DocReview = &DocReview{Passed: true, RubricScore: 90, Round: 1, ReviewedAt: time.Now(), HeadCommit: GetHeadCommit(root)}
	if ok, _ := CheckDocGate(root, legacy); !ok {
		t.Fatal("空指纹的旧版评审不应触发指纹过期")
	}
}

func TestChangedMarkdownSinceIncludesUntrackedAndSkipsDeleted(t *testing.T) {
	root, base := newDocGateRepo(t)
	if err := os.WriteFile(root+"/new-untracked.md", []byte("新文件\n"), 0644); err != nil {
		t.Fatal(err)
	}
	docs, err := ChangedMarkdownSince(root, base)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range docs {
		if d == "new-untracked.md" {
			found = true
		}
	}
	if !found {
		t.Fatalf("未跟踪新文件应进入集合（与门禁一致），got %v", docs)
	}

	// 基线后删除的文件应被剔除而不是报 IO 错误（回归 I1a：旧 CLI --base 把
	// 它们读成硬 IO 失败）。
	del := root + "/gone.md"
	if err := os.WriteFile(del, []byte("x\n"), 0644); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) {
		t.Helper()
		if out, gerr := exec.Command("git", append([]string{"-C", root, "-c", "user.email=t@t", "-c", "user.name=t"}, args...)...).CombinedOutput(); gerr != nil {
			t.Fatalf("git %v: %v\n%s", args, gerr, out)
		}
	}
	git("add", "gone.md")
	git("commit", "-q", "-m", "add gone")
	if err := os.Remove(del); err != nil {
		t.Fatal(err)
	}
	docs, err = ChangedMarkdownSince(root, base)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range docs {
		if d == "gone.md" {
			t.Fatalf("已删除文件不应出现在集合中, got %v", docs)
		}
	}
}

// TestCheckDocGateBlockedDetailRecordsReasons pins the audit-side fix (2026-08
// review-observability): a BLOCKED doc-gate checklog entry must carry the reason
// TEXTS in Detail, not just the count.
//
// TestCheckDocGateBlockedDetailRecordsReasons 钉住审计侧修复（2026-08 评审
// 可观测性）：BLOCKED 的 doc-gate checklog 条目 Detail 必须带原因【文本】而非
// 只有数量——光记「N reasons」会让复盘不得不对源码反推。通过条目保持只记数量
// （不添噪声）。
func TestCheckDocGateBlockedDetailRecordsReasons(t *testing.T) {
	root, base := newDocGateRepo(t)
	if err := os.WriteFile(root+"/notes.md", []byte("# 笔记\n干净内容。\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// No DocReview evidence at all → blocked with the "L2 文档回检未记录" reason.
	//
	// 完全没有 DocReview 证据 → 以「L2 文档回检未记录」原因阻断。
	state := &TaskState{TaskRef: "feat/x", HeadCommit: base}
	ok, reasons := CheckDocGate(root, state)
	if ok {
		t.Fatal("无 DocReview 证据应阻断")
	}
	if len(reasons) == 0 {
		t.Fatal("阻断应给出原因")
	}

	entries, err := checklog.LoadAll(root)
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	var gate *checklog.Entry
	for i := range entries {
		if entries[i].Check == CheckNameDocGate {
			gate = &entries[i]
		}
	}
	if gate == nil {
		t.Fatal("doc-gate 条目未记录")
	}
	if gate.Passed {
		t.Fatal("阻断场景条目应 Passed=false")
	}
	if !strings.Contains(gate.Detail, "L2 文档回检未记录") {
		t.Errorf("BLOCKED 条目 Detail 应含原因文本而非仅数量, got %q", gate.Detail)
	}

	// 通过的运行 → 只记数量（不倾倒原因）。
	head := GetHeadCommit(root)
	state2 := &TaskState{TaskRef: "feat/y", HeadCommit: base}
	state2.DocReview = &DocReview{Passed: true, RubricScore: 90, Round: 1, ReviewedAt: time.Now(), HeadCommit: head}
	if ok, _ := CheckDocGate(root, state2); !ok {
		t.Fatal("fresh 通过应放行")
	}
	entries2, _ := checklog.LoadAll(root)
	var passGate *checklog.Entry
	for i := range entries2 {
		if entries2[i].Check == CheckNameDocGate && entries2[i].TaskRef == "feat/y" {
			passGate = &entries2[i]
		}
	}
	if passGate == nil || !passGate.Passed {
		t.Fatal("通过场景应有 Passed=true 条目")
	}
	if strings.Contains(passGate.Detail, "回检") {
		t.Errorf("通过条目 Detail 不应含原因文本, got %q", passGate.Detail)
	}
}

// TestCheckDocGateReasonsPointToDocReviewSkill pins the post-inversion guidance
// text: doc-gate refusal reasons must point at the doc-review skill (the process
// source of truth), never the pre-migration code-review-gate internal path.
//
// TestCheckDocGateReasonsPointToDocReviewSkill 钉住依赖倒置后的指引文案：doc gate
// 的拒绝原因必须指向 doc-review skill（流程真相源），不得回退到 code-review-gate
// 时代的内部路径——rubric-docs.md 已迁至 skills/doc-review/references/。
// TestCheckDocGateReasonsPointToDocReviewSkill pins the post-inversion guidance
// text: doc-gate refusal reasons must point at the doc-review skill (the process
// source of truth), never the pre-migration code-review-gate internal path.
func TestCheckDocGateReasonsPointToDocReviewSkill(t *testing.T) {
	root, base := newDocGateRepo(t)
	if err := os.WriteFile(root+"/notes.md", []byte("# 笔记\n干净内容，`forge docs lint` 通过。\n"), 0644); err != nil {
		t.Fatal(err)
	}
	_, reasons := CheckDocGate(root, &TaskState{TaskRef: "feat/x", HeadCommit: base})
	joined := strings.Join(reasons, "; ")
	if !strings.Contains(joined, "doc-review skill") {
		t.Fatalf("未记录原因应指向 doc-review skill, got %s", joined)
	}
	if strings.Contains(joined, "code-review-gate/references/rubric-docs.md") {
		t.Fatalf("不得引用已迁移的旧路径 code-review-gate/references/rubric-docs.md, got %s", joined)
	}
}
