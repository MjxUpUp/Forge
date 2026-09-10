package cli

import (
	"testing"

	"github.com/MjxUpUp/Forge/internal/forgedata/forgedatatest"
	"github.com/MjxUpUp/Forge/internal/hazard"
	"github.com/spf13/cobra"
)

// TestRunHazardLog_StampsSessionFromHookEnv pins the F.3 wiring at the CLI layer: `forge hazard
// log` (called by the hazard-guard hook script) carries the hook environment's FORGE_SESSION_ID
// into the event so the double-delivery dedupe key gains the session dimension, and a second
// identical call inside the window is collapsed.
//
// TestRunHazardLog_StampsSessionFromHookEnv 钉死 F.3 在 CLI 层的接线：hazard-guard 脚本调的
// `forge hazard log` 把 hook 环境的 FORGE_SESSION_ID 带进事件（去重键获得会话维度），窗口内
// 第二次同调用被合并；无 env 时字段留空（终端直跑形态）。
func TestRunHazardLog_StampsSessionFromHookEnv(t *testing.T) {
	root, p := forgedatatest.RealProject(t)
	t.Chdir(root)
	t.Setenv("FORGE_SESSION_ID", "sess-hazard-x")

	args := []string{"block", "git", "push", "origin", "--force"}
	for i := 0; i < 2; i++ {
		if err := runHazardLog(&cobra.Command{}, args); err != nil {
			t.Fatalf("runHazardLog #%d: %v", i+1, err)
		}
	}
	events, err := hazard.LoadEvents(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("double delivery within window must collapse to 1 event, got %d", len(events))
	}
	if events[0].SessionID != "sess-hazard-x" {
		t.Fatalf("event session_id = %q, want FORGE_SESSION_ID value", events[0].SessionID)
	}
	if events[0].Fingerprint != hazard.Fingerprint("git push origin --force") {
		t.Fatalf("fingerprint must be computed from the joined command, got %q", events[0].Fingerprint)
	}

	// 无 env（终端直跑）：字段留空；与前一条会话不同但一侧为空 → 退化为只比 type+指纹，仍去重。
	t.Setenv("FORGE_SESSION_ID", "")
	if err := runHazardLog(&cobra.Command{}, args); err != nil {
		t.Fatal(err)
	}
	events, _ = hazard.LoadEvents(p)
	if len(events) != 1 {
		t.Fatalf("session-less follow-up must still dedupe against the stamped row, got %d", len(events))
	}
	// 不同命令：新事件，且无 env 时 session 留空。
	if err := runHazardLog(&cobra.Command{}, []string{"block", "shred", "/dev/null"}); err != nil {
		t.Fatal(err)
	}
	events, _ = hazard.LoadEvents(p)
	if len(events) != 2 || events[1].SessionID != "" {
		t.Fatalf("terminal-run event must be appended with empty session, got %+v", events)
	}
}
