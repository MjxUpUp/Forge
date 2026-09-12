package hooks

import (
	"testing"
)

// Hook 数量冻结棘轮（2026-09 W0「瘦身」方向的用户痛点守卫）。
//
// Hook freeze ratchet — 用户对 Forge 的头号痛点是 hook 太多太重（每次工具调用
// 起进程、settings 接线面积、注入噪音）。本测试把 ForgeHookSpec 的接线规模
// 钉死为常量：任何新增（新事件/新 matcher/新条目）都必须有意识地改这里的
// 数字并过 code review——摩擦即特性。三个失败方向各有明确指引：
//   - 实际 > 常量：新增接线。先回答「这个功能让用户的会话变重了多少？」
//     （W0 接入宪法）；新检查应优先挂进既有拦截点（一入口多检），确需新条目
//     时 bump 常量并在 census/任务里记录理由。
//   - 实际 < 常量：删除接线（瘦身成果）。欢迎——但必须同步调低常量，让
//     棘轮只记录有意识的变更，数字本身永远等于当前现实。
//   - 事件数变化：新事件类型（如平台新增生命周期）或下线整个事件——同上，
//     有意识变更。
//
// 与 TestGenerateSettingsHooksMirrorForgeHookSpec 分工：那个钉「接线与 spec
// 一致」（防漂移），本测试钉「spec 规模不暗增」（防膨胀）。两者缺一不可。
func TestHookWiring_FreezeRatchet(t *testing.T) {
	// 2026-09-12 基线（W1 语义分词层落地时点）。只许调低或有记录地调高。
	const wantEvents = 8
	const wantMatchers = 11
	const wantEntries = 33

	spec := ForgeHookSpec()
	events, matchers, entries := 0, 0, 0
	for _, ms := range spec {
		events++
		for _, m := range ms {
			matchers++
			entries += len(m.Hooks)
		}
	}

	if events != wantEvents {
		t.Errorf("hook 事件数 %d != 棘轮 %d（实际 != 常量即有变更）——新增事件须先过「会话变重了吗」审查并 bump 常量；下线事件须调低常量（详见测试头注）", events, wantEvents)
	}
	if matchers != wantMatchers {
		t.Errorf("hook matcher 数 %d != 棘轮 %d——瘦身删除后请调低常量留痕；新增 matcher 须优先合并进既有 matcher（一入口多检）并 bump 常量（详见测试头注）", matchers, wantMatchers)
	}
	if entries != wantEntries {
		t.Errorf("hook 条目数 %d != 棘轮 %d——这是用户「hook 太多太重」痛点的直接计量；新增条目必须给出为何不能挂进既有条目的理由并 bump 常量（详见测试头注）", entries, wantEntries)
	}
}

// TestHookWiring_NoDuplicateCommandInSpec：同一 (事件, matcher) 内不得出现
// 重复的 hook 命令——那意味着一次工具调用双倍进程拉起，直接加重用户痛点。
// 同一命令挂在不同事件/matcher 是合法的（skill-trigger 的决策点/动作点通道
// 就是按事件分挂），不在本测试射程；跨接线面（settings vs plugin hooks.json）
// 的重复由 forge plugin dedupe / init 体检管。
func TestHookWiring_NoDuplicateCommandInSpec(t *testing.T) {
	spec := ForgeHookSpec()
	for ev, ms := range spec {
		for _, m := range ms {
			seen := map[string]bool{}
			for _, h := range m.Hooks {
				if seen[h.Command] {
					t.Errorf("hook 命令在 %s/%s 下重复注册: %q——一次工具调用会双倍拉起进程；如确需多实例语义，改为单入口分派内部去重", ev, m.Matcher, h.Command)
				}
				seen[h.Command] = true
			}
		}
	}
}
