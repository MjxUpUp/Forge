package hazard

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/forgedata/forgedatatest"
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
	// Whitespace jitter (common on agent retries) should not require re-confirmation.
	//
	// 空白抖动（agent 重试时常发生）不该要求重新确认
	a := Fingerprint("rm  -rf   /tmp/x")
	b := Fingerprint("rm -rf /tmp/x")
	if a != b {
		t.Fatalf("whitespace variance must normalize to same fingerprint:\n  %q -> %s\n  %q -> %s",
			"rm  -rf   /tmp/x", a, "rm -rf /tmp/x", b)
	}
	// Leading/trailing whitespace is also normalized.
	//
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
	root := forgedatatest.ForDataDir(t.TempDir())
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

// TestConfirmByFingerprint verifies the --fingerprint path: register directly with a given fingerprint, not relying on computing the fingerprint from the command.
// This is the recommended HITL-loop path—the hook emits a hex fingerprint, the agent passes it back, bypassing command-string copy distortion.
//
// TestConfirmByFingerprint 验证 --fingerprint 路径：按给定指纹直接登记，不依赖命令算指纹。
// 这是 HITL 闭环推荐路径——hook 输出 hex 指纹，agent 回传，绕过命令串复制失真。
func TestConfirmByFingerprint(t *testing.T) {
	root := forgedatatest.ForDataDir(t.TempDir())
	// Fake fingerprint (not Fingerprint(any cmd)), proving registration does not go through Fingerprint(cmd).
	//
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
	// The fingerprint computed via the command-string path differs from the passed-in fake fingerprint—proving the two paths are independent.
	//
	// 命令串路径算出的指纹与传入假指纹不同——证明两条路径独立。
	if Fingerprint(cmd) == fp {
		t.Fatal("fixture: fp must differ from Fingerprint(cmd) to prove independence")
	}
}

// TestConfirmByFingerprint_RejectsInvalidFormat verifies the format validation of the --fingerprint path:
// truncated/over-long/non-hex fingerprints must error, not write to a wrong filename and report false success (2026-07 AgentWorld incident:
// agent ran confirm three times with miscopied fingerprints, each printing 「✅ 已确认」, but the hook could not find them with the real fingerprint and kept blocking).
//
// TestConfirmByFingerprint_RejectsInvalidFormat 验证 --fingerprint 路径的格式校验：
// 残缺/超长/非 hex 的指纹必须报错，而非写入错文件名报虚假成功（2026-07 AgentWorld 事故：
// agent 三次 confirm 抄错指纹都打印「✅ 已确认」，hook 用真指纹查不到继续拦）。
func TestConfirmByFingerprint_RejectsInvalidFormat(t *testing.T) {
	root := forgedatatest.ForDataDir(t.TempDir())
	cmd := "rm -rf x"
	invalid := []struct{ name, fp string }{
		{"empty", ""},
		{"truncated 63 chars", strings.Repeat("a", 63)},
		{"too long 65 chars", strings.Repeat("a", 65)},
		{"64 chars but non-hex (g)", strings.Repeat("a", 63) + "g"},
	}
	for _, c := range invalid {
		err := ConfirmByFingerprint(root, c.fp, cmd)
		if err == nil {
			t.Errorf("%s: ConfirmByFingerprint should reject invalid fingerprint %q", c.name, c.fp)
			continue
		}
		// A rejected invalid fingerprint must not be persisted—otherwise DataDir/hazards/ is left with junk files.
	//
	// 非法指纹被拒不应落盘——否则 DataDir/hazards/ 留垃圾文件
		if c.fp != "" {
			if _, err := os.Stat(root.HazardsConfirmPath(c.fp)); err == nil {
				t.Errorf("%s: rejected fingerprint must not be written to disk", c.name)
			}
		}
	}

	// A valid 64-char hex passes and is written to the corresponding file.
	//
	// 合法 64 hex 通过，且写入对应文件
	valid := strings.Repeat("a", 64)
	if err := ConfirmByFingerprint(root, valid, cmd); err != nil {
		t.Fatalf("valid 64-char hex fingerprint should pass: %v", err)
	}
	if _, err := os.Stat(root.HazardsConfirmPath(valid)); err != nil {
		t.Fatal("valid fingerprint must be written to disk")
	}
}

// TestValidateFingerprint pins the exported pure-input validation contract: the cli layer uses it before findProjectRoot as
// a pre-check (so that when CI has no .forge/, not-in-a-forge-project does not mask fingerprint-validation failure). It is a
// pure function extracted from ConfirmByFingerprint—it needs no root/persistence, so an independent test pins the contract boundary, preventing future
// refactors of ConfirmByFingerprint from losing its indirect coverage (TestConfirmByFingerprint_RejectsInvalidFormat)
// without anyone noticing a regression in ValidateFingerprint itself. The error message must contain invalid fingerprint;
// the cli-layer TestRunHazardConfirm_RejectsInvalidFingerprint relies on that text to assert the error source.
//
// TestValidateFingerprint 钉住导出的纯输入校验契约：cli 层在 findProjectRoot 前用它做
// 前置校验（CI 无 .forge/ 时不让 not-in-a-forge-project 掩盖指纹校验失败）。它是从
// ConfirmByFingerprint 抽出的纯函数——不需 root/落盘，故独立测试钉契约边界，避免未来
// 重构 ConfirmByFingerprint 时其间接覆盖（TestConfirmByFingerprint_RejectsInvalidFormat）
// 丢失而 ValidateFingerprint 本身回归无人察觉。错误信息必须含 invalid fingerprint，
// cli 层 TestRunHazardConfirm_RejectsInvalidFingerprint 依赖该文本断言错误来源。
func TestValidateFingerprint(t *testing.T) {
	cases := []struct {
		name    string
		fp      string
		wantErr bool
	}{
		{"valid lowercase 64 hex", strings.Repeat(`a`, 64), false},
		{"valid uppercase normalized", strings.Repeat(`A`, 64), false},
		{"empty", ``, true},
		{"truncated 63 chars", strings.Repeat(`a`, 63), true},
		{"too long 65 chars", strings.Repeat(`a`, 65), true},
		{"64 chars but non-hex (g)", strings.Repeat(`a`, 63) + `g`, true},
	}
	for _, c := range cases {
		err := ValidateFingerprint(c.fp)
		if c.wantErr {
			if err == nil {
				t.Errorf("%s: ValidateFingerprint(%q) must error", c.name, c.fp)
				continue
			}
			if !strings.Contains(err.Error(), `invalid fingerprint`) {
				t.Errorf("%s: error must contain invalid fingerprint, got: %v", c.name, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: ValidateFingerprint(%q) must pass, got: %v", c.name, c.fp, err)
		}
	}
}

// TestConfirmByFingerprint_NormalizesUppercase verifies that upper/mixed-case hex is normalized to lowercase
// before being persisted—transcription-style agents (GPT family) prefer uppercase when regenerating long hexes; if persisted as-is, on case-sensitive filesystems
// the hook's lowercase lookup would miss them (reproducing "reporting success yet still blocked"). Normalize rather than reject: tolerant of input while guaranteeing a hit.
//
// TestConfirmByFingerprint_NormalizesUppercase 验证大写/混合大小写 hex 被归一化为小写
// 后落盘——转写型 agent（GPT 系）重生长 hex 偏好大写，若原样落盘会在大小写敏感文件系统
// 上被 hook 的小写查询漏掉（复现「报成功仍被拦」）。归一化而非拒绝，既宽容输入又保证命中。
func TestConfirmByFingerprint_NormalizesUppercase(t *testing.T) {
	root := forgedatatest.ForDataDir(t.TempDir())
	cmd := "rm -rf x"
	upper := strings.ToUpper(strings.Repeat("a", 64)) // 全大写 64 hex
	lower := strings.Repeat("a", 64)                  // 归一化后期望的小写

	if err := ConfirmByFingerprint(root, upper, cmd); err != nil {
		t.Fatalf("uppercase hex should be normalized and accepted: %v", err)
	}
	// After normalization what is persisted is the lowercase filename—so the hook's Fingerprint() lowercase lookup can hit. Use ReadDir
	// to see the actual filename rather than os.Stat(upper): Windows NTFS is case-insensitive, so Stat on an uppercase path would match the
	// lowercase file and could not distinguish them (the test must run on Windows/Linux/macOS alike).
	//
	// 归一化后落盘的是小写文件名——hook 用 Fingerprint() 的小写查询才能命中。用 ReadDir
	// 看实际文件名而非 os.Stat(upper)：Windows NTFS 大小写不敏感，Stat 大写路径会匹配到
	// 小写文件、无法区分（测试必须在 Windows/Linux/macOS 都能跑）。
	entries, err := os.ReadDir(root.HazardsDir())
	if err != nil {
		t.Fatalf("read hazards dir: %v", err)
	}
	for _, e := range entries {
		name := strings.TrimSuffix(e.Name(), ".json")
		if name != strings.ToLower(name) {
			t.Fatalf("confirmation must be stored under lowercase fingerprint, got file: %s", e.Name())
		}
	}
	if _, err := os.Stat(root.HazardsConfirmPath(lower)); err != nil {
		t.Fatalf("normalized lowercase confirmation file must exist: %v", err)
	}
	ok, _ := IsConfirmed(root, lower)
	if !ok {
		t.Fatal("IsConfirmed(lowercase) must be true after an uppercase confirm")
	}
}

func TestIsConfirmed_NotExist(t *testing.T) {
	root := forgedatatest.ForDataDir(t.TempDir())
	ok, err := IsConfirmed(root, "deadbeef")
	if err != nil {
		t.Fatalf("IsConfirmed on missing file: unexpected err %v", err)
	}
	if ok {
		t.Fatal("missing confirmation file must report not confirmed")
	}
}

func TestIsConfirmed_Expired(t *testing.T) {
	root := forgedatatest.ForDataDir(t.TempDir())
	cmd := "git push --force"
	fp := Fingerprint(cmd)

	// Hand-write an expired stamp (simulating outside the 5min window).
	//
	// 手工写过期标记（模拟 5min 窗口外）
	c := Confirmation{
		Fingerprint: fp,
		Command:     cmd,
		ConfirmedAt: time.Now().Add(-10 * time.Minute),
		ExpiresAt:   time.Now().Add(-5 * time.Minute), // 已过期
	}
	data, _ := json.MarshalIndent(c, "", "  ")
	if err := os.MkdirAll(root.HazardsDir(), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root.HazardsConfirmPath(fp), data, 0o644); err != nil {
		t.Fatal(err)
	}

	ok, _ := IsConfirmed(root, fp)
	if ok {
		t.Fatal("expired confirmation must not be honored")
	}
}

func TestIsConfirmed_CorruptFile(t *testing.T) {
	root := forgedatatest.ForDataDir(t.TempDir())
	fp := Fingerprint("rm -rf x")
	if err := os.MkdirAll(root.HazardsDir(), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root.HazardsConfirmPath(fp), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A corrupt file counts as not confirmed (rather than erroring)—next time it is blocked, re-confirm.
	//
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
	root := forgedatatest.ForDataDir(t.TempDir())
	cmd := "kubectl delete ns prod"
	fp := Fingerprint(cmd)

	// First write a stamp that is about to expire.
	//
	// 先写一个即将过期的标记
	c := Confirmation{
		Fingerprint: fp, ConfirmedAt: time.Now().Add(-4 * time.Minute),
		ExpiresAt: time.Now().Add(-1 * time.Minute), // 已过期
	}
	data, _ := json.MarshalIndent(c, "", "  ")
	os.MkdirAll(root.HazardsDir(), 0755)
	os.WriteFile(root.HazardsConfirmPath(fp), data, 0o644)

	// Confirm renews the window.
	//
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
	root := forgedatatest.ForDataDir(t.TempDir())
	// Register two active ones + hand-write one expired.
	//
	// 登记两个活跃 + 手工写一个过期
	Confirm(root, "rm -rf /tmp/a")
	Confirm(root, "git push --force")
	os.MkdirAll(root.HazardsDir(), 0755)
	expired := Confirmation{
		Fingerprint: Fingerprint("x"), ExpiresAt: time.Now().Add(-time.Hour),
	}
	ed, _ := json.MarshalIndent(expired, "", "  ")
	os.WriteFile(root.HazardsConfirmPath(Fingerprint("x")), ed, 0o644)

	active, err := ActiveConfirmations(root)
	if err != nil {
		t.Fatalf("ActiveConfirmations: %v", err)
	}
	if len(active) != 2 {
		t.Fatalf("expected 2 active confirmations, got %d", len(active))
	}
	// Expired files should be pruned.
	//
	// 过期文件应被清理
	if _, err := os.Stat(root.HazardsConfirmPath(Fingerprint("x"))); !os.IsNotExist(err) {
		t.Fatal("ActiveConfirmations must prune expired confirmation files")
	}
}

func TestActiveConfirmations_NoDir(t *testing.T) {
	root := forgedatatest.ForDataDir(t.TempDir())
	active, err := ActiveConfirmations(root)
	if err != nil {
		t.Fatalf("missing dir should not error: %v", err)
	}
	if active != nil {
		t.Fatalf("expected nil for missing dir, got %v", active)
	}
}
