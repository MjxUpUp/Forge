package projectroot

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/MjxUpUp/Forge/internal/registry"
)

// policy_test.go — Project Policy Layer P1 的 Find 收编契约：declined 项目的
// legacy .forge/ 自愈分支不得复活成员资格（对抗复查 H2-①），自愈本身保留。

// TestFind_DeclinedLegacyForgeDirNotSelfHealed 团队模式/老项目（含项目级 .forge/）
// declined 后：Find 从子目录解析必须返回 ErrDeclinedProject（而非自愈登记成
// managed 成员），注册表条目保持 declined。这是"退出一票否决"在 Find 层的钉子。
func TestFind_DeclinedLegacyForgeDirNotSelfHealed(t *testing.T) {
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, `.forge`), 0755); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, `pkg`)
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}

	if err := registry.Add(root); err != nil {
		t.Fatal(err)
	}
	if err := registry.SetStatus(root, registry.StatusDeclined, `forge off`); err != nil {
		t.Fatal(err)
	}

	chdirTo(t, sub)
	_, err := Find()
	if !errors.Is(err, registry.ErrDeclinedProject) {
		t.Fatalf(`FindAt err = %v, want ErrDeclinedProject`, err)
	}
	// 条目不得被自愈翻回 managed。
	if _, state := registry.State(sub); state != registry.StatusDeclined {
		t.Errorf(`entry state after Find = %q, want declined（自愈不得复活）`, state)
	}
}

// TestFind_LegacySelfHealStillWorks 无 declined 状态的 legacy .forge/ 项目自愈
// 行为保持：Find 返回根并登记（钉住"收编不误伤"）。
func TestFind_LegacySelfHealStillWorks(t *testing.T) {
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, `.forge`), 0755); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, `pkg`)
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}

	chdirTo(t, sub)
	got, err := Find()
	if err != nil {
		t.Fatalf(`Find err = %v, want nil`, err)
	}
	// t.TempDir 在 macOS 是 /var → /private/var 符号链接，返回的是解析形态——
	// 与既有 TestFind_LocatesForgeRoot 同款稳健断言（.forge 在 got 下）。
	if _, serr := os.Stat(filepath.Join(got, `.forge`)); serr != nil {
		t.Fatalf(`Find returned %q, no .forge/ under it: %v`, got, serr)
	}
	if _, ok := registry.IsMember(sub); !ok {
		t.Error(`self-heal did not register member`)
	}
}
