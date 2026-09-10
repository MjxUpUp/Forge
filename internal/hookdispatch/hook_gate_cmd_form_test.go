package hookdispatch

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/checklog"
)

// gate-cmd-form hook tests (design C, 1.56 advisory form): a non-compliant gate command on
// PreToolUse Bash records one warn row + stderr advisory and never blocks; compliant and
// non-gate commands are silent.
//
// gate-cmd-form hook 测试（设计 C，1.56 advisory 形态）：PreToolUse Bash 上的非合规门禁命令
// 落一条 warn 行 + stderr advisory 且永不阻断；合规与非门禁命令静默。
func TestGateCmdFormHook_AdvisoryAndSilence(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FORGE_DATA_HOME", t.TempDir())

	mk := func(cmd string) HookInput {
		return HookInput{HookEventName: "PreToolUse", ToolName: "Bash",
			ToolInput: json.RawMessage(`{"command":` + string(mustJSON(t, cmd)) + `}`), SessionID: "s1"}
	}

	// 非合规（管道截断）：warn 行 + 不阻断
	if err := runGateCmdFormHook(mk("cd E:/Forge && forge task gate task-verify --ref x 2>&1 | tail -3"), dir, "v", "zcode"); err != nil {
		t.Fatalf("advisory form must never block, got: %v", err)
	}
	all, err := checklog.LoadAll(dir)
	if err != nil {
		t.Fatal(err)
	}
	var rows []checklog.Entry
	for _, e := range all {
		if e.Check == checklog.CheckGateCmdForm {
			rows = append(rows, e)
		}
	}
	if len(rows) != 1 || rows[0].EffectiveLevel() != checklog.LevelWarn || rows[0].Passed {
		t.Fatalf("expected one warn row (passed=false), got %+v", rows)
	}
	if rows[0].Meta["pipe"] != "true" || rows[0].Meta["gates"] != "1" {
		t.Fatalf("form flags must land in Meta, got %v", rows[0].Meta)
	}
	if !strings.Contains(rows[0].Detail, "1.58") {
		t.Fatalf("advisory text must carry the 1.58 ratchet notice (compat §二.1①), got: %s", rows[0].Detail)
	}

	// 合规（standalone）：静默零行
	if err := runGateCmdFormHook(mk("forge task gate task-verify --ref x"), dir, "v", "zcode"); err != nil {
		t.Fatal(err)
	}
	all, _ = checklog.LoadAll(dir)
	n := 0
	for _, e := range all {
		if e.Check == checklog.CheckGateCmdForm {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("compliant command must be silent (still 1 row from before), got %d", n)
	}

	// 非门禁命令：静默
	if err := runGateCmdFormHook(mk("go test ./... | tail -3"), dir, "v", "zcode"); err != nil {
		t.Fatal(err)
	}
}
