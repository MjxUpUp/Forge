package projectroot

import (
	"errors"
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
	if gotRoot != root || source != SourceSpecContract || status != registry.StatusManaged {
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
	if gotRoot != root || source != SourceRegistry || status != registry.StatusManaged {
		t.Fatalf("State = (%q, %q, %q), want (%q, registry, managed)", gotRoot, source, status, root)
	}
}
