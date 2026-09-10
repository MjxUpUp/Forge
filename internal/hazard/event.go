package hazard

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/util"
)

// 事件日志：记录 hazard-guard 的拦截/放行事件流，补全"逃生码（指纹）审计记录"。
//
// 背景：Confirmation（<fp>.json）只记 confirm 登记的最终态（5min 窗口内有效）；
// block 拦截和 release 放行事件原本只进 hook stdout/checklog，无结构化落盘——做
// 误伤审计时只能扒 checklog（2026-06 hazards 审计 19 条 FAIL 的痛点：被拦但未
// confirm 的命令无独立记录）。events.jsonl 把完整事件流落盘，可追溯"何时拦了
// 什么、是否被 confirm 放行、是否被判为数据上下文放行"。设计参照 checklog/store.go
// （mutex + O_APPEND 追加 + scanner 读）。

// 事件类型。
const (
	// EventBlock: hazard-guard blocks a high-risk command (unconfirmed, awaiting
	// HITL).
	//
	// EventBlock：hazard-guard 拦截高危命令（未确认，等待 HITL）。
	EventBlock = "block"
	// EventRelease: released because forge hazard confirm registered it (within the
	// 5min window).
	//
	// EventRelease：因 forge hazard confirm 登记（5min 窗口内）而放行。
	EventRelease = "release"
	// EventData: context classification judged the dangerous string is only inside
	// quotes (data, not execution) and released it, e.g. grep `rm -rf` / git commit
	// -m `fix rm -rf bug`.
	//
	// EventData：context classification 判定危险串仅在引号内（数据，非执行）而放行，
	// 如 grep "rm -rf" / git commit -m "fix rm -rf bug"。
	EventData = "data"
	// EventConfirm: a confirmation marker was registered (forge hazard confirm).
	//
	// EventConfirm：确认标记被登记（forge hazard confirm）。由 writeConfirmation 内部追加
	// ——而非 hook 脚本——伪造路径（手写标记文件）至少造不出这条事件，每次合法 confirm 都可审计。
	EventConfirm = "confirm"
)

var eventMu sync.Mutex

// Event records a single hazard-guard event, appended to
// DataDir/hazards/events.jsonl.
//
// Event 记录一次 hazard-guard 事件，追加写 DataDir/hazards/events.jsonl。
type Event struct {
	Ts          time.Time `json:"ts"`
	Type        string    `json:"type"`        // EventBlock/EventRelease/EventData/EventConfirm
	Fingerprint string    `json:"fingerprint"` // Fingerprint(command)；算不出时为空
	Command     string    `json:"command"`     // 截断的命令串（审计用，maxCommandStore）
	// SessionID is the host session that produced the event (from FORGE_SESSION_ID in the hook environment); empty for terminal-run commands and rows written before this field existed.
	//
	// SessionID 是产生事件的宿主会话（hook 环境的 FORGE_SESSION_ID）；人类终端直跑与本字段
	// 引入前的旧行为空。参与双投递去重键（与 checklog 侧 blockRecordMarker 的 session 维度
	// 对齐）；任一侧为空时退化为只比 (Type, Fingerprint)。
	SessionID string `json:"session_id,omitempty"`
}

// EventDedupWindow is the window within which a same-session, same-type, same-fingerprint event is treated as a host double-delivery of one logical hook call and not appended again.
//
// EventDedupWindow 内同会话、同 type、同指纹的事件视为宿主对同一逻辑 hook 调用的双投递，
// 不再追加。乙机实录 52 条 block 里 24 条是短窗同指纹重复（kimi PreToolUse 98ms 双发的
// hazard 版），safe-halt 按原始事件计数被直接翻倍——3 次逻辑拦截记成 6 次越过阈值 3。
// 窗口与 hookdispatch.blockRecordDedupWindow 同量级（3s）；空指纹（算不出）永不去重；
// 会话维度任一侧为空（旧行/终端直跑）退化为不比会话——两个并行会话 3s 内各拦一次同命令
// 各记一条。docs/design/harness-fixes-a-g-2026-09.md F.3。
const EventDedupWindow = 3 * time.Second

// AppendEvent appends an event to <DataDir>/hazards/events.jsonl.
//
// AppendEvent 追加一条事件到 <DataDir>/hazards/events.jsonl。Ts 由本函数盖时间戳，
// Command 截断到 maxCommandStore（与 Confirmation 一致），避免超长命令撑大日志。
// 线程安全：进程内 eventMu 串行化。hook 是多进程调用 `forge hazard log` 子命令，跨进程
// 靠 O_APPEND——POSIX 下单行 Write 原子；Windows 无 PIPE_BUF 保证，但 hook 触发低频、
// 交错风险可接受（审计日志容忍偶发坏行，LoadEvents 跳过损坏行）。
//
// 同 type 同指纹且距末行不足 EventDedupWindow 的事件按宿主双投递去重（不落盘、返回
// nil）——只比对文件末行：双投递总是紧邻的，全文件扫描既贵又会把真实的隔时重试误吞。
//
// Failure should not affect the hook main flow — callers (hook scripts) tolerate it with `|| true`; audit failure does not block.
// 失败不应影响 hook 主流程——调用方（hook 脚本）用 `|| true` 容错，审计失败不 block。
func AppendEvent(p *forgedata.Project, e Event) error {
	eventMu.Lock()
	defer eventMu.Unlock()

	e.Ts = time.Now()
	e.Command = util.TruncateRunes(e.Command, maxCommandStore)

	path := p.HazardsEventsPath()
	if e.Fingerprint != "" {
		if last, ok := lastEvent(path); ok && IsDoubleDelivery(last, e) {
			return nil
		}
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}
	_, err = f.Write(append(data, '\n'))
	return err
}

// IsDoubleDelivery reports whether next is a host double-delivery of last: same type and fingerprint, same session when both carry one, within EventDedupWindow (clock-skew negative gaps never dedupe). Exported so harness-audit rebuilds historical double-deliveries with the writer's exact rule.
//
// IsDoubleDelivery 判断 next 是否 last 的宿主双投递：同 type 同指纹、双方都带会话时同会话、
// 间隔在 EventDedupWindow 内（时钟回拨的负间隔不去重，fail-open）。导出给 harness-audit
// 按写侧同一规则重建历史双投递（F2 基线）——审计侧不得手抄第二份规则。
func IsDoubleDelivery(last, next Event) bool {
	if last.Type != next.Type || last.Fingerprint != next.Fingerprint {
		return false
	}
	if last.SessionID != "" && next.SessionID != "" && last.SessionID != next.SessionID {
		return false
	}
	d := next.Ts.Sub(last.Ts)
	return d >= 0 && d < EventDedupWindow
}

// lastEvent reads the last valid event line of events.jsonl (dedupe input); ok=false when the file is missing/empty or the tail line is corrupt — dedupe is best-effort, an unreadable tail means append as usual (over-record rather than lose an audit row). Only the trailing 4KB is read.
//
// lastEvent 读取 events.jsonl 的末条有效事件（双投递去重用）。文件缺失/空/末行损坏
// 返回 ok=false——去重尽力而为，读不到就照常追加（宁多记不丢审计行）。只读文件尾
// 4KB：单条事件远小于此，避免每次追加全量读盘。
func lastEvent(path string) (Event, bool) {
	f, err := os.Open(path)
	if err != nil {
		return Event{}, false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.Size() == 0 {
		return Event{}, false
	}
	const tail = 4096
	off := info.Size() - tail
	if off < 0 {
		off = 0
	}
	buf := make([]byte, info.Size()-off)
	if _, err := f.ReadAt(buf, off); err != nil && err != io.EOF {
		return Event{}, false
	}
	lines := strings.Split(strings.TrimRight(string(buf), "\n"), "\n")
	last := strings.TrimSpace(lines[len(lines)-1])
	if last == "" {
		return Event{}, false
	}
	var ev Event
	if err := json.Unmarshal([]byte(last), &ev); err != nil {
		return Event{}, false
	}
	return ev, true
}

// LoadEvents reads all events (in-file time order).
//
// LoadEvents 读取全部事件（文件内时间序）。文件不存在或损坏行返回已解析的部分。
// 损坏行跳过（不报错）——审计日志容忍个别行损坏，不因一行坏数据丢弃全量。
func LoadEvents(p *forgedata.Project) ([]Event, error) {
	f, err := os.Open(p.HazardsEventsPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var events []Event
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var e Event
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			// skip corrupted line
			continue // 跳过损坏行
		}
		events = append(events, e)
	}
	return events, scanner.Err()
}

// CountSince counts events of a given type after `since`. Provides `forge hazard status` with 「past 24h
// block/release counts」 so users can see at a glance the workload and false-positive scale of hazard-guard.
//
// CountSince 统计 since 之后某类型事件数。给 `forge hazard status` 展示"近 24h
// 拦截/放行次数"，让用户一眼看到 hazard-guard 的工作量与误伤规模。
func CountSince(p *forgedata.Project, eventType string, since time.Time) (int, error) {
	events, err := LoadEvents(p)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, e := range events {
		if e.Type == eventType && e.Ts.After(since) {
			n++
		}
	}
	return n, nil
}

// ConfirmLastBlock registers a confirmation for the NEWEST block event in the
// event log and returns its fingerprint and command.
//
// ConfirmLastBlock 为事件日志中最新一条 block 事件登记确认，返回其指纹与命令。
// 这是免复制的 HITL 路径（`forge hazard confirm --last`）：agent 确认"刚被拦的那条
// 命令"，无需转写 64 字符 hex 指纹、也无需重新引用命令串——两种转写形态都已被证实
// 是失真源（2026-07 AgentWorld：三次手抄指纹两次损坏；2026-08-24 Forge 会话：裸命令
// confirm 与 hook 对含管道后缀完整命令行的指纹失配）。事件日志由 hook 在拦截时自己
// 写入，其指纹天然权威。
//
// 语义：最新的带非空 Fingerprint 的 EventBlock 胜出（无指纹的 block 事件本就不可确
// 认）；对已确认过的 block 重复 confirm 只是续期（与 Confirm 同）。不检查该 block 是
// 否已被放行——续期无害，且保持流程单步。
//
// 已知竞态（接受并披露）：events.jsonl 按项目 DataDir 共享。若 agent 被拦命令 A 与
// 其 `--last` 调用之间，同项目另一会话/工具触发了 block B，`--last` 确认的是 B——为
// 一条用户从未见过的命令登记了 5min 放行，而 A 仍被拦。失败模式自愈（A 重拦 → 重走
// HITL；B 的确认大概率空过期），且任何确认的创建都以真实 block 事件为前提、审计链
// 保持如实——这是零转写的代价。单会话流程（常态）无此窗口：hook 在打印 agent 所确认
// 的拦截消息之前就落了 block 事件。
func ConfirmLastBlock(p *forgedata.Project) (fp, cmd string, err error) {
	events, err := LoadEvents(p)
	if err != nil {
		return "", "", fmt.Errorf("load hazard events: %w", err)
	}
	for i := len(events) - 1; i >= 0; i-- {
		e := events[i]
		if e.Type != EventBlock || e.Fingerprint == "" {
			continue
		}
		if err := ConfirmByFingerprint(p, e.Fingerprint, e.Command); err != nil {
			return "", "", fmt.Errorf("confirm last block: %w", err)
		}
		return e.Fingerprint, e.Command, nil
	}
	return "", "", fmt.Errorf("事件流中没有带指纹的 block 事件——先触发一次拦截（hazard-guard block），或用 --fingerprint/<命令> 显式确认")
}
