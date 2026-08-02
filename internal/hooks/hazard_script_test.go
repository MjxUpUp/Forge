package hooks

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Script-level tests for the embedded HazardGuardHook diagnostics path
// (weekly-hardening fix a): when `forge hazard confirmed` itself fails with a
// stderr error (confirm-chain fault — kimi 30s timeout, autoSync slowing forge
// startup, environment breakage), the block output must carry the stderr
// snippet instead of swallowing it (the old >/dev/null 2>&1 made the failure
// invisible). A shim `forge` on PATH simulates the confirm chain — the real
// binary is not needed, mirroring the sentinel_script_test pattern.

// writeForgeShim installs a fake `forge` executable into a temp dir. The shim
// answers the three hazard subcommands the hook script calls:
//   - hazard fingerprint → a fixed 64-hex fingerprint on stdout
//   - hazard confirmed   → behavior controlled by confirmedMode:
//     "release" = exit 0 (confirmed), anything else = stderr error + exit 1
//   - hazard log         → exit 0 (audit sink)
func writeForgeShim(t *testing.T, confirmedMode string) string {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not on PATH — skipping script-level hazard test")
	}
	dir := t.TempDir()
	shim := `#!/bin/bash
if [ "$1" = "hazard" ] && [ "$2" = "fingerprint" ]; then
  echo "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
  exit 0
fi
if [ "$1" = "hazard" ] && [ "$2" = "confirmed" ]; then
  if [ "` + confirmedMode + `" = "release" ]; then
    exit 0
  fi
  echo "[hazard] simulated confirm-chain store corruption" >&2
  exit 1
fi
if [ "$1" = "hazard" ] && [ "$2" = "log" ]; then
  exit 0
fi
exit 1
`
	path := filepath.Join(dir, "forge")
	if err := os.WriteFile(path, []byte(shim), 0755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// runHazardScript executes the embedded HazardGuardHook with FORGE_COMMAND and
// a shim forge on PATH.
func runHazardScript(t *testing.T, shimDir, command string) (string, error) {
	t.Helper()
	f, err := os.CreateTemp("", "forge-hook-*.sh")
	if err != nil {
		t.Fatalf("createtemp: %v", err)
	}
	if _, err := f.WriteString(HazardGuardHook); err != nil {
		t.Fatalf("write script: %v", err)
	}
	f.Close()
	defer os.Remove(f.Name())

	cmd := exec.Command("bash", f.Name())
	cmd.Dir = t.TempDir()
	cmd.Env = append(os.Environ(),
		"FORGE_COMMAND="+command,
		"PATH="+shimDir+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	out, runErr := cmd.CombinedOutput()
	return string(out), runErr
}

// TestHazardGuardScript_ConfirmedFailureDiagnostic: a confirmed-call fault
// (stderr + exit 1) must surface in the block output — the pre-fix
// >/dev/null 2>&1 swallowed it, leaving the kimi confirm-chain failure with no
// trace.
//
// TestHazardGuardScript_ConfirmedFailureDiagnostic：confirmed 调用本身故障
// （stderr + exit 1）必须出现在 block 输出里——修复前的 >/dev/null 2>&1 会吞掉，
// kimi 确认链失败曾无迹可查。
func TestHazardGuardScript_ConfirmedFailureDiagnostic(t *testing.T) {
	shimDir := writeForgeShim(t, "fail")
	out, err := runHazardScript(t, shimDir, "rm -rf ./important-data")
	if err == nil {
		t.Fatalf("hazard-guard must block hazardous command when confirmed fails, got exit 0:\n%s", out)
	}
	if !strings.Contains(out, "确认链诊断") {
		t.Errorf("block output must carry the confirm-chain diagnostic section, got:\n%s", out)
	}
	if !strings.Contains(out, "simulated confirm-chain store corruption") {
		t.Errorf("block output must include the confirmed stderr snippet (first 200 chars), got:\n%s", out)
	}
	if !strings.Contains(out, "forge hazard status") {
		t.Errorf("block output must point at 'forge hazard status' for further diagnosis, got:\n%s", out)
	}
}

// TestHazardGuardScript_ConfirmedCleanRelease: a successful confirmed call
// (exit 0) must release — the diagnostic plumbing must not break the normal
// HITL release path.
//
// TestHazardGuardScript_ConfirmedCleanRelease：confirmed 成功（exit 0）必须
// 放行——诊断管线不得破坏正常 HITL 放行路径。
func TestHazardGuardScript_ConfirmedCleanRelease(t *testing.T) {
	shimDir := writeForgeShim(t, "release")
	out, err := runHazardScript(t, shimDir, "rm -rf ./important-data")
	if err != nil {
		t.Fatalf("hazard-guard must release after confirm, got block:\n%s", out)
	}
	if !strings.Contains(out, "已确认放行") {
		t.Errorf("release path must report the confirmed-pass message, got:\n%s", out)
	}
}

// TestHazardGuardScript_EnvBypassRemoved: FORGE_ALLOW_HAZARD=1 must have no
// effect on the script anymore — the env escape branch was removed (agent
// self-release abuse). With an unconfirmed fingerprint the command still
// blocks even when the env is set.
//
// TestHazardGuardScript_EnvBypassRemoved：FORGE_ALLOW_HAZARD=1 对脚本不再
// 生效——env 豁免分支已移除（agent 自我放行滥用）。未确认时即便设了 env 仍
// 必须拦截。
func TestHazardGuardScript_EnvBypassRemoved(t *testing.T) {
	shimDir := writeForgeShim(t, "fail")

	f, err := os.CreateTemp("", "forge-hook-*.sh")
	if err != nil {
		t.Fatalf("createtemp: %v", err)
	}
	if _, err := f.WriteString(HazardGuardHook); err != nil {
		t.Fatalf("write script: %v", err)
	}
	f.Close()
	defer os.Remove(f.Name())

	cmd := exec.Command("bash", f.Name())
	cmd.Dir = t.TempDir()
	cmd.Env = append(os.Environ(),
		"FORGE_COMMAND=rm -rf ./important-data",
		"FORGE_ALLOW_HAZARD=1",
		"PATH="+shimDir+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	out, runErr := cmd.CombinedOutput()
	if runErr == nil {
		t.Fatalf("hazard-guard must block with FORGE_ALLOW_HAZARD=1 (env escape removed), got exit 0:\n%s", out)
	}
	if strings.Contains(string(out), "FORGE_ALLOW_HAZARD=1 跳过") {
		t.Errorf("env escape branch must be gone from the script output, got:\n%s", out)
	}
}

// TestHazardGuardScript_InterpDeleteBypass: script-level pin for the
// interpreter inline-delete bypass (weekly-hardening c), including the node
// require('fs').rmSync quote-normalization case that "fs.rm" substring matching
// alone misses (normalized form is require(fs).rmsync( — a ")" intervenes).
// The e2e counterpart is TestHook_HazardGuard_InterpreterDeleteBypassBlocked;
// this script-level twin gives fast iteration without a forge binary build.
//
// TestHazardGuardScript_InterpDeleteBypass：解释器内联删除旁路的脚本级钉
// （周复盘 c），含 node require('fs').rmSync 的引号归一案例——仅匹配 "fs.rm"
// 子串会漏（归一形态是 require(fs).rmsync(，中间隔了 ")"）。e2e 对照是
// TestHook_HazardGuard_InterpreterDeleteBypassBlocked；脚本级孪生测试不用
// 构建 forge 二进制，迭代更快。
func TestHazardGuardScript_InterpDeleteBypass(t *testing.T) {
	shimDir := writeForgeShim(t, "fail")
	block := []string{
		`python -c "import os;os.remove('./important.txt')"`,
		`python3 -c "import shutil;shutil.rmtree('./build')"`,
		`node -e "require('fs').rmSync('./data',{recursive:true})"`,
		`node -e "require('fs').unlink('./f')"`,
	}
	for _, cmd := range block {
		out, err := runHazardScript(t, shimDir, cmd)
		if err == nil {
			t.Errorf("hazard-guard must block interpreter inline-delete %q, got exit 0:\n%s", cmd, out)
		}
	}
	pass := []string{
		`python -c "print(1)"`,
		`node -e "console.log('ok')"`,
		`python scripts/train.py --epochs 3`,
	}
	for _, cmd := range pass {
		out, err := runHazardScript(t, shimDir, cmd)
		if err != nil {
			t.Errorf("hazard-guard must pass benign interpreter command %q, got block:\n%s", cmd, out)
		}
	}
}
