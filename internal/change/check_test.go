package change

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// CheckProblems 是 CI 与 pre-commit 共用的判据：每个未归档 change 都得有一份
// 与当前代码和判据一致的封印证据。这几个用例钉住三件事——封印的放行、三种
// 不合格各有理由、--staged 只查碰到的那些。

func TestCheckProblems_SealedPasses(t *testing.T) {
	requireBash(t)
	root, dir := repoWithChange(t, "demo")
	writeAcceptance(t, dir, "test -d spec")
	if _, err := Verify(context.Background(), root, "demo", VerifyOptions{Timeout: 30 * time.Second}); err != nil {
		t.Fatal(err)
	}

	problems, checked, err := CheckProblems(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 0 {
		t.Fatalf("封印后的 change 不该有问题：%+v", problems)
	}
	if len(checked) != 1 || checked[0] != "demo" {
		t.Fatalf("checked = %v，想要 [demo]", checked)
	}
}

func TestCheckProblems_UnsealedAndStaleExplainWhy(t *testing.T) {
	requireBash(t)
	root, dir := repoWithChange(t, "demo")
	writeAcceptance(t, dir, "test -d spec")

	// 从没跑过：产物齐了但没有封印证据。
	problems, _, err := CheckProblems(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 1 || !strings.Contains(problems[0].Reason, "没有封印的证据") {
		t.Fatalf("未验证的 change 应报「没有封印的证据」，实际 %+v", problems)
	}
	if !strings.Contains(problems[0].Reason, "forge change verify demo") {
		t.Fatalf("理由里要给出可执行的修复命令，实际 %q", problems[0].Reason)
	}

	if _, err := Verify(context.Background(), root, "demo", VerifyOptions{Timeout: 30 * time.Second}); err != nil {
		t.Fatal(err)
	}
	// 封印后改判据：旧证据失效，必须重跑。
	writeAcceptance(t, dir, "test -d spec && echo changed")
	problems, _, err = CheckProblems(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 1 || !strings.Contains(problems[0].Reason, "失效") {
		t.Fatalf("判据变过应报失效，实际 %+v", problems)
	}
}

func TestCheckProblems_FailedBeatsOtherReasons(t *testing.T) {
	requireBash(t)
	root, dir := repoWithChange(t, "demo")
	writeAcceptance(t, dir, "false")
	res, err := Verify(context.Background(), root, "demo", VerifyOptions{Timeout: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if res.Summary.Passed {
		t.Fatal("false 判据不该通过")
	}

	problems, _, err := CheckProblems(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 1 || !strings.Contains(problems[0].Reason, "没通过") {
		t.Fatalf("最新一次失败应优先报「没通过」，实际 %+v", problems)
	}
	if !strings.Contains(problems[0].Reason, "evidence/") {
		t.Fatalf("理由里要指向证据目录（别让人删证据），实际 %q", problems[0].Reason)
	}
}

func TestCheckProblems_OnlyTouchedSkipsOthers(t *testing.T) {
	root, dirA := repoWithChange(t, "alpha")
	_, dirB := repoWithChange(t, "beta")
	writeAcceptance(t, dirA, "test -d spec")
	writeAcceptance(t, dirB, "test -d spec")

	problems, checked, err := CheckProblems(root, []string{"alpha"})
	if err != nil {
		t.Fatal(err)
	}
	if len(checked) != 1 || checked[0] != "alpha" {
		t.Fatalf("checked = %v，想要只查 alpha", checked)
	}
	if len(problems) != 1 || problems[0].ID != "alpha" {
		t.Fatalf("beta 没被碰到就不该出现在结果里：%+v", problems)
	}
}

func TestTouchedChanges_MapsRepoSelfPaths(t *testing.T) {
	root, _ := repoWithChange(t, "demo")
	paths := []string{
		"spec/changes/demo/proposal.md",
		"spec/changes/demo/evidence/20260101T000000Z/summary.json",
		"README.md",
		"spec/changes/archive/20260101-old/proposal.md",
		"spec/changes/../escape.md",
		"spec/changes/demo/proposal.md", // 重复
	}
	got := TouchedChanges(root, paths)
	if len(got) != 1 || got[0] != "demo" {
		t.Fatalf("TouchedChanges = %v，想要 [demo]", got)
	}
}

func TestCheckProblems_ArchivedNeverBlocks(t *testing.T) {
	root, _ := repoWithChange(t, "demo")
	// 归档目录里没有证据也不该拦：归档意味着这件事已经交付过。
	writeFile(t, filepath.Join(ChangesDir(root), ArchiveDirName, "20260101-old", FileProposal), "# old\n")
	problems, checked, err := CheckProblems(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 1 || problems[0].ID != "demo" {
		t.Fatalf("归档项不该进问题列表：%+v", problems)
	}
	if len(checked) != 1 || checked[0] != "demo" {
		t.Fatalf("checked 只该含未归档项：%v", checked)
	}
}
