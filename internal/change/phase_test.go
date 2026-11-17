package change

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/specroot"
)

// phase_test.go —— 入口路由 A2：阶段推导（specify → clarify → plan → tasks → ready）。

func writeChangeFile(t *testing.T, root, id, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "spec", "changes", id, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func phaseGoal(id string) string {
	return "# 目标：" + id + "\n\n" +
		"change: `" + id + "`\n\n" +
		"## Objective\n\n把入口路由变成状态。\n\n" +
		"## Why\n\n脏树直接任务会让 change 旁路。\n\n" +
		"## Constraints\n\n只对声明 spec 的仓库生效。\n\n" +
		"## Out of Scope\n\n不做 fragment 组装。\n\n" +
		"## Check\n\n- forge change phase 能报阶段\n"
}

func TestStage_Progression(t *testing.T) {
	root := t.TempDir()
	id := "demo"
	if _, err := Init(root, specroot.ProfileMini); err != nil {
		t.Fatal(err)
	}
	if _, err := New(root, id, "demo"); err != nil {
		t.Fatal(err)
	}

	phase, missing, err := ComputePhase(root, id)
	if err != nil {
		t.Fatal(err)
	}
	if phase != PhaseSpecify {
		t.Fatalf("fresh change phase = %s, want specify (missing: %v)", phase, missing)
	}

	writeChangeFile(t, root, id, FileGoal, phaseGoal(id))
	phase, _, err = ComputePhase(root, id)
	if err != nil {
		t.Fatal(err)
	}
	if phase != PhaseClarify {
		t.Fatalf("goal filled phase = %s, want clarify", phase)
	}

	writeChangeFile(t, root, id, FileClarify, RenderClarify(id, "只做入口路由，不做组装。", "", time.Now()))
	phase, _, err = ComputePhase(root, id)
	if err != nil {
		t.Fatal(err)
	}
	if phase != PhasePlan {
		t.Fatalf("clarify written phase = %s, want plan", phase)
	}

	writeChangeFile(t, root, id, FileAcceptance, "## 判据\n\n```bash\ngo version\n```\n")
	phase, _, err = ComputePhase(root, id)
	if err != nil {
		t.Fatal(err)
	}
	if phase != PhasePlan {
		t.Fatalf("acceptance without change.md phase = %s, want plan", phase)
	}

	writeChangeFile(t, root, id, FileChange, "# 改动\n\n实现三件事。\n\n## 影响\n\n无。\n\n## 验证\n\nacceptance #1。\n")
	phase, missing, err = ComputePhase(root, id)
	if err != nil {
		t.Fatal(err)
	}
	if phase != PhaseReady {
		t.Fatalf("all artifacts phase = %s, want ready (missing: %v)", phase, missing)
	}
}

func TestStage_UnboundTaskBlocksReady(t *testing.T) {
	root := t.TempDir()
	id := "demo"
	if _, err := Init(root, specroot.ProfileMini); err != nil {
		t.Fatal(err)
	}
	if _, err := New(root, id, "demo"); err != nil {
		t.Fatal(err)
	}
	writeChangeFile(t, root, id, FileGoal, phaseGoal(id))
	writeChangeFile(t, root, id, FileClarify, RenderClarify(id, "边界：只改 cli。", "", time.Now()))
	writeChangeFile(t, root, id, FileProposal, "# 提案\n\n为什么。\n")
	writeChangeFile(t, root, id, FileAcceptance, "## 判据\n\n```bash\ngo version\n```\n")
	writeChangeFile(t, root, id, FileDesign, "# 设计\n\n实现。\n")
	writeChangeFile(t, root, id, FileTasks, "- [ ] 1. 做 A（判据：acceptance #1）\n- [ ] 2. 忘了写判据\n")
	// 老布局：删掉 change.md 与 .goal-required，tasks 绑定才参与阶段判定。
	for _, f := range []string{FileChange, GoalRequiredFile} {
		_ = os.Remove(filepath.Join(root, "spec", "changes", id, f))
	}

	phase, missing, err := ComputePhase(root, id)
	if err != nil {
		t.Fatal(err)
	}
	if phase != PhaseTasks {
		t.Fatalf("unbound task phase = %s, want tasks", phase)
	}
	if len(missing) == 0 || !strings.Contains(strings.Join(missing, " "), "#2") {
		t.Fatalf("missing hints should name task #2: %v", missing)
	}
}

func TestClarifyRecord_RequiresByHuman(t *testing.T) {
	// 没有「人确认」行的 clarify.md 必须被判未完成。
	if _, err := ParseClarify([]byte("## 边界\n\n只改 cli。\n\n## 非目标\n\n不做 UI。\n")); err == nil ||
		!strings.Contains(err.Error(), "人确认") {
		t.Fatalf("missing 人确认 line must fail with 人确认 hint, got %v", err)
	}
	// 有人确认但边界为空，同样不行。
	if _, err := ParseClarify([]byte("## 边界\n\n## 非目标\n\n不做 UI。\n\n人确认：2026-11-15T00:00:00Z\n")); err == nil {
		t.Fatal("empty boundary must fail")
	}
	// 正常记录可解析。
	got, err := ParseClarify([]byte(RenderClarify("demo", "只改 cli。", "不动 server。", time.Now())))
	if err != nil {
		t.Fatal(err)
	}
	if got.Boundary != "只改 cli。" || got.NonGoals != "不动 server。" || got.ConfirmedAt.IsZero() {
		t.Fatalf("parsed clarify = %+v", got)
	}
}

func TestUnboundTasks(t *testing.T) {
	tasks := ParseTasks([]byte("- [ ] 1. A（判据：acceptance #1）\n- [ ] 2. B\n- [x] 3. C（判据：acceptance #2）\n"))
	if got := UnboundTasks(tasks); len(got) != 1 || got[0] != 2 {
		t.Fatalf("UnboundTasks = %v, want [2]", got)
	}
}

// TestStage_FastChangeReadyAfterClarify：fast change 没有 design/tasks，是形态
// 差异而不是半成品——填完 goal/proposal/acceptance 并 clarify 后必须 ready。
func TestStage_FastChangeReadyAfterClarify(t *testing.T) {
	root := t.TempDir()
	id := "fastdemo"
	if _, err := Init(root, specroot.ProfileMini); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFast(root, id, "fast demo"); err != nil {
		t.Fatal(err)
	}
	writeChangeFile(t, root, id, FileGoal, phaseGoal(id))
	writeChangeFile(t, root, id, FileChange, "# 改动\n\n小改动。\n\n## 影响\n\n无。\n\n## 验证\n\nacceptance #1。\n")
	writeChangeFile(t, root, id, FileAcceptance, "## 判据\n\n```bash\ngo version\n```\n")
	writeChangeFile(t, root, id, FileClarify, RenderClarify(id, "只改一处。", "", time.Now()))

	phase, missing, err := ComputePhase(root, id)
	if err != nil {
		t.Fatal(err)
	}
	if phase != PhaseReady {
		t.Fatalf("fast change phase = %s, want ready (missing: %v)", phase, missing)
	}
}

// TestStage_LegacyChangeWithoutGoal：没有 goal.md 也没有 .goal-required 的老 change
// 跳过 goal 段，停在 clarify（而不是永远 specify）。
func TestStage_LegacyChangeWithoutGoal(t *testing.T) {
	root := t.TempDir()
	id := "legacy"
	if _, err := Init(root, specroot.ProfileMini); err != nil {
		t.Fatal(err)
	}
	if _, err := New(root, id, "legacy"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "spec", "changes", id, FileGoal)); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "spec", "changes", id, GoalRequiredFile)); err != nil {
		t.Fatal(err)
	}
	phase, _, err := ComputePhase(root, id)
	if err != nil {
		t.Fatal(err)
	}
	if phase != PhaseClarify {
		t.Fatalf("legacy change phase = %s, want clarify", phase)
	}
}

// TestClarify_NonGoalsHyphenVariant：`## Non-Goals` 应被接受（常见英文写法）。
func TestClarify_NonGoalsHyphenVariant(t *testing.T) {
	c, err := ParseClarify([]byte("## 边界\n\n只改 cli。\n\n## Non-Goals\n\n不做 UI。\n\n人确认: 2026-11-15T00:00:00Z\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(c.NonGoals, "不做 UI") {
		t.Fatalf("non-goals lost: %q", c.NonGoals)
	}
}

// TestClarify_NonGoalsUnderscoreVariant：`## Non_Goals` 也应被接受（标题归一删 `_`）。
func TestClarify_NonGoalsUnderscoreVariant(t *testing.T) {
	c, err := ParseClarify([]byte("## 边界\n\n只改 cli。\n\n## Non_Goals\n\n不做 UI。\n\n人确认: 2026-11-15T00:00:00Z\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(c.NonGoals, "不做 UI") {
		t.Fatalf("non-goals lost: %q", c.NonGoals)
	}
}
