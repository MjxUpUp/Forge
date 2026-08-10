package cli

import (
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/taskpipeline"
)

// TestTaskStart_TTLFlagPersists (#6, design §3/§9 --ttl): --ttl on task start persists into
// state.TTL, and omitting it leaves the zero value (backward-compatible fallback to the global 7d
// constant via health.effectiveTTL). Pins the CLI write side of the per-task TTL override — the
// read side is covered by taskpipeline.TestEffectiveTTL / TestIsOfferedZombie_PerTaskTTL.
//
// LoadTaskState takes the PROJECT root (the dir), not a data dir: its dataHome(root) runs
// forgedata.DataDirFor(root) itself, so passing an already-resolved projects/<key> path would
// double-wrap. The subprocess (runForge) and this load both derive from the same project dir,
// hence the same projects/<key>/tasks/<sanitized-ref>.json.
//
// TestTaskStart_TTLFlagPersists（#6，设计 §3/§9 --ttl）：task start 的 --ttl 持久化进 state.TTL，
// 不带时留零值（经 health.effectiveTTL 向后兼容回落全局 7d 常量）。钉住 per-task TTL 覆盖的 CLI
// 写入侧——读取侧由 taskpipeline.TestEffectiveTTL / TestIsOfferedZombie_PerTaskTTL 覆盖。
// LoadTaskState 接收 PROJECT 根（dir），非 data dir：其 dataHome(root) 自跑 forgedata.DataDirFor(root)，
// 传已解析的 projects/<key> 路径会 double-wrap。子进程（runForge）与本 load 都从同一 project dir
// 派生，故同一 projects/<key>/tasks/<sanitized-ref>.json。
func TestTaskStart_TTLFlagPersists(t *testing.T) {
	dir := setupDelegateProject(t)

	if out, _, code := runForge(t, dir, "task", "start", "--ref", "feat/ttl", "--title", "ttl task", "--ttl", "24h"); code != 0 {
		t.Fatalf("task start --ttl: %s", out)
	}
	state, err := taskpipeline.LoadTaskState(dir, "feat/ttl")
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	if want := 24 * time.Hour; state.TTL != want {
		t.Errorf("state.TTL 应持久化 24h, got=%v want=%v", state.TTL, want)
	}

	// Omitting --ttl leaves TTL at zero — backward-compatible global fallback (health.effectiveTTL
	// returns the 7d constant for a zero TTL, so legacy tasks behave exactly as before).
	//
	// 不带 --ttl 时 TTL 留零值——向后兼容回落全局（health.effectiveTTL 对零 TTL 返回 7d 常量，
	// 故 legacy 任务行为完全不变）。
	if out, _, code := runForge(t, dir, "task", "start", "--ref", "feat/no-ttl", "--title", "no ttl"); code != 0 {
		t.Fatalf("task start: %s", out)
	}
	state2, err := taskpipeline.LoadTaskState(dir, "feat/no-ttl")
	if err != nil {
		t.Fatalf("load state2: %v", err)
	}
	if state2.TTL != 0 {
		t.Errorf("不带 --ttl 时 TTL 应为零值（回落全局）, got=%v", state2.TTL)
	}
}
