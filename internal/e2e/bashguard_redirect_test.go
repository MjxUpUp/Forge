package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/forgedata"
)

// TestBashGuard_TmpRedirectNotWrite 钉住 2026-08-24 豁免：重定向目标在仓库外
// （/tmp、$TMPDIR、~/.forge、/dev/null 等明确非源码路径）不算「Bash write」——
// 该 guard 拦的是未被任务追踪的源码变更，日志重定向不是。生产误报：
// `go test ./... > /tmp/final.txt 2>&1` 与 `gh run watch ... >
// /tmp/forge-ci-watch.log 2>&1` 都在会话中触发过 "[bash-guard] Bash write
// without active task"。
func TestBashGuard_TmpRedirectNotWrite(t *testing.T) {
	dir := freshProject(t)
	const sid = "sess-bg-redir"
	tmp := t.TempDir()

	// 置 source-touched 会话标记（task-guard 在源码 Write|Edit 后会置）——否则
	// WARN 分支因另一原因（调研模式静默）本就不进，豁免测不到。
	markersDir := filepath.Join(forgedata.DataDirFor(dir), "markers")
	if err := os.MkdirAll(markersDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(markersDir, "forge-source-touched-"+sid), []byte("1"), 0644); err != nil {
		t.Fatal(err)
	}

	// /tmp 重定向：非源码变更 → IS_WRITE_CMD=0，无 no-task WARN，write-flag
	// 为空（file-sentinel 次级门按只读处理）。
	in := hookStdin(t, sid, "PreToolUse", "Bash", map[string]any{
		"command": "go test ./... > /tmp/forge-final.txt 2>&1",
	})
	out, _, err := forgeHookShared(t, dir, tmp, "bash-guard", in)
	if err != nil {
		t.Fatalf("bash-guard: %v", err)
	}
	if strings.Contains(out, "without active task") {
		t.Errorf("/tmp redirect must not trip the no-task WARN (log redirect is not a source change). Got:\n%s", out)
	}
	wflags, _ := filepath.Glob(filepath.Join(tmp, "forge-write-"+sid+"-*"))
	if len(wflags) != 1 {
		t.Fatalf("bash-guard must create exactly one per-invocation write-flag, got %v", wflags)
	}
	if info, serr := os.Stat(wflags[0]); serr != nil || info.Size() != 0 {
		t.Errorf("/tmp redirect must record an EMPTY write-flag (read-only class), err=%v", serr)
	}

	// 对照：重定向到仓库内文件仍是 write——write-flag 非空且有首条 WARN。
	in2 := hookStdin(t, sid, "PreToolUse", "Bash", map[string]any{
		"command": "echo x > notes.md",
	})
	out2, _, err := forgeHookShared(t, dir, tmp, "bash-guard", in2)
	if err != nil {
		t.Fatalf("bash-guard control: %v", err)
	}
	if !strings.Contains(out2, "without active task") {
		t.Errorf("repo-file redirect must still trip the no-task WARN (control). Got:\n%s", out2)
	}
	wflags2, _ := filepath.Glob(filepath.Join(tmp, "forge-write-"+sid+"-*"))
	foundNonEmpty := false
	for _, f := range wflags2 {
		if info, serr := os.Stat(f); serr == nil && info.Size() > 0 {
			foundNonEmpty = true
		}
	}
	if !foundNonEmpty {
		t.Errorf("repo-file redirect must record a non-empty write-flag (control), flags=%v", wflags2)
	}

	// 穿越出豁免区（/tmp/../...）不得豁免。
	in3 := hookStdin(t, sid, "PreToolUse", "Bash", map[string]any{
		"command": "cat x > /tmp/../etc/forge-should-not-exempt.conf",
	})
	if _, _, err := forgeHookShared(t, dir, tmp, "bash-guard", in3); err != nil {
		t.Fatalf("bash-guard traversal case: %v", err)
	}
	wflags3, _ := filepath.Glob(filepath.Join(tmp, "forge-write-"+sid+"-*"))
	nonEmptyCount := 0
	for _, f := range wflags3 {
		if info, serr := os.Stat(f); serr == nil && info.Size() > 0 {
			nonEmptyCount++
		}
	}
	if nonEmptyCount != 2 {
		t.Errorf("traversal target must record a non-empty write-flag (two non-empty flags total: control + traversal), got %d of %v", nonEmptyCount, wflags3)
	}

	// 命令替换/变量目标静态不可判定——提取目标会在 $(...) 内首个空白处截断、
	// `..` 残留被丢弃（'cmd > /tmp/$(echo ../../repo)/x' 即 review Major 的
	// 绕过样本）——必须保守判 write。豁免仅限字面路径。
	for _, cmd := range []string{
		"cat x > /tmp/$(echo ../../repo)/evil.go",
		"cat x > /tmp/`echo ../../repo`/evil.go",
	} {
		inCmd := hookStdin(t, sid, "PreToolUse", "Bash", map[string]any{
			"command": cmd,
		})
		if _, _, err := forgeHookShared(t, dir, tmp, "bash-guard", inCmd); err != nil {
			t.Fatalf("bash-guard substitution case %q: %v", cmd, err)
		}
	}
	wflags4, _ := filepath.Glob(filepath.Join(tmp, "forge-write-"+sid+"-*"))
	nonEmptyCount = 0
	for _, f := range wflags4 {
		if info, serr := os.Stat(f); serr == nil && info.Size() > 0 {
			nonEmptyCount++
		}
	}
	if nonEmptyCount != 4 {
		t.Errorf("substitution/variable targets must record non-empty write-flags (4 total: control + traversal + 2 substitution), got %d of %v", nonEmptyCount, wflags4)
	}
}
