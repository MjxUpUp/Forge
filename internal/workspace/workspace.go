// Package workspace maintains the user-level multi-repo workspace manifest at
// ~/.forge/workspaces.json (a sibling of registry's projects.json).
//
// A workspace is a LOGICAL grouping of forge project keys (repos that ship
// together, e.g. an app + its backend + its infra repo). Members reference
// project KEYS, not paths — paths drift (moves, worktrees, case variants), keys
// do not; the stored Path is a display cache only, refreshed on add and flagged
// by Doctor when it diverges from the registry's current path. The store sits
// ON TOP of the registry (it never feeds projectroot.Find/IsMember hot paths).
// One key may belong to multiple workspaces — a global tool cannot assume
// exclusivity; overlap is surfaced by Doctor as an advisory, never hard-refused.
//
// File handling replicates the registry.go contract: writes go through
// util.AtomicWrite (temp + fsync + rename, no half-written JSON); a corrupt
// file is backed aside to workspaces.json.corrupt-<ts> and rebuilt from empty
// on the write path (LoadForWrite), while the read path (Load) reports the
// error so read-only callers (the task-verify gate) can fail open.
//
// Package workspace 维护用户级多仓 workspace 清单 ~/.forge/workspaces.json
// （与 registry 的 projects.json 平级）。
//
// workspace 是一组 forge 项目 key 的逻辑分组（共同交付的若干仓库，如 app +
// 后端 + infra 仓）。成员引用项目 KEY 而非路径——路径会漂移（移动、
// worktree、大小写变体），key 不会；存的 Path 仅是展示缓存，add 时刷新，
// 与 registry 现路径分叉时由 Doctor 标出。本 store 位于 registry 之上的
// 聚合层（绝不喂 projectroot.Find/IsMember 热路径）。一个 key 允许属于多个
// workspace——全局工具无法假设互斥；重叠由 Doctor 检出为 advisory，不做
// 硬性拒绝。
//
// 文件处理复刻 registry.go 契约：写走 util.AtomicWrite（临时文件 + fsync +
// rename，不留写一半的 JSON）；损坏文件在写路径（LoadForWrite）备份为
// workspaces.json.corrupt-<ts> 后从空重建，读路径（Load）则返回错误，
// 让只读调用方（task-verify 门禁）fail-open。
package workspace

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/util"
)

// RepoRef is one workspace member. Key is the forge project key (the identity);
// Path is a DISPLAY CACHE of where the repo lived at add time — resolution
// always goes through Key (registry / forgedata.RootDir), never through Path.
//
// RepoRef 是一个 workspace 成员。Key 是 forge 项目 key（身份）；Path 是 add
// 时刻仓库位置的展示缓存——解析永远走 Key（registry / forgedata.RootDir），
// 绝不走 Path。
type RepoRef struct {
	Key  string `json:"key"`
	Path string `json:"path,omitempty"` // 展示缓存（add 时刻路径；漂移由 doctor 检出）
}

// Workspace is a named group of repos that ship together.
//
// Workspace 是一组共同交付的仓库的具名分组。
type Workspace struct {
	Name      string    `json:"name"`
	Repos     []RepoRef `json:"repos"`
	CreatedAt time.Time `json:"created_at"`
}

// File is the on-disk structure of ~/.forge/workspaces.json.
//
// File 是 ~/.forge/workspaces.json 的磁盘结构。
type File struct {
	Workspaces []Workspace `json:"workspaces"`
}

// globalPath returns the manifest path. Same rule as registry's projects.json:
// the global home goes through forgedata.GlobalHome() (FORGE_DATA_HOME first,
// otherwise ~/.forge) so tests and power users isolate the whole store family
// with one env.
//
// globalPath 返回清单路径。与 registry 的 projects.json 同规则：全局 home 走
// forgedata.GlobalHome()（FORGE_DATA_HOME 优先，否则 ~/.forge），测试与高级
// 用户用一个 env 隔离整族 store。
func globalPath() (string, error) {
	home, err := forgedata.GlobalHome()
	if err != nil {
		return ``, err
	}
	return filepath.Join(home, `workspaces.json`), nil
}

// Load reads the manifest (the READ path). A missing file is an empty File,
// not an error (no workspaces yet); a corrupt JSON is an explicit error so
// read-only callers (the task-verify cross-repo-impact gate) can fail open
// with an INFRA advisory instead of silently treating the store as empty.
//
// Load 读清单（读路径）。文件缺失 = 空 File，非错误（还没有任何 workspace）；
// JSON 损坏返回显式错误，让只读调用方（task-verify 的 cross-repo-impact
// 门禁）能以 INFRA advisory fail-open，而非把 store 静默当空。
func Load() (*File, error) {
	var f File
	p, err := globalPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return &f, nil
		}
		return nil, err
	}
	if uerr := json.Unmarshal(data, &f); uerr != nil {
		return nil, fmt.Errorf("workspace: corrupt workspaces.json: %w", uerr)
	}
	return &f, nil
}

// LoadForWrite reads the manifest for a mutation (create/add/remove). A
// corrupt file is backed aside to <path>.corrupt-<ts> with a stderr warning
// and rebuilt from empty — the registry.Add pattern: never let one unparseable
// file brick every future workspace command, and never silently drop the
// evidence either.
//
// LoadForWrite 为变更（create/add/remove）读清单。损坏文件先备份为
// <path>.corrupt-<ts>（stderr 告警）再从空重建——registry.Add 同款模式：
// 绝不让一个不可解析的文件卡死后续所有 workspace 命令，也绝不静默丢弃现场。
func LoadForWrite() (*File, error) {
	p, err := globalPath()
	if err != nil {
		return nil, err
	}
	var f File
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return &f, nil
		}
		return nil, err
	}
	if uerr := json.Unmarshal(data, &f); uerr != nil {
		corrupt := fmt.Sprintf("%s.corrupt-%s", p, time.Now().Format("20060102-150405"))
		if rerr := os.Rename(p, corrupt); rerr != nil {
			fmt.Fprintf(os.Stderr, "warn: 备份损坏的 workspace 清单 %s 失败: %v\n", p, rerr)
		} else {
			fmt.Fprintf(os.Stderr, "warn: workspaces.json 损坏（%v），已备份到 %s，从空表重建\n", uerr, corrupt)
		}
		return &File{}, nil
	}
	return &f, nil
}

// Save atomically writes the manifest via util.AtomicWrite (temp file + fsync +
// rename) — same crash-safety contract as registry's writeEntries: a reader
// sees the complete old or new file, never a truncated mix. read-modify-write
// is not concurrency-safe (two simultaneous writers may lose one update), but
// local-tool concurrency is rare and a lost entry can be re-added; corrupt JSON
// is what must be prevented.
//
// Save 原子写清单，走 util.AtomicWrite（临时文件 + fsync + rename）——与
// registry 的 writeEntries 同一崩溃安全契约：读者只见完整旧版或完整新版，
// 绝不见截断混合体。read-modify-write 非并发安全（两进程同时写可能丢一条
// 更新），但本地工具并发罕见、丢了可重加；损坏 JSON 才是必防的。
func (f *File) Save() error {
	p, err := globalPath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(f, ``, `  `)
	if err != nil {
		return err
	}
	return util.AtomicWrite(p, append(data, '\n'), 0644)
}

// validateName rejects names that are empty or carry path separators — the
// name is a logical label printed in output and (later) usable in refs, so
// keep it a single clean token.
//
// validateName 拒绝空名或带路径分隔符的名——name 是输出里打印的逻辑标签
// （后续可能进 ref），保持为单个干净 token。
func validateName(name string) error {
	if strings.TrimSpace(name) == `` {
		return fmt.Errorf(`workspace: name must not be empty`)
	}
	if strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("workspace: name %q must not contain path separators", name)
	}
	return nil
}

// Find returns the workspace named name, or nil.
//
// Find 返回名为 name 的 workspace，没有则 nil。
func (f *File) Find(name string) *Workspace {
	for i := range f.Workspaces {
		if f.Workspaces[i].Name == name {
			return &f.Workspaces[i]
		}
	}
	return nil
}

// WorkspacesFor returns every workspace that contains the given project key.
// One key may legitimately belong to several workspaces (a shared library repo
// serving two products); Doctor flags the overlap as advisory, reads must not.
//
// WorkspacesFor 返回包含给定项目 key 的全部 workspace。一个 key 合法地属于
// 多个 workspace（服务两个产品的共享库仓）；重叠由 Doctor 标 advisory，
// 读路径不管。
func (f *File) WorkspacesFor(key string) []Workspace {
	var out []Workspace
	for _, w := range f.Workspaces {
		for _, r := range w.Repos {
			if r.Key == key {
				out = append(out, w)
				break
			}
		}
	}
	return out
}

// Create adds a new empty workspace. Duplicate names are refused — the name is
// the handle every command takes.
//
// Create 新增一个空 workspace。重名拒绝——name 是所有命令的句柄。
func (f *File) Create(name string) error {
	if err := validateName(name); err != nil {
		return err
	}
	if f.Find(name) != nil {
		return fmt.Errorf("workspace %q already exists", name)
	}
	f.Workspaces = append(f.Workspaces, Workspace{Name: name, CreatedAt: time.Now()})
	return nil
}

// AddRepo adds repo to the named workspace. Upsert semantics within the
// workspace: an existing member with the same key gets its display-cache Path
// refreshed (the repo may have moved); adding the same key twice is therefore
// idempotent and never duplicates.
//
// AddRepo 把 repo 加进指定 workspace。workspace 内 upsert 语义：同 key 的
// 既有成员刷新展示缓存 Path（仓库可能搬过家）——故重复 add 同 key 幂等，
// 绝不重复。
func (f *File) AddRepo(name string, repo RepoRef) error {
	w := f.Find(name)
	if w == nil {
		return fmt.Errorf("workspace %q not found (run `forge workspace create %s` first)", name, name)
	}
	if repo.Key == `` {
		return fmt.Errorf(`workspace: repo key must not be empty`)
	}
	for i, r := range w.Repos {
		if r.Key == repo.Key {
			w.Repos[i].Path = repo.Path
			return nil
		}
	}
	w.Repos = append(w.Repos, repo)
	return nil
}

// RemoveRepo drops key from the named workspace. Returns false (no error) when
// the workspace exists but the key is not a member — the caller prints the
// "not a member" hint; a missing workspace is an error.
//
// RemoveRepo 从指定 workspace 移除 key。workspace 存在但 key 不是成员时返回
// false（不报错）——由调用方打印「非成员」提示；workspace 不存在是错误。
func (f *File) RemoveRepo(name, key string) (bool, error) {
	w := f.Find(name)
	if w == nil {
		return false, fmt.Errorf("workspace %q not found", name)
	}
	for i, r := range w.Repos {
		if r.Key == key {
			w.Repos = append(w.Repos[:i], w.Repos[i+1:]...)
			return true, nil
		}
	}
	return false, nil
}
