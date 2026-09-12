package hooks

import (
	"path/filepath"
	"strings"
	"testing"
)

// specStats 汇总一个 spec 的事件/matcher/条目数。
func specStats(spec map[string][]HookMatcher) (events, matchers, entries int) {
	for _, ms := range spec {
		events++
		for _, m := range ms {
			matchers++
			entries += len(m.Hooks)
		}
	}
	return events, matchers, entries
}

// specHookNames 收集 spec 内全部 hook 名（"forge hook <name>" 的 <name>）。
func specHookNames(spec map[string][]HookMatcher) map[string]bool {
	names := map[string]bool{}
	for _, ms := range spec {
		for _, m := range ms {
			for _, h := range m.Hooks {
				names[h.Command[len("forge hook "):]] = true
			}
		}
	}
	return names
}

// TestForgeHookSpecForProfile_Tiers：三档的规模与内容契约。lite 是核心最小集
// （拦截 + 管线 + 状态机缺一不可——drop 即破坏任务循环的 hook 必须在场），
// 明显小于 full；standard/full 与完整名册逐条相等。
func TestForgeHookSpecForProfile_Tiers(t *testing.T) {
	fullEvents, fullMatchers, fullEntries := specStats(ForgeHookSpec())
	if fullEvents != 8 || fullMatchers != 11 || fullEntries != 33 {
		t.Fatalf("完整名册基线漂移（棘轮与档位测试同源钉 8/11/33），got %d/%d/%d", fullEvents, fullMatchers, fullEntries)
	}

	std := ForgeHookSpecForProfile(ProfileStandard)
	stdE, stdM, stdN := specStats(std)
	if stdE != fullEvents || stdM != fullMatchers || stdN != fullEntries {
		t.Errorf("standard 应与完整名册等价，got %d/%d/%d", stdE, stdM, stdN)
	}
	ful := ForgeHookSpecForProfile(ProfileFull)
	fulE, fulM, fulN := specStats(ful)
	if fulE != fullEvents || fulM != fullMatchers || fulN != fullEntries {
		t.Errorf("full 应与完整名册等价，got %d/%d/%d", fulE, fulM, fulN)
	}

	lite := ForgeHookSpecForProfile(ProfileLite)
	liteE, liteM, liteN := specStats(lite)
	if liteE >= fullEvents || liteM >= fullMatchers || liteN >= fullEntries {
		t.Errorf("lite 应严格小于完整名册（瘦身目标），got %d/%d/%d", liteE, liteM, liteN)
	}
	if liteN != 13 || liteE != 6 {
		t.Errorf("lite 基线 6 事件/13 条目（改白名单须同步本断言与文档），got %d 事件/%d 条目", liteE, liteN)
	}

	// 核心集必须全在 lite：缺任一即破坏管线或丢拦截/HITL 决策点。
	liteNames := specHookNames(lite)
	for _, must := range []string{"task-guard", "bash-guard", "hazard-guard", "file-sentinel", "tool-track", "task-verify", "review-stop", "freeze-guard", "gate-cmd-form", "task-resume"} {
		if !liteNames[must] {
			t.Errorf("lite 缺核心 hook %q（管线/安全完整性被破坏）", must)
		}
	}
	// advisory 增强一律不入 lite。
	for _, out := range []string{"auto-compile", "assertion-check", "read-before-edit", "skill-trigger", "test-nudge", "conventions-write", "skill-scan", "mcp-scan", "resume-reinject"} {
		if liteNames[out] {
			t.Errorf("lite 不应含 advisory/增强 hook %q", out)
		}
	}
}

// TestParseProfile_Fallback：未知/空/大小写混杂 token 一律回落 standard，
// 永不报错——档位选择不能弄断接线。
func TestParseProfile_Fallback(t *testing.T) {
	for input, want := range map[string]Profile{
		"":         ProfileStandard,
		"standard": ProfileStandard,
		"Lite":     ProfileLite,
		"  full  ": ProfileFull,
		"nonsense": ProfileStandard,
		"LITE":     ProfileLite,
	} {
		if got := ParseProfile(input); got != want {
			t.Errorf("ParseProfile(%q) = %q, want %q", input, got, want)
		}
	}
}

// TestActiveProfile_Precedence：FORGE_PROFILE env 显式覆盖（含非法值回落
// standard）> ~/.forge/profile 持久值 > 默认 standard。经 FORGE_DATA_HOME 指
// 向临时目录隔离真机状态。
func TestActiveProfile_Precedence(t *testing.T) {
	// 先清 shell 可能导出的 FORGE_PROFILE——本测试钉的就是「无显式 env 时回落
	// 持久值/默认」的优先级链（审查 M1：env 泄漏会击穿第一条断言）。
	t.Setenv("FORGE_PROFILE", "")
	t.Setenv("FORGE_DATA_HOME", filepath.Join(t.TempDir(), "forge-home"))

	// 无持久值、无 env → 默认 standard。
	if got := ActiveProfile(); got != ProfileStandard {
		t.Fatalf("无任何配置时应为默认 standard，got %q", got)
	}
	// 持久值 lite 生效。
	if err := WriteGlobalProfile(ProfileLite); err != nil {
		t.Fatalf("WriteGlobalProfile: %v", err)
	}
	if got := ActiveProfile(); got != ProfileLite {
		t.Fatalf("持久值 lite 应生效，got %q", got)
	}
	// env 显式覆盖持久值；env 非法值回落 standard（不继承持久值）。
	t.Setenv("FORGE_PROFILE", "full")
	if got := ActiveProfile(); got != ProfileFull {
		t.Fatalf("env 应覆盖持久值，got %q", got)
	}
	t.Setenv("FORGE_PROFILE", "nonsense")
	if got := ActiveProfile(); got != ProfileStandard {
		t.Fatalf("env 非法值应回落 standard，got %q", got)
	}
}

// TestProfileAllowsHook：standard/full 全允许；lite 按白名单。运行时门
// （hookdispatch.runHook）据此秒过，是插件渠道的兜底。
func TestProfileAllowsHook(t *testing.T) {
	for _, h := range []string{"skill-trigger", "auto-compile", "skill-scan", "task-guard"} {
		if !ProfileAllowsHook(h, ProfileStandard) || !ProfileAllowsHook(h, ProfileFull) {
			t.Errorf("standard/full 应全允许，%q 被拒", h)
		}
	}
	if ProfileAllowsHook("skill-trigger", ProfileLite) {
		t.Error("lite 不应允许 skill-trigger（advisory 触发通道）")
	}
	if !ProfileAllowsHook("hazard-guard", ProfileLite) {
		t.Error("lite 必须允许 hazard-guard（HITL 核心）")
	}
}

// TestForgeHookWiring_BatchTransform：W0.2 单入口分派的接线变换——多 hook
// matcher 组收敛为一条 `forge hook batch --event E --matcher M`（进程拉起从
// N 降到 1）；单 hook matcher 保持原条目；档位过滤在变换前生效（lite 的接线
// 更小）。变换本体（batch 命令）已实现（hookdispatch.hook_batch.go），本测试
// 钉住接线形态供 flip 批次消费。
func TestForgeHookWiring_BatchTransform(t *testing.T) {
	t.Setenv("FORGE_PROFILE", "standard")
	wiring := ForgeHookWiring()
	fullE, fullM, fullN := specStats(ForgeHookSpec())
	wE, wM, wN := specStats(wiring)
	if wE != fullE {
		t.Errorf("事件数不变（8），got %d", wE)
	}
	if wM != fullM {
		t.Errorf("matcher 数不变（11），got %d", wM)
	}
	if wN >= fullN {
		t.Errorf("batch 变换必须减少条目数（33 → 每 matcher 一条），got %d", wN)
	}
	// PreToolUse/Write|Edit 组（6 hook）收敛后恰一条 batch 条目，且命令含
	// 事件与 matcher（shell 引号安全由 %q 保证）。
	var found bool
	for _, ms := range wiring["PreToolUse"] {
		for _, h := range ms.Hooks {
			if ms.Matcher == "Write|Edit" && strings.HasPrefix(h.Command, "forge hook batch --event PreToolUse --matcher") {
				found = true
			}
		}
	}
	if !found {
		t.Error("PreToolUse/Write|Edit 组未收敛为 batch 单入口条目")
	}
}
