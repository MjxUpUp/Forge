// Package attribution is the L3 change-attribution service of the multi-task-concurrency design (§6).
//
// Package attribution is the L3 change-attribution service of the multi-task-concurrency
// design (§6): a single session→file ledger plus a Stop-time reconciliation view, so every
// consumer (skill-trigger conditions, HANDOFF现场, review fingerprints, taskChangedFiles)
// stops reading the working tree raw. Invariant I2: the working tree is untrusted input —
// it needs attribution filtering before anyone treats it as "this task's state".
//
// 包 attribution 是 multi-task-concurrency 设计（§6）的 L3 变更归属服务：单一的
// 会话→文件台账 + Stop 时对账视图，让所有消费方（skill 触发条件、HANDOFF 现场、
// review 指纹、taskChangedFiles）不再裸读工作树。不变式 I2：工作树是不可信输入——
// 谁要把它当「本任务状态」，先过归属过滤。
//
// 诚实降级契约：归属是尽力而为。台账条目只覆盖 PostToolUse Write/Edit 事件（加保守
// 推断的 Bash 写目标）；台账解释不了的变更以 ORPHAN（无主）暴露——绝不静默归属、
// 绝不静默丢弃。这与 GitButler、STORM 的停止点相同（"bash-based writes bypass
// mediation"）——没有银弹，只有诚实暴露。
package attribution

import (
	"bufio"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/worktree"
)

// Kind classifies how a ledger entry was observed.
//
// Kind 分类台账条目的观测方式。
type Kind string

const (
	// KindWrite: a Write tool call touched the path (host-normalized tool_input.file_path).
	//
	// KindWrite：Write 工具调用触碰过该路径（宿主归一化的 tool_input.file_path）。
	KindWrite Kind = "write"
	// KindEdit: an Edit/patch tool call touched the path.
	//
	// KindEdit：Edit/patch 工具调用触碰过该路径。
	KindEdit Kind = "edit"
	// KindBashInfer: a Bash command's write target, conservatively parsed from the command text (sed -i / mv / cp / redirections / tee).
	//
	// KindBashInfer：Bash 命令的写目标，从命令文本保守解析（sed -i / mv / cp / 重定向 /
	// tee）。构造上就是低置信——shell 是图灵完备的，解析器只是启发式。
	KindBashInfer Kind = "bash-infer"
)

// Event is one ledger line: session sid touched path at ts, observed via kind.
//
// Event 是一条台账行：会话 sid 于 ts 经 kind 触碰了 path。
type Event struct {
	Ts   time.Time `json:"ts"`
	Sid  string    `json:"sid"`
	Kind Kind      `json:"kind"`
	Path string    `json:"path"` // repo-relative, forward slashes
}

// Confidence reports how trustworthy the entry is: Write/Edit observations are direct host-normalized facts; bash-infer is a heuristic guess.
//
// Confidence 报告条目可信度：Write/Edit 观测是宿主归一化的直接事实；bash-infer 是
// 启发式猜测。
func (e Event) Confidence() string {
	if e.Kind == KindBashInfer {
		return "low"
	}
	return "high"
}

// Enabled reports whether the attribution layer is active.
//
// Enabled 报告归属层是否启用。FORGE_ATTRIBUTION=0 是设计的逃生舱
// （multi-task-concurrency §11）：所有消费方降级回 L3 之前的全树行为——一个开关，
// 零中间态。
func Enabled() bool { return os.Getenv("FORGE_ATTRIBUTION") != "0" }

var ledgers sync.Map // string(ledgerPath) -> *sync.Mutex，跨 goroutine 的每台账锁

func ledgerPath(root string) string {
	return filepath.Join(forgedata.DataDirFor(root), "attribution", worktree.ID(root)+".jsonl")
}

func ledgerMu(root string) *sync.Mutex {
	m, _ := ledgers.LoadOrStore(ledgerPath(root), &sync.Mutex{})
	return m.(*sync.Mutex)
}

// Record appends events to the workspace's attribution ledger.
//
// Record 追加事件到该 workspace 的归属台账。设计上静默失败：归属是可观测性输入，
// 追加失败绝不能打断 hook（路径只会在对账时降级为无主——诚实方向）。
func Record(root string, events ...Event) {
	if root == "" || len(events) == 0 {
		return
	}
	m := ledgerMu(root)
	m.Lock()
	defer m.Unlock()
	path := ledgerPath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	for _, e := range events {
		if e.Path == "" || e.Sid == "" {
			continue // 身份不全的条目没有归属价值
		}
		e.Path = filepath.ToSlash(filepath.Clean(e.Path))
		line, err := json.Marshal(e)
		if err != nil {
			continue
		}
		_, _ = w.Write(append(line, '\n'))
	}
	_ = w.Flush()
}

// ledgerTTL 限定条目有效期（review HIGH）：不设的话，僵尸任务会话的数月前事件
// 会在本任务经由台账看不见的通道改同一路径时赢过最后写入者——把当前工作误归类
// 为外来并藏出 review 指纹。7d 与项目的会话 marker TTL 族对齐（marker 7d 清扫；
// 孤儿会话指针 7d 守卫）。读时过滤：便宜，v1 无需重写 jsonl。
const ledgerTTL = 7 * 24 * time.Hour

// loadLedger 读 workspace 台账并过滤过期条目（尽力而为；文件缺失 = 空）。
func loadLedger(root string) []Event {
	data, err := os.ReadFile(ledgerPath(root))
	if err != nil {
		return nil
	}
	cutoff := time.Now().Add(-ledgerTTL)
	var events []Event
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var e Event
		if json.Unmarshal([]byte(line), &e) == nil && e.Path != "" && e.Sid != "" {
			if e.Ts.Before(cutoff) {
				continue // 过期：陈旧归因比漏归属更糟（漏 = 无主 = 可见）
			}
			events = append(events, e)
		}
	}
	return events
}

// ChangedFiles returns the repo-wide uncommitted change set (relative, forward slashes) from `git status --porcelain` — both staged and unstaged, tracked and untracked.
//
// ChangedFiles 返回全仓未提交变更集（相对路径、正斜杠），来自 git status
// --porcelain——暂存与未暂存、tracked 与 untracked 全含；rename 取目标路径。
// 非 git / git 失败 = 空集 + error。
// PorcelainLines runs `git status --porcelain` and returns the raw status lines
// (quotepath=off so non-ASCII paths stay UTF-8).
//
// PorcelainLines 跑 `git status --porcelain` 返回原始状态行——porcelain 调用的
// 单一入口（2026-09 普查 P3-3：曾两处各自起 porcelain 进程——attribution 与
// cli/task_continuity；quotepath=off 让非 ASCII
// 路径保持原生 UTF-8 而非 C 转义八进制串，永远匹配得上台账里的 Unicode 路径）。
func PorcelainLines(root string) ([]string, error) {
	out, err := exec.Command("git", "-c", "core.quotepath=off", "-C", root, "status", "--porcelain").Output()
	if err != nil {
		return nil, err
	}
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		return nil, nil
	}
	return strings.Split(trimmed, "\n"), nil
}

// ChangedFiles returns the working-tree changed file paths parsed from
// PorcelainLines (rename target taken, quotes stripped).
//
// ChangedFiles 返回工作区变更文件路径——自 PorcelainLines 派生（rename 取目标、剥引号）。
func ChangedFiles(root string) ([]string, error) {
	lines, err := PorcelainLines(root)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, line := range lines {
		if len(line) < 3 {
			continue
		}
		p := line[3:]
		if idx := strings.Index(p, " -> "); idx >= 0 {
			p = p[idx+4:] // rename 取目标路径
		}
		p = strings.Trim(p, `"`)
		if p != "" {
			files = append(files, p)
		}
	}
	return files, nil
}

// View is the Stop-time reconciliation result: the working tree's changed set, split by last-writer attribution.
//
// View 是 Stop 时对账结果：工作树变更集按最后写入者归属拆分。已提交变更不在此——
// 那属于 committed 区间与既有跨任务归因（taskpipeline/taskattribution.go）。
type View struct {
	Changed   []string
	BySession map[string][]string // sid → attributed changed paths (last-writer-wins per path)
	Orphans   []string            // changed paths the ledger cannot explain
}

// AttributionRate is attributed / (attributed + orphans); 1 when nothing changed.
//
// AttributionRate = attributed / (attributed + orphans)；无变更时为 1。T2 的覆盖率
// 度量——台账实际解释现实的频率。
func (v *View) AttributionRate() float64 {
	total := len(v.Orphans)
	for _, fs := range v.BySession {
		total += len(fs)
	}
	if total == 0 {
		return 1
	}
	return float64(total-len(v.Orphans)) / float64(total)
}

// Reconcile builds the View: git status's changed set joined against the ledger, per-path last-writer-wins by timestamp.
//
// Reconcile 构建视图：git status 的变更集与台账按时间戳做每路径最后写入者判定。
func Reconcile(root string) *View {
	v := &View{BySession: map[string][]string{}}
	changed, err := ChangedFiles(root)
	if err != nil {
		return v // 非 git / git 失败：空视图（消费方各自降级）
	}
	v.Changed = changed
	changedSet := make(map[string]bool, len(changed))
	for _, p := range changed {
		changedSet[filepath.ToSlash(filepath.Clean(p))] = true
	}
	last := map[string]Event{} // path → latest event（时间戳最后写入者胜）
	for _, e := range loadLedger(root) {
		if !changedSet[e.Path] {
			continue // 台账里的历史路径不在当前变更集——对账只解释现在
		}
		if prev, ok := last[e.Path]; ok && !e.Ts.After(prev.Ts) {
			continue
		}
		last[e.Path] = e
	}
	for path := range changedSet {
		e, ok := last[path]
		if !ok {
			v.Orphans = append(v.Orphans, path)
			continue
		}
		v.BySession[e.Sid] = append(v.BySession[e.Sid], path)
	}
	sort.Strings(v.Orphans)
	for sid := range v.BySession {
		sort.Strings(v.BySession[sid])
	}
	return v
}

// SessionTouched returns the set of repo-relative paths this session has ever touched (ledger union, no reconciliation against current status — the fast predicate for trigger conditions like source_changed_uncommitted).
//
// SessionTouched 返回本会话触碰过的路径全集（台账并集，不对账当前状态——
// source_changed_uncommitted 这类触发条件的快速谓词）。
func SessionTouched(root, sid string) map[string]bool {
	set := map[string]bool{}
	if sid == "" {
		return set
	}
	for _, e := range loadLedger(root) {
		if e.Sid == sid {
			set[e.Path] = true
		}
	}
	return set
}
