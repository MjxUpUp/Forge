package projectroot

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/registry"
)

// TestState_SpecContractManaged：declared-only 仓库是 managed，来源 spec-contract
// ——展示面不能再把「有契约」报成 unknown（status 与 policy state 曾互相矛盾）。
func TestState_SpecContractManaged(t *testing.T) {
	root := t.TempDir()
	writeSpecContract(t, root)

	gotRoot, source, status, err := State(root)
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	if !sameProjectRoot(t, gotRoot, root) || source != SourceSpecContract || status != registry.StatusManaged {
		t.Fatalf("State = (%q, %q, %q), want (%q, spec-contract, managed)", gotRoot, source, status, root)
	}
}

// TestState_SpecContractDeclined：declined 是状态而不是错误——报告面要看得到它，
// 只有门禁面（ResolveFrom）才把它折成 ErrDeclinedProject。
func TestState_SpecContractDeclined(t *testing.T) {
	root := t.TempDir()
	writeSpecContract(t, root)
	if err := registry.SetStatus(root, registry.StatusDeclined, "forge off"); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}

	_, source, status, err := State(root)
	if err != nil {
		t.Fatalf("State 不应把 declined 当错误: %v", err)
	}
	if source != SourceSpecContract || status != registry.StatusDeclined {
		t.Fatalf("State = (%q, %q), want (spec-contract, declined)", source, status)
	}

	// 门禁面语义不变：ResolveFrom 仍拒绝 declined。
	if _, _, err := ResolveFrom(root); !errors.Is(err, registry.ErrDeclinedProject) {
		t.Fatalf("ResolveFrom declined err = %v, want ErrDeclinedProject", err)
	}
}

// TestState_NoMembership：既无契约也无登记 → ErrNoForgeConfig（调用方据此打印 unknown）。
func TestState_NoMembership(t *testing.T) {
	root := t.TempDir()

	if _, _, _, err := State(root); !errors.Is(err, forgedata.ErrNoForgeConfig) {
		t.Fatalf("State err = %v, want ErrNoForgeConfig", err)
	}
}

// TestState_RegistryFallback：没有契约时仍按注册表解析，来源保持 registry。
func TestState_RegistryFallback(t *testing.T) {
	root := t.TempDir()
	if err := registry.Add(root); err != nil {
		t.Fatalf("Add: %v", err)
	}

	gotRoot, source, status, err := State(root)
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	if !sameProjectRoot(t, gotRoot, root) || source != SourceRegistry || status != registry.StatusManaged {
		t.Fatalf("State = (%q, %q, %q), want (%q, registry, managed)", gotRoot, source, status, root)
	}
}

// TestState_RegistryDeclinedWithoutContract：注册表里明确退出、仓库又没有契约时，
// State 必须报 declined 而不是 unknown——旧的布尔成员资格判定把 declined 当非成员，只看它
// 的旧实现会把「拒绝」读成「从未登记」。
func TestState_RegistryDeclinedWithoutContract(t *testing.T) {
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	root := t.TempDir()
	if err := registry.Add(root); err != nil {
		t.Fatalf("registry add: %v", err)
	}
	if err := registry.SetStatus(root, registry.StatusDeclined, `test`); err != nil {
		t.Fatalf("set status: %v", err)
	}

	gotRoot, source, status, err := State(root)
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	if !sameProjectRoot(t, gotRoot, root) || source != SourceRegistry || status != registry.StatusDeclined {
		t.Fatalf("State = (%q, %q, %q), want (%q, registry, declined)", gotRoot, source, status, root)
	}
}

// TestState_LegacyConfigManagedWithoutHealing：遗留 .forge/ 项目在 State 下报
// managed 但不写入注册表——State 是只读探针，自愈只属于 ResolveFrom。
func TestState_LegacyConfigManagedWithoutHealing(t *testing.T) {
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, `.forge`), 0o755); err != nil {
		t.Fatal(err)
	}

	gotRoot, source, status, err := State(root)
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	if !sameProjectRoot(t, gotRoot, root) || source != SourceLegacyConfig || status != registry.StatusManaged {
		t.Fatalf("State = (%q, %q, %q), want (%q, legacy, managed)", gotRoot, source, status, root)
	}
	if _, state := registry.State(root); state != registry.StatusUnknown {
		t.Fatal("State 不该自愈登记——只读探针不能改全局注册表")
	}

	// ResolveFrom 仍保留自愈：门禁/热路径第一次解析就把项目登记下来。
	if _, _, err := ResolveFrom(root); err != nil {
		t.Fatalf("ResolveFrom: %v", err)
	}
	if _, state := registry.State(root); state != registry.StatusManaged {
		t.Fatal("ResolveFrom 应自愈登记遗留项目")
	}
}
