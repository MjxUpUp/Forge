// Package registry maintains the global registry of forge projects (~/.forge/projects.json).
//
// Package registry 维护 forge 项目的全局注册表 ~/.forge/projects.json。
//
// user-level-assets 重构后，本注册表是"这是不是 forge 项目"的锚点：forge init 默认
// 不再写项目级 .forge/ 标记，成员资格（git key 或路径前缀匹配）取代旧的 .forge/
// 存在性检查。projectroot.Find/FindProject 经 IsMember 解析项目根；遗留的 .forge/
// walk-up 仅作为老版本 init 项目的向后兼容兜底（命中后自愈登记）。
//
// forge dashboard 聚合全局 Pulse 面板即读 List()——同一 store。
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

// pathKey 归一化一个已 Clean 的绝对路径用于去重/相等判断。Windows 文件系统大小写
// 不敏感，C:\Proj 与 c:\proj 是同一个项目——纯字符串比较会把它们登记成两条。
// macOS 默认大小写不敏感 APFS 有同样问题且更绕：文件系统保留拼写，变体拼写
// （Forge vs forge）是同一目录的两个不同字符串。比较因此走
// forgedata.CanonicalCase（与 Key 推导共享的单一真相源——旧注释「其他平台大小写
// 有区分」的假设对 APFS 是事实性错误）。Linux/大小写敏感文件系统：CanonicalCase
// 恒等，精确比较语义不变。
func pathKey(cleanedAbs string) string {
	if runtime.GOOS == "windows" {
		return strings.ToLower(cleanedAbs)
	}
	return forgedata.CanonicalCase(cleanedAbs)
}

// Entry is one registered project.
//
// Entry 是一个已登记项目。Key 是 forge 项目 key（git common-dir hash，非 git 项目
// 为 PathKey）；老版本 forge 写入的条目可能为空——匹配时惰性补算（不阻塞读）。
// Status/DecisionBy/DecisionAt 是 Project Policy Layer 的接管状态与决策审计
// （见 policy.go）：Status 空串（零值）= managed——存量 JSON 无此字段，升级不改变
// 任何既有项目的成员资格；"declined" = 已退出接管（IsMember 不再命中）。
type Entry struct {
	Path string `json:"path"`
	Key  string `json:"key,omitempty"`
	// Status is the takeover status: empty = managed (zero-value compat), "declined" = opted out. See policy.go.
	//
	// Status 是接管状态：空 = managed（零值兼容存量条目），"declined" = 已退出接管。
	Status string `json:"status,omitempty"`
	// DecisionBy records who/what made the latest status decision (e.g. "forge off").
	//
	// DecisionBy 记录最近一次状态决策的来源（如 "forge off"）。
	DecisionBy string `json:"decision_by,omitempty"`
	// DecisionAt records when the latest status decision was made.
	//
	// DecisionAt 记录最近一次状态决策的时间。
	DecisionAt time.Time `json:"decision_at,omitempty"`
}

// File is the on-disk structure of ~/.forge/projects.json.
//
// File 是 ~/.forge/projects.json 的磁盘结构。老版本 forge 写的是
// {"projects": ["path1", ...]}（纯字符串列表）——UnmarshalJSON 两种形态都接受。
type File struct {
	Projects []Entry `json:"projects"`
}

// UnmarshalJSON accepts both the current entry-list shape and the legacy string-list shape, so upgrading forge never strands existing registrations.
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

// readFile 加载注册表。文件缺失/损坏返回空 File 与 ok=false（空 = 无项目，非错误——
// 与之前契约一致）。读侧经 util.ReadJSONFile 单一入口（2026-09 普查 P3-6）。
func readFile() (File, bool) {
	var f File
	p, err := globalPath()
	if err != nil {
		return f, false
	}
	if err := util.ReadJSONFile(p, &f); err != nil {
		return File{}, false
	}
	return f, true
}

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

// List reads the registered project paths, deduped and existence-filtered.
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
	// 读-判定-惰性写回整体入锁：只锁写回段时读快照可在入锁前被并发写者更新，
	// 写回仍整文件覆盖丢条目（审查 MAJOR）。List 调用方低频（dashboard/assignment）。
	var out []string
	_ = withLock(func() error {
		out = listLocked()
		return nil
	})
	return out
}

// listLocked 是 List 的锁内实现（读 + 去重 + 存活过滤 + 惰性精简写回）。
func listLocked() []string {
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
		// Lazy prune, write failure does not affect reads. 锁内直调（listLocked 已在
		// List 的 withLock 内——withLock 非重入，嵌套会空转 2s 后走放弃路径）。
		_ = writeEntries(kept) // 惰性精简，写失败不影响读
	}
	return out
}

// Keys returns the registered project keys, excluding entries whose path no
// longer exists——与 List 同存在性语义：只有 IsNotExist 算消失，其他 stat 错误按存在
// 保留，防权限抖动把活项目判成孤儿。
// 只读不写（无 List 的惰性精简副作用）——供 registry gc 判定 <home>/projects/<key>
// 孤儿数据目录：不在集合内的 key 才是 gc 候选。
func Keys() []string {
	f, ok := readFile()
	if !ok {
		return nil
	}
	seen := make(map[string]bool)
	var out []string
	for _, e := range f.Projects {
		if _, err := os.Stat(filepath.Clean(e.Path)); err != nil && os.IsNotExist(err) {
			continue
		}
		k := keyOf(e)
		if k == `` || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, k)
	}
	return out
}

// writeEntries 原子写注册表，走 util.AtomicWrite（临时文件 + fsync + rename，含
// Windows rename 重试）——os.WriteFile 整文件覆盖非原子，写到一半崩溃/断电会留下
// 截断的损坏 JSON（让读整个失败）；rename 是原子的（Windows 上 Go os.Rename 走
// MoveFileEx REPLACE_EXISTING）。read-modify-write 仍非并发安全（两进程同时写可能
// 后写覆盖先写丢一条），但本地工具并发概率低，丢失重跑 init 可补；损坏 JSON 才是
// 必防的。供 Add/SetStatus（经 loadForWrite）和 List 惰性精简共用。
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

// Add registers absPath into the global registry (deduped, idempotent).
//
// Add 把 absPath 登记到全局注册表（去重、幂等）。路径会 Abs + Clean 规范化。
// forge 项目 key（git common-dir hash，非 git 为 PathKey）一并计算存储，让成员
// 检查跨 worktree 命中、无需 .forge/。Upsert 语义：同路径条目刷新 key；同 key
// 不同路径的条目仅当旧路径已消失（项目被移动）才更新路径——旧路径仍活说明身处
// worktree，保留旧路径。用于 forge init 自登记 + dashboard 启动时自登记当前项目。
//
// P1 契约：upsert **保留既有 Status/Decision 字段**——declined 条目不得被自登记/
// 自愈复活；declined→managed 的唯一通道是 SetStatus（forge on）。
func Add(absPath string) error {
	ap, err := filepath.Abs(absPath)
	if err != nil {
		return err
	}
	ap = filepath.Clean(ap)

	key := entryKey(ap)

	return withLock(func() error {
		f, err := loadForWrite()
		if err != nil {
			return err
		}
		for i, e := range f.Projects {
			samePath := pathKey(filepath.Clean(e.Path)) == pathKey(ap)
			sameKey := key != `` && keyOf(e) == key
			if samePath || sameKey {
				if sameKey && !samePath {
					// 同 key 不同路径：项目被移动，或这是已登记 repo 的 worktree。仅当
					// 旧路径已不存在（os.Stat IsNotExist）才换路径——旧路径仍活说明身处
					// worktree，必须保留旧路径：换成 worktree 路径会让 List 在 worktree
					// 删除后把整条（含 key）prune 掉，主项目静默丢成员资格。其他
					// 非 IsNotExist 的 stat 错误是「此刻不可读」而非「已消失」，同样保留。
					if _, serr := os.Stat(filepath.Clean(e.Path)); !os.IsNotExist(serr) {
						ne := e // 保留 Status/决策字段：自登记不复活 declined
						ne.Key = key
						f.Projects[i] = ne
						return writeEntries(f.Projects)
					}
				}
				// Upsert：刷新 key（遗留条目）与路径（被移动的项目）；状态字段保留。
				ne := e
				ne.Path = ap
				ne.Key = key
				f.Projects[i] = ne
				return writeEntries(f.Projects)
			}
		}
		f.Projects = append(f.Projects, Entry{Path: ap, Key: key})
		return writeEntries(f.Projects)
	})
}

// entryKey computes the forge project key for a cleaned absolute path: git
// common-dir hash when resolvable, else PathKey. Shared by Add and SetStatus so
// the two write paths can never derive different keys for the same project.
//
// entryKey 为已 Clean 的绝对路径计算 forge 项目 key：可解析 git 时取 common-dir
// hash，否则 PathKey。Add 与 SetStatus 共享，两条写路径对同一项目不可能推出不同的 key。
func entryKey(ap string) string {
	if k, kerr := forgedata.Key(ap); kerr == nil {
		return k
	}
	return forgedata.PathKey(ap)
}

// backupCorrupt is the corrupt-registry contract shared by all mutators: rename the
// damaged file aside (recoverable) and warn on stderr — never silently overwrite.
//
// backupCorrupt 是所有变更方共享的注册表损坏处置契约：把损坏文件改名挪开（可恢复）
// 并在 stderr 告警——绝不静默覆盖后丢失全部登记。
func backupCorrupt(p string, uerr error) {
	corrupt := fmt.Sprintf("%s.corrupt-%s", p, time.Now().Format("20060102-150405"))
	if cerr := os.Rename(p, corrupt); cerr != nil {
		fmt.Fprintf(os.Stderr, "warn: 备份损坏的注册表 %s 失败: %v\n", p, cerr)
	} else {
		fmt.Fprintf(os.Stderr, "warn: 注册表 JSON 损坏（%v），已备份到 %s，从空表重建\n", uerr, corrupt)
	}
}

// IsMember reports whether cwd is inside a registered AND managed forge project,
// returning the project root. Declined entries do not confer membership (Project
// Policy Layer P1): every project-scoped hook gates through this call, so on this
// surface a declined project is indistinguishable from an unregistered one.
//
// IsMember 报告 cwd 是否在某个已登记且 managed 的 forge 项目内，并返回项目根。
// declined 条目不赋予成员资格（Project Policy Layer P1）：所有 project-scoped
// hook 都经本调用闸门，declined 项目在此表面上与未登记项目不可区分。匹配规则与
// State 共享同一 lookup 核心，成员资格与状态判定不会漂移：
//   - git cwd：repo 的 forge key（git common-dir hash）等于某个已登记 key——
//     跨 worktree 安全、无需 .forge/。返回的根是 git working tree 根。
//   - 非 git cwd：等于 cwd 或为其祖先的最长已登记路径（边界感知前缀匹配）。
//
// 热路径只读（不写回）；无存储 key 的遗留条目每次调用在内存补算——代价是每个
// 候选条目几次 stat。
func IsMember(cwd string) (root string, ok bool) {
	root, e, ok := lookup(cwd)
	if !ok || e.IsDeclined() {
		return ``, false
	}
	return root, true
}

// DeclineFileName is the committed team-declaration marker at the repo root:
// its existence means the repo is managed by its own harness and forge must
// yield (deny-wins over any managed state). Written by `forge off --commit`
// (the team commits it); removed by `forge on`. Lives in registry (not cli)
// because lookup enforces it on the hot path.
//
// DeclineFileName 是仓库根的 committed 团队声明标记：存在即"本仓归自己 harness
// 管"，forge 让位（deny-wins 压过 managed 状态）。`forge off --commit` 写入（团队
// 提交该文件）；`forge on` 移除。定义在 registry（非 cli）是因为 lookup 热路径
// 执行该判定。
const DeclineFileName = `.forge-decline`

// lookup is the shared matching core of IsMember/State/ListManaged: it resolves cwd
// to the matched registry entry (managed or declined alike) or reports no match.
// The returned root is the matched project's root; callers derive membership from
// the entry's Status. Matching semantics live here exactly once (P1: IsMember 与
// State 的判定单一真相源).
//
// lookup 是 IsMember/State/ListManaged 共享的匹配核心：把 cwd 解析到命中的注册表
// 条目（无论 managed/declined）或报告未命中。返回的 root 是命中项目的根；调用方
// 自行从条目 Status 推导成员资格。匹配语义只在此处存在一份。
func lookup(cwd string) (root string, entry Entry, ok bool) {
	abs, err := filepath.Abs(cwd)
	if err != nil {
		return ``, Entry{}, false
	}
	abs = filepath.Clean(abs)

	// Project Policy Layer P4：仓库根的 .forge-decline 文件是团队级"本仓归自己
	// harness 管"的 committed 声明——deny-wins（个人/团队否决一票终审），压过注册表
	// managed 状态。检查**前置且不依赖注册表存在**：fresh teammate clone 带声明的
	// 仓时 projects.json 尚不存在，若只在注册表非空时生效，deny-wins 在核心场景
	// fail-open（审查 BLOCKER）。合成 declined 条目（真实条目可能不存在——未接管的
	// 项目也可声明）。forge on 移除该文件；forge off --commit 写入。
	gitRoot := forgedata.FindGitRoot(abs)
	if gitRoot != `` {
		if _, derr := os.Stat(filepath.Join(gitRoot, DeclineFileName)); derr == nil {
			return gitRoot, Entry{Path: gitRoot, Status: StatusDeclined, DecisionBy: `committed .forge-decline`}, true
		}
	}

	f, valid := readFile()
	if !valid || len(f.Projects) == 0 {
		return ``, Entry{}, false
	}
	// 可能时解析 symlink，与 PathKey 语义一致：经 symlink 进入的 cwd 必须能
	// 匹配到已登记的物理路径。两种形态都留作匹配候选——有的系统 temp/home 目录
	// 本身就是 symlink（macOS /var → /private/var），条目是按未解析形态登记的，
	// 只按解析后形态匹配会把它们弄丢。
	absForms := pathForms(abs)

	if gitRoot != `` {
		k, kerr := forgedata.Key(abs)
		if kerr != nil {
			return ``, Entry{}, false
		}
		for _, e := range f.Projects {
			if keyOf(e) == k {
				return gitRoot, e, true
			}
		}
		// key 漂移路径回退：项目在非 git 状态下 forge init 存的是 PathKey；`git init`
		// 之后算出的 git-key 永不匹配该陈旧 path-key（keyOf 信任已存的非空 key），于是
		// key 循环落空、项目被「遗忘」——forge 报「not a forge project」，所有 project-scoped
		// 强制 hook 降级放行（AgentOffice bug）。改为按 git working-tree 根匹配条目路径：
		// 登记路径等于 git 根的条目就是同一项目（在它变 git 之前登记的）。刻意只读——
		// IsMember 是被并发 hook 进程触发的热路径，此处写回会竞态（writeEntries 非并发安全）；
		// 陈旧 key 由下次 `forge init`（Add upsert）刷新。
		for _, e := range f.Projects {
			for _, ep := range pathForms(e.Path) {
				for _, grf := range pathForms(gitRoot) {
					if pathKey(ep) == pathKey(grf) {
						return gitRoot, e, true
					}
				}
			}
		}
		return ``, Entry{}, false
	}

	// 非 git：边界感知的最长前缀匹配。两侧都按字面 + symlink 解析双形态比较：
	// 条目可能按未解析形态登记（macOS /var → /private/var 的 temp 目录），而
	// cwd 经 symlink 到达（或反之）——单边单形态会漏配。
	best := ``
	var bestE Entry
	for _, e := range f.Projects {
		// 死条目不赋予成员资格（与 List 精简同一条存活规则）：已删除/移走的
		// 项目目录不得命中——IsMember 只读不精简，少了这道检查，一个路径恰好
		// 是 cwd 祖先的失效条目会把已消失的项目复活。
		if _, err := os.Stat(e.Path); err != nil {
			if os.IsNotExist(err) {
				continue
			}
		}
		// 返回的根恒为条目的字面登记形态（解析形态只用于放宽匹配）——Windows 上
		// EvalSymlinks 会展开 8.3 短名，把长名形式返回会让按登记路径比较的
		// 调用方/测试困惑。
		epLexical := filepath.Clean(e.Path)
		matched := false
		for _, ep := range pathForms(e.Path) {
			for _, af := range absForms {
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
				// 大小写变体前缀匹配：两侧都过 pathKey（CanonicalCase），大小写
				// 不敏感文件系统上变体拼写的 cwd 仍能命中已登记根。分隔符在归一
				// 之后追加——CanonicalCase 会剥掉尾部分隔符，折进 key 里会破坏
				// 边界判定。
				if strings.HasPrefix(pathKey(af), pathKey(ep)+string(filepath.Separator)) {
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
			bestE = e
		}
	}
	if best == `` {
		return ``, Entry{}, false
	}
	return best, bestE, true
}

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

// Prune explicitly prunes the global registry and atomically writes it back.
//
// Prune 显式精简全局注册表：移除项目目录已不存在的死路径 + JSON 内重复条目，原子写回。
// 返回 (pruned, remain)：pruned=本次移除条数（死路径+重复），remain=保留的活跃项目数。
//
// 与 List() 的惰性精简同逻辑，但显式触发并返回计数——List 只在 forge dashboard
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
