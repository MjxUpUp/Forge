package worktree

import (
	"encoding/json"
	"os"
	"path/filepath"
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
	ID         string    `json:"id"`          // wtid
	Path       string    `json:"path"`        // EvalSymlinks 后的绝对路径
	Branch     string    `json:"branch"`      // 绑定时分支（展示用；解析以 TaskRef 为准）
	TaskRef    string    `json:"task_ref"`    // 绑定的任务
	CreatedBy  string    `json:"created_by"`  // 建绑会话
	Node       string    `json:"node"`        // 机器身份（跨机诊断；绑定本身本机性）
	CreatedAt  time.Time `json:"created_at"`
	LastSeenAt time.Time `json:"last_seen_at"` // heartbeat（hook dispatcher 顺带刷新；只影响展示不影响解析）
}

func bindingDir(root string) string {
	return filepath.Join(forgedata.DataDirFor(root), "workspaces")
}

func bindingPath(root string) string {
	return filepath.Join(bindingDir(root), ID(root)+".json")
}

// Load reads the workspace's binding; nil when absent/corrupt (degrade = no anchor,
// resolution falls through — the safe direction).
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

// BindTask upserts the cwd→taskRef binding (multi-task-concurrency §4). Re-binding an
// already-bound workspace re-points it (task start in a bound directory = an explicit
// switch, last explicit action wins) while preserving original CreatedBy/CreatedAt for
// traceability.
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

// Clear removes the workspace binding when it still points at taskRef (a stale clear for
// another task must not unbind a newer switch — compare before delete).
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

// Touch refreshes the heartbeat (display-only; resolution never gates on it).
// Best-effort silent — the dispatcher calls it on every hook.
//
// Touch 刷新心跳（仅展示用；解析从不对它设门）。尽力而为静默——分发器每个 hook
// 顺带调用。
func Touch(root string) {
	b := Load(root)
	if b == nil {
		return
	}
	b.LastSeenAt = time.Now()
	if data, err := json.MarshalIndent(b, "", "  "); err == nil {
		_ = util.AtomicWrite(bindingPath(root), data, 0o644)
	}
}
