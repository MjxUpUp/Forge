package hookdispatch

import (
	"os"
	"strings"

	"github.com/MjxUpUp/Forge/internal/hooks"
	"github.com/MjxUpUp/Forge/internal/specroot"
)

// contract_gate.go — 仓库契约决定 hook 要不要运行（spec-in-repo 第二块）。
//
// 用户头号痛点是「项目没被 forge 接管，命令/hook/文件检查照样无脑执行」。
// 第一块把成员资格前移到了仓库内的 spec/forge.yaml；这一块让它真正决定执法：
//
//   - 仓库带契约且 hooks: off → 所有 hook 静默放行（clone 下来就是安静的，
//     不需要 forge init，也不需要 forge off）；
//   - 仓库带契约且指定 profile → 该仓库的 hook 名册按契约档位裁（mini＝lite），
//     覆盖全局 ~/.forge/profile；
//   - 没有契约（或契约读不出来）→ 维持既有行为（注册表/遗留目录照旧），
//     契约解析失败绝不能让 hook 炸掉。
//
// 判据放在「解析出项目根之后」而不是入口：入口只有 cwd，还没有仓库身份；
// 提前 gates 会把 global hook（skill-scan 等）一起挡掉。

// contractAllowsHook 报告仓库契约是否允许该 hook 运行。无契约或解析失败返回 true。
func contractAllowsHook(root, name string) bool {
	if root == "" || !specroot.Declared(root) {
		return true
	}
	c, err := specroot.Load(root)
	if err != nil {
		return true
	}
	if c.Hooks == specroot.HooksOff {
		return false
	}
	return hooks.ProfileAllowsHook(name, profileForContract(c.Profile))
}

// profileForContract 把契约档位映射到接线档位：mini 是「声明但不执法」的轻档，
// 对应 lite 名册；standard/full 原样对应。
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
