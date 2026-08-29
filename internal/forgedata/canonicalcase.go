package forgedata

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// CanonicalCase normalizes an absolute path to the on-disk spelling (canonical case), component by component: each level is rewritten to the actual entry name from the parent directory's ReadDir.
//
// CanonicalCase 把绝对路径逐级归一到磁盘真实拼写（canonical case）：每级用父目录
// ReadDir 的实际条目名回写。它为 macOS 默认大小写不敏感但保留拼写的 APFS 而存在：
// 同一目录的任意大小写拼写都能 stat 成功，而 filepath.EvalSymlinks 不做大小写归一
// ——于是拼写变体的路径字符串（agent 宿主经 argv/JSON 传入 cwd，绕过 getcwd 的
// 拼写归一）会给同一项目推导出不同身份 hash（2026-08-18 Forge/forge key 分裂
// dogfood bug）。
//
// 匹配规则（正确性关键）：精确匹配优先；仅当大小写不敏感命中唯一时才回退。
// 大小写敏感文件系统（Linux、敏感 APFS）上每个组件都走精确分支，行为恒等于
// identity——绝不折叠两个真正不同的目录。
//
// 平台策略：darwin 执行组件级归一；其他平台 no-op（Windows 的大小写折叠由
// PathKey / registry.pathKey 现有的 ToLower 分支负责，刻意不动）。
//
// 不存在的路径（或父目录不可读）保留字面尾部：归一是 best-effort 永不失败——
// 缺失的尾部无法解析，按字面 hash 是既有行为。
//
// 结果按进程 memoize：Key() 是 hook 热路径，registry 匹配在嵌套循环里调用本函数；
// ReadDir walk 每个不同路径约耗「路径深度」次 syscall，小 map 缓存让重复推导保持廉价。
//
// 已知限制：Unicode 规范化（APFS 的 NFD vs NFC）不折叠——strings.EqualFold 不
// 等同不同组合形式的非 ASCII 名，非 ASCII 项目目录仍可能分裂。超出本 bug
// （ASCII 大小写）范围（code-review nit #7）。
func CanonicalCase(path string) string {
	if runtime.GOOS != `darwin` {
		return path
	}
	if path == `` || !filepath.IsAbs(path) {
		return path
	}
	if v, ok := canonicalCaseCache.Load(path); ok {
		return v.(string)
	}
	out := canonicalCaseWalk(path)
	canonicalCaseCache.Store(path, out)
	return out
}

// canonicalCaseCache memoize CanonicalCase 结果（输入字面 → canonical 形态）。进程
// 生命周期、不设上限但实际很小：hook 进程短命，每进程的不同路径数有限（项目根 +
// 注册表条目）。
var canonicalCaseCache sync.Map

// canonicalCaseWalk 执行逐级 ReadDir 回写。仅 darwin 调用方（CanonicalCase 已守
// GOOS）；拆出来让缓存快路径保持扁平。
func canonicalCaseWalk(path string) string {
	// 当前已解析前缀；从文件系统根开始。
	cur := string(filepath.Separator)
	rest := strings.TrimPrefix(path, string(filepath.Separator))
	comps := strings.Split(rest, string(filepath.Separator))
	for i, comp := range comps {
		if comp == `` {
			continue
		}
		entries, err := os.ReadDir(cur)
		if err != nil {
			// 父目录不可读或到此的前缀不存在：剩余尾部无法解析——保留字面
			// （既有行为）。
			return filepath.Join(append([]string{cur}, comps[i:]...)...)
		}
		actual := ``
		ciHit := ``
		ciCount := 0
		for _, e := range entries {
			name := e.Name()
			if name == comp {
				actual = comp
				break
			}
			if strings.EqualFold(name, comp) {
				ciHit = name
				ciCount++
			}
		}
		switch {
		case actual != ``:
			// exact match — case-sensitive filesystems always land here
		case ciCount == 1:
			// 唯一大小写不敏感命中：同一目录、拼写变体——回写磁盘真实名。
			actual = ciHit
		default:
			// 零命中（尾部不存在）或歧义（敏感 FS 上有多个折叠后相等的条目）：
			// 保留字面组件。
			actual = comp
		}
		cur = filepath.Join(cur, actual)
	}
	return cur
}
