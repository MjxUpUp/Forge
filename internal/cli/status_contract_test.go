package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeContractForStatus(t *testing.T, root, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "spec"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "spec", "forge.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// 契约能读：status 显示实际生效的 profile/hooks，而不是只有一句「有契约」。
func TestContractStatusLines_ShowsEffectiveSettings(t *testing.T) {
	root := t.TempDir()
	writeContractForStatus(t, root, "version: \"1\"\nprofile: mini\nhooks: off\n")

	got := strings.Join(contractStatusLines(root), "\n")
	if !strings.Contains(got, "profile=mini hooks=off") {
		t.Fatalf("应显示契约实际档位，得到：%s", got)
	}
}

// 契约读不出来：必须让人看见审计模式——写错拼写时执法没开（ADR-0002 阶段 B：
// opt-in posture 下坏契约不打开执法，也不回落全局档位）。
func TestContractStatusLines_WarnsOnBrokenContract(t *testing.T) {
	root := t.TempDir()
	writeContractForStatus(t, root, "version: \"1\"\nhooks: of\n")

	got := strings.Join(contractStatusLines(root), "\n")
	if !strings.Contains(got, "契约无法解析") {
		t.Fatalf("契约写坏时应告警，得到：%s", got)
	}
	if !strings.Contains(got, "审计模式，不执法") {
		t.Fatalf("坏契约应显示审计模式（不执法），得到：%s", got)
	}
	if !strings.Contains(got, "hooks: on") {
		t.Fatalf("应提示修好契约并写 hooks: on 才执法，得到：%s", got)
	}
}
