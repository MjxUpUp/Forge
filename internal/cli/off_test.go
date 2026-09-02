package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/hookdispatch"
	"github.com/MjxUpUp/Forge/internal/registry"
)

// off_test.go — Project Policy Layer P1 的命令面契约：forge off/on 对称翻转、
// init 对 declined 的硬门禁、suggest decline 委托同一核心。
// 行为契约见 docs/design/project-policy-layer.md「行为契约」表。

// markerPathOf 返回指定项目根的 legacy 提示标记路径（与 suggest.go/hook 同源）。
func markerPathOf(root string) string {
	return filepath.Join(suggestStateDir(), hookdispatch.SuggestTagFor(root))
}

// assertState 断言注册表状态并在失败时给出可读信息。
func assertState(t *testing.T, root, want string) {
	t.Helper()
	_, state := registry.State(root)
	if state != want {
		t.Fatalf(`registry state = %q, want %q`, state, want)
	}
}

// TestOff_LifecycleManagedProject managed 项目 off→declined（注册表 + legacy 标记
// 双写）、on→managed（清标记）。inited 项目（DataDir 已有 protocol.yml）的 on 不做
// 重新 init。
func TestOff_LifecycleManagedProject(t *testing.T) {
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	proj := t.TempDir()
	t.Chdir(proj)

	if err := registry.Add(proj); err != nil {
		t.Fatal(err)
	}
	// 模拟已 init（runOn 据此跳过重新 init 提示）。
	if err := writeTestProtocol(proj); err != nil {
		t.Fatal(err)
	}

	if err := runOff(offCmd, nil); err != nil {
		t.Fatalf(`runOff: %v`, err)
	}
	assertState(t, proj, registry.StatusDeclined)
	if data, err := os.ReadFile(markerPathOf(proj)); err != nil || strings.TrimSpace(string(data)) != `declined` {
		t.Errorf(`legacy marker = (%q, %v), want "declined"`, string(data), err)
	}
	// off 后 IsMember 必须为假——所有 project-scoped hook 的闸门。
	if _, ok := registry.IsMember(proj); ok {
		t.Error(`IsMember true after forge off`)
	}

	if err := runOn(onCmd, nil); err != nil {
		t.Fatalf(`runOn: %v`, err)
	}
	assertState(t, proj, registry.StatusManaged)
	if _, err := os.Stat(markerPathOf(proj)); !os.IsNotExist(err) {
		t.Errorf(`marker not removed by forge on (err=%v)`, err)
	}
	if _, ok := registry.IsMember(proj); !ok {
		t.Error(`IsMember false after forge on`)
	}
}

// TestOff_Idempotent 重复 off 幂等、不报错。
func TestOff_Idempotent(t *testing.T) {
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	proj := t.TempDir()
	t.Chdir(proj)

	if err := runOff(offCmd, nil); err != nil {
		t.Fatalf(`first off: %v`, err)
	}
	if err := runOff(offCmd, nil); err != nil {
		t.Fatalf(`second off: %v`, err)
	}
	assertState(t, proj, registry.StatusDeclined)
}

// TestInit_RefusesDeclined declined 项目 forge init 必须拒绝（错误文案指向
// forge on）——plugin auto-takeover / FORGE_AUTO_INIT 静默 init 的 Go 侧硬门禁。
func TestInit_RefusesDeclined(t *testing.T) {
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	proj := t.TempDir()
	t.Chdir(proj)

	if err := registry.SetStatus(proj, registry.StatusDeclined, `forge off`); err != nil {
		t.Fatal(err)
	}

	err := runInit(initCmd, nil)
	if err == nil {
		t.Fatal(`runInit succeeded on declined project`)
	}
	if !strings.Contains(err.Error(), `forge on`) {
		t.Errorf(`error does not point to forge on: %v`, err)
	}
	assertState(t, proj, registry.StatusDeclined)
}

// TestSuggestDecline_DelegatesToRegistry forge suggest decline 与 forge off 写同一
// 状态（双通道一致，防"标记 declined 但注册表 managed"的漂移）。
func TestSuggestDecline_DelegatesToRegistry(t *testing.T) {
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	proj := t.TempDir()
	t.Chdir(proj)

	if err := suggestDeclineCmd.RunE(suggestDeclineCmd, nil); err != nil {
		t.Fatalf(`suggest decline: %v`, err)
	}
	assertState(t, proj, registry.StatusDeclined)
	if data, err := os.ReadFile(markerPathOf(proj)); err != nil || strings.TrimSpace(string(data)) != `declined` {
		t.Errorf(`marker = (%q, %v), want declined`, string(data), err)
	}
}

// TestOffAll_FlipsEveryAliveEntry off --all 把全部存活条目置 declined（含逐条
// legacy 标记）；declined-only 条目幂等重跑无害。
func TestOffAll_FlipsEveryAliveEntry(t *testing.T) {
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	a, b := t.TempDir(), t.TempDir()
	t.Chdir(a)
	if err := registry.Add(a); err != nil {
		t.Fatal(err)
	}
	if err := registry.Add(b); err != nil {
		t.Fatal(err)
	}

	if err := offCmd.Flags().Set(`all`, `true`); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = offCmd.Flags().Set(`all`, `false`) // cobra flag 粘滞，跨测试复位
	}()
	if err := runOff(offCmd, nil); err != nil {
		t.Fatalf(`runOff --all: %v`, err)
	}
	for _, p := range []string{a, b} {
		assertState(t, p, registry.StatusDeclined)
		if _, ok := registry.IsMember(p); ok {
			t.Errorf(`IsMember(%s) true after off --all`, p)
		}
		if data, rerr := os.ReadFile(markerPathOf(p)); rerr != nil || strings.TrimSpace(string(data)) != `declined` {
			t.Errorf(`marker for %s = (%q, %v), want declined`, p, string(data), rerr)
		}
	}
}

// TestOn_UnknownRejects 从未登记的项目 forge on 拒绝并指向 forge init（on 只负责
// declined→managed，不是第二个 init）。
func TestOn_UnknownRejects(t *testing.T) {
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	proj := t.TempDir()
	t.Chdir(proj)

	err := runOn(onCmd, nil)
	if err == nil {
		t.Fatal(`runOn succeeded on unknown project`)
	}
	if !strings.Contains(err.Error(), `forge init`) {
		t.Errorf(`error does not point to forge init: %v`, err)
	}
}

// TestStatus_DeclinedFriendlyError off 后 forge status 以 ErrDeclinedProject 文案
// 非零退出（成员探测契约：init-suggest 依赖该退出码）。
func TestStatus_DeclinedFriendlyError(t *testing.T) {
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	proj := t.TempDir()
	t.Chdir(proj)

	if err := registry.SetStatus(proj, registry.StatusDeclined, `forge off`); err != nil {
		t.Fatal(err)
	}
	err := runStatus(statusCmd, nil)
	if !errors.Is(err, registry.ErrDeclinedProject) {
		t.Fatalf(`runStatus err = %v, want ErrDeclinedProject`, err)
	}
}

// TestSuggestReset_ResumesDeclined suggest reset 在 declined 时 ≡ forge on
// （否则 reset 清了标记但注册表仍 declined，init 被门禁拒绝——reset 语义落空）。
func TestSuggestReset_ResumesDeclined(t *testing.T) {
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	proj := t.TempDir()
	t.Chdir(proj)

	if err := registry.SetStatus(proj, registry.StatusDeclined, `forge off`); err != nil {
		t.Fatal(err)
	}
	if err := writeSuggestMarker(hookdispatch.SuggestTagFor(proj), `declined`); err != nil {
		t.Fatal(err)
	}

	if err := suggestResetCmd.RunE(suggestResetCmd, nil); err != nil {
		t.Fatalf(`suggest reset: %v`, err)
	}
	assertState(t, proj, registry.StatusManaged)
	if _, err := os.Stat(markerPathOf(proj)); !os.IsNotExist(err) {
		t.Errorf(`marker not removed by suggest reset (err=%v)`, err)
	}
}

// writeTestProtocol 在用户级 DataDir 放一个 protocol.yml 占位（模拟已 init 项目），
// 避免单测触达真实用户级 agent 配置（runInitUserLevel 会写 ~/.claude 等）。
// DataDir 用 forgedata.DataDirFor（与 init 的 protocol.EnsureDefault 同一键）。
func writeTestProtocol(proj string) error {
	dir := forgedata.DataDirFor(proj)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, `protocol.yml`), []byte("stack: go\n"), 0644)
}
