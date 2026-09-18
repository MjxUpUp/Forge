package cli

// update_apply_test.go —— `forge update --apply` 的守卫（发布流程排查 P1-1：
// 发版后发布者本机停在旧版直到人工质疑——update 通知只打印命令不代执行，
// "发版完成"与"本机可用"之间断层）。--apply 让 npm 通道代跑安装命令
// （npmUpdateCommand 单一真相源），装后跑 --version 自验（warning 级）。

import (
	"errors"
	"os"
	"strings"
	"testing"
)

// TestUpdateApplyRunsNpmInstall pins the apply path: with the npm channel and
// a newer remote version, --apply executes the package-manager install command
// (single truth source npmUpdateCommand) instead of printing guidance; a
// failed install surfaces the installer output as an error.
//
// TestUpdateApplyRunsNpmInstall 钉住 apply 路径：npm 通道且远端更新时，--apply
// 执行包管理器安装命令（npmUpdateCommand 单一真相源构造）而非打印指引；安装
// 失败把安装器输出作为 error 上抛。
func TestUpdateApplyRunsNpmInstall(t *testing.T) {
	restore := patchUpdateDeps(t, installChannel{kind: channelNPM, pm: "npm"}, "9.9.9")
	defer restore()

	var gotCmd []string
	installErr := error(nil)
	updateApplyInstallFn = func(args []string) (string, error) {
		gotCmd = args
		if installErr != nil {
			return "installer boom", installErr
		}
		return "added 2 packages", nil
	}

	// 直接函数调用绕过 cobra 组树（updateCmd 挂在带 GroupID 的真树上，裸 root
	// 会 panic）；Version 设在真根（runUpdate 读 cmd.Root().Version），flag 置位。
	oldVersion := rootCmd.Version
	rootCmd.Version = "1.62.0 (commit: x, built: y)"
	t.Cleanup(func() { rootCmd.Version = oldVersion })
	updateApplyFlag = true
	t.Cleanup(func() { updateApplyFlag = false })

	// 成功路径：执行安装命令而非打印指引。
	stderr := captureStderr(t, func() {
		if err := runUpdate(updateCmd, nil); err != nil {
			t.Fatalf("--apply must succeed when the installer succeeds: %v", err)
		}
	})
	want := []string{"npm", "install", "-g", "@agent_forge/forge@9.9.9"}
	if strings.Join(gotCmd, " ") != strings.Join(want, " ") {
		t.Errorf("installer args = %v, want %v (npmUpdateCommand truth source)", gotCmd, want)
	}
	if !strings.Contains(stderr, "@agent_forge/forge@9.9.9") {
		t.Errorf("stderr should echo the executed command, got: %q", stderr)
	}

	// 失败路径：安装器输出随 error 上抛（error 由调用方打印，不经本函数 stderr）。
	installErr = errors.New("EPERM")
	var runErr error
	captureStderr(t, func() {
		runErr = runUpdate(updateCmd, nil)
	})
	if runErr == nil {
		t.Fatal("installer failure must surface as an error")
	}
	if !strings.Contains(runErr.Error(), "installer boom") || !strings.Contains(runErr.Error(), "EPERM") {
		t.Errorf("failure must carry installer output, got: %v", runErr)
	}
}

// patchUpdateDeps 注入通道/版本源/安装器并返回恢复函数。
func patchUpdateDeps(t *testing.T, ch installChannel, latest string) func() {
	t.Helper()
	oldChannel, oldLatest, oldApply := detectInstallChannelFn, updateLatestFromNPMFn, updateApplyInstallFn
	detectInstallChannelFn = func() installChannel { return ch }
	updateLatestFromNPMFn = func() (string, error) { return latest, nil }
	updateApplyInstallFn = func(args []string) (string, error) { return "", nil }
	if oldReg, ok := os.LookupEnv("FORGE_NPM_REGISTRY"); ok {
		t.Cleanup(func() { _ = os.Setenv("FORGE_NPM_REGISTRY", oldReg) })
	}
	_ = os.Unsetenv("FORGE_NPM_REGISTRY") // 复审 P3-6：保存旧值恢复，不向后续用例泄漏
	return func() {
		detectInstallChannelFn, updateLatestFromNPMFn, updateApplyInstallFn = oldChannel, oldLatest, oldApply
	}
}

// TestNpmInstallArgsMatchesGuidanceCommand pins the two command constructions
// against drift: npmUpdateCommand renders what the user is TOLD to run,
// npmInstallArgs builds what --apply ACTUALLY runs — the printed command must
// stay byte-equal to the executed argv or --apply loses its "no surprise" trust.
//
// TestNpmInstallArgsMatchesGuidanceCommand 钉住两份命令构造不漂移：
// npmUpdateCommand 渲染「告诉用户跑什么」，npmInstallArgs 构造 --apply「实际
// 跑什么」——展示串与执行 argv 必须逐字节一致，否则 --apply 失去"所见即所跑"
// 的信任。
func TestNpmInstallArgsMatchesGuidanceCommand(t *testing.T) {
	for _, pm := range []string{"npm", "pnpm", "yarn", "bun", "unknown-fallback"} {
		version := "9.9.9"
		args, err := npmInstallArgs(pm, version)
		if err != nil {
			t.Fatalf("pm %q: %v", pm, err)
		}
		if got, want := strings.Join(args, " "), npmUpdateCommand(pm, version); got != want {
			t.Errorf("pm %q: executed argv %q != printed guidance %q (drift!)", pm, got, want)
		}
	}
	// 非 semver 版本：消费点拒绝（防御纵深，不依赖生产 seam 内部校验）。
	if _, err := npmInstallArgs("npm", "9.9.9; rm -rf /"); err == nil {
		t.Fatal("non-semver version must be rejected at the consumption point")
	}
}
