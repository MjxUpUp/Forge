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

// 阶段 B（ADR-0002）把执法从「默认开」翻成「opt-in，默认 audit-only」。下面的
// 策略测试逐个钉住新语义；每个用例用 t.Setenv 显式声明 FORGE_HOOKS，与 TestMain
// 为端到端行为测试设置的 FORGE_HOOKS=on 隔离，避免新默认被包级默认掩盖。

// TestContractAllowsHook_DefaultQuiet：无契约且未开 FORGE_HOOKS → 一切 hook
// （含安全底线）静默放行。root 为空（宿主层未解析出根）同样默认安静。
func TestContractAllowsHook_DefaultQuiet(t *testing.T) {
	t.Setenv("FORGE_HOOKS", "")
	for _, name := range []string{"bash-guard", "task-guard", "auto-compile", "hazard-guard", "mcp-scan", "skill-scan"} {
		if contractAllowsHook(t.TempDir(), name) {
			t.Fatalf("默认 audit-only：无契约时 %s 不该执法", name)
		}
	}
	if contractAllowsHook("", "bash-guard") {
		t.Fatal("root 为空且未开 FORGE_HOOKS 时应默认安静")
	}
	if contractAllowsHook("", "hazard-guard") {
		t.Fatal("root 为空时安全底线也应默认安静（阶段 B 底线随执法开关）")
	}
}

// TestContractAllowsHook_ContractOff：契约 hooks:off（含缺省）静默放行，安全底线
// 也不例外；且仓库契约 off 优先于宿主逃生舱 FORGE_HOOKS=on（仓库自治）。
func TestContractAllowsHook_ContractOff(t *testing.T) {
	t.Setenv("FORGE_HOOKS", "on") // 即便宿主逃生舱开着，仓库 off 仍优先。
	off := t.TempDir()
	writeContract(t, off, "version: \"1\"\nhooks: off\n")
	for _, name := range []string{"bash-guard", "task-guard", "auto-compile", "hazard-guard", "mcp-scan", "skill-scan"} {
		if contractAllowsHook(off, name) {
			t.Fatalf("hooks: off 时 %s 不该执法（含安全底线，阶段 B 语义）", name)
		}
	}
	// 缺省 hooks（specroot 默认 off）等价于 off。
	dflt := t.TempDir()
	writeContract(t, dflt, "version: \"1\"\nprofile: standard\n")
	if contractAllowsHook(dflt, "bash-guard") {
		t.Fatal("契约缺省 hooks 应为 off（阶段 B），不该执法")
	}
}

// TestContractAllowsHook_ContractOn：显式 hooks:on 执法，不依赖宿主逃生舱；
// 底线执法不受契约 profile 收窄，其余按契约档位裁（mini＝lite）。
func TestContractAllowsHook_ContractOn(t *testing.T) {
	t.Setenv("FORGE_HOOKS", "") // 契约 on 独立生效。
	on := t.TempDir()
	writeContract(t, on, "version: \"1\"\nhooks: on\n")
	for _, name := range []string{"bash-guard", "auto-compile", "hazard-guard", "mcp-scan", "skill-scan"} {
		if !contractAllowsHook(on, name) {
			t.Fatalf("hooks: on 时 %s 应执法", name)
		}
	}
	// profile: mini → lite 名册；advisory 类不在其中，但底线不受收窄。
	mini := t.TempDir()
	writeContract(t, mini, "version: \"1\"\nhooks: on\nprofile: mini\n")
	if !contractAllowsHook(mini, "bash-guard") {
		t.Fatal("mini 档里 bash-guard 属于 lite 名册")
	}
	if contractAllowsHook(mini, "auto-compile") {
		t.Fatal("mini 档不该跑 advisory 类 hook")
	}
	if !contractAllowsHook(mini, "mcp-scan") {
		t.Fatal("hooks: on 时安全底线不该被 profile: mini 收窄掉")
	}
}

// TestContractAllowsHook_BrokenQuiet：契约解析失败 → 安静。opt-in posture 下坏
// 契约不该把执法打开（翻转前是 fail-open）；返回 false 也不会让 hook 炸掉。
func TestContractAllowsHook_BrokenQuiet(t *testing.T) {
	t.Setenv("FORGE_HOOKS", "on")
	broken := t.TempDir()
	writeContract(t, broken, "version: \"1\"\nprofile: 不存在的档\n")
	if contractAllowsHook(broken, "bash-guard") {
		t.Fatal("契约非法时应安静（audit-only），不该 fail-open 执法")
	}
}

// TestContractAllowsHook_HostOptIn：无契约宿主层由 FORGE_HOOKS=on 显式开启执法，
// 名册按全局档位（TestMain 设 standard）裁。
func TestContractAllowsHook_HostOptIn(t *testing.T) {
	t.Setenv("FORGE_HOOKS", "on")
	for _, name := range []string{"bash-guard", "hazard-guard", "mcp-scan", "skill-scan"} {
		if !contractAllowsHook(t.TempDir(), name) {
			t.Fatalf("FORGE_HOOKS=on 时宿主层 %s 应执法", name)
		}
	}
	if !contractAllowsHook(t.TempDir(), "auto-compile") {
		t.Fatal("standard 档位宿主层应跑 advisory 类 auto-compile")
	}
}

// TestHooksEnforceEnv：FORGE_HOOKS 取值解析（1/on/true/yes，大小写/首尾空白不敏感）。
func TestHooksEnforceEnv(t *testing.T) {
	for _, v := range []string{"1", "on", "true", "yes", "ON", " true "} {
		t.Setenv("FORGE_HOOKS", v)
		if !hooksEnforceEnv() {
			t.Fatalf("FORGE_HOOKS=%q 应开启执法", v)
		}
	}
	for _, v := range []string{"", "0", "off", "no", "随便"} {
		t.Setenv("FORGE_HOOKS", v)
		if hooksEnforceEnv() {
			t.Fatalf("FORGE_HOOKS=%q 不该开启执法（默认安静）", v)
		}
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
