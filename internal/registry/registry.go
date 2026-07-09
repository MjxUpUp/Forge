// Package registry 维护 forge 项目的全局注册表 ~/.forge/projects.json。
//
// 单项目看板（forge dashboard）只读当前 .forge/。全局视图（forge dashboard --global）
// 需要一处知道"用户在哪些项目跑过 forge"——本包就是那个登记处。forge init 时自登记
// 当前项目绝对路径；dashboard --global 也会自登记当前项目（兼容已 init 但未登记的老项目）。
//
// 与 knowledge store 同根（~/.forge/ 全局状态目录，home 下；区别于项目级 .forge/）。
package registry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"

	"github.com/MjxUpUp/Forge/internal/forgedata"
)

// File 是 ~/.forge/projects.json 的磁盘结构：去重的项目绝对路径列表。
type File struct {
	Projects []string `json:"projects"`
}

// globalPath 返回注册表路径。全局 home 走 forgedata.GlobalHome()（FORGE_DATA_HOME 优先，
// 否则 ~/.forge）——refactor-data-home commit E 统一真相源，废弃旧的 FORGE_HOME env。
// env 优先让子进程（forge 二进制经 exec 跑）也能被测试隔离——仅靠进程内变量注入，子进程不继承。
func globalPath() (string, error) {
	home, err := forgedata.GlobalHome()
	if err != nil {
		return ``, err
	}
	return filepath.Join(home, `projects.json`), nil
}

// List 读取已登记的项目路径，去重 + 仅保留仍含 .forge/ 的（项目被删/移动后自动淡出，
// 不让幽灵路径污染全局视图）。读失败/无注册表返回 nil（空 = 无项目，非错误）。
//
// 惰性精简：若注册表含已失效条目（项目移走/删除/JSON 内重复），写回精简版——清理
// 测试污染（e2e subprocess 注册的 Temp 目录）+ 已淡出项目，让 projects.json 收敛而非
// 无限膨胀（dogfood 实测 1819 条/1814 垃圾）。写仅在检测到失效时发生，常态读不写，
// 避免给高频读路径加写开销。
func List() []string {
	p, err := globalPath()
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	var f File
	if json.Unmarshal(data, &f) != nil {
		return nil
	}
	seen := make(map[string]bool)
	var out []string
	pruned := false
	for _, pr := range f.Projects {
		ap := filepath.Clean(pr)
		if seen[ap] {
			pruned = true // JSON 内重复条目
			continue
		}
		// 仅保留仍是 forge 项目的（.forge/ 存在）；移走/删除的不出现在全局视图。
		if _, err := os.Stat(filepath.Join(ap, `.forge`)); err != nil {
			pruned = true
			continue
		}
		seen[ap] = true
		out = append(out, ap)
	}
	sort.Strings(out) // 稳定顺序，看板渲染可复现
	if pruned {
		_ = writeFile(p, out) // 惰性精简，写失败不影响读
	}
	return out
}

// writeFile 原子写注册表（projects 列表，已去重排序）。先写临时文件再 rename 覆盖——
// os.WriteFile 整文件覆盖非原子，写到一半崩溃/断电会留下截断的损坏 JSON（让 List 整个
// 失败）；rename 是原子的（Windows 上 Go os.Rename 走 MoveFileEx REPLACE_EXISTING）。
// read-modify-write 仍非并发安全（两进程同时写可能后写覆盖先写丢一条），但本地工具并发
// 概率低，丢失重跑 init 可补；损坏 JSON 才是必防的。供 Add 和 List 惰性精简共用。
func writeFile(path string, projects []string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f := File{Projects: projects}
	data, err := json.MarshalIndent(f, ``, `  `)
	if err != nil {
		return err
	}
	tmp := path + `.tmp`
	if err := os.WriteFile(tmp, append(data, '\n'), 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Add 把 absPath 登记到全局注册表（去重、幂等）。路径会 Abs + Clean 规范化。
// 注册表/目录不存在则创建。用于 forge init 自登记 + dashboard --global 自登记当前项目。
func Add(absPath string) error {
	ap, err := filepath.Abs(absPath)
	if err != nil {
		return err
	}
	ap = filepath.Clean(ap)

	p, err := globalPath()
	if err != nil {
		return err
	}
	var f File
	if data, rerr := os.ReadFile(p); rerr == nil {
		_ = json.Unmarshal(data, &f)
	}
	for _, e := range f.Projects {
		if filepath.Clean(e) == ap {
			return nil // 已登记，幂等
		}
	}
	f.Projects = append(f.Projects, ap)
	return writeFile(p, f.Projects)
}
