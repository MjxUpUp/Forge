package hookdispatch

import (
	"os"
	"strings"

	"github.com/MjxUpUp/Forge/internal/hooks"
	"github.com/MjxUpUp/Forge/internal/specroot"
)

// contract_gate.go — 执法 opt-in：仓库契约 / 宿主逃生舱决定 hook 要不要运行
// （ADR-0002 阶段 B）。
//
// 用户头号痛点是「项目没被 forge 接管，命令/hook/文件检查照样无脑执行」。
// 阶段 B 把默认从「执法」翻成「安静（audit-only）」：hook 默认静默放行，
// 只有显式 opt-in 才执法。归属/审计记录在本门之前已落（见 hook.go），所以
// 静音的仓库仍进审计——这就是 audit-only 的「audit」。
//
//   - 仓库带契约：按契约执法。hooks: on 才执法；hooks: off（含缺省，specroot
//     缺省已翻成 off）一律静默放行。仓库自治：契约 off 优先于一切 env，
//     clone 下来就是安静的。契约只能**收窄**名册：全局档位（FORGE_PROFILE/
//     ~/.forge/profile）在 hook.go 里更早就把 hook 滤掉了，契约写 full 也接不回来。
//   - 没有契约（宿主层）：默认 audit-only，只有临时逃生舱 FORGE_HOOKS=on 显式
//     开启才执法（阶段 C 由 forge integrations enable 取代）。
//   - 契约解析失败：返回 false（安静）。opt-in posture 下坏契约不该把执法打开；
//     返回 false 也不会让 hook 炸掉。
//
// 判据放在「解析出项目根之后」而不是入口：入口只有 cwd，还没有仓库身份。
// 判据也放在 session 登记/归属记录之后——静音的仓库仍要进审计（fleet 归属缺口，2026-08）。

// safetyFloor 是三条安全底线：hazard-guard / mcp-scan / skill-scan。
//
// 阶段 B 语义变更（用户 m07455 明确同意）：底线不再「不受契约 hooks/profile
// 控制恒执法」，改为随执法开关——默认 audit-only，显式 opt-in（契约 hooks: on
// 或宿主 FORGE_HOOKS=on）后才执法。执法时底线不受契约 profile 收窄（但仍受
// hook.go 的全局档位门约束：lite 名册本就没接线 mcp-scan/skill-scan）。
//
// 历史理由（为何曾不给仓库开关）：spec/forge.yaml 随 git 走，一个 PR 加一行
// hooks: off 就能关掉这些扫描器。阶段 B 接受这一权衡——阻断式 hazard-guard
// 取证 109 次拦截零真实损失、18 天停机（ADR-0002），默认安静优先；危险命令
// 防护由宿主自带确认层承接。
var safetyFloor = map[string]bool{
	"hazard-guard": true, // 破坏性命令拦截（rm -rf、force push 等）
	"mcp-scan":     true, // .mcp.json 供应链（pipe-exec/insecure-url）
	"skill-scan":   true, // 恶意 skill 指令扫描
}

// contractAllowsHook 报告该 hook 是否被允许执法。默认 false（audit-only），
// 只有显式 opt-in 才返回 true。root 为已解析的项目根（可为空＝宿主层）。
func contractAllowsHook(root, name string) bool {
	if root != "" && specroot.Declared(root) {
		c, err := specroot.Load(root)
		if err != nil {
			// 契约读不出来 → 安静。opt-in posture 下坏契约不该打开执法。
			return false
		}
		if c.Hooks != specroot.HooksOn {
			// hooks: off（含缺省）→ 静默放行，安全底线也不例外。
			return false
		}
		// hooks: on → 执法。底线不受契约 profile 收窄。
		if safetyFloor[name] {
			return true
		}
		return hooks.ProfileAllowsHook(name, profileForContract(c.Profile))
	}
	// 无契约（宿主层）：默认 audit-only，FORGE_HOOKS=on 才执法。
	if !hooksEnforceEnv() {
		return false
	}
	if safetyFloor[name] {
		return true
	}
	return hooks.ProfileAllowsHook(name, hooks.ActiveProfile())
}

// hooksEnforceEnv 报告宿主层（无契约）是否显式开启执法。阶段 B 的临时逃生舱，
// 阶段 C 由 forge integrations enable 取代。只开宿主层，不覆盖仓库契约的
// hooks: off（仓库自治）。取值 1/on/true/yes（大小写/首尾空白不敏感）。
func hooksEnforceEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("FORGE_HOOKS"))) {
	case "1", "on", "true", "yes":
		return true
	}
	return false
}

// profileForContract 把契约档位映射到接线档位：mini 是最小执法名册（lite：拦截类
// hook，无 advisory 类），不是「不执法」——要只声明不执法用 hooks: off。
// standard/full 原样对应。
func profileForContract(p string) hooks.Profile {
	switch p {
	case specroot.ProfileMini:
		return hooks.ProfileLite
	case specroot.ProfileFull:
		return hooks.ProfileFull
	default:
		return hooks.ProfileStandard
	}
}

// initSuggestEnabled 报告是否允许 init-suggest 在非 forge 仓库里主动建议接管。
//
// 默认关闭：这个 hook 是全局 SessionStart，会在任意 git 仓库往会话里注入
// 「建议 forge init」——对不打算接管的项目就是纯噪音。要恢复旧行为设
// FORGE_INIT_SUGGEST=1（1/on/true/yes）。
func initSuggestEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("FORGE_INIT_SUGGEST"))) {
	case "1", "on", "true", "yes":
		return true
	}
	return false
}
