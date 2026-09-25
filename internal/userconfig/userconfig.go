// Package userconfig persists forge's user-level preferences (~/.forge/config.json).
//
// Package userconfig 持久化 forge 的用户级偏好（~/.forge/config.json）。
//
// Project Policy Layer P2：takeover 三档偏好是 init-suggest 自动接管的行为开关——
// ask（出厂默认，首次接触询问一次）/ auto（静默自动接管，P1 及之前的行为）/
// off（全面保守，不询问不接管）。env 覆盖链：FORGE_TAKEOVER > FORGE_AUTO_INIT=1
// （legacy 映射 auto）> 配置文件 > 默认 ask。文件在用户级 store（升级链路不触碰）。
package userconfig

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/util"
)

// Takeover preference values.
//
// Takeover 偏好取值。
const (
	// TakeoverAsk asks once per project on first contact (shipped default since P2).
	//
	// TakeoverAsk 每项目首次接触询问一次（P2 起的出厂默认）。
	TakeoverAsk = `ask`
	// TakeoverAuto silently takes over git projects (pre-P2 default behavior).
	//
	// TakeoverAuto 静默自动接管 git 项目（P2 之前的默认行为）。
	TakeoverAuto = `auto`
	// TakeoverOff takes over nothing new and asks nothing (fully conservative).
	//
	// TakeoverOff 不接管新项目也不询问（全面保守）。
	TakeoverOff = `off`
)

// file is the on-disk shape of ~/.forge/config.json. Only takeover exists today;
// when a second key appears, implement read-merge-write so concurrent forge
// versions don't clobber each other's keys.
//
// file 是 ~/.forge/config.json 的磁盘结构。当前只有 takeover 一个键；
// 出现第二个键时实现读-合并-写，避免新旧版本互抹对方的键。
type file struct {
	Takeover string `json:"takeover,omitempty"`
	// Compat 是本机的兼容基线声明（mechanism-hardening P2-1，指纹分流 v1）：
	// 声明"我的行为按哪个 forge 版本的默认走"（GODEBUG 的 go 行声明的轻量版）。
	// 空 = 跟最新（不钉）。完整分流机制刻意不做（见 compat-commitments.md §四）。
	Compat string `json:"compat,omitempty"`
}

// configPath resolves the store path under forgedata.GlobalHome (FORGE_DATA_HOME
// first, else ~/.forge) — same root as the project registry.
//
// configPath 解析 store 路径（forgedata.GlobalHome——FORGE_DATA_HOME 优先，
// 否则 ~/.forge），与项目注册表同一根。
func configPath() (string, error) {
	home, err := forgedata.GlobalHome()
	if err != nil {
		return ``, err
	}
	return filepath.Join(home, `config.json`), nil
}

// read loads the config; missing/corrupt file = zero value (fail-open：偏好缺失
// 回落默认 ask，损坏不阻断 hook)。
func read() file {
	var f file
	p, err := configPath()
	if err != nil {
		return f
	}
	if err := util.ReadJSONFile(p, &f); err != nil {
		return file{}
	}
	return f
}

// validTakeover reports whether v is a legal takeover value.
func validTakeover(v string) bool {
	return v == TakeoverAsk || v == TakeoverAuto || v == TakeoverOff
}

// TakeoverMode resolves the effective takeover preference with env precedence:
// FORGE_TAKEOVER > FORGE_AUTO_INIT=1 (legacy → auto) > config file > ask.
// Illegal values fall back down the chain (fail-safe read side).
//
// TakeoverMode 解析生效的 takeover 偏好，env 优先链：FORGE_TAKEOVER >
// FORGE_AUTO_INIT=1（legacy 映射 auto）> 配置文件 > ask。非法值沿链回落
// （fail-safe 读侧宽容）。
func TakeoverMode() string {
	if v := os.Getenv(`FORGE_TAKEOVER`); validTakeover(v) {
		return v
	}
	if os.Getenv(`FORGE_AUTO_INIT`) == `1` {
		return TakeoverAuto
	}
	if v := read().Takeover; validTakeover(v) {
		return v
	}
	return TakeoverAsk
}

// SetTakeover persists the takeover preference (validated). Atomic write via
// util.AtomicWrite — half-written config must never take over the store.
//
// SetTakeover 落盘 takeover 偏好（先校验）。util.AtomicWrite 原子写——半写的
// 配置绝不能占住 store。
func SetTakeover(v string) error {
	if !validTakeover(v) {
		return fmt.Errorf(`userconfig: invalid takeover %q (want ask|auto|off)`, v)
	}
	return mutate(func(f *file) { f.Takeover = v })
}

// CompatPref returns the persisted compat baseline declaration ("" when unset
// = follow latest, no pinning).
//
// CompatPref 返回持久化的兼容基线声明（空 = 跟最新，不钉）。
func CompatPref() string {
	return read().Compat
}

// SetCompat sets the compat baseline declaration (mechanism-hardening P2-1，
// GODEBUG 的 go 行声明的轻量版). Accepts a version string or "" to clear.
//
// SetCompat 设置兼容基线声明。接受版本串或空串清除。
func SetCompat(v string) error {
	return mutate(func(f *file) { f.Compat = v })
}

// mutate 读-改-写：字段级 setter 共用通道（SetTakeover 曾全量覆写单字段——
// 新增 Compat 后任何单字段覆写都会抹掉另一字段，读改写保共存）。
func mutate(fn func(*file)) error {
	f := read()
	fn(&f)
	p, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return err
	}
	body, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return util.AtomicWrite(p, append(body, '\n'), 0644)
}

// TakeoverPref returns the persisted preference ("" when unset) for display.
//
// TakeoverPref 返回已落盘的偏好（未设置返回空串）供展示。
func TakeoverPref() string {
	return read().Takeover
}
