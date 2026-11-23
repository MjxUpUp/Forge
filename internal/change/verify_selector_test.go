package change

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
)

// writeGoFixture 造一个最小 go module：一个真测试，供 -run 选择器命中/落空。
func writeGoFixture(t *testing.T, root string) {
	t.Helper()
	writeFile(t, filepath.Join(root, "go.mod"), "module fixture\n\ngo 1.21\n")
	writeFile(t, filepath.Join(root, "x_test.go"), "package fixture\n\nimport \"testing\"\n\nfunc TestPresent(t *testing.T) {}\n")
}

// TestVerifyFailsWhenSelectorMatchesNothing：go test -run <不存在的测试名> 退出 0，
// 但一条测试都没跑。change 侧此前只看退出码，会把这种空跑封印成"已验证"——
// task 侧 B1 已判负，这里钉住 change 侧同一语义。
func TestVerifyFailsWhenSelectorMatchesNothing(t *testing.T) {
	requireBash(t)
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go 不可用")
	}
	root, dir := repoWithChange(t, "selector-empty")
	writeGoFixture(t, root)
	writeFile(t, filepath.Join(dir, FileAcceptance), "# 验收\n\n\x60\x60\x60bash\ngo test -run TestNoSuchName ./...\n\x60\x60\x60\n")

	res, err := Verify(context.Background(), root, "selector-empty", VerifyOptions{})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if res.Summary.Sealed {
		t.Fatalf("选择器没匹配到测试时不能封印，checks=%+v", res.Summary.Checks)
	}
	if len(res.Summary.Checks) != 1 || !res.Summary.Checks[0].SelectorEmpty {
		t.Fatalf("应记 selector_empty=true：%+v", res.Summary.Checks)
	}
}

// TestVerifySealsWhenSelectorRuns：选择器真匹配到测试、命令通过 → 正常封印，
// 避免判定过宽把真跑过的判据也拦下来。
func TestVerifySealsWhenSelectorRuns(t *testing.T) {
	requireBash(t)
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go 不可用")
	}
	root, dir := repoWithChange(t, "selector-hit")
	writeGoFixture(t, root)
	writeFile(t, filepath.Join(dir, FileAcceptance), "# 验收\n\n\x60\x60\x60bash\ngo test -run TestPresent ./...\n\x60\x60\x60\n")

	res, err := Verify(context.Background(), root, "selector-hit", VerifyOptions{})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !res.Summary.Sealed {
		t.Fatalf("选择器命中且命令通过时应封印，checks=%+v", res.Summary)
	}
	for _, c := range res.Summary.Checks {
		if c.SelectorEmpty {
			t.Fatalf("选择器命中不该记 selector_empty：%+v", c)
		}
	}
}
