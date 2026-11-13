// Package userconfig persists forge's user-level preferences (~/.forge/config.json).
//
// Package userconfig 持久化 forge 的用户级偏好（~/.forge/config.json）——目前是
// 兼容基线声明（compat）。文件在用户级 store（升级链路不触碰）。
package userconfig

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/util"
)

// file is the on-disk shape of ~/.forge/config.json. Only compat exists today;
// when a second key appears, keep read-merge-write so concurrent forge
// versions don't clobber each other's keys.
//
// file 是 ~/.forge/config.json 的磁盘结构。当前只有 compat 一个键；
// 出现第二个键时保持读-合并-写，避免新旧版本互抹对方的键。
type file struct {
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

// mutate 读-改-写：字段级 setter 共用通道（新增字段后任何单字段覆写都会
// 抹掉另一字段，读改写保共存）。
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
