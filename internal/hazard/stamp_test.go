package hazard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFingerprint_Stable(t *testing.T) {
	cmd := "rm -rf /tmp/forge-test"
	a := Fingerprint(cmd)
	b := Fingerprint(cmd)
	if a != b {
		t.Fatalf("same command must yield same fingerprint: %s vs %s", a, b)
	}
	if a == "" {
		t.Fatal("fingerprint empty")
	}
}

func TestFingerprint_WhitespaceNormalized(t *testing.T) {
	// 空白抖动（agent 重试时常发生）不该要求重新确认
	a := Fingerprint("rm  -rf   /tmp/x")
	b := Fingerprint("rm -rf /tmp/x")
	if a != b {
		t.Fatalf("whitespace variance must normalize to same fingerprint:\n  %q -> %s\n  %q -> %s",
			"rm  -rf   /tmp/x", a, "rm -rf /tmp/x", b)
	}
	// 首尾空白也归一
	if Fingerprint("  rm -rf /tmp/x  ") != b {
		t.Fatal("leading/trailing whitespace must normalize")
	}
}

func TestFingerprint_DifferentCommandsDiffer(t *testing.T) {
	if Fingerprint("rm -rf /tmp/x") == Fingerprint("rm -rf /tmp/y") {
		t.Fatal("different targets must yield different fingerprints")
	}
	if Fingerprint("rm -rf /tmp/x") == Fingerprint("git push --force") {
		t.Fatal("different commands must yield different fingerprints")
	}
}

func TestConfirm_AndIsConfirmed(t *testing.T) {
	root := t.TempDir()
	cmd := "rm -rf /tmp/forge-test"

	if ok, _ := IsConfirmed(root, Fingerprint(cmd)); ok {
		t.Fatal("unconfirmed command should not be confirmed")
	}

	fp, err := Confirm(root, cmd)
	if err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	if fp != Fingerprint(cmd) {
		t.Fatal("Confirm must return Fingerprint(cmd)")
	}

	ok, err := IsConfirmed(root, fp)
	if err != nil {
		t.Fatalf("IsConfirmed: %v", err)
	}
	if !ok {
		t.Fatal("after Confirm, IsConfirmed must be true")
	}
}

// TestConfirmByFingerprint 验证 --fingerprint 路径：按给定指纹直接登记，不依赖命令算指纹。
// 这是 HITL 闭环推荐路径——hook 输出 hex 指纹，agent 回传，绕过命令串复制失真。
func TestConfirmByFingerprint(t *testing.T) {
	root := t.TempDir()
	// 假指纹（非 Fingerprint(any cmd)），证明登记不经 Fingerprint(cmd)。
	fp := "abc123def4567890abc123def4567890abc123def4567890abc123def4567890"
	cmd := "mysql -e 'DROP TABLE t'" // 含单引号——命令串路径易失真的典型

	if ok, _ := IsConfirmed(root, fp); ok {
		t.Fatal("unconfirmed fingerprint should not be confirmed")
	}
	if err := ConfirmByFingerprint(root, fp, cmd); err != nil {
		t.Fatalf("ConfirmByFingerprint: %v", err)
	}
	ok, err := IsConfirmed(root, fp)
	if err != nil {
		t.Fatalf("IsConfirmed: %v", err)
	}
	if !ok {
		t.Fatal("after ConfirmByFingerprint, IsConfirmed(fp) must be true")
	}
	// 命令串路径算出的指纹与传入假指纹不同——证明两条路径独立。
	if Fingerprint(cmd) == fp {
		t.Fatal("fixture: fp must differ from Fingerprint(cmd) to prove independence")
	}
}

func TestIsConfirmed_NotExist(t *testing.T) {
	root := t.TempDir()
	ok, err := IsConfirmed(root, "deadbeef")
	if err != nil {
		t.Fatalf("IsConfirmed on missing file: unexpected err %v", err)
	}
	if ok {
		t.Fatal("missing confirmation file must report not confirmed")
	}
}

func TestIsConfirmed_Expired(t *testing.T) {
	root := t.TempDir()
	cmd := "git push --force"
	fp := Fingerprint(cmd)

	// 手工写过期标记（模拟 5min 窗口外）
	c := Confirmation{
		Fingerprint: fp,
		Command:     cmd,
		ConfirmedAt: time.Now().Add(-10 * time.Minute),
		ExpiresAt:   time.Now().Add(-5 * time.Minute), // 已过期
	}
	data, _ := json.MarshalIndent(c, "", "  ")
	if err := os.MkdirAll(filepath.Join(root, ".forge", "hazards"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pathOf(root, fp), data, 0o644); err != nil {
		t.Fatal(err)
	}

	ok, _ := IsConfirmed(root, fp)
	if ok {
		t.Fatal("expired confirmation must not be honored")
	}
}

func TestIsConfirmed_CorruptFile(t *testing.T) {
	root := t.TempDir()
	fp := Fingerprint("rm -rf x")
	if err := os.MkdirAll(filepath.Join(root, ".forge", "hazards"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pathOf(root, fp), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 损坏视为未确认（而非报错）——下次拦了重新确认
	ok, err := IsConfirmed(root, fp)
	if err != nil {
		t.Fatalf("corrupt file should not error: %v", err)
	}
	if ok {
		t.Fatal("corrupt confirmation must report not confirmed")
	}
}

func TestConfirm_RenewsWindow(t *testing.T) {
	root := t.TempDir()
	cmd := "kubectl delete ns prod"
	fp := Fingerprint(cmd)

	// 先写一个即将过期的标记
	c := Confirmation{
		Fingerprint: fp, ConfirmedAt: time.Now().Add(-4 * time.Minute),
		ExpiresAt: time.Now().Add(-1 * time.Minute), // 已过期
	}
	data, _ := json.MarshalIndent(c, "", "  ")
	os.MkdirAll(filepath.Join(root, ".forge", "hazards"), 0755)
	os.WriteFile(pathOf(root, fp), data, 0o644)

	// Confirm 续期
	if _, err := Confirm(root, cmd); err != nil {
		t.Fatal(err)
	}
	ok, _ := IsConfirmed(root, fp)
	if !ok {
		t.Fatal("Confirm must renew the window so an expired-stamped command is confirmed again")
	}
}

func TestActiveConfirmations_ListsAndPrunes(t *testing.T) {
	root := t.TempDir()
	// 登记两个活跃 + 手工写一个过期
	Confirm(root, "rm -rf /tmp/a")
	Confirm(root, "git push --force")
	os.MkdirAll(filepath.Join(root, ".forge", "hazards"), 0755)
	expired := Confirmation{
		Fingerprint: Fingerprint("x"), ExpiresAt: time.Now().Add(-time.Hour),
	}
	ed, _ := json.MarshalIndent(expired, "", "  ")
	os.WriteFile(pathOf(root, Fingerprint("x")), ed, 0o644)

	active, err := ActiveConfirmations(root)
	if err != nil {
		t.Fatalf("ActiveConfirmations: %v", err)
	}
	if len(active) != 2 {
		t.Fatalf("expected 2 active confirmations, got %d", len(active))
	}
	// 过期文件应被清理
	if _, err := os.Stat(pathOf(root, Fingerprint("x"))); !os.IsNotExist(err) {
		t.Fatal("ActiveConfirmations must prune expired confirmation files")
	}
}

func TestActiveConfirmations_NoDir(t *testing.T) {
	root := t.TempDir()
	active, err := ActiveConfirmations(root)
	if err != nil {
		t.Fatalf("missing dir should not error: %v", err)
	}
	if active != nil {
		t.Fatalf("expected nil for missing dir, got %v", active)
	}
}
