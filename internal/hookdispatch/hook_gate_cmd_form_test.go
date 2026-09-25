package hookdispatch

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/checklog"
)

// gate-cmd-form hook tests（delivery-hardening 墙-1）：1.67 版本门前的 advisory
// 形态、版本门后的 BLOCK（deny + Level=fail + Meta blocked）、env 逃生（审计行
// + 会话节流）、会话阻断上限（超限降级 advisory）、合规/非门禁命令静默。

func mkGateCmdInput(cmd string) HookInput {
	b, _ := json.Marshal(cmd)
	return HookInput{HookEventName: "PreToolUse", ToolName: "Bash",
		ToolInput: json.RawMessage(`{"command":` + string(b) + `}`), SessionID: "s1"}
}

func TestGateCmdFormHook_AdvisoryAndSilence(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	mk := func(cmd string) HookInput { return mkGateCmdInput(cmd) }

	// 版本门前（1.66）：warn 行 + 不阻断；文案带 1.67 ratchet 预告。
	if err := runGateCmdFormHook(mk("cd E:/Forge && forge task gate task-verify --ref x 2>&1 | tail -3"), dir, "1.66.1", "zcode"); err != nil {
		t.Fatalf("advisory form must never block pre-ratchet, got: %v", err)
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
	if !strings.Contains(rows[0].Detail, "1.67") {
		t.Fatalf("advisory text must carry the 1.67 ratchet notice, got: %s", rows[0].Detail)
	}

	// 合规（standalone）：静默零行
	if err := runGateCmdFormHook(mk("forge task gate task-verify --ref x"), dir, "1.67.0", "zcode"); err != nil {
		t.Fatal(err)
	}
	// 非门禁命令：静默
	if err := runGateCmdFormHook(mk("go test ./... | tail -3"), dir, "1.67.0", "zcode"); err != nil {
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
		t.Fatalf("compliant/non-gate commands must be silent (still 1 row), got %d", n)
	}
}

// TestGateCmdFormHook_BlockRatchet：版本门后（≥1.67）非合规 → deny（返回
// HookBlockError/非 nil）+ Level=fail + Meta blocked=true + 会话计数递增。
func TestGateCmdFormHook_BlockRatchet(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FORGE_DATA_HOME", t.TempDir())

	err := runGateCmdFormHook(mkGateCmdInput("forge task gate task-verify 2>&1 | tail -3"), dir, "1.67.0", "zcode")
	if err == nil {
		t.Fatal("block 模式（版本 ≥1.67、非合规形态）必须 deny")
	}
	all, _ := checklog.LoadAll(dir)
	var row *checklog.Entry
	for i := range all {
		if all[i].Check == checklog.CheckGateCmdForm {
			row = &all[i]
		}
	}
	if row == nil {
		t.Fatal("deny 也必须落 checklog 行")
	}
	if row.EffectiveLevel() != checklog.LevelFail || row.Meta["blocked"] != "true" {
		t.Fatalf("阻断行应 Level=fail + blocked=true, got %+v", row)
	}
	if got := gateCmdFormBlockCount(dir, "s1", false); got != 1 {
		t.Errorf("会话阻断计数应递增到 1, got %d", got)
	}
	// deny 错误文本自足（出口 + 逃生 + 上限）——真断言（审查 P2-9：曾只 Logf）。
	if msg := err.Error(); !strings.Contains(msg, "FORGE_GATE_CMD_FORM") || !strings.Contains(msg, "阻断上限") {
		t.Errorf("deny 文本须含逃生 env 与上限说明: %v", msg)
	}
}

// TestGateCmdFormHook_EscapeAndCap：env 逃生降 advisory（审计行落盘、会话节流）；
// 会话阻断上限 5 次后自动降级 advisory（有界 deny）。
func TestGateCmdFormHook_EscapeAndCap(t *testing.T) {
	dir := t.TempDir()
	home := t.TempDir()
	t.Setenv("FORGE_DATA_HOME", home)

	// 逃生：env=0 → 不 deny + 落一条 escape-hatch 审计行（同会话第二次不再落）。
	t.Setenv(gateCmdFormEscapeEnv, "0")
	for i := 0; i < 2; i++ {
		if err := runGateCmdFormHook(mkGateCmdInput("forge task gate task-verify 2>&1 | tail -3"), dir, "1.67.0", "zcode"); err != nil {
			t.Fatalf("env 逃生必须降级 advisory（第 %d 次）: %v", i+1, err)
		}
	}
	all, _ := checklog.LoadAll(dir)
	esc := 0
	for _, e := range all {
		if e.Check == checklog.CheckEscapeHatch && e.Meta["gate"] == "gate-cmd-form" {
			esc++
		}
	}
	if esc != 1 {
		t.Fatalf("逃生审计行应会话节流为 1 条, got %d", esc)
	}

	// 上限：清逃生 env，预置计数=cap，第 cap+1 次降级 advisory。
	t.Setenv(gateCmdFormEscapeEnv, "")
	for i := 0; i < gateCmdFormBlockCap; i++ {
		_ = gateCmdFormBlockCount(dir, "s1", true)
	}
	if err := runGateCmdFormHook(mkGateCmdInput("forge task gate task-verify 2>&1 | tail -3"), dir, "1.67.0", "zcode"); err != nil {
		t.Fatalf("会话阻断上限已过应降级 advisory, got: %v", err)
	}
	// 2.x 主版本：版本门失明回落 advisory。
	if err := runGateCmdFormHook(mkGateCmdInput("forge task gate task-verify 2>&1 | tail -3"), dir, "2.0.0", "zcode"); err != nil {
		t.Fatalf("2.x 版本门失明应回落 advisory, got: %v", err)
	}
}
