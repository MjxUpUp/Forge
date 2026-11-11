package projectroot

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/registry"
	"github.com/MjxUpUp/Forge/internal/specroot"
)

// writeSpecContract 在 root 下写最小契约 spec/forge.yaml。
func writeSpecContract(t *testing.T, root string) {
	t.Helper()
	dir := filepath.Join(root, specroot.DirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, specroot.MarkerName), []byte("version: \"1\"\n"), 0o644); err != nil {
		t.Fatalf("write contract: %v", err)
	}
}

func mkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
}

// TestResolveFrom_SpecContractDeclaresMembership：仓库自带契约即成员资格，
// 且来源标注为 spec-contract（不依赖注册表登记）。
func TestResolveFrom_SpecContractDeclaresMembership(t *testing.T) {
	root := t.TempDir()
	writeSpecContract(t, root)
	deep := filepath.Join(root, "pkg", "sub")
	mkdir(t, deep)

	got, src, err := ResolveFrom(deep)
	if err != nil {
		t.Fatalf("ResolveFrom: %v", err)
	}
	if filepath.Clean(got) != filepath.Clean(root) {
		t.Fatalf("root=%q, want %q", got, root)
	}
	if src != SourceSpecContract {
		t.Fatalf("source=%q, want %q", src, SourceSpecContract)
	}
	// 成员资格必须让 ProjectFor 可用（DataDir 稳定），否则 hook/CLI 拿不到 store。
	if _, perr := forgedata.ProjectFor(got); perr != nil {
		t.Fatalf("ProjectFor(%q): %v", got, perr)
	}
}

// TestResolveFrom_SpecContractBeatsRegistry：同一仓库既在注册表又有契约时，
// 来源仍报 spec-contract（契约是仓库态锚点，注册表退居兜底）。
func TestResolveFrom_SpecContractBeatsRegistry(t *testing.T) {
	root := t.TempDir()
	writeSpecContract(t, root)
	if err := registry.Add(root); err != nil {
		t.Fatalf("registry.Add: %v", err)
	}
	deep := filepath.Join(root, "src")
	mkdir(t, deep)

	got, src, err := ResolveFrom(deep)
	if err != nil {
		t.Fatalf("ResolveFrom: %v", err)
	}
	if filepath.Clean(got) != filepath.Clean(root) || src != SourceSpecContract {
		t.Fatalf("got (%q,%q), want (%q,%q)", got, src, root, SourceSpecContract)
	}
}

// TestResolveFrom_DeclinedVetoesContract：人已明确退出接管时，仓库里的契约
// 不得让 forge 自动复活。
func TestResolveFrom_DeclinedVetoesContract(t *testing.T) {
	root := t.TempDir()
	writeSpecContract(t, root)
	if err := registry.Add(root); err != nil {
		t.Fatalf("registry.Add: %v", err)
	}
	if err := registry.SetStatus(root, registry.StatusDeclined, "forge off"); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}

	if _, _, err := ResolveFrom(root); !errors.Is(err, registry.ErrDeclinedProject) {
		t.Fatalf("err=%v, want ErrDeclinedProject", err)
	}
}

// TestResolveFrom_RegistryFallback：无契约但在注册表登记的项目仍解析成功，
// 来源标注 registry（既有行为不回退）。
func TestResolveFrom_RegistryFallback(t *testing.T) {
	root := t.TempDir()
	if err := registry.Add(root); err != nil {
		t.Fatalf("registry.Add: %v", err)
	}
	deep := filepath.Join(root, "src")
	mkdir(t, deep)

	got, src, err := ResolveFrom(deep)
	if err != nil {
		t.Fatalf("ResolveFrom: %v", err)
	}
	if filepath.Clean(got) != filepath.Clean(root) {
		t.Fatalf("root=%q, want %q", got, root)
	}
	if src != SourceRegistry {
		t.Fatalf("source=%q, want %q", src, SourceRegistry)
	}
}

// TestResolveFrom_Neither：既无契约也未登记 → ErrNoForgeConfig。
func TestResolveFrom_Neither(t *testing.T) {
	if _, _, err := ResolveFrom(t.TempDir()); !errors.Is(err, forgedata.ErrNoForgeConfig) {
		t.Fatalf("err=%v, want ErrNoForgeConfig", err)
	}
}
