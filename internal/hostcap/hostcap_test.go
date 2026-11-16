package hostcap

import (
	"testing"
)

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

// TestRegistryHygiene — 注册表数据不变量（包文档 ONBOARDING 契约的守卫面，
// 2026-09-16 随扩展面硬化落地补配）：宿主名唯一、方言唯一、提升规则必须显式
// 判准（Contains 非空——PromoteAdvisory 的判准缺失等于无差别提升）、安装指示
// 良构。数据行的语法错误在此拦住，不用等 hookdispatch 侧消费时才炸。
func TestRegistryHygiene(t *testing.T) {
	names := map[string]bool{}
	dialects := map[string]string{}
	for _, h := range Hosts {
		if h.Name == "" {
			t.Fatal("存在空 Name 宿主行")
		}
		if names[h.Name] {
			t.Errorf("宿主 %q 重复注册", h.Name)
		}
		names[h.Name] = true
		if h.StdinDialect != "" {
			if prev, dup := dialects[h.StdinDialect]; dup {
				t.Errorf("StdinDialect %q 被宿主 %q 与 %q 重复声明", h.StdinDialect, prev, h.Name)
			}
			dialects[h.StdinDialect] = h.Name
		}
		for hook, rule := range h.PromoteAdvisory {
			if rule.Contains == "" {
				t.Errorf("宿主 %q 的 %s 提升规则缺少 Contains（判准必须显式）", h.Name, hook)
			}
		}
		for _, ind := range h.InstallIndicators {
			if ind.Env == "" && ind.Path == "" {
				t.Errorf("宿主 %q 的 InstallIndicator Env/Path 均空", h.Name)
			}
		}
		for ev := range h.ContextChannels {
			if ev == "" {
				t.Errorf("宿主 %q 的 ContextChannels 存在空事件键", h.Name)
			}
		}
	}
	if len(names) == 0 {
		t.Fatal("注册表为空——宿主清单丢失")
	}
}
