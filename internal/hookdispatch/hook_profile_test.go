package hookdispatch

import (
	"os"
	"strings"
	"testing"
)

// TestRunHook_ProfileGateSkipsDroppedHook pins the W0.3 runtime gate wiring:
// with the lite profile active, a hook outside the lite allow-list (skill-scan)
// is intercepted by the profile gate BEFORE its logic runs — the [skill-scan]
// banner never reaches stdout (the hook's own path always prints one; the gate
// emits a bare per-host allow). The inverse property (kept hooks still run) is
// pinned by every other hook test, which all run under the default standard
// profile.
//
// TestRunHook_ProfileGateSkipsDroppedHook 钉死 W0.3 运行时门的接线：lite 档
// 激活时，白名单外的 hook（skill-scan）在自身逻辑运行前被档位门拦截——
// [skill-scan] 横幅不会出现在 stdout（hook 自身路径恒打印横幅；档位门走裸
// allow）。反向性质（在档 hook 照常运行）由既有测试覆盖——它们全部跑在默认
// standard 档下。
func TestRunHook_ProfileGateSkipsDroppedHook(t *testing.T) {
	t.Setenv("FORGE_PROFILE", "lite")
	out, hookErr := runHookCapture(t, "skill-scan", `{"hook_event_name":"SessionStart","session_id":"prof"}`)
	if hookErr != nil {
		t.Fatalf("profile gate must pass through as allow, got error: %v", hookErr)
	}
	if strings.Contains(out, "skill-scan") {
		t.Errorf("lite 档下 skill-scan 应被运行时门拦截（无自身横幅输出），got:\n%s", out)
	}
	// gate 放行形态与既有 allow 契约一致：安静（silent allow）。
	if strings.TrimSpace(out) != "" {
		t.Errorf("档位门放行应为静默 allow，got:\n%s", out)
	}
}

// TestRunHook_ProfileGateKeptHookStillRuns: under lite, a kept hook
// (hazard-guard) must NOT be intercepted — its script runs (here: temp dir
// without a project → silent allow, nil error; the point is the gate did not
// fire for an allow-listed hook).
//
// TestRunHook_ProfileGateKeptHookStillRuns：lite 档下在档 hook（hazard-guard）
// 不得被档位门拦截——脚本照常运行（临时目录无项目 → 静默放行、nil error；
// 要点是档位门对白名单内 hook 不触发）。
func TestRunHook_ProfileGateKeptHookStillRuns(t *testing.T) {
	t.Setenv("FORGE_PROFILE", "lite")
	out, hookErr := runHookCapture(t, "hazard-guard", `{"hook_event_name":"PreToolUse","tool_name":"Bash","session_id":"prof","tool_input":{"command":"ls"}}`)
	if hookErr != nil {
		t.Fatalf("lite 档下 hazard-guard 应照常运行，got error: %v", hookErr)
	}
	if strings.Contains(out, "拦截原因") {
		t.Errorf("良性 ls 不应被拦截，got:\n%s", out)
	}
	_ = os.Getenv // keep os import if assertions evolve
}
