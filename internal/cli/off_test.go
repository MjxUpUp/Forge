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

// TestPolicyYield_ForeignHarnessLetsGo P4 让位：高置信信号命中 → declined（by
// 带信号名）+ 输出让位说明；无信号输出为空且不改变状态。
func TestPolicyYield_ForeignHarnessLetsGo(t *testing.T) {
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	proj := t.TempDir()
	t.Chdir(proj)
	if err := os.MkdirAll(filepath.Join(proj, `.specify`), 0755); err != nil {
		t.Fatal(err)
	}

	stdout, _, err := captureOutput(t, func() error {
		return policyYieldCmd.RunE(policyYieldCmd, nil)
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, `已让位`) || !strings.Contains(stdout, `.specify`) {
		t.Errorf(`yield 输出缺让位说明: %q`, stdout)
	}
	assertState(t, proj, registry.StatusDeclined)

	// 无信号：空输出、不改状态（unknown 保持）。
	clean := t.TempDir()
	t.Chdir(clean)
	stdout2, _, err := captureOutput(t, func() error {
		return policyYieldCmd.RunE(policyYieldCmd, nil)
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(stdout2) != `` {
		t.Errorf(`无信号应静默，实得 %q`, stdout2)
	}
	assertState(t, clean, registry.StatusUnknown)
}

// TestOffCommit_WritesDeclineFile / TestOn_RemovesDeclineFile 团队声明文件
// （.forge-decline，deny-wins）：off --commit 写入；on 移除并翻转。
func TestOffCommit_WritesDeclineFile(t *testing.T) {
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	proj := t.TempDir()
	mkGitProjCLI(t, proj) // --commit 声明按 git 根键控，非 git 仓库被拒
	t.Chdir(proj)

	if err := offCmd.Flags().Set(`commit`, `true`); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = offCmd.Flags().Set(`commit`, `false`) }()
	if err := runOff(offCmd, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(proj, registry.DeclineFileName)); err != nil {
		t.Fatalf(`.forge-decline not written: %v`, err)
	}
	assertState(t, proj, registry.StatusDeclined)
}

func TestDeclineFile_DenyWinsOverManagedRegistry(t *testing.T) {
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	proj := t.TempDir()
	mkGitProjCLI(t, proj) // 声明文件按 git 根键控——deny-wins 只在 git 分支生效
	t.Chdir(proj)

	// 注册表 managed + 声明文件存在 → State=declined（deny-wins），IsMember false。
	if err := registry.SetStatus(proj, registry.StatusManaged, `forge on`); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, registry.DeclineFileName), []byte(`x`), 0644); err != nil {
		t.Fatal(err)
	}
	if _, ok := registry.IsMember(proj); ok {
		t.Error(`声明文件存在时 IsMember 必须为假（deny-wins）`)
	}
	if _, state := registry.State(proj); state != registry.StatusDeclined {
		t.Errorf(`State = %q, want declined（声明文件压制）`, state)
	}

	// forge on：移除声明文件 + 翻转 → managed。
	if err := runOn(onCmd, nil); err != nil {
		t.Fatalf(`forge on under decline file: %v`, err)
	}
	if _, err := os.Stat(filepath.Join(proj, registry.DeclineFileName)); !os.IsNotExist(err) {
		t.Errorf(`forge on 未移除声明文件 (err=%v)`, err)
	}
	assertState(t, proj, registry.StatusManaged)
}

// TestWarnForeignHarness 手动 init 的外来 harness 警告（P4）：命中信号打警告
// （不阻断——init 接线由 runInit 调用本 helper，这里单测 helper 本体；runInit 的
// 全量路径写用户级 agent 资产，不在 cli 单测里跑）。
func TestWarnForeignHarness(t *testing.T) {
	proj := t.TempDir()
	if err := os.MkdirAll(filepath.Join(proj, `.specify`), 0755); err != nil {
		t.Fatal(err)
	}

	_, stderr, err := captureOutput(t, func() error {
		warnForeignHarness(proj)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr, `自有 harness 信号`) || !strings.Contains(stderr, `.specify`) {
		t.Errorf(`警告缺信号说明: %q`, stderr)
	}

	// 无信号静默。
	clean := t.TempDir()
	_, stderr2, _ := captureOutput(t, func() error {
		warnForeignHarness(clean)
		return nil
	})
	if strings.TrimSpace(stderr2) != `` {
		t.Errorf(`无信号应静默，实得 %q`, stderr2)
	}
}
