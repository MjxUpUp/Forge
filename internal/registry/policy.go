// policy.go — Project Policy Layer P1：per-project 接管状态（managed/declined）。
//
// 设计背景见 docs/design/project-policy-layer.md：装 plugin = 机制存在（全局），
// 接管某项目 = 策略授权（本地），每次 hook 触发 = 仲裁检查（IsMember/State）。
// 本文件是状态模型的单一真相源：Entry.Status 字段、SetStatus 翻转、State 三态查询、
// ListManaged 过滤视图、ErrDeclinedProject sentinel。
package registry

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Takeover status constants. StatusManaged being the empty string is deliberate
// zero-value compatibility: legacy projects.json entries carry no status field, so
// they deserialize as managed — upgrading forge never changes any existing
// project's membership.
//
// 接管状态常量。StatusManaged 为空串是刻意的零值兼容：存量 projects.json 条目没有
// status 字段，反序列化后零值 = managed，升级 forge 不改变任何既有项目的成员资格。
const (
	// StatusManaged means the project is under forge takeover (membership in
	// effect). Empty string = zero value = managed.
	//
	// StatusManaged 表示项目由 forge 接管（成员资格生效）。空串 = 零值 = managed。
	StatusManaged = ``
	// StatusDeclined means the project has opted out: every project-scoped hook
	// passes silently, init/auto-takeover refuses; the only restore path is an
	// explicit forge on (SetStatus).
	//
	// StatusDeclined 表示项目已退出接管：一切 project-scoped hook 静默放行，
	// init/自动接管拒绝；唯一恢复路径是显式 forge on（SetStatus）。
	StatusDeclined = `declined`
	// StatusUnknown means cwd is inside no registered project (one of State's
	// three outcomes; never persisted).
	//
	// StatusUnknown 表示 cwd 不在任何已登记项目内（State 的三态之一，不落盘）。
	StatusUnknown = `unknown`
)

// ErrDeclinedProject is returned by projectroot.Find when the resolved project has
// declined forge takeover. Hook dispatch treats any Find failure as "not a forge
// project" (silent pass), so this error needs no special handling there; CLI
// surfaces it as the user-facing declined message pointing to `forge on`.
//
// ErrDeclinedProject 在项目已退出接管（declined）时由 projectroot.Find 返回。
// hook 分发对任何 Find 失败都按"非 forge 项目"静默放行，故无需特判；CLI 侧把它
// 作为面向用户的 declined 提示展示（指向 forge on）。
var ErrDeclinedProject = errors.New(`forge: 本项目已退出接管（declined）——不会静默重置；恢复请运行 'forge on'`)

// IsDeclined reports whether the entry has declined takeover.
//
// IsDeclined 报告条目是否已退出接管。除字面 "declined" 外一切值（含空）按
// managed 处理——零值兼容存量条目，未知值不剥夺成员资格（fail-safe：读侧宽容）。
func (e Entry) IsDeclined() bool { return e.Status == StatusDeclined }

// SetStatus upserts the takeover status of absPath, recording who decided and when.
//
// SetStatus upsert absPath 的接管状态并记录决策审计字段（by/at）。语义：
//   - 未登记项目可直接 decline（首次接触前退出，forge off 的语义）；
//   - declined→managed 的翻转即 `forge on`——本函数是唯一合法翻转通道；
//     Add（dashboard 自登记、legacyFind 自愈）不复活 declined；
//   - 同 key 不同路径（worktree/移动）遵循 Add 的路径保留规则，只改状态字段。
//
// 注册表损坏时与 Add 同款处置：备份损坏文件后从空表重建（见 loadForWrite）。
func SetStatus(absPath, status, decidedBy string) error {
	if status != StatusManaged && status != StatusDeclined {
		return fmt.Errorf(`registry: invalid status %q (want %q or %q)`, status, StatusManaged, StatusDeclined)
	}
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
		now := time.Now()
		for i, e := range f.Projects {
			samePath := pathKey(filepath.Clean(e.Path)) == pathKey(ap)
			sameKey := key != `` && keyOf(e) == key
			if samePath || sameKey {
				// 只改状态与决策字段；Path/Key 保留既有值（key 惰性补算的遗留条目除外），
				// 不触发 Add 的路径迁移分支——状态翻转不应顺带移动登记路径。
				if e.Key == `` && key != `` {
					f.Projects[i].Key = key
				}
				f.Projects[i].Status = status
				f.Projects[i].DecisionBy = decidedBy
				f.Projects[i].DecisionAt = now
				return writeEntries(f.Projects)
			}
		}
		f.Projects = append(f.Projects, Entry{Path: ap, Key: key, Status: status, DecisionBy: decidedBy, DecisionAt: now})
		return writeEntries(f.Projects)
	})
}

// State resolves the takeover state of cwd: StatusManaged / StatusDeclined (with
// the project root) or StatusUnknown. It shares IsMember's matching core (lookup),
// so membership and state can never drift apart.
//
// State 解析 cwd 的接管三态：managed / declined（附带项目根）/ unknown。与
// IsMember 共享同一匹配核心（lookup），成员资格与状态判定不会漂移。
func State(cwd string) (root, status string) {
	root, e, ok := lookup(cwd)
	if !ok {
		return ``, StatusUnknown
	}
	if e.IsDeclined() {
		return root, StatusDeclined
	}
	return root, StatusManaged
}

// ListManaged returns alive managed project paths (declined entries filtered) —
// the aggregation view for dashboard/task-assignment. Declined projects stay in
// List() (still registered; workspace doctor needs to see their DataDirs).
//
// ListManaged 返回存活的 managed 项目路径（过滤 declined）——dashboard/任务分派
// 的聚合视图。declined 条目仍留在 List()（它们仍是已登记项目，workspace doctor
// 需要看到其 DataDir 做漂移检测）。
func ListManaged() []string {
	var out []string
	for _, p := range List() {
		if _, e, ok := lookup(p); ok && !e.IsDeclined() {
			out = append(out, p)
		}
	}
	return out
}

// loadForWrite reads the registry for mutation, applying Add's corrupt-file
// contract (backup + rebuild from empty) so all mutators share one behavior.
//
// loadForWrite 读取注册表供变更使用，套用 Add 的损坏处置契约（备份 + 从空表
// 重建），让所有变更方（Add/SetStatus）共享同一行为。
func loadForWrite() (File, error) {
	p, err := globalPath()
	if err != nil {
		return File{}, err
	}
	var f File
	if data, rerr := os.ReadFile(p); rerr == nil {
		if uerr := json.Unmarshal(data, &f); uerr != nil {
			backupCorrupt(p, uerr)
			f = File{}
		}
	} else if !os.IsNotExist(rerr) {
		return File{}, rerr
	}
	return f, nil
}
