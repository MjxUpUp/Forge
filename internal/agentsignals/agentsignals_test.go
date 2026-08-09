package agentsignals_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/MjxUpUp/Forge/internal/agentsignals"
)

// TestProjectAgentMarker_AllSupportedAgents verifies EVERY entry in the marker table
// resolves a fresh project dir to the right agent. This is precisely the coverage that
// was missing before the agentsignals refactor — detectAgentType only had four markers,
// so reasonix/kimi/codex/opencode/cline were invisible to session attribution (the
// "53% agent_type missing" root cause in the 2026-08-09 weekly audit).
//
// TestProjectAgentMarker_AllSupportedAgents 验证标记表里每一条都能把一个全新项目目录
// 解析到正确的 agent。这正是 agentsignals 重构前缺失的覆盖——detectAgentType 只有四个
// 标记，reasonix/kimi/codex/opencode/cline 对会话归因不可见（2026-08-09 周审计里
// "53% agent_type 缺失"的根因）。
func TestProjectAgentMarker_AllSupportedAgents(t *testing.T) {
	cases := []struct {
		name  string
		setup func(dir string)
		want  string
	}{
		{`claude`, func(d string) { os.MkdirAll(filepath.Join(d, `.claude`), 0755) }, `claude-code`},
		{`cursor`, func(d string) { os.MkdirAll(filepath.Join(d, `.cursor`), 0755) }, `cursor`},
		{`copilot`, func(d string) { os.MkdirAll(filepath.Join(d, `.github`, `instructions`), 0755) }, `copilot`},
		{`windsurf-file`, func(d string) { os.WriteFile(filepath.Join(d, `.windsurfrules`), []byte(`x`), 0644) }, `windsurf`},
		{`codex`, func(d string) { os.MkdirAll(filepath.Join(d, `.codex`), 0755) }, `codex`},
		{`opencode`, func(d string) { os.MkdirAll(filepath.Join(d, `.opencode`), 0755) }, `opencode`},
		{`cline-dir`, func(d string) { os.MkdirAll(filepath.Join(d, `.cline`), 0755) }, `cline`},
		{`clinerules`, func(d string) { os.MkdirAll(filepath.Join(d, `.clinerules`), 0755) }, `cline`},
		{`kimi`, func(d string) { os.MkdirAll(filepath.Join(d, `.kimi-code`), 0755) }, `kimi`},
		{`reasonix`, func(d string) { os.MkdirAll(filepath.Join(d, `.reasonix`), 0755) }, `reasonix`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			tc.setup(dir)
			if got := agentsignals.ProjectAgentMarker(dir); got != tc.want {
				t.Errorf("ProjectAgentMarker(%s) = %q, want %q", tc.name, got, tc.want)
			}
		})
	}
}

// TestProjectAgentMarker_NoneMatch: a project with no agent markers resolves to empty
// (the marker-absent case the hook stamp exists to fill).
//
// TestProjectAgentMarker_NoneMatch：无任何 agent 标记的项目解析为空（hook 盖戳正是为
// 补此无标记场景而存在）。
func TestProjectAgentMarker_NoneMatch(t *testing.T) {
	if got := agentsignals.ProjectAgentMarker(t.TempDir()); got != "" {
		t.Errorf("ProjectAgentMarker on empty dir = %q, want empty", got)
	}
}

// TestProjectAgentMarker_PrecedenceClaudeWins: .claude is first in the table, so a
// project with both .claude and .reasonix attributes to claude-code. This pins the
// ordered-slice first-hit-wins contract — the bug that prompted detectAgentType's
// fixed-priority fix (an earlier map-range version returned a random pick per process
// start). Iterating 20× would surface a map-iteration regression as a flip.
//
// TestProjectAgentMarker_PrecedenceClaudeWins：.claude 在表中居首，故同时有 .claude 与
// .reasonix 的项目归为 claude-code。钉死有序切片"首个命中胜出"契约——正是
// detectAgentType 固定优先级修复所针对的 bug（早期 map 遍历版本每次进程启动随机取）。
// 重复 20 次会让 map 遍历回归表现为结果翻转。
func TestProjectAgentMarker_PrecedenceClaudeWins(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, `.claude`), 0755)
	os.MkdirAll(filepath.Join(dir, `.reasonix`), 0755)
	for i := 0; i < 20; i++ {
		if got := agentsignals.ProjectAgentMarker(dir); got != `claude-code` {
			t.Fatalf("iter %d: ProjectAgentMarker = %q, want claude-code (deterministic first-hit wins)", i, got)
		}
	}
}

// TestProjectAgentMarker_WindsurfrulesMustBeFile: .windsurfrules is isFile=true, so a
// DIRECTORY at that name must NOT match — otherwise any project that happens to have a
// .windsurfrules/ directory would be mis-attributed to windsurf.
//
// TestProjectAgentMarker_WindsurfrulesMustBeFile：.windsurfrules 是 isFile=true，故同名
// 目录绝不能命中——否则任何恰好有 .windsurfrules/ 目录的项目都会被误归 windsurf。
func TestProjectAgentMarker_WindsurfrulesMustBeFile(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, `.windsurfrules`), 0755) // a dir, not a file
	if got := agentsignals.ProjectAgentMarker(dir); got != "" {
		t.Errorf("ProjectAgentMarker with .windsurfrules DIR = %q, want empty (marker requires a file)", got)
	}
}

// TestProjectAgentMarkers_AllMatchesDeduped: a project with .cline + .clinerules (both
// map to cline) + .reasonix returns ["cline","reasonix"] — cline appears once. Order
// follows the table (cline before reasonix).
//
// TestProjectAgentMarkers_AllMatchesDeduped：含 .cline + .clinerules（都映射 cline）+
// .reasonix 的项目返回 ["cline","reasonix"]——cline 只出现一次。顺序遵循表（cline 在
// reasonix 之前）。
func TestProjectAgentMarkers_AllMatchesDeduped(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, `.cline`), 0755)
	os.MkdirAll(filepath.Join(dir, `.clinerules`), 0755)
	os.MkdirAll(filepath.Join(dir, `.reasonix`), 0755)
	got := agentsignals.ProjectAgentMarkers(dir)
	want := []string{`cline`, `reasonix`}
	if len(got) != len(want) {
		t.Fatalf("ProjectAgentMarkers = %v, want %v", got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("ProjectAgentMarkers[%d] = %q, want %q (full: %v)", i, got[i], w, got)
		}
	}
}
