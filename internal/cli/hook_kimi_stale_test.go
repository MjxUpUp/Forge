package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeKimiPluginRecord writes a forge plugin record with the given github.ref into
// installed.json under home, mirroring agentbridge's installed.json schema (the same
// shape kimi-code writes on /plugins install). fmt %q avoids hand-writing quotes in the
// test source.
//
// writeKimiPluginRecord 把带指定 github.ref 的 forge plugin 记录写进 home 下的
// installed.json，镜像 agentbridge 的 installed.json schema（kimi-code 在 /plugins
// install 时写的同一形状）。用 fmt %q 避免在测试源码里手写引号。
func writeKimiPluginRecord(t *testing.T, home, refKind, refValue string, enabled bool) {
	t.Helper()
	dir := filepath.Join(home, "plugins")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	rec := fmt.Sprintf(`{"plugins":[{"id":"forge","enabled":%v,"github":{"ref":{"kind":%q,"value":%q}}}]}`, enabled, refKind, refValue)
	if err := os.WriteFile(filepath.Join(dir, "installed.json"), []byte(rec), 0644); err != nil {
		t.Fatal(err)
	}
}

// TestAppendKimiStaleAdvisoryAt pins the staleness advisory core: now + dataHome are
// injected so the once-daily throttle and the version compare are asserted without a real
// clock. installed/ok come from KIMI_CODE_HOME (set per subtest), matching production.
//
// TestAppendKimiStaleAdvisoryAt 钉住 staleness advisory 核心：now + dataHome 注入，使按日
// 节流与版本比对可脱离真实时钟断言。installed/ok 来自 KIMI_CODE_HOME（每子测试设置），
// 与生产行为一致。
func TestAppendKimiStaleAdvisoryAt(t *testing.T) {
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	today := now.Format("2006-01-02")

	t.Run("stale appends advisory and writes marker", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("KIMI_CODE_HOME", home)
		writeKimiPluginRecord(t, home, "tag", "v1.19.0", true)
		dataHome := t.TempDir()
		got := appendKimiStaleAdvisoryAt("", "1.28.3 (commit: 356dff0, built: ...)", now, dataHome)
		if !strings.Contains(got, "1.19.0") || !strings.Contains(got, "1.28.3") || !strings.Contains(got, "落后") {
			t.Fatalf("stale install must append advisory mentioning both versions + 落后, got: %q", got)
		}
		marker, err := os.ReadFile(filepath.Join(dataHome, kimiStaleMarker))
		if err != nil {
			t.Fatalf("marker must be written when advisory fires: %v", err)
		}
		if strings.TrimSpace(string(marker)) != today {
			t.Errorf("marker content = %q, want today %q", marker, today)
		}
	})

	t.Run("equal versions no append no marker", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("KIMI_CODE_HOME", home)
		writeKimiPluginRecord(t, home, "tag", "1.28.3", true)
		dataHome := t.TempDir()
		got := appendKimiStaleAdvisoryAt("", "1.28.3 (commit: x)", now, dataHome)
		if got != "" {
			t.Errorf("equal versions must not append, got: %q", got)
		}
		if _, err := os.Stat(filepath.Join(dataHome, kimiStaleMarker)); err == nil {
			t.Errorf("marker must not be written when not stale")
		}
	})

	t.Run("dev build no append", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("KIMI_CODE_HOME", home)
		writeKimiPluginRecord(t, home, "tag", "v1.19.0", true)
		dataHome := t.TempDir()
		got := appendKimiStaleAdvisoryAt("", "dev", now, dataHome)
		if got != "" {
			t.Errorf("dev build must not append, got: %q", got)
		}
	})

	t.Run("no forge entry no append", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("KIMI_CODE_HOME", home)
		dir := filepath.Join(home, "plugins")
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "installed.json"), []byte(`{"plugins":[{"id":"other","enabled":true}]}`), 0644); err != nil {
			t.Fatal(err)
		}
		dataHome := t.TempDir()
		got := appendKimiStaleAdvisoryAt("", "1.28.3", now, dataHome)
		if got != "" {
			t.Errorf("no forge entry (ok=false) must not append, got: %q", got)
		}
	})

	t.Run("no installed.json no append", func(t *testing.T) {
		t.Setenv("KIMI_CODE_HOME", t.TempDir())
		dataHome := t.TempDir()
		got := appendKimiStaleAdvisoryAt("", "1.28.3", now, dataHome)
		if got != "" {
			t.Errorf("missing installed.json must not append, got: %q", got)
		}
	})

	t.Run("commit ref no append", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("KIMI_CODE_HOME", home)
		writeKimiPluginRecord(t, home, "commit", "abc123", true)
		dataHome := t.TempDir()
		got := appendKimiStaleAdvisoryAt("", "1.28.3", now, dataHome)
		if got != "" {
			t.Errorf("non-tag ref (ok=false) must not append, got: %q", got)
		}
	})

	t.Run("throttled today no append", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("KIMI_CODE_HOME", home)
		writeKimiPluginRecord(t, home, "tag", "v1.19.0", true)
		dataHome := t.TempDir()
		// Pre-write today's marker → already nudged today, must suppress.
		// 预写今日 marker → 今日已提醒，须抑制。
		if err := os.WriteFile(filepath.Join(dataHome, kimiStaleMarker), []byte(today), 0644); err != nil {
			t.Fatal(err)
		}
		got := appendKimiStaleAdvisoryAt("", "1.28.3", now, dataHome)
		if got != "" {
			t.Errorf("today's marker must suppress (once-daily), got: %q", got)
		}
	})

	t.Run("cross-day marker re-appends and updates", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("KIMI_CODE_HOME", home)
		writeKimiPluginRecord(t, home, "tag", "v1.19.0", true)
		dataHome := t.TempDir()
		yesterday := "2026-08-08"
		if err := os.WriteFile(filepath.Join(dataHome, kimiStaleMarker), []byte(yesterday), 0644); err != nil {
			t.Fatal(err)
		}
		got := appendKimiStaleAdvisoryAt("", "1.28.3", now, dataHome)
		if !strings.Contains(got, "落后") {
			t.Errorf("stale install + yesterday's marker must re-append, got: %q", got)
		}
		marker, _ := os.ReadFile(filepath.Join(dataHome, kimiStaleMarker))
		if strings.TrimSpace(string(marker)) != today {
			t.Errorf("marker must be updated to today %q, got %q", today, marker)
		}
	})

	t.Run("preserves existing detail", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("KIMI_CODE_HOME", home)
		writeKimiPluginRecord(t, home, "tag", "v1.19.0", true)
		dataHome := t.TempDir()
		got := appendKimiStaleAdvisoryAt("prior context line", "1.28.3", now, dataHome)
		if !strings.HasPrefix(got, "prior context line") {
			t.Errorf("existing detail must be preserved as prefix, got: %q", got)
		}
		if !strings.Contains(got, "1.19.0") {
			t.Errorf("advisory must still be appended after detail, got: %q", got)
		}
	})
}
