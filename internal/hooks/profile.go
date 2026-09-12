package hooks

// profile.go — 接线档位（W0.3）：响应用户头号痛点「hook 太多太重」。三档：
//
//	lite     拦截 + 管线 + 状态机最小集（13 条目/6 事件）——安全与任务循环完整，
//	         摘掉全部 advisory 增强（质量提醒/触发通道/会话引导/集成扫描）。
//	standard 完整现行名册（33 条目/8 事件）——与 ForgeHookSpec() 等价，默认档。
//	full     与 standard 内容等价的预留位（未来增强层在此扩展，standard 保持
//	         稳定语义）。
//
// profile.go — wiring profiles (W0.3). Three tiers as above; lite keeps the
// safety + pipeline + state minimum (dropping it would break the verify loop —
// deviation from the original "only guard+sentinel" sketch is deliberate and
// documented), standard is the current full roster and the default, full is a
// reserved superset slot.
//
// 两层生效机制（审查定案）：
//  1. 接线过滤（init/sync 写 user-level settings 与各 translator 时按档位过滤
//     ForgeHookSpecForProfile）——真正减少条目数；
//  2. 运行时门（hookdispatch.runHook 对不在档内的 hook 立即静默 PASS）——覆盖
//     插件渠道用户（其接线来自完整 plugin payload，forge 无权删改）。
//
// CLI 门禁不受影响：taskpipeline executor 有自己的 runEmbeddedHook，不经
// runHook——lite 只瘦「会话内常驻面」，CLI 门禁保持完整。

import (
	"fmt"
	"os"
	"strings"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/util"
)

// Profile is a wiring tier name.
//
// Profile 是接线档位名。
type Profile string

const (
	// ProfileLite keeps only the blocking + pipeline + state minimum.
	//
	// ProfileLite 只保留拦截 + 管线 + 状态机最小集。
	ProfileLite Profile = "lite"
	// ProfileStandard is the current full roster (default).
	//
	// ProfileStandard 是完整现行名册（默认档）。
	ProfileStandard Profile = "standard"
	// ProfileFull is content-equal to standard today (reserved superset slot).
	//
	// ProfileFull 今日与 standard 内容等价（预留超集位）。
	ProfileFull Profile = "full"
)

// profileLiteHooks is the lite allow-list. Membership rule: a hook is in lite
// iff dropping it would either break the task pipeline/verify loop or remove a
// block/human-in-the-loop decision point. Everything advisory, triggering,
// session-guidance or scan-shaped stays out.
//
// profileLiteHooks 是 lite 白名单。准入规则：去掉该 hook 会破坏任务管线/验证
// 循环，或丢掉一个拦截/HITL 决策点的，才留在 lite。advisory 提醒、触发通道、
// 会话引导、集成扫描一律不入。
var profileLiteHooks = map[string]bool{
	"freeze-guard":   true, // freeze 优先判定契约的第一入口
	"task-guard":     true, // 源码写入受管（管线）
	"bash-guard":     true, // Bash 写检测 + 快照（file-sentinel 前置）
	"hazard-guard":   true, // 高危 HITL 拦截（语义分词层所在）
	"gate-cmd-form":  true, // gate 命令形态门（管线）
	"file-sentinel":  true, // 事后对账/自保护（第二层兜底）
	"tool-track":     true, // toollog 是 work-activity/read-before-edit/toolusage 的数据源
	"task-verify":    true, // Stop 验证门（管线）
	"review-stop":    true, // 审查-返工循环（管线）
	"failure-track":  true, // 失败观测（编译/测试启发式输入）
	"subagent-track": true, // 子代理观测（管线归因）
	"task-resume":    true, // 跨会话接续（状态层核心）
}

// ParseProfile maps a user-supplied token to a Profile. Unknown/empty tokens
// fall back to ProfileStandard (never an error — profile selection must not
// break wiring).
//
// ParseProfile 把用户输入的 token 映射为 Profile。未知/空 token 回落
// ProfileStandard（永不报错——档位选择不能弄断接线）。
func ParseProfile(s string) Profile {
	switch Profile(strings.TrimSpace(strings.ToLower(s))) {
	case ProfileLite:
		return ProfileLite
	case ProfileFull:
		return ProfileFull
	default:
		return ProfileStandard
	}
}

// ActiveProfile resolves the wiring profile: FORGE_PROFILE env (explicit
// override, wins) → persisted global profile (~/.forge/profile, written by
// `forge init --profile`) → ProfileStandard. Read failures fail-open to the
// default; a hook spawn must never break over profile selection.
//
// ActiveProfile 解析接线档位：FORGE_PROFILE 环境变量（显式覆盖，优先）→
// 持久化全局档（~/.forge/profile，由 forge init --profile 写入）→
// ProfileStandard。读失败 fail-open 到默认档；hook 拉起永远不能因档位选择
// 而失败。
func ActiveProfile() Profile {
	if env := os.Getenv("FORGE_PROFILE"); strings.TrimSpace(env) != "" {
		return ParseProfile(env)
	}
	path := forgedata.GlobalProfile()
	if path == "" {
		return ProfileStandard
	}
	if data, err := os.ReadFile(path); err == nil {
		if token := strings.TrimSpace(string(data)); token != "" {
			return ParseProfile(token)
		}
	}
	return ProfileStandard
}

// ProfileAllowsHook reports whether a hook runs under the given profile.
//
// ProfileAllowsHook 报告某 hook 在给定档位下是否运行。
func ProfileAllowsHook(name string, p Profile) bool {
	if p == ProfileStandard || p == ProfileFull {
		return true
	}
	return profileLiteHooks[name]
}

// ForgeHookSpecForProfile filters ForgeHookSpec() down to the profile's hook
// set, dropping matchers and events that end up empty (an event with no hooks
// must not be written to settings at all). standard/full return the roster
// unchanged — equal content, distinct semantics.
//
// ForgeHookSpecForProfile 把 ForgeHookSpec() 过滤到档位 hook 集，丢弃清空的
// matcher 与事件（无 hook 的事件根本不该写进 settings）。standard/full 原样
// 返回名册——内容相等、语义有别。
func ForgeHookSpecForProfile(p Profile) map[string][]HookMatcher {
	full := ForgeHookSpec()
	if p == ProfileStandard || p == ProfileFull {
		return full
	}
	out := make(map[string][]HookMatcher, len(full))
	for event, matchers := range full {
		var kept []HookMatcher
		for _, m := range matchers {
			var hooks []HookEntry
			for _, h := range m.Hooks {
				name := strings.TrimPrefix(h.Command, "forge hook ")
				if ProfileAllowsHook(name, p) {
					hooks = append(hooks, h)
				}
			}
			if len(hooks) > 0 {
				kept = append(kept, HookMatcher{Matcher: m.Matcher, Hooks: hooks})
			}
		}
		if len(kept) > 0 {
			out[event] = kept
		}
	}
	return out
}

// WriteGlobalProfile persists the profile token to ~/.forge/profile (atomic
// write — hook spawns read it concurrently).
//
// WriteGlobalProfile 把档位 token 持久化到 ~/.forge/profile（原子写——hook 拉起
// 会并发读它）。
func WriteGlobalProfile(p Profile) error {
	if p != ProfileLite && p != ProfileStandard && p != ProfileFull {
		return fmt.Errorf("unknown profile: %q", p)
	}
	path := forgedata.GlobalProfile()
	if path == "" {
		return fmt.Errorf("cannot resolve global home for profile file")
	}
	return util.AtomicWrite(path, []byte(string(p)+"\n"), 0o644)
}
