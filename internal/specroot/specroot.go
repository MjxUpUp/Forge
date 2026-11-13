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
	"runtime"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/MjxUpUp/Forge/internal/forgedata"
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
// Hooks 是仓库级的执法开关：默认 off（ADR-0002 阶段 B：执法 opt-in，默认安静）。
// off 时该仓库内的 hook 一律静默放行（clone 下来先读契约、再决定要不要跑检查，
// 不需要安装或注册）；要执法须显式写 hooks: on。
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

// Find 从 cwd 向上查找仓库根的 spec/forge.yaml，返回项目根。
//
// 契约位置只有仓库根：在 git 仓库里先归一到 git 根（.git 是目录或文件都算，
// worktree 同样适用），只在那一层找标记——子目录里的 spec/forge.yaml 不算，
// 否则同一个仓库会因 cwd 深浅得到不同身份，`forge status` 与 `forge policy
// state` 也会互相矛盾。非 git 目录（还没 git init 的项目）向上查到用户 home
// 或文件系统根为止，home 本身不算项目根：home 下一个契约不该把整棵 home 认成
// 一个项目。
//
// 路径先取真实路径（符号链接归一）：从 symlink 进入仓库与直接进入必须是同一个
// 项目身份。未找到返回 ("", false)。
func Find(cwd string) (string, bool) {
	dir, err := canonicalDir(cwd)
	if err != nil {
		return "", false
	}

	if gitRoot := forgedata.FindGitRoot(dir); gitRoot != `` {
		if root, cerr := canonicalDir(gitRoot); cerr == nil {
			gitRoot = root
		}
		if isFile(MarkerPath(gitRoot)) {
			return gitRoot, true
		}
		return "", false
	}

	home := homeDir()
	for {
		if home != `` && sameDir(dir, home) {
			return "", false
		}
		if isFile(MarkerPath(dir)) {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// canonicalDir 返回 dir 的真实路径（解析符号链接）。
//
// 路径尚不存在时（CLI 允许传新建目录，Find 也可能被喂进未创建的深层路径），
// 先解析最深的已存在祖先、再把剩余段拼回去——否则从 symlink 进入一个尚未创建
// 的子目录会得到「link/a」这种未归一形态，与真实路径不是同一个身份。
func canonicalDir(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	if real, rerr := filepath.EvalSymlinks(abs); rerr == nil {
		return real, nil
	}

	var tail []string
	cur := abs
	for {
		parent := filepath.Dir(cur)
		if parent == cur {
			return abs, nil
		}
		tail = append([]string{filepath.Base(cur)}, tail...)
		cur = parent
		real, rerr := filepath.EvalSymlinks(cur)
		if rerr != nil {
			continue
		}
		return filepath.Join(append([]string{real}, tail...)...), nil
	}
}

// homeDir 返回用户 home（解析失败返回空串，此时不设 home 边界）。
func homeDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == `` {
		return ``
	}
	if real, cerr := canonicalDir(home); cerr == nil {
		return real
	}
	return home
}

// sameDir 报告两个路径是否指向同一目录：优先文件身份（同一 inode/卷），退化为
// 清理后的字符串比较；Windows 大小写不敏感，额外做 EqualFold（Linux 上大小写
// 不同的目录确实是两个目录，不能折叠）。
func sameDir(a, b string) bool {
	ia, ea := os.Stat(a)
	ib, eb := os.Stat(b)
	if ea == nil && eb == nil {
		return os.SameFile(ia, ib)
	}
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	return runtime.GOOS == "windows" && strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
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
		// ADR-0002 阶段 B：执法 opt-in。缺省 off（默认安静/audit-only），
		// 要执法须显式 hooks: on。翻转前缺省是 on。
		c.Hooks = HooksOff
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
