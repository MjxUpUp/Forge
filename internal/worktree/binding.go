package worktree

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/nodeid"
	"github.com/MjxUpUp/Forge/internal/util"
)

// Binding is the durable cwd→task anchor of the Task/Session/Workspace triad
// (multi-task-concurrency design §4, L1). Unlike session pointers (which die with the
// session id) the binding is keyed by the workspace PATH: a new window in the same
// directory/worktree resolves the same task — "退出重进" works because the directory is
// stable, not because any session survived. Bindings are machine-local by construction
// (wtid is a path hash) and carry no freshness gate: existence IS the anchor.
//
// Binding 是 Task/Session/Workspace 三元组中持久的 cwd→任务锚（multi-task-concurrency
// 设计 §4，L1）。与会话指针（随 session id 一起消亡）不同，绑定按 workspace 路径
// 键控：同一目录/worktree 里的新窗口解析到同一任务——「退出重进」之所以成立，是
// 因为目录稳定，不是因为任何会话还活着。绑定按构造即机器本地（wtid 是路径哈希），
// 不带新鲜度门：存在即锚。
type Binding struct {
	ID         string    `json:"id"`         // wtid
	Path       string    `json:"path"`       // EvalSymlinks 后的绝对路径
	Branch     string    `json:"branch"`     // 绑定时分支（展示用；解析以 TaskRef 为准）
	TaskRef    string    `json:"task_ref"`   // 绑定的任务
	CreatedBy  string    `json:"created_by"` // 建绑会话
	Node       string    `json:"node"`       // 机器身份（跨机诊断；绑定本身本机性）
	CreatedAt  time.Time `json:"created_at"`
	LastSeenAt time.Time `json:"last_seen_at"` // heartbeat（hook dispatcher 顺带刷新；只影响展示不影响解析）
}

func bindingDir(root string) string {
	return filepath.Join(forgedata.DataDirFor(root), "workspaces")
}

func bindingPath(root string) string {
	return filepath.Join(bindingDir(root), ID(root)+".json")
}

// Load reads the workspace's binding; nil when absent/corrupt (degrade = no anchor, resolution falls through — the safe direction).
//
// Load 读该 workspace 的绑定；缺失/损坏返回 nil（降级 = 无锚，解析穿落——安全方向）。
func Load(root string) *Binding {
	data, err := os.ReadFile(bindingPath(root))
	if err != nil {
		return nil
	}
	var b Binding
	if err := json.Unmarshal(data, &b); err != nil {
		return nil
	}
	return &b
}

// BindTask upserts the cwd→taskRef binding (multi-task-concurrency §4).
//
// BindTask upsert cwd→taskRef 绑定（multi-task-concurrency §4）。对已绑定 workspace
// 重新绑定即改指（在已绑定目录里 task start = 显式切换，最后显式动作胜），但保留
// 原始 CreatedBy/CreatedAt 以便追溯。
func BindTask(root, taskRef, branch, sessionID string) error {
	b := Load(root)
	now := time.Now()
	if b == nil {
		abs, _ := filepath.Abs(root)
		b = &Binding{
			ID:        ID(root),
			Path:      abs,
			CreatedBy: sessionID,
			CreatedAt: now,
		}
	}
	b.TaskRef = taskRef
	b.Branch = branch
	b.LastSeenAt = now
	if b.Node == "" {
		if id, err := nodeid.Load(); err == nil && id != nil {
			b.Node = id.NodeID
		}
	}
	if err := os.MkdirAll(bindingDir(root), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return err
	}
	return util.AtomicWrite(bindingPath(root), data, 0o644)
}

// Clear removes the workspace binding when it still points at taskRef (a stale clear for another task must not unbind a newer switch — compare before delete).
//
// Clear 仅当绑定仍指向 taskRef 时移除（为别的任务做的过期 clear 不得解掉更新的切换
// ——先比对再删）。
func Clear(root, taskRef string) error {
	if b := Load(root); b == nil || b.TaskRef != taskRef {
		return nil
	}
	err := os.Remove(bindingPath(root))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// ClearByID removes the binding file by wtid, resolved inside ANY live root of the same
// project（dogfood 发现 #4，2026-08-27）：finish 先删 worktree 目录再解绑时，原路径
// 已消失——DataDirFor(消失路径) 的身份推导漂移、Clear 静默 no-op，绑定残留。按
// ID 直删 + TaskRef 比对，不依赖原路径存活；幂等（文件缺失即成功）。
func ClearByID(root, id, taskRef string) error {
	if id == "" {
		return nil
	}
	p := filepath.Join(bindingDir(root), id+".json")
	data, err := os.ReadFile(p)
	if err != nil {
		return nil // 无此绑定：幂等
	}
	var b Binding
	if json.Unmarshal(data, &b) != nil || b.TaskRef != taskRef {
		return nil // 不指向该任务：同 Clear 的比对语义
	}
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// ClearAllForTask removes EVERY binding file whose TaskRef matches — the task-scoped
// sweep（dogfood #4 深挖，2026-08-27）：abort 一个 --worktree 任务时，cwd 键控的
// Clear 只能清调用方所在目录的绑定，任务的 worktree 绑定无人清；任务被 abort/
// 删除后，任何指向它的绑定都是死锚。幂等，静默容忍读取失败。
func ClearAllForTask(root, taskRef string) error {
	dir := bindingDir(root)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil // 无 workspaces 目录：幂等
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		p := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var b Binding
		if json.Unmarshal(data, &b) != nil || b.TaskRef != taskRef {
			continue
		}
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// Touch refreshes the heartbeat (display-only; resolution never gates on it).
//
// Touch 刷新心跳（仅展示用；解析从不对它设门）。尽力而为静默——分发器每个 hook
// 顺带调用。
func Touch(root string) {
	orig, rerr := os.ReadFile(bindingPath(root))
	if rerr != nil {
		return // no binding on disk → nothing to touch (also: never RESURRECT a cleared one)
	}
	b := Load(root)
	if b == nil {
		return
	}
	b.LastSeenAt = time.Now()
	if data, err := json.MarshalIndent(b, "", "  "); err == nil {
		// 按内容 CAS：文件自读取后已变即他人胜出——丢弃本次 LastSeenAt 无害；
		// 重写会回滚并发的改绑（BindTask 是显式动作，Touch 只是 advisory）。
		if cur, err := os.ReadFile(bindingPath(root)); err == nil && string(cur) == string(orig) {
			_ = util.AtomicWrite(bindingPath(root), data, 0o644)
		}
	}
}
