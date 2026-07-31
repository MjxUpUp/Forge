// Package registry maintains the global registry of forge projects at ~/.forge/projects.json.
//
// Single-project dashboard (forge dashboard) only reads the current .forge/. Global view (forge dashboard --global)
// needs a single place to know which projects the user has run forge in — this package is that registry. forge init self-registers
// the current project absolute path; dashboard --global also self-registers the current project (compatible with old projects that were init'd but not registered).
//
// Same root as knowledge store (~/.forge/ global state dir, under home; distinct from project-level .forge/).
//
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
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/forgedata"
)

// pathKey normalizes a cleaned absolute path for dedupe/equality. Windows filesystems are
// case-insensitive, so C:\Proj and c:\proj are the same project — plain string comparison
// would register them as two entries. Other platforms keep exact comparison (case matters there).
//
// pathKey 归一化一个已 Clean 的绝对路径用于去重/相等判断。Windows 文件系统大小写
// 不敏感，C:\Proj 与 c:\proj 是同一个项目——纯字符串比较会把它们登记成两条。
// 其他平台保持精确比较（那里大小写有区分）。
func pathKey(cleanedAbs string) string {
	if runtime.GOOS == "windows" {
		return strings.ToLower(cleanedAbs)
	}
	return cleanedAbs
}

// File is the on-disk structure of ~/.forge/projects.json: a deduped list of project absolute paths.
//
// File 是 ~/.forge/projects.json 的磁盘结构：去重的项目绝对路径列表。
type File struct {
	Projects []string `json:"projects"`
}

// globalPath returns the registry path. Global home goes through forgedata.GlobalHome() (FORGE_DATA_HOME first,
// otherwise ~/.forge) — refactor-data-home commit E unified the source of truth, deprecating the old FORGE_HOME env.
// Env precedence lets subprocesses (forge binary run via exec) also be isolated in tests — in-process variable injection alone is not inherited by subprocesses.
//
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

// List reads registered project paths, dedupes + keeps only those still containing .forge/ (projects deleted/moved fade out automatically,
// preventing ghost paths from polluting the global view). Read failure / no registry returns nil (empty = no projects, not an error).
//
// Lazy prune: if the registry contains stale entries (projects moved/deleted/duplicated in JSON), write back a pruned version — cleans
// test pollution (Temp dirs registered by e2e subprocess) + faded projects, so projects.json converges rather than
// growing unbounded (dogfood measured 1819 entries / 1814 junk). Write only happens when staleness is detected; normal reads do not write,
// avoiding write overhead on the high-frequency read path.
//
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
		key := pathKey(ap)
		if seen[key] {
			// Duplicate entry within JSON.
			pruned = true // JSON 内重复条目
			continue
		}
		// Keep only entries that are still forge projects (.forge/ exists); moved/deleted ones do not appear in the global view.
		// Only os.IsNotExist counts as "gone": any other stat error (permission, invalid path, I/O)
		// means "unreadable right now", not "disappeared" — pruning on those would silently drop
		// live projects from the global registry.
		//
		// 仅保留仍是 forge 项目的（.forge/ 存在）；移走/删除的不出现在全局视图。
		// 只有 os.IsNotExist 算「已消失」：其他 stat 错误（权限、非法路径、I/O）是
		// 「此刻不可读」而非「不存在」——按那些 prune 会把活项目静默踢出全局注册表。
		if _, err := os.Stat(filepath.Join(ap, `.forge`)); err != nil {
			if os.IsNotExist(err) {
				pruned = true
				continue
			}
		}
		seen[key] = true
		out = append(out, ap)
	}
	// Stable order, dashboard rendering is reproducible.
	slices.Sort(out) // 稳定顺序，看板渲染可复现
	if pruned {
		// Lazy prune, write failure does not affect reads.
		_ = writeFile(p, out) // 惰性精简，写失败不影响读
	}
	return out
}

// writeFile atomically writes the registry (projects list, deduped and sorted). Writes a temp file then renames over it —
// os.WriteFile whole-file overwrite is not atomic, a crash/power loss mid-write leaves a truncated corrupt JSON (making List fail
// entirely); rename is atomic (on Windows Go os.Rename goes through MoveFileEx REPLACE_EXISTING).
// read-modify-write is still not concurrency-safe (two processes writing simultaneously may have the later overwrite the earlier and lose one entry), but local-tool
// concurrency is rare, lost entries can be re-added by re-running init; corrupt JSON is what must be prevented. Shared by Add and List lazy prune.
//
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

// Add registers absPath into the global registry (deduped, idempotent). Path is normalized via Abs + Clean.
// Creates registry/dir if absent. Used by forge init self-registration + dashboard --global self-registration of the current project.
//
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
		if uerr := json.Unmarshal(data, &f); uerr != nil {
			// Corrupt registry: back the file aside before rebuilding from empty — the old
			// code swallowed the error and then atomically overwrote the registry with just
			// the current project, silently wiping every other registration. Backup + stderr
			// warning keep the failure explicit and recoverable.
			//
			// 注册表损坏：重建前先把文件备份到一边——旧代码吞掉错误后把仅含当前项目的
			// 表原子覆盖回去，其他所有登记被静默清空。备份 + stderr 告警让失败显式、可恢复。
			corrupt := fmt.Sprintf("%s.corrupt-%s", p, time.Now().Format("20060102-150405"))
			if cerr := os.Rename(p, corrupt); cerr != nil {
				fmt.Fprintf(os.Stderr, "warn: 备份损坏的注册表 %s 失败: %v\n", p, cerr)
			} else {
				fmt.Fprintf(os.Stderr, "warn: 注册表 JSON 损坏（%v），已备份到 %s，从空表重建\n", uerr, corrupt)
			}
			f = File{}
		}
	}
	for _, e := range f.Projects {
		if pathKey(filepath.Clean(e)) == pathKey(ap) {
			// Already registered, idempotent.
			return nil // 已登记，幂等
		}
	}
	f.Projects = append(f.Projects, ap)
	return writeFile(p, f.Projects)
}

// Prune explicitly prunes the global registry: removes dead paths where .forge/ does not exist + duplicate entries within JSON, atomically writes back.
// Returns (pruned, remain): pruned = number of entries removed this time (dead paths + duplicates), remain = number of active projects kept.
//
// Same logic as List() lazy prune, but explicitly triggered and returns counts — List only prunes when forge dashboard --global
// reads (and that command starts a web server that blocks), so ordinary users have no way to clean up proactively. Prune gives forge registry
// prune a cleanup entry point that does not start a web server (the root-cause gap for dogfood registry historical-residue cleanup).
//
// Returns (0,0,nil) when the registry file is missing or JSON is corrupt — consistent with List (empty = no projects, not an error).
//
// Prune 显式精简全局注册表：移除 .forge/ 不存在的死路径 + JSON 内重复条目，原子写回。
// 返回 (pruned, remain)：pruned=本次移除条数（死路径+重复），remain=保留的活跃项目数。
//
// 与 List() 的惰性精简同逻辑，但显式触发并返回计数——List 只在 forge dashboard --global
// 读时精简（且该命令启 web server 阻塞），普通用户无从主动清理。Prune 给 forge registry
// prune 提供不启动 web 的清理入口（dogfood registry 历史残留清理的治本缺口）。
//
// 无注册表文件或 JSON 损坏时返回 (0,0,nil)——与 List 一致（空=无项目，非错误）。
func Prune() (pruned, remain int, err error) {
	p, err := globalPath()
	if err != nil {
		return 0, 0, err
	}
	data, rerr := os.ReadFile(p)
	if rerr != nil {
		// No registry file.
		return 0, 0, nil // 无注册表文件
	}
	var f File
	if json.Unmarshal(data, &f) != nil {
		// Corrupt JSON: not fatal, consistent with List (List also returns nil).
		return 0, 0, nil // 损坏 JSON：与 List 一致不致命（List 也返回 nil）
	}
	before := len(f.Projects)
	// List prunes and writes back (removes dead paths + dedup + sort + atomic rename).
	remain = len(List()) // List 精简写回（去死路径+去重+排序+原子 rename）
	return before - remain, remain, nil
}
