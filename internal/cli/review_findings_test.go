package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/taskpipeline"
)

func TestParseFindings(t *testing.T) {
	raw := "- [ ] P1-a 门禁可绕过\n- [x] P1-b 已修\n- [~] P2-c 已知未修 (waived: 下一版)\n"
	fs, err := parseFindings(raw)
	if err != nil {
		t.Fatalf("parseFindings: %v", err)
	}
	if len(fs) != 3 {
		t.Fatalf("want 3 findings, got %+v", fs)
	}
	if fs[0].ID != "P1-a" || !fs[0].Open {
		t.Errorf("P1-a 应未闭合: %+v", fs[0])
	}
	if fs[1].Open {
		t.Errorf("P1-b 应已关闭: %+v", fs[1])
	}
	if !fs[2].Waived || fs[2].Open {
		t.Errorf("P2-c 应豁免且不算未闭合: %+v", fs[2])
	}
	if _, err := parseFindings("随便一行不是清单\n"); err == nil {
		t.Error("非清单行必须报错（否则评审可以用散文绕过承接）")
	}
	if _, err := parseFindings("- [ ]\n"); err == nil {
		t.Error("缺 id 的清单项必须报错")
	}
}

// TestReviewFindingsCarry 钉住「未闭合项必须承接」：上一轮 open 的 id 在下一轮
// 必须关闭或显式豁免，否则 review pass 拒绝——这是「每轮都重新扫一遍、旧问题
// 悄悄消失」的机械防线。
func TestReviewFindingsCarry(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "t@t.com")
	runGit(t, dir, "config", "user.name", "T")
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "initial")

	const ref = "feat/findings"
	if err := taskpipeline.SaveTaskState(dir, &taskpipeline.TaskState{TaskRef: ref, Branch: ref}); err != nil {
		t.Fatal(err)
	}

	first := filepath.Join(dir, "findings-1.md")
	if err := os.WriteFile(first, []byte("- [ ] P1-a 门禁可绕过\n- [x] P2-b 已修\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runReviewPassAtOpts(dir, ref, "", "rev-1", reviewOptions{FindingsPath: first}); err != nil {
		t.Fatalf("首轮带清单应放行: %v", err)
	}
	st, err := taskpipeline.LoadTaskState(dir, ref)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(st.ReviewRounds); n != 1 {
		t.Fatalf("want 1 round, got %d", n)
	}
	if st.ReviewRounds[0].OpenFindings != 1 || len(st.ReviewRounds[0].OpenIDs) != 1 || st.ReviewRounds[0].OpenIDs[0] != "P1-a" {
		t.Fatalf("首轮应记 1 条未闭合 P1-a, got %+v", st.ReviewRounds[0])
	}

	// 修复后提交 → 内容增量；不带清单的复审拒绝。
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() { println(1) }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "fix: P1-a")
	err = runReviewPassAt(dir, ref, "", "rev-1")
	if err == nil || !strings.Contains(err.Error(), "--findings") {
		t.Fatalf("上一轮有未闭合项时，复审必须带 --findings 承接, got %v", err)
	}

	// 清单里 P1-a 直接消失（没关闭也没豁免）→ 拒绝。
	missing := filepath.Join(dir, "findings-2.md")
	if err := os.WriteFile(missing, []byte("- [x] P2-b 已修\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err = runReviewPassAtOpts(dir, ref, "", "rev-1", reviewOptions{FindingsPath: missing})
	if err == nil || !strings.Contains(err.Error(), "没有交代") {
		t.Fatalf("未闭合项消失必须拒绝, got %v", err)
	}

	// 关闭 P1-a → 放行，本轮未闭合为 0。
	closed := filepath.Join(dir, "findings-3.md")
	if err := os.WriteFile(closed, []byte("- [x] P1-a 门禁已收口\n- [x] P2-b 已修\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runReviewPassAtOpts(dir, ref, "", "rev-1", reviewOptions{FindingsPath: closed}); err != nil {
		t.Fatalf("关闭未闭合项后应放行: %v", err)
	}
	st, err = taskpipeline.LoadTaskState(dir, ref)
	if err != nil {
		t.Fatal(err)
	}
	last := st.ReviewRounds[len(st.ReviewRounds)-1]
	if last.OpenFindings != 0 || last.ClosedFindings != 2 {
		t.Errorf("本轮应记 0 未闭合 / 2 已关闭, got %+v", last)
	}
}

// TestParseFindings_RejectsBlankAndDuplicate 钉住两条退路：豁免原因只写空白
// （含全角空格）不算原因；同一 id 出现两次时「哪行生效」不该靠猜。
func TestParseFindings_RejectsBlankAndDuplicate(t *testing.T) {
	if _, err := parseFindings("- [~] P1-a (waived: \u3000)\n"); err == nil {
		t.Fatal("全角空格的豁免原因必须被拒")
	}
	if _, err := parseFindings("- [x] P1-a 关\n- [ ] P1-a 又开\n"); err == nil {
		t.Fatal("重复 ID 必须被拒")
	}
}

// TestWaivedHasReason_RejectsInvisible 钉住：只由不可见字符组成的「原因」不算原因。
func TestWaivedHasReason_RejectsInvisible(t *testing.T) {
	for _, bad := range []string{"- [~] P1-a (waived: \u200c)", "- [~] P1-a (waived: \ufeff)", "- [~] P1-a （waived：\u3000）"} {
		if _, err := parseFindings(bad); err == nil {
			t.Errorf("只有不可见字符/空白的原因应被拒: %q", bad)
		}
	}
	if _, err := parseFindings("- [~] P1-a (waived: 需要时间线证明)"); err != nil {
		t.Errorf("真实原因应通过: %v", err)
	}
}
