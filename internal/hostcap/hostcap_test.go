package hostcap

import (
	"path/filepath"
	"testing"
)

// TestLookup_CoversAllSupportedHosts pins the registry against the
// agentbridge.AgentType names.
//
// TestLookup_CoversAllSupportedHosts 把注册表钉在 agentbridge.AgentType 名上——
// 缺失的宿主在所有查表处静默回落 Claude 兼容默认，丢失其身份信号。
func TestLookup_CoversAllSupportedHosts(t *testing.T) {
	for _, name := range []string{
		"claude-code", "cursor", "copilot", "windsurf", "codex",
		"opencode", "cline", "kimi", "codebuddy", "reasonix", "dsh", "zcode",
	} {
		if h := Lookup(name); h == nil {
			t.Errorf("Lookup(%q) = nil, want registry row", name)
		} else if h.Name != name {
			t.Errorf("Lookup(%q).Name = %q", name, h.Name)
		}
	}
	if h := Lookup("no-such-host"); h != nil {
		t.Errorf("Lookup(unknown) = %+v, want nil", h)
	}
}

// TestLookup_CursorConversationID pins cursor's two-field session identity.
//
// TestLookup_CursorConversationID 钉住 cursor 的双字段会话身份——此处丢掉
// conversation_id 会重开「cursor 事件落 legacy 全局键」的缺口。
func TestLookup_CursorConversationID(t *testing.T) {
	h := Lookup("cursor")
	if h == nil {
		t.Fatal("cursor row missing")
	}
	found := false
	for _, f := range h.StdinSessionFields {
		if f == "conversation_id" {
			found = true
		}
	}
	if !found {
		t.Errorf("cursor StdinSessionFields = %v, want conversation_id fallback", h.StdinSessionFields)
	}
}

// TestProbeShellIdentity verifies the env probe: claude-code's
// CLAUDE_CODE_SESSION_ID resolves to (claude-code, sid); no host env → empty.
//
// TestProbeShellIdentity 验证 env 探测：claude-code 的 CLAUDE_CODE_SESSION_ID
// 解析为 (claude-code, sid)；无宿主 env → 空。
func TestProbeShellIdentity(t *testing.T) {
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	if host, sid := ProbeShellIdentity(); host != "" || sid != "" {
		t.Errorf("no env: got (%q, %q), want empty", host, sid)
	}
	t.Setenv("CLAUDE_CODE_SESSION_ID", "sess-123")
	host, sid := ProbeShellIdentity()
	if host != "claude-code" || sid != "sess-123" {
		t.Errorf("claude env: got (%q, %q), want (claude-code, sess-123)", host, sid)
	}
}

// TestContextChannel_RowsMatchPreRegistrySwitch pins every ContextChannel row
// against the exact (agent, event) → (delivered, label) table the cli
// contextChannelDelivered switch encoded before the phase-2 migration.
//
// TestContextChannel_RowsMatchPreRegistrySwitch 把每行 ContextChannel 钉在 cli
// contextChannelDelivered switch 在阶段 2 迁移前编码的 (agent, event) →
// (delivered, label) 表上——只有这张表成立，迁移才是纯行为保持。
func TestContextChannel_RowsMatchPreRegistrySwitch(t *testing.T) {
	cases := []struct {
		agent, event string
		delivered    bool
		label        string
	}{
		// kimi: stdout reaches the model only on UserPromptSubmit.
		{"kimi", "UserPromptSubmit", true, "kimi/stdout-UserPromptSubmit"},
		{"kimi", "PostToolUse", false, "kimi/no-channel"},
		{"kimi", "Stop", false, "kimi/no-channel"},
		{"kimi", "SessionStart", false, "kimi/no-channel"},
		// codex: hookSpecificOutput honored on four events.
		{"codex", "SessionStart", true, "codex/hookSpecificOutput"},
		{"codex", "PreToolUse", true, "codex/hookSpecificOutput"},
		{"codex", "PostToolUse", true, "codex/hookSpecificOutput"},
		{"codex", "UserPromptSubmit", true, "codex/hookSpecificOutput"},
		{"codex", "Stop", false, "codex/no-channel"},
		{"codex", "PreCompact", false, "codex/no-channel"},
		// cursor / copilot: additional(_c|C)ontext on PostToolUse/SessionStart only.
		{"cursor", "PostToolUse", true, "cursor/additional_context"},
		{"cursor", "SessionStart", true, "cursor/additional_context"},
		{"cursor", "UserPromptSubmit", false, "cursor/no-channel"},
		{"cursor", "Stop", false, "cursor/no-channel"},
		{"copilot", "PostToolUse", true, "copilot/additionalContext"},
		{"copilot", "SessionStart", true, "copilot/additionalContext"},
		{"copilot", "UserPromptSubmit", false, "copilot/no-channel"},
		// windsurf: no stdout JSON protocol at all.
		{"windsurf", "PostToolUse", false, "windsurf/no-context-channel"},
		{"windsurf", "SessionStart", false, "windsurf/no-context-channel"},
		// cline: contextModification injects on every fanned-out event.
		{"cline", "PostToolUse", true, "cline/contextModification"},
		{"cline", "Stop", true, "cline/contextModification"},
		// Claude-compatible default: claude-code, hosts without channel rows
		// (codebuddy/opencode/reasonix/zcode), and unknown agents all deliver everywhere.
		{"claude-code", "PostToolUse", true, "claude/additionalContext"},
		{"codebuddy", "PreToolUse", true, "claude/additionalContext"},
		{"opencode", "Stop", true, "claude/additionalContext"},
		{"reasonix", "SessionStart", true, "claude/additionalContext"},
		{"zcode", "PostToolUse", true, "claude/additionalContext"},
		{"", "UserPromptSubmit", true, "claude/additionalContext"},
		{"no-such-host", "Stop", true, "claude/additionalContext"},
	}
	for _, c := range cases {
		delivered, label := ContextChannel(c.agent, c.event)
		if delivered != c.delivered || label != c.label {
			t.Errorf("ContextChannel(%q, %q) = (%v, %q), want (%v, %q)",
				c.agent, c.event, delivered, label, c.delivered, c.label)
		}
	}
}

// TestKimiDroppedStdoutEvents pins kimi's dropped-stdout event list.
//
// TestKimiDroppedStdoutEvents 钉住 kimi 的 stdout 丢弃事件清单——它驱动
// SessionStart handoff 回填（cli sessionStartOutputDropped）、stale advisory
// 改道（kimiStaleRidesHook）与 skill-trigger 打印门控，此处漂移会静默改道
// （或搁浅）这些 advisory。
func TestKimiDroppedStdoutEvents(t *testing.T) {
	h := Lookup("kimi")
	if h == nil {
		t.Fatal("kimi row missing")
	}
	for _, event := range []string{"PostToolUse", "SessionStart", "PostCompact"} {
		if !h.DropsStdoutEvent(event) {
			t.Errorf("kimi DropsStdoutEvent(%q) = false, want true", event)
		}
	}
	if h.DropsStdoutEvent("UserPromptSubmit") {
		t.Error("kimi DropsStdoutEvent(UserPromptSubmit) = true — UserPromptSubmit is kimi's ONE delivered channel")
	}
	// 目前没有其他宿主丢 stdout：cli 的派生判断必须保持仅 kimi 命中。
	for _, name := range []string{"claude-code", "cursor", "copilot", "windsurf", "codex", "opencode", "cline", "codebuddy", "reasonix", "dsh", "zcode"} {
		if h := Lookup(name); h != nil && h.DropsStdoutEvent("SessionStart") {
			t.Errorf("%s DropsStdoutEvent(SessionStart) = true, want false (backfill would double-inject)", name)
		}
	}
}

// TestKimiNoPromoteAdvisory pins the 2026-08-24 retirement of kimi's advisory-promotion rules.
//
// TestKimiNoPromoteAdvisory 钉住 kimi advisory 提升规则的 2026-08-24 退役：
// 被提升的 exit-2 deny 的 reason 自述「allowed」，自相矛盾（且 kimi 把
// PreToolUse 上**任何** stdout 都当 deny，诚实的 advisory 文案也没有安全的
// 通路）。kimi 的 advisory 现按项目入队、UserPromptSubmit 攒发（cli
// hook_kimi_advisory.go）——注册表行必须保持无规则，防止未来改动静默复活
// 阻断路径。
func TestKimiNoPromoteAdvisory(t *testing.T) {
	h := Lookup("kimi")
	if h == nil {
		t.Fatal("kimi row missing")
	}
	if len(h.PromoteAdvisory) > 0 {
		t.Errorf("kimi PromoteAdvisory = %v, want empty (advisories queue + drain on UserPromptSubmit, never block)", h.PromoteAdvisory)
	}
	// advisory 通道送达且足够的宿主不得带提升规则。dsh（2026-08-22）与 zcode
	// （2026-08-30）是已文档化、经取证实证「通道送达但被无视」的例外（准入路径
	// (b)，由下方 TestDshTaskGuardPromotion/TestZcodeTaskGuardPromotion 钉死），
	// 故刻意不在本清单内。
	for _, name := range []string{"claude-code", "codex", "cursor"} {
		if h := Lookup(name); h != nil && len(h.PromoteAdvisory) > 0 {
			t.Errorf("%s PromoteAdvisory = %v, want empty", name, h.PromoteAdvisory)
		}
	}
}

// TestDshTaskGuardPromotion pins dsh's registry row: task-guard ONLY (admission
// path (b).
//
// TestDshTaskGuardPromotion 钉住 dsh 的注册表行：仅 task-guard（准入路径 (b)——
// 通道经 agent.inject 送达但 advisory 在 2026-08-22 事件中被实证无视），带与 kimi
// 相同的承重排除项（Auto-created 成功路径不得提升）。范围钉死很重要：dsh 不得
// 继承 kimi 的 bash-guard/assertion-check 规则——它们的后果链在 dsh 上仍有效
// （file-sentinel 会 quarantine Bash 写文件；assertion-check 的 advisory 送达）。
func TestDshTaskGuardPromotion(t *testing.T) {
	h := Lookup("dsh")
	if h == nil {
		t.Fatal("dsh row missing")
	}
	cases := []struct {
		hook, detail string
		want         bool
	}{
		// 两种文案都提升：已发布的 advisory 旧文案与提升脚本路径输出的指令式
		// block reason。
		{"task-guard", "[task-guard] No active task. Source changes are allowed but not tracked by a Forge task.", true},
		{"task-guard", "[task-guard] No active task. Source edit DENIED until one exists — run: forge task start ...", true},
		{"task-guard", "[task-guard] Auto-created task 'feat/x' from branch. Source changes tracked.", false}, // success path
		{"task-guard", "", false}, // bare PASS
		{"bash-guard", "[bash-guard] Bash write without active task.", false},     // out of scope on dsh
		{"assertion-check", "Advisory: assertion weakened in foo_test.go", false}, // out of scope on dsh
	}
	for _, c := range cases {
		if got := h.ShouldPromoteAdvisory(c.hook, c.detail); got != c.want {
			t.Errorf("dsh.ShouldPromoteAdvisory(%q, %q) = %v, want %v", c.hook, c.detail, got, c.want)
		}
	}
	// PromotesHook（与 detail 无关的存在性）驱动 cli 的 FORGE_TASKGUARD_PROMOTED
	// env：恰对持 task-guard 规则的宿主为真（dsh 2026-08-22、zcode 2026-08-30
	// 双实证入列；kimi 的提升已于 2026-08-24 退役，改为 advisory 队列）。
	if hh := Lookup("dsh"); hh == nil || !hh.PromotesHook("task-guard") {
		t.Error("dsh PromotesHook(task-guard) = false, want true")
	}
	if hz := Lookup("zcode"); hz == nil || !hz.PromotesHook("task-guard") {
		t.Error("zcode PromotesHook(task-guard) = false, want true (2026-08-30 incident admission)")
	}
	for _, name := range []string{"claude-code", "codex", "cursor", "copilot", "windsurf", "opencode", "cline", "codebuddy", "reasonix", "kimi"} {
		if hh := Lookup(name); hh != nil && hh.PromotesHook("task-guard") {
			t.Errorf("%s PromotesHook(task-guard) = true, want false", name)
		}
	}
	// kimi 必须保持无规则：其 advisory 走 pending 队列而非 exit-2 deny（见
	// TestKimiNoPromoteAdvisory）。
	if kk := Lookup("kimi"); kk != nil {
		for _, hook := range []string{"task-guard", "bash-guard", "assertion-check"} {
			if kk.PromotesHook(hook) {
				t.Errorf("kimi PromotesHook(%q) = true, want false (kimi advisories queue, never block)", hook)
			}
		}
	}
}

// TestZcodeTaskGuardPromotion pins zcode's registry row: task-guard ONLY, same
// rule shape as dsh. Admission path (b) got its second documented instance on
// 2026-08-30: a zcode session completed the whole registry-gc change set on
// main with zero task/commit — the WARN reached the model context (attached to
// the tool result via additionalContext) and was ignored, its copy reading as
// clearance ("allowed"). Forensics: ~/.zcode/cli/rollout/model-io-sess_8647540f*
// (four-layer penetration analysis, 2026-08-31 session).
//
// TestZcodeTaskGuardPromotion 钉住 zcode 的注册表行：仅 task-guard、与 dsh 同形
// 的规则（准入路径 (b) 第二例实证：2026-08-30 zcode 会话在 main 无任务完成全部
// 改动——WARN 送达模型上下文却被无视，文案自述「allowed」被读作放行声明）。
// 范围钉死：zcode 不得继承 bash-guard/assertion-check 的提升（后果链仍有效）。
func TestZcodeTaskGuardPromotion(t *testing.T) {
	h := Lookup("zcode")
	if h == nil {
		t.Fatal("zcode row missing")
	}
	cases := []struct {
		hook, detail string
		want         bool
	}{
		{"task-guard", "[task-guard] Untracked source edit — no active task. Why:", true},
		{"task-guard", "[task-guard] Second untracked source edit — stop editing and start a task first:", true},
		{"task-guard", "[task-guard] No active task. Source edit DENIED until one exists — run: forge task start ...", true},
		{"task-guard", "[task-guard] Auto-created task 'feat/x' from branch. Source changes tracked.", false}, // success path
		{"task-anchor", "[task-anchor] FYI: test-file edits without an active task", false},                   // test-file FYI never promotes (own tag)
		{"task-guard", "", false}, // bare PASS
		{"bash-guard", "[bash-guard] Bash write without active task.", false},     // out of scope on zcode
		{"assertion-check", "Advisory: assertion weakened in foo_test.go", false}, // out of scope on zcode
	}
	for _, c := range cases {
		if got := h.ShouldPromoteAdvisory(c.hook, c.detail); got != c.want {
			t.Errorf("zcode.ShouldPromoteAdvisory(%q, %q) = %v, want %v", c.hook, c.detail, got, c.want)
		}
	}
}

// TestCodexPatchTool pins codex's apply_patch capability row and the IsPatchTool
// scan the cli read-before-edit exemption / file_path synthesis key on.
//
// TestCodexPatchTool 钉住 codex 的 apply_patch 能力行与 cli read-before-edit
// 豁免 / file_path 合成所依据的 IsPatchTool 扫描。
func TestCodexPatchTool(t *testing.T) {
	h := Lookup("codex")
	if h == nil {
		t.Fatal("codex row missing")
	}
	if h.PatchToolName != "apply_patch" {
		t.Errorf("codex PatchToolName = %q, want apply_patch", h.PatchToolName)
	}
	if !IsPatchTool("apply_patch") {
		t.Error("IsPatchTool(apply_patch) = false, want true")
	}
	for _, name := range []string{"Write", "Edit", "Read", ""} {
		if IsPatchTool(name) {
			t.Errorf("IsPatchTool(%q) = true, want false", name)
		}
	}
}

// TestStdinDialects pins the dialect column against the cli stdinNormalizers map
// keys (windsurf/kimi/reasonix/cline) and kimi's parse-replacing flag.
//
// TestStdinDialects 把方言列钉在 cli stdinNormalizers map 的键
// （windsurf/kimi/reasonix/cline）与 kimi 的替代解析标志上——此处漂移会静默
// 跳过归一化（拦截类 hook fail open）或对 kimi 数组形 prompt 跑默认
// unmarshal（每次调用类型错误告警）。
func TestStdinDialects(t *testing.T) {
	want := map[string]bool{ // host → StdinReplacesParse
		"windsurf": false,
		"kimi":     true,
		"reasonix": false,
		"cline":    false,
	}
	for name, replaces := range want {
		h := Lookup(name)
		if h == nil {
			t.Errorf("%s row missing", name)
			continue
		}
		if h.StdinDialect != name {
			t.Errorf("%s StdinDialect = %q, want %q", name, h.StdinDialect, name)
		}
		if h.StdinReplacesParse != replaces {
			t.Errorf("%s StdinReplacesParse = %v, want %v", name, h.StdinReplacesParse, replaces)
		}
	}
	// 其余宿主必须保持 Claude 形（方言为空）。
	for _, name := range []string{"claude-code", "cursor", "copilot", "codex", "opencode", "codebuddy", "zcode"} {
		if h := Lookup(name); h != nil && (h.StdinDialect != "" || h.StdinReplacesParse) {
			t.Errorf("%s StdinDialect=%q StdinReplacesParse=%v, want Claude-shape", name, h.StdinDialect, h.StdinReplacesParse)
		}
	}
}

// TestInstallIndicators pins the user-level detection rows against the
// pre-registry detect.go branches, including the two env-override semantics:
// full-dir override (CLAUDE_CONFIG_DIR/CODEX_HOME) vs BASE-dir override
// (XDG_CONFIG_HOME + EnvSuffix).
//
// TestInstallIndicators 把用户级检测行钉在注册表迁移前的 detect.go 分支上，
// 含两种 env 覆盖语义：整目录覆盖（CLAUDE_CONFIG_DIR/CODEX_HOME）对比**基**
// 目录覆盖（XDG_CONFIG_HOME + EnvSuffix）。
func TestInstallIndicators(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CODEX_HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")

	wantRows := map[string]InstallIndicator{
		"claude-code": {Env: "CLAUDE_CONFIG_DIR", Path: "~/.claude"},
		"codex":       {Env: "CODEX_HOME", Path: "~/.codex"},
		"cursor":      {Path: "~/.cursor"},
		"windsurf":    {Path: "~/.codeium"},
		"opencode":    {Env: "XDG_CONFIG_HOME", EnvSuffix: "opencode", Path: "~/.config/opencode"},
		"zcode":       {Path: "~/.zcode"},
	}
	for name, want := range wantRows {
		h := Lookup(name)
		if h == nil || len(h.InstallIndicators) != 1 {
			t.Errorf("%s InstallIndicators = %+v, want exactly one", name, h)
			continue
		}
		if got := h.InstallIndicators[0]; got != want {
			t.Errorf("%s InstallIndicators[0] = %+v, want %+v", name, got, want)
		}
	}
	// 仅经项目标记检测的宿主不带用户级指示。
	for _, name := range []string{"copilot", "cline", "kimi", "codebuddy", "reasonix"} {
		if h := Lookup(name); h != nil && len(h.InstallIndicators) > 0 {
			t.Errorf("%s InstallIndicators = %+v, want empty (project-marker detection only)", name, h.InstallIndicators)
		}
	}

	// Resolve 语义：home 回落、整目录 env 覆盖、基目录 env 覆盖——且 env 已设
	// 时**不**回落 Path。所有期望值经 filepath.Join 构造（绝不手写 "/" 字面
	// 量）：Resolve 按 OS 分隔符拼接，Windows 上是反斜杠（CI 抓到过正斜杠写
	// 法——仅在 mac 上跑绿不算数）。
	if got := InstallDir("cursor"); got != filepath.Join(home, ".cursor") {
		t.Errorf("InstallDir(cursor) = %q, want %q", got, filepath.Join(home, ".cursor"))
	}
	codexEnv := filepath.Join(string(filepath.Separator), "tmp", "codex-env")
	t.Setenv("CODEX_HOME", codexEnv)
	if got := InstallDir("codex"); got != codexEnv {
		t.Errorf("InstallDir(codex) with CODEX_HOME = %q, want %q", got, codexEnv)
	}
	xdgEnv := filepath.Join(string(filepath.Separator), "tmp", "xdg")
	t.Setenv("XDG_CONFIG_HOME", xdgEnv)
	if got := InstallDir("opencode"); got != filepath.Join(xdgEnv, "opencode") {
		t.Errorf("InstallDir(opencode) with XDG_CONFIG_HOME = %q, want %q", got, filepath.Join(xdgEnv, "opencode"))
	}
	if got := InstallDir("no-such-host"); got != "" {
		t.Errorf("InstallDir(unknown) = %q, want empty", got)
	}
}

// TestLookup_DshRow pins the dsh registry row: every wired event has an honest
// delivered context channel (the plugins/forge-dsh wrapper folds or injects
// allow-path context on all of them.
//
// TestLookup_DshRow 钉住 dsh 注册表行：每个已接事件都有诚实的 delivered 上下文
// 通道（plugins/forge-dsh 包装层在全部事件上折叠或注入 allow 路径上下文——记
// Delivered=false 正是本注册表要防的虚假归因缺口），安装指示遵循
// DSH_HOME ?? ~/.dsh 约定。
func TestLookup_DshRow(t *testing.T) {
	h := Lookup("dsh")
	if h == nil {
		t.Fatal("dsh row missing — detect/attribution falls back to claude defaults")
	}
	for _, event := range []string{"PreToolUse", "PostToolUse", "UserPromptSubmit", "SessionStart", "Stop", "PostCompact"} {
		ch, ok := h.ContextChannels[event]
		if !ok || !ch.Delivered {
			t.Errorf("dsh ContextChannels[%s] = (%+v, %v) — the wrapper delivers context on this event; record it honestly", event, ch, ok)
		}
	}
	if len(h.InstallIndicators) != 1 || h.InstallIndicators[0].Env != "DSH_HOME" || h.InstallIndicators[0].Path != "~/.dsh" {
		t.Errorf("dsh InstallIndicators = %+v, want [{Env: DSH_HOME, Path: ~/.dsh}]", h.InstallIndicators)
	}
}
