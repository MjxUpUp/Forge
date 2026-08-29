package agentbridge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// zcodeHookCommandsByEvent 把 ZCode 的 ~/.zcode/cli/config.json 解析成
// event → 命令集合，镜像 Claude settings 形态的 hookCommandsByEvent
// （events 层是唯一的结构差异）。
func zcodeHookCommandsByEvent(t *testing.T, path string) map[string]map[string]bool {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var cfg struct {
		Hooks struct {
			Events map[string][]struct {
				Hooks []struct {
					Command string `json:"command"`
				} `json:"hooks"`
			} `json:"events"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	out := map[string]map[string]bool{}
	for event, groups := range cfg.Hooks.Events {
		set := map[string]bool{}
		for _, g := range groups {
			for _, h := range g.Hooks {
				set[h.Command] = true
			}
		}
		out[event] = set
	}
	return out
}

// writeZcodeFixture 在 <home>/.zcode/cli/config.json 播种 ZCode config.json
// （同时创建 .zcode 安装目录，让 translator 的自毒防线看到「已安装」的
// zcode）。content 逐字节写入。
func writeZcodeFixture(t *testing.T, home, content string) string {
	t.Helper()
	path := filepath.Join(home, ".zcode", "cli", "config.json")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestZcodeWiringMirrorsClaudeSettings guards the sync between zcode.go (buildZcodeHooks) and hooks/settings.go (ForgeHookSpec).
//
// TestZcodeWiringMirrorsClaudeSettings 守卫 zcode.go（buildZcodeHooks）与
// hooks/settings.go（ForgeHookSpec）的同步。ZCode 逐字复用 Claude 的
// PascalCase 事件名，故事件 1:1 映射——命令集漂移会静默禁用 ZCode 上的门禁。
// 每条生成的命令必须带 ` --agent zcode` 归因后缀。与
// TestCursorWiringMirrorsClaudeSettings 平行。
func TestZcodeWiringMirrorsClaudeSettings(t *testing.T) {
	home := isolateHome(t)
	writeZcodeFixture(t, home, `{}`)
	claudeDir := t.TempDir()
	writeClaudeSettingsFixture(t, claudeDir)
	if err := (&ZcodeTranslator{}).Translate(t.TempDir(), testInput()); err != nil {
		t.Fatalf("zcode Translate: %v", err)
	}
	claude := hookCommandsByEvent(t, filepath.Join(claudeDir, ".claude", "settings.local.json"))
	zcode := zcodeHookCommandsByEvent(t, filepath.Join(home, ".zcode", "cli", "config.json"))

	// ZCode reuses Claude's PascalCase event names verbatim, so the mapping is
	// identity — events map 1:1 and drift between the command sets silently
	// disables a gate on ZCode. Every generated command must carry the
	// ` --agent zcode` attribution suffix (enforced per-command by the helper —
	// without it session attribution falls back to marker guesswork).
	assertHostMirrorsClaude(t, "zcode", zcode, claude, map[string]string{
		"PreToolUse": "PreToolUse", "PostToolUse": "PostToolUse", "Stop": "Stop",
		"SessionStart": "SessionStart", "UserPromptSubmit": "UserPromptSubmit",
		"PostToolUseFailure": "PostToolUseFailure",
	})
}

// TestZcodeHooks_OnlyLegalZcodeEvents pins the zcode event whitelist against the official roster (zcode.z.ai/en/docs/hooks): SessionStart, UserPromptSubmit, PreToolUse, PermissionRequest, PostToolUse, PostToolUseFailure, Stop.
//
// TestZcodeHooks_OnlyLegalZcodeEvents 把 zcode event 白名单钉在官方名册上
// （zcode.z.ai/en/docs/hooks）：SessionStart、UserPromptSubmit、PreToolUse、
// PermissionRequest、PostToolUse、PostToolUseFailure、Stop。接名册外的 event
// 永不触发（静默 no-op）。有 ZCode 对应物的六个 ForgeHookSpec event 必须全部
// 在位；PostCompact/SubagentStop（无 ZCode 对应物）与 PermissionRequest
// （ZCode 独有、无 spec 对应物）必须保持缺席。仿
// TestCodexHooks_OnlyLegalCodexEvents。
func TestZcodeHooks_OnlyLegalZcodeEvents(t *testing.T) {
	generated := buildZcodeHooks()
	legal := map[string]bool{
		"SessionStart": true, "UserPromptSubmit": true,
		"PreToolUse": true, "PermissionRequest": true,
		"PostToolUse": true, "PostToolUseFailure": true,
		"Stop": true,
	}
	// The six ForgeHookSpec events with a ZCode analogue must all be present;
	// PostCompact/SubagentStop (no ZCode analogue) and PermissionRequest (ZCode-only,
	// no spec counterpart) must stay absent.
	assertOnlyLegalEvents(t, "zcode", generated, legal,
		[]string{`PreToolUse`, `PostToolUse`, `Stop`, `SessionStart`, `UserPromptSubmit`, `PostToolUseFailure`},
		[]string{`PostCompact`, `SubagentStop`, `PermissionRequest`},
		"no analogue on the other side")
}

// TestZcodeTranslator_GuardNoInstall pins the detection self-poison guard: with no ~/.zcode (zcode not installed), Translate is a nil-error no-op and must NOT create the directory — creating it would make DetectAgents wire a non-existent tool on every later forge init.
//
// TestZcodeTranslator_GuardNoInstall 钉死检测自毒防线：无 ~/.zcode（zcode 未
// 安装）时 Translate 是 nil 错误 no-op，且不得创建该目录——创建了会让
// DetectAgents 在后续每次 forge init 误接一个不存在的工具。
func TestZcodeTranslator_GuardNoInstall(t *testing.T) {
	home := isolateHome(t)
	if err := (&ZcodeTranslator{}).Translate(t.TempDir(), testInput()); err != nil {
		t.Fatalf("Translate on zcode-less machine: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".zcode")); !os.IsNotExist(err) {
		t.Error("Translate created ~/.zcode on a zcode-less machine — detection self-poison")
	}
}

// TestZcodeTranslator_MergePreservesUserContent: existing user settings (top-level keys, hooks.timeoutMs) and user hook entries survive Translate; hooks.enabled is forced true (ZCode executes nothing without it); forge entries land alongside user entries under the same event.
//
// TestZcodeTranslator_MergePreservesUserContent：既有用户设置（顶层键、
// hooks.timeoutMs）与用户 hook 条目在 Translate 后存活；hooks.enabled 强制为
// true（没有它 ZCode 什么都不执行）；forge 条目与用户条目落在同一 event 下。
func TestZcodeTranslator_MergePreservesUserContent(t *testing.T) {
	home := isolateHome(t)
	seed := `{
  "model": "glm-5.2",
  "hooks": {
    "enabled": false,
    "timeoutMs": 30000,
    "events": {
      "PreToolUse": [
        {
          "matcher": "Write|Edit",
          "hooks": [
            {"type": "command", "command": "my-own-linter --check", "async": false},
            {"type": "command", "command": "forge hook stale-removed-hook --agent zcode"}
          ]
        }
      ]
    }
  }
}
`
	path := writeZcodeFixture(t, home, seed)
	if err := (&ZcodeTranslator{}).Translate(t.TempDir(), testInput()); err != nil {
		t.Fatalf("Translate: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]json.RawMessage
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("parse merged config.json: %v", err)
	}
	if _, ok := cfg["model"]; !ok {
		t.Error("top-level user key (model) lost in merge")
	}
	var hooksObj map[string]json.RawMessage
	if err := json.Unmarshal(cfg["hooks"], &hooksObj); err != nil {
		t.Fatal(err)
	}
	if string(hooksObj["enabled"]) != "true" {
		t.Errorf("hooks.enabled = %s, want true (forced — zcode runs nothing without it)", hooksObj["enabled"])
	}
	if string(hooksObj["timeoutMs"]) != "30000" {
		t.Errorf("hooks.timeoutMs = %s, want 30000 (user value preserved)", hooksObj["timeoutMs"])
	}
	cmds := zcodeHookCommandsByEvent(t, path)
	if !cmds["PreToolUse"]["my-own-linter --check"] {
		t.Error("user hook entry lost in merge")
	}
	if cmds["PreToolUse"]["forge hook stale-removed-hook --agent zcode"] {
		t.Error("stale forge entry survived — merge must replace forge entries wholesale")
	}
	if !cmds["PreToolUse"]["forge hook task-guard --agent zcode"] {
		t.Error("generated forge entry missing after merge")
	}
}

// TestZcodeTranslator_NullConfig pins the nil-map guard: a hand-emptied config (`null` body, or `{"hooks": null}`) unmarshals into nil maps — merging must take the fresh-file path (valid wiring written), never panic.
//
// TestZcodeTranslator_NullConfig 钉死 nil-map 防线：手工清空的配置（`null`
// 正文或 `{"hooks": null}`）会 unmarshal 成 nil map——merge 必须走全新文件
// 路径（写入合法接线），绝不 panic。
func TestZcodeTranslator_NullConfig(t *testing.T) {
	for _, body := range []string{`null`, `{"hooks": null}`} {
		t.Run(body, func(t *testing.T) {
			home := isolateHome(t)
			path := writeZcodeFixture(t, home, body)
			if err := (&ZcodeTranslator{}).Translate(t.TempDir(), testInput()); err != nil {
				t.Fatalf("Translate on %s: %v", body, err)
			}
			cmds := zcodeHookCommandsByEvent(t, path)
			if !cmds["PreToolUse"]["forge hook task-guard --agent zcode"] {
				t.Errorf("generated wiring missing after merge onto %s", body)
			}
			data, _ := os.ReadFile(path)
			if !strings.Contains(string(data), `"enabled": true`) {
				t.Errorf("hooks.enabled not forced true after merge onto %s", body)
			}
		})
	}
}

// TestZcodeTranslator_Idempotent: a second Translate is a byte-identical no-op (deterministic output — no spurious rewrites bumping the file mtime).
//
// TestZcodeTranslator_Idempotent：第二次 Translate 是逐字节不变的 no-op
// （输出确定——不会因重复写入无谓 bump 文件 mtime）。
func TestZcodeTranslator_Idempotent(t *testing.T) {
	home := isolateHome(t)
	path := writeZcodeFixture(t, home, `{}`)
	assertTranslateIdempotent(t, &ZcodeTranslator{}, path)
}

// TestStripZcodeHooks covers the uninstall roundtrip: Translate then Strip leaves user content intact with zero forge commands remaining; a second Strip and a missing file are both clean no-ops.
//
// TestStripZcodeHooks 覆盖卸载往返：Translate 后 Strip 使用户内容完好、forge
// 命令清零；第二次 Strip 与缺失文件均为干净 no-op。
func TestStripZcodeHooks(t *testing.T) {
	home := isolateHome(t)
	seed := `{
  "hooks": {
    "enabled": true,
    "events": {
      "Stop": [
        {"hooks": [{"type": "command", "command": "my-stop-check"}]}
      ]
    }
  }
}
`
	path := writeZcodeFixture(t, home, seed)
	if err := (&ZcodeTranslator{}).Translate(t.TempDir(), testInput()); err != nil {
		t.Fatal(err)
	}
	stripped, err := StripZcodeHooks()
	if err != nil || !stripped {
		t.Fatalf("StripZcodeHooks = (%v, %v), want (true, nil)", stripped, err)
	}
	cmds := zcodeHookCommandsByEvent(t, path)
	for event, set := range cmds {
		for cmd := range set {
			if strings.HasPrefix(cmd, "forge hook ") {
				t.Errorf("forge command %q survived strip under %s", cmd, event)
			}
		}
	}
	if !cmds["Stop"]["my-stop-check"] {
		t.Error("user Stop hook lost in strip")
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), `"enabled": true`) {
		t.Error("hooks.enabled flipped by strip — the user's own hooks may depend on it")
	}
	stripped, err = StripZcodeHooks()
	if err != nil || stripped {
		t.Errorf("second StripZcodeHooks = (%v, %v), want (false, nil)", stripped, err)
	}

	// Missing file: clean no-op.
	isolateHome(t)
	stripped, err = StripZcodeHooks()
	if err != nil || stripped {
		t.Errorf("StripZcodeHooks on missing file = (%v, %v), want (false, nil)", stripped, err)
	}
}
