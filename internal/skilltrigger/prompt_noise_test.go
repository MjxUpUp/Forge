package skilltrigger

import "testing"

// TestSanitizePromptEnvelopes pins which host envelope blocks are stripped and that user-authored text and intent blocks survive.
//
// TestSanitizePromptEnvelopes 钉住哪些宿主信封块被剥离，以及用户原文与意图块原样保留。
func TestSanitizePromptEnvelopes(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"无尖括号直返", "帮我深度调研下 AI coding", "帮我深度调研下 AI coding"},
		{"纯通知整块剥离",
			"<task-notification>\n<task-id>a1</task-id>\n<result>红队评审：鉴权与设计文档均已核对</result>\n</task-notification>",
			""},
		{"system-reminder 包裹的通知",
			"<system-reminder>\n[SYSTEM NOTIFICATION]\n<task-notification><result>合并推送完成</result></task-notification>\n</system-reminder>",
			""},
		{"通知后跟用户原文",
			"<task-notification><result>交叉验证完成</result></task-notification>\n本次调研相关的结论有哪些 forge 需要落地",
			"本次调研相关的结论有哪些 forge 需要落地"},
		{"带属性的开标签", `<system-reminder kind="x">修复 bug</system-reminder>用户说的话`, "用户说的话"},
		{"未闭合开标签剥到末尾", "先看这个\n<task-notification><result>红队 鉴权", "先看这个"},
		{"大小写不敏感", "<SYSTEM-REMINDER>设计文档</SYSTEM-REMINDER>ok", "ok"},
		{"本地命令输出剥离、slash 命令保留",
			"<command-name>/goal</command-name><command-args>全做</command-args><local-command-stdout>Goal set: 修复</local-command-stdout>",
			"<command-name>/goal</command-name><command-args>全做</command-args>"},
		{"bash-input 保留、bash-stdout 剥离",
			"<bash-input>forge hazard confirm --last</bash-input><bash-stdout>✅ 已确认</bash-stdout><bash-stderr></bash-stderr>",
			"<bash-input>forge hazard confirm --last</bash-input>"},
		{"会话事件与 CI 事件剥离",
			"<side-sessions-event>设计文档已合并</side-sessions-event><ci-monitor-event>fix CI</ci-monitor-event>继续",
			"继续"},
		{"非信封标签原样", "看下 <div>红队</div> 这段", "看下 <div>红队</div> 这段"},
		{"正文里的字面标签不吞后文", "修 <system-reminder> 的解析，然后实现 X", "修 <system-reminder> 的解析，然后实现 X"},
		{"行首缩进的未闭合开标签仍剥离", "继续\n  <system-reminder>截断的提醒", "继续"},
	}
	for _, tc := range cases {
		if got := SanitizePrompt(tc.in); got != tc.want {
			t.Errorf("%s: SanitizePrompt(%q) = %q, want %q", tc.name, tc.in, got, tc.want)
		}
		if again := SanitizePrompt(SanitizePrompt(tc.in)); again != tc.want {
			t.Errorf("%s: not idempotent, second pass = %q", tc.name, again)
		}
	}
}

// TestEvalIgnoresHarnessEnvelopes checks end to end that keyword and coding_intent triggers ignore envelope-only prompts but still fire on the user text next to an envelope.
//
// TestEvalIgnoresHarnessEnvelopes 端到端核对：只含信封块的 prompt 不触发关键词与 coding_intent
// 触发器；信封块旁的用户原文照常触发，且命中来源仍是 prompt。
func TestEvalIgnoresHarnessEnvelopes(t *testing.T) {
	all := []SkillTriggers{
		{Skill: "adversarial-verification", Triggers: []Trigger{{Event: "UserPromptSubmit", Keywords: []string{"红队"}}}},
		{Skill: "implementation-discipline", Triggers: []Trigger{{Event: "UserPromptSubmit", When: "coding_intent"}}},
	}
	notice := "<task-notification>\n<status>completed</status>\n<result>红队检验通过，下一步修复并实现 lint</result>\n</task-notification>"

	if hits, _ := Eval(Context{Event: "UserPromptSubmit", Prompt: notice}, all, nil); len(hits) != 0 {
		t.Fatalf("envelope-only prompt must not fire, got %+v", hits)
	}

	hits, _ := Eval(Context{Event: "UserPromptSubmit", Prompt: notice + "\n请做一轮红队检验"}, all, nil)
	if len(hits) != 1 || hits[0].Skill != "adversarial-verification" {
		t.Fatalf("user text beside envelope must fire only the keyword skill, got %+v", hits)
	}
	if hits[0].MatchSource != MatchSourcePrompt || hits[0].MatchedKeyword != "红队" {
		t.Fatalf("hit evidence = (%q, %q), want (prompt, 红队)", hits[0].MatchSource, hits[0].MatchedKeyword)
	}

	hits, _ = Eval(Context{Event: "UserPromptSubmit", Prompt: notice + "\n帮我修复这个 bug"}, all, nil)
	if len(hits) != 1 || hits[0].Skill != "implementation-discipline" {
		t.Fatalf("coding intent in user text must fire, got %+v", hits)
	}
}
