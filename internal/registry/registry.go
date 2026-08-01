// Package registry maintains the global registry of forge projects at ~/.forge/projects.json.
//
// After the user-level-assets refactor this registry is THE anchor of "is this a forge
// project": forge init no longer writes a project-level .forge/ marker by default, so
// membership here (matched by git key or path prefix) replaces the old .forge/-existence
// check. projectroot.Find/FindProject resolve the project root via IsMember; the legacy
// .forge/ walk-up survives only as a backward-compat fallback for projects init'd by
// older versions (and self-heals by registering them).
//
// Single-project dashboard (forge dashboard) only reads the current project. Global view
// (forge dashboard --global) reads List() — the same store.
//
// Package registry 维护 forge 项目的全局注册表 ~/.forge/projects.json。
//
// user-level-assets 重构后，本注册表是"这是不是 forge 项目"的锚点：forge init 默认
// 不再写项目级 .forge/ 标记，成员资格（git key 或路径前缀匹配）取代旧的 .forge/
// 存在性检查。projectroot.Find/FindProject 经 IsMember 解析项目根；遗留的 .forge/
// walk-up 仅作为老版本 init 项目的向后兼容兜底（命中后自愈登记）。
//
// 单项目看板（forge dashboard）只读当前项目。全局视图（forge dashboard --global）
// 读 List()——同一 store。
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
	"github.com/MjxUpUp/Forge/internal/util"
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

// Entry is one registered project. Key is the forge project key (git common-dir hash,
// or PathKey for non-git projects); it may be empty for entries written by older forge
// versions — those are backfilled lazily at match time (never blocking reads).
//
// Entry 是一个已登记项目。Key 是 forge 项目 key（git common-dir hash，非 git 项目
// 为 PathKey）；老版本 forge 写入的条目可能为空——匹配时惰性补算（不阻塞读）。
type Entry struct {
	Path string `json:"path"`
	Key  string `json:"key,omitempty"`
}

// File is the on-disk structure of ~/.forge/projects.json. Older forge versions wrote
// {"projects": ["path1", ...]} (plain string list) — UnmarshalJSON accepts both shapes.
//
// File 是 ~/.forge/projects.json 的磁盘结构。老版本 forge 写的是
// {"projects": ["path1", ...]}（纯字符串列表）——UnmarshalJSON 两种形态都接受。
type File struct {
	Projects []Entry `json:"projects"`
}

// UnmarshalJSON accepts both the current entry-list shape and the legacy string-list
// shape, so upgrading forge never strands existing registrations.
//
// UnmarshalJSON 同时接受当前的 entry 列表形态与遗留的字符串列表形态，
// 升级 forge 不会丢失既有登记。
func (f *File) UnmarshalJSON(data []byte) error {
	var raw struct {
		Projects []json.RawMessage `json:"projects"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	f.Projects = f.Projects[:0]
	for _, r := range raw.Projects {
		var e Entry
		if err := json.Unmarshal(r, &e); err == nil {
			// Defensive: null / {} entries carry no path and can never match —
			// skip them instead of registering a ghost entry.
			//
			// 防御：null / {} 条目没有 path，永远匹配不上——跳过而非登记幽灵条目。
			if e.Path == `` {
				continue
			}
			f.Projects = append(f.Projects, e)
			continue
		}
		var s string
		if err := json.Unmarshal(r, &s); err != nil {
			return fmt.Errorf("registry: invalid project entry: %s", string(r))
		}
		f.Projects = append(f.Projects, Entry{Path: s})
	}
	return nil
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

// readFile loads the registry. Missing/corrupt file returns an empty File with ok=false
// (empty = no projects, not an error — same contract as before).
//
// readFile 加载注册表。文件缺失/损坏返回空 File 与 ok=false（空 = 无项目，非错误——
// 与之前契约一致）。
func readFile() (File, bool) {
	var f File
	p, err := globalPath()
	if err != nil {
		return f, false
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return f, false
	}
	if json.Unmarshal(data, &f) != nil {
		return File{}, false
	}
	return f, true
}

// keyOf returns the entry's project key, backfilling legacy entries (empty Key) by
// deriving from the entry path. Returns "" when the path is unusable.
//
// keyOf 返回条目的项目 key，遗留条目（Key 为空）按条目路径惰性补算。
// 路径不可用时返 ""。
func keyOf(e Entry) string {
	if e.Key != `` {
		return e.Key
	}
	if k, err := forgedata.Key(e.Path); err == nil {
		return k
	}
	if _, err := os.Stat(e.Path); err == nil {
		return forgedata.PathKey(e.Path)
	}
	return ``
}

// List reads registered project paths, dedupes + keeps only those whose path still
// exists (projects deleted/moved fade out automatically, preventing ghost paths from
// polluting the global view). Read failure / no registry returns nil (empty = no
// projects, not an error).
//
// Note: the pre-refactor prune condition was ".forge/ still exists"; after
// user-level-assets, init writes no project-level .forge/ at all, so liveness is
// judged by the project path itself.
//
// Lazy prune: if the registry contains stale entries (projects moved/deleted/duplicated in JSON), write back a pruned version — cleans
// test pollution (Temp dirs registered by e2e subprocess) + faded projects, so projects.json converges rather than
// growing unbounded (dogfood measured 1819 entries / 1814 junk). Write only happens when staleness is detected; normal reads do not write,
// avoiding write overhead on the high-frequency read path.
//
// List 读取已登记的项目路径，去重 + 仅保留路径仍存在的（项目被删/移动后自动淡出，
// 不让幽灵路径污染全局视图）。读失败/无注册表返回 nil（空 = 无项目，非错误）。
//
// 注意：重构前的 prune 条件是".forge/ 仍存在"；user-level-assets 之后 init 完全不写
// 项目级 .forge/，存活改按项目路径本身判定。
//
// 惰性精简：若注册表含已失效条目（项目移走/删除/JSON 内重复），写回精简版——清理
// 测试污染（e2e subprocess 注册的 Temp 目录）+ 已淡出项目，让 projects.json 收敛而非
// 无限膨胀（dogfood 实测 1819 条/1814 垃圾）。写仅在检测到失效时发生，常态读不写，
// 避免给高频读路径加写开销。
func List() []string {
	f, ok := readFile()
	if !ok {
		return nil
	}
	seen := make(map[string]bool)
	var out []string
	var kept []Entry
	pruned := false
	for _, e := range f.Projects {
		ap := filepath.Clean(e.Path)
		key := pathKey(ap)
		if seen[key] {
			// Duplicate entry within JSON.
			pruned = true // JSON 内重复条目
			continue
		}
		// Keep only entries whose path still exists; moved/deleted ones do not appear
		// in the global view. Only os.IsNotExist counts as "gone": any other stat
		// error (permission, invalid path, I/O) means "unreadable right now", not
		// "disappeared" — pruning on those would silently drop live projects.
		//
		// 仅保留路径仍存在的条目；移走/删除的不出现在全局视图。
		// 只有 os.IsNotExist 算「已消失」：其他 stat 错误（权限、非法路径、I/O）是
		// 「此刻不可读」而非「不存在」——按那些 prune 会把活项目静默踢出全局注册表。
		if _, err := os.Stat(ap); err != nil {
			if os.IsNotExist(err) {
				pruned = true
				continue
			}
		}
		seen[key] = true
		e.Path = ap
		out = append(out, ap)
		kept = append(kept, e)
	}
	// Stable order, dashboard rendering is reproducible.
	slices.Sort(out) // 稳定顺序，看板渲染可复现
	if pruned {
		// Lazy prune, write failure does not affect reads.
		_ = writeEntries(kept) // 惰性精简，写失败不影响读
	}
	return out
}

// writeEntries atomically writes the registry via util.AtomicWrite (temp file + fsync +
// rename, with Windows rename retry) — os.WriteFile whole-file overwrite is not atomic,
// a crash/power loss mid-write leaves a truncated corrupt JSON (making reads fail
// entirely); rename is atomic (on Windows Go os.Rename goes through MoveFileEx
// REPLACE_EXISTING). read-modify-write is still not concurrency-safe (two processes
// writing simultaneously may have the later overwrite the earlier and lose one entry),
// but local-tool concurrency is rare, lost entries can be re-added by re-running init;
// corrupt JSON is what must be prevented. Shared by Add and List lazy prune.
//
// writeEntries 原子写注册表，走 util.AtomicWrite（临时文件 + fsync + rename，含
// Windows rename 重试）——os.WriteFile 整文件覆盖非原子，写到一半崩溃/断电会留下
// 截断的损坏 JSON（让读整个失败）；rename 是原子的（Windows 上 Go os.Rename 走
// MoveFileEx REPLACE_EXISTING）。read-modify-write 仍非并发安全（两进程同时写可能
// 后写覆盖先写丢一条），但本地工具并发概率低，丢失重跑 init 可补；损坏 JSON 才是
// 必防的。供 Add 和 List 惰性精简共用。
func writeEntries(entries []Entry) error {
	p, err := globalPath()
	if err != nil {
		return err
	}
	f := File{Projects: entries}
	data, err := json.MarshalIndent(f, ``, `  `)
	if err != nil {
		return err
	}
	return util.AtomicWrite(p, append(data, '\n'), 0644)
}

// Add registers absPath into the global registry (deduped, idempotent). Path is
// normalized via Abs + Clean. The forge project key (git common-dir hash, or PathKey
// for non-git) is computed and stored so membership checks survive worktrees and
// match without .forge/. Upsert semantics: an existing entry for the same path gets
// its key refreshed; an entry with the same key but a different path gets its path
// updated only when the old path is gone (project moved) — a live old path means we
// are inside a worktree and the old path is kept. Used by forge init
// self-registration + dashboard --global self-registration of the current project.
//
// Add 把 absPath 登记到全局注册表（去重、幂等）。路径会 Abs + Clean 规范化。
// forge 项目 key（git common-dir hash，非 git 为 PathKey）一并计算存储，让成员
// 检查跨 worktree 命中、无需 .forge/。Upsert 语义：同路径条目刷新 key；同 key
// 不同路径的条目仅当旧路径已消失（项目被移动）才更新路径——旧路径仍活说明身处
// worktree，保留旧路径。用于 forge init 自登记 + dashboard --global 自登记当前项目。
func Add(absPath string) error {
	ap, err := filepath.Abs(absPath)
	if err != nil {
		return err
	}
	ap = filepath.Clean(ap)

	key := ``
	if k, kerr := forgedata.Key(ap); kerr == nil {
		key = k
	} else {
		key = forgedata.PathKey(ap)
	}

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
	for i, e := range f.Projects {
		samePath := pathKey(filepath.Clean(e.Path)) == pathKey(ap)
		sameKey := key != `` && keyOf(e) == key
		if samePath || sameKey {
			if sameKey && !samePath {
				// Same key but a different path: the project moved, OR this is a
				// worktree of an already-registered repo. Swap the path only when the
				// old one is gone (os.Stat IsNotExist) — if it is still alive we are
				// in a worktree and must keep the old path: overwriting it with the
				// worktree path would let List prune the whole entry (key included)
				// once the worktree is deleted, silently stripping the main project's
				// membership. Any non-IsNotExist stat error means "unreadable right
				// now", not "gone" — keep the old path then too.
				//
				// 同 key 不同路径：项目被移动，或这是已登记 repo 的 worktree。仅当
				// 旧路径已不存在（os.Stat IsNotExist）才换路径——旧路径仍活说明身处
				// worktree，必须保留旧路径：换成 worktree 路径会让 List 在 worktree
				// 删除后把整条（含 key）prune 掉，主项目静默丢成员资格。其他
				// 非 IsNotExist 的 stat 错误是「此刻不可读」而非「已消失」，同样保留。
				if _, serr := os.Stat(filepath.Clean(e.Path)); !os.IsNotExist(serr) {
					f.Projects[i] = Entry{Path: filepath.Clean(e.Path), Key: key}
					return writeEntries(f.Projects)
				}
			}
			// Upsert: refresh key (legacy entry) and path (moved project).
			//
			// Upsert：刷新 key（遗留条目）与路径（被移动的项目）。
			f.Projects[i] = Entry{Path: ap, Key: key}
			return writeEntries(f.Projects)
		}
	}
	f.Projects = append(f.Projects, Entry{Path: ap, Key: key})
	return writeEntries(f.Projects)
}

// Remove unregisters absPath, matched by path OR by project key — the key match
// (git common-dir hash, or PathKey for non-git) makes removal worktree-safe: asking
// to remove a worktree path removes the main project's entry that shares the key.
// Idempotent: absent path is a no-op. Used by forge uninstall-style flows and tests.
//
// Remove 注销 absPath，按路径或项目 key 匹配——key 匹配（git common-dir hash，
// 非 git 为 PathKey）让注销跨 worktree 生效：传 worktree 路径也能删掉共享同一
// key 的主项目条目。幂等：不存在即 no-op。供 forge uninstall 类流程与测试使用。
func Remove(absPath string) error {
	ap, err := filepath.Abs(absPath)
	if err != nil {
		return err
	}
	ap = filepath.Clean(ap)

	key := ``
	if k, kerr := forgedata.Key(ap); kerr == nil {
		key = k
	} else {
		key = forgedata.PathKey(ap)
	}

	f, ok := readFile()
	if !ok {
		return nil
	}
	kept := f.Projects[:0]
	removed := false
	for _, e := range f.Projects {
		if pathKey(filepath.Clean(e.Path)) == pathKey(ap) || (key != `` && keyOf(e) == key) {
			removed = true
			continue
		}
		kept = append(kept, e)
	}
	if !removed {
		return nil
	}
	return writeEntries(kept)
}

// IsMember reports whether cwd is inside a registered forge project, returning the
// project root. Match rules:
//   - git cwd: the repo's forge key (git common-dir hash) equals a registered key —
//     worktree-safe, .forge/-free. The root returned is the git working-tree root.
//   - non-git cwd: longest registered path that is cwd itself or its ancestor
//     (boundary-aware prefix match).
//
// Read-only on the hot path (no write-back); legacy entries without a stored key are
// backfilled in-memory per call — the cost is a few stats per candidate entry.
//
// IsMember 报告 cwd 是否在某个已登记 forge 项目内，并返回项目根。匹配规则：
//   - git cwd：repo 的 forge key（git common-dir hash）等于某个已登记 key——
//     跨 worktree 安全、无需 .forge/。返回的根是 git working tree 根。
//   - 非 git cwd：等于 cwd 或为其祖先的最长已登记路径（边界感知前缀匹配）。
//
// 热路径只读（不写回）；无存储 key 的遗留条目每次调用在内存补算——代价是每个
// 候选条目几次 stat。
func IsMember(cwd string) (root string, ok bool) {
	f, valid := readFile()
	if !valid || len(f.Projects) == 0 {
		return ``, false
	}
	abs, err := filepath.Abs(cwd)
	if err != nil {
		return ``, false
	}
	abs = filepath.Clean(abs)
	// Resolve symlinks when possible, matching PathKey semantics: a cwd reached
	// through a symlink must match the registered physical path. Keep BOTH forms as
	// match candidates — on systems where the temp/home dir is itself a symlink
	// (macOS /var → /private/var), entries were registered under the unresolved
	// form, so matching only the resolved form would break those.
	//
	// 可能时解析 symlink，与 PathKey 语义一致：经 symlink 进入的 cwd 必须能
	// 匹配到已登记的物理路径。两种形态都留作匹配候选——有的系统 temp/home 目录
	// 本身就是 symlink（macOS /var → /private/var），条目是按未解析形态登记的，
	// 只按解析后形态匹配会把它们弄丢。
	absForms := pathForms(abs)

	if gitRoot := forgedata.FindGitRoot(abs); gitRoot != `` {
		k, kerr := forgedata.Key(abs)
		if kerr != nil {
			return ``, false
		}
		for _, e := range f.Projects {
			if keyOf(e) == k {
				return gitRoot, true
			}
		}
		return ``, false
	}

	// Non-git: boundary-aware longest-prefix match. BOTH sides are compared in
	// lexical + symlink-resolved forms: entries may be registered under the
	// unresolved form (macOS /var → /private/var temp dirs) while the cwd arrives
	// through a symlink (or vice versa) — matching only one form per side misses.
	//
	// 非 git：边界感知的最长前缀匹配。两侧都按字面 + symlink 解析双形态比较：
	// 条目可能按未解析形态登记（macOS /var → /private/var 的 temp 目录），而
	// cwd 经 symlink 到达（或反之）——单边单形态会漏配。
	best := ``
	for _, e := range f.Projects {
		// The returned root is always the entry's LEXICAL registered form (resolved
		// forms only widen matching) — EvalSymlinks on Windows expands 8.3 short
		// names, and handing back the long form would surprise callers/tests
		// comparing against the registered path.
		//
		// 返回的根恒为条目的字面登记形态（解析形态只用于放宽匹配）——Windows 上
		// EvalSymlinks 会展开 8.3 短名，把长名形式返回会让按登记路径比较的
		// 调用方/测试困惑。
		epLexical := filepath.Clean(e.Path)
		matched := false
		for _, ep := range pathForms(e.Path) {
			for _, af := range absForms {
				// Exact match goes through pathKey so Windows case variants (C:\Proj vs
				// c:\proj) hit — plain == would miss them.
				//
				// 精确匹配走 pathKey，Windows 大小写变体（C:\Proj vs c:\proj）也能命中——
				// 裸 == 会漏。
				if pathKey(ep) == pathKey(af) {
					matched = true
					break
				}
				prefix := ep + string(filepath.Separator)
				if runtime.GOOS == "windows" {
					if strings.HasPrefix(strings.ToLower(af), strings.ToLower(prefix)) {
						matched = true
						break
					}
					continue
				}
				if strings.HasPrefix(af, prefix) {
					matched = true
					break
				}
			}
			if matched {
				break
			}
		}
		if matched && len(epLexical) > len(best) {
			best = epLexical
		}
	}
	if best == `` {
		return ``, false
	}
	return best, true
}

// pathForms returns the match-candidate forms of a path: the cleaned lexical form,
// plus the symlink-resolved physical form when it differs (macOS /var→/private/var,
// symlinked project dirs). Both IsMember sides (cwd and registry entries) are
// matched across these forms.
//
// pathForms 返回路径的匹配候选形态：Clean 后的字面形态，以及 symlink 解析后的
// 物理形态（不同时才含；macOS /var→/private/var、symlink 项目目录）。IsMember
// 的两侧（cwd 与注册条目）都跨这些形态匹配。
func pathForms(p string) []string {
	clean := filepath.Clean(p)
	forms := []string{clean}
	if eval, err := filepath.EvalSymlinks(clean); err == nil {
		if ev := filepath.Clean(eval); pathKey(ev) != pathKey(clean) {
			forms = append(forms, ev)
		}
	}
	return forms
}

// Prune explicitly prunes the global registry: removes dead paths (project dir no
// longer exists) + duplicate entries within JSON, atomically writes back.
// Returns (pruned, remain): pruned = number of entries removed this time (dead paths + duplicates), remain = number of active projects kept.
//
// Same logic as List() lazy prune, but explicitly triggered and returns counts — List only prunes when forge dashboard --global
// reads (and that command starts a web server that blocks), so ordinary users have no way to clean up proactively. Prune gives forge registry
// prune a cleanup entry point that does not start a web server (the root-cause gap for dogfood registry historical-residue cleanup).
//
// Returns (0,0,nil) when the registry file is missing or JSON is corrupt — consistent with List (empty = no projects, not an error).
//
// Prune 显式精简全局注册表：移除项目目录已不存在的死路径 + JSON 内重复条目，原子写回。
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
	if _, rerr := os.ReadFile(p); rerr != nil {
		// No registry file.
		return 0, 0, nil // 无注册表文件
	}
	var f File
	if data, rerr := os.ReadFile(p); rerr == nil {
		if json.Unmarshal(data, &f) != nil {
			// Corrupt JSON: not fatal, consistent with List (List also returns nil).
			return 0, 0, nil // 损坏 JSON：与 List 一致不致命（List 也返回 nil）
		}
	}
	before := len(f.Projects)
	// List prunes and writes back (removes dead paths + dedup + sort + atomic rename).
	remain = len(List()) // List 精简写回（去死路径+去重+排序+原子 rename）
	return before - remain, remain, nil
}
