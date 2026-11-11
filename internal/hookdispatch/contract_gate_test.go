package hookdispatch

import (
	"os"
	"path/filepath"
	"testing"
)

func writeContract(t *testing.T, root, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "spec"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "spec", "forge.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestContractAllowsHook(t *testing.T) {
	// 无契约：维持既有行为，hook 照常跑。
	if !contractAllowsHook(t.TempDir(), "bash-guard") {
		t.Fatal("无契约时不应改变既有行为")
	}

	// hooks: off → 全部放行（仓库明确表示不要执法）。
	off := t.TempDir()
	writeContract(t, off, "version: \"1\"\nhooks: off\n")
	for _, name := range []string{"bash-guard", "task-guard", "auto-compile"} {
		if contractAllowsHook(off, name) {
			t.Fatalf("hooks: off 时 %s 不该运行", name)
		}
	}

	// profile: mini → 只有 lite 名册；advisory 类不在其中。
	mini := t.TempDir()
	writeContract(t, mini, "version: \"1\"\nprofile: mini\n")
	if !contractAllowsHook(mini, "bash-guard") {
		t.Fatal("mini 档里 bash-guard 属于 lite 名册")
	}
	if contractAllowsHook(mini, "auto-compile") {
		t.Fatal("mini 档不该跑 advisory 类 hook")
	}

	// 契约写坏：fail-open，不能让 hook 因为解析失败而炸掉。
	broken := t.TempDir()
	writeContract(t, broken, "version: \"1\"\nprofile: 不存在的档\n")
	if !contractAllowsHook(broken, "bash-guard") {
		t.Fatal("契约非法时应 fail-open")
	}
}

func TestProfileForContract(t *testing.T) {
	cases := map[string]string{
		"mini":     "lite",
		"standard": "standard",
		"full":     "full",
		"":         "standard",
		"garbage":  "standard",
	}
	for in, want := range cases {
		if got := string(profileForContract(in)); got != want {
			t.Fatalf("profileForContract(%q)=%s，期望 %s", in, got, want)
		}
	}
}

func TestInitSuggestEnabled(t *testing.T) {
	for _, v := range []string{"1", "on", "true", "yes", "ON", " true "} {
		t.Setenv("FORGE_INIT_SUGGEST", v)
		if !initSuggestEnabled() {
			t.Fatalf("FORGE_INIT_SUGGEST=%q 应开启", v)
		}
	}
	for _, v := range []string{"", "0", "off", "no", "随便"} {
		t.Setenv("FORGE_INIT_SUGGEST", v)
		if initSuggestEnabled() {
			t.Fatalf("FORGE_INIT_SUGGEST=%q 不该开启（默认关闭）", v)
		}
	}
}
