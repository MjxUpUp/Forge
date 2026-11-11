// Package specroot 解析仓库内声明式契约目录 spec/（repo-declared activation）。
//
// spec-in-repo 重构的第一块：成员资格的锚点从「安装态」（全局注册表
// ~/.forge/projects.json）前移到「仓库态」——仓库根下存在 spec/forge.yaml，
// 该仓库即被认定为 forge 项目，clone 到新机器、换个 agent、不跑 forge init
// 都能解析到同一个项目身份（身份由 forgedata.ProjectFor 纯路径推导）。
//
// 这里只做两件事：找标记（Find）与读契约（Load）。契约内容的最小面是
// version/profile/hooks；profile 决定 hook 名册档位（mini＝lite 最小执法名册，
// 要只声明不执法用 hooks: off），具体门禁策略仍由上层解释。
package specroot

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	// DirName 是契约目录名（位于仓库根）。
	DirName = "spec"
	// MarkerName 是契约标记文件名（spec/forge.yaml）。
	MarkerName = "forge.yaml"

	// ProfileMini 是最小执法名册：lite 档（拦截类 hook，不含 advisory 类）。
	// 要「只声明、不执法」用 hooks: off，不是 profile: mini。
	ProfileMini = "mini"
	// ProfileStandard 是标准名册，也是缺省：不写 profile 时按它跑（与
	// hooks.ActiveProfile / ParseProfile 的缺省保持一致）。
	ProfileStandard = "standard"
	// ProfileFull 启用全部 hook 与门禁。
	ProfileFull = "full"

	// CurrentVersion 是当前支持的契约版本。
	CurrentVersion = "1"
)

// ErrNoContract 表示从 cwd 向上未找到 spec/forge.yaml。
var ErrNoContract = errors.New("specroot: 未找到 spec/forge.yaml（仓库未声明 forge 契约）")

// Contract 是 spec/forge.yaml 的最小解析结果。
//
// Hooks 是仓库级的执法开关：默认 on；off 时该仓库内的 hook 一律静默放行
// （clone 下来先读契约、再决定要不要跑检查，不需要安装或注册）。
type Contract struct {
	Version string `yaml:"version"`
	Profile string `yaml:"profile"`
	Hooks   string `yaml:"hooks"`
}

// HooksOn / HooksOff 是 hooks 字段的合法取值。
const (
	HooksOn  = "on"
	HooksOff = "off"
)

// MarkerPath 返回 root 下的契约文件路径。
func MarkerPath(root string) string {
	return filepath.Join(root, DirName, MarkerName)
}

// Find 从 cwd 向上查找最近的、含 spec/forge.yaml 的目录。
//
// 遇到仓库根（含 .git 文件或目录的目录）即停，不越界到父仓库——子模块/嵌套仓库
// 不应被外层契约激活。返回的目录即项目根；未找到返回 ("", false)。
func Find(cwd string) (string, bool) {
	dir, err := filepath.Abs(cwd)
	if err != nil {
		return "", false
	}
	for {
		if isFile(MarkerPath(dir)) {
			return dir, true
		}
		if exists(filepath.Join(dir, ".git")) {
			return "", false
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// Load 读取并校验 root/spec/forge.yaml。
//
// 缺省值：version=1、profile=standard、hooks=on。未知 profile、未知键或不受支持
// 的主版本返回错误——
// 契约是人写的文件，拼错档位时静默降级比报错更危险。
//
// 消费现状（2026-11-11）：成员资格认「文件存在」；hooks 开关已接线——
// hookdispatch 在解析出项目根后读契约，off 则静默放行、profile 决定名册档位。
func Load(root string) (*Contract, error) {
	path := MarkerPath(root)
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, ErrNoContract
		}
		return nil, fmt.Errorf("specroot: 读取 %s: %w", path, err)
	}

	// KnownFields(true)：拼错键名（hooks 写成 hook）必须报错而不是静默按缺省跑
	// ——那个仓库以为关了执法，实际还在拦。空文件按全缺省处理。
	var parsed Contract
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&parsed); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("specroot: 解析 %s: %w", path, err)
	}

	c := &Contract{
		Version: strings.TrimSpace(parsed.Version),
		Profile: strings.ToLower(strings.TrimSpace(parsed.Profile)),
		Hooks:   strings.ToLower(strings.TrimSpace(parsed.Hooks)),
	}
	if c.Version == "" {
		c.Version = CurrentVersion
	}
	if majorOf(c.Version) != majorOf(CurrentVersion) {
		return nil, fmt.Errorf("specroot: %s 的 version=%q 不受支持（当前支持 %s.x）", path, c.Version, majorOf(CurrentVersion))
	}
	if c.Profile == "" {
		c.Profile = ProfileStandard
	}
	switch c.Profile {
	case ProfileMini, ProfileStandard, ProfileFull:
	default:
		return nil, fmt.Errorf("specroot: %s 的 profile=%q 无效（可选 %s/%s/%s）",
			path, c.Profile, ProfileMini, ProfileStandard, ProfileFull)
	}
	if c.Hooks == "" {
		c.Hooks = HooksOn
	}
	switch c.Hooks {
	case HooksOn, HooksOff:
	default:
		return nil, fmt.Errorf("specroot: %s 的 hooks=%q 无效（可选 %s/%s）",
			path, c.Hooks, HooksOn, HooksOff)
	}
	return c, nil
}

// majorOf 取版本号的主版本段（"1" 与 "1.1" 同属主版本 1）。契约按主版本兼容：
// 小版本号留给后续字段演进，不能让 version: "1.1" 被当成「不支持的版本」拒掉。
func majorOf(v string) string {
	if i := strings.IndexByte(v, '.'); i >= 0 {
		return v[:i]
	}
	return v
}

// Declared 报告 root 是否带契约标记（不解析内容）。
func Declared(root string) bool {
	return isFile(MarkerPath(root))
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
