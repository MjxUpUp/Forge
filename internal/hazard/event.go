package hazard

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/util"
)

// Event log: records the hazard-guard block/release event stream, completing the "escape code (fingerprint) audit trail".
//
// 事件日志：记录 hazard-guard 的拦截/放行事件流，补全"逃生码（指纹）审计记录"。
//
// Background: Confirmation (<fp>.json) only records the final state of confirm registrations (valid within the 5min window);
// block and release events used to flow only into hook stdout/checklog with no structured persistence — when running
// false-positive audits one could only dig through checklog (the pain point of the 2026-06 hazards audit of 19 FAILs: blocked-but-not-confirm commands had no independent record). events.jsonl persists the full event stream so we can trace 「when something was blocked,
// what it was, whether it was released by confirm, whether it was judged as data-context release」. Design mirrors checklog/store.go
// (mutex + O_APPEND append + scanner read).
//
// 背景：Confirmation（<fp>.json）只记 confirm 登记的最终态（5min 窗口内有效）；
// block 拦截和 release 放行事件原本只进 hook stdout/checklog，无结构化落盘——做
// 误伤审计时只能扒 checklog（2026-06 hazards 审计 19 条 FAIL 的痛点：被拦但未
// confirm 的命令无独立记录）。events.jsonl 把完整事件流落盘，可追溯"何时拦了
// 什么、是否被 confirm 放行、是否被判为数据上下文放行"。设计参照 checklog/store.go
// （mutex + O_APPEND 追加 + scanner 读）。

// Event types.
//
// 事件类型。
const (
	// EventBlock: hazard-guard blocks a high-risk command (unconfirmed, awaiting HITL).
	//
	// EventBlock：hazard-guard 拦截高危命令（未确认，等待 HITL）。
	EventBlock = "block"
	// EventRelease: released because forge hazard confirm registered it (within the 5min window).
	//
	// EventRelease：因 forge hazard confirm 登记（5min 窗口内）而放行。
	EventRelease = "release"
	// EventData: context classification judged the dangerous string is only inside quotes (data, not execution) and released it,
	// e.g. grep `rm -rf` / git commit -m `fix rm -rf bug`.
	//
	// EventData：context classification 判定危险串仅在引号内（数据，非执行）而放行，
	// 如 grep "rm -rf" / git commit -m "fix rm -rf bug"。
	EventData = "data"
	// EventConfirm: a confirmation marker was registered (forge hazard confirm). Appended inside
	// writeConfirmation itself — not by the hook script — so the forgery path (hand-writing the
	// marker file) at least cannot fake this event, and every legitimate confirm is auditable.
	//
	// EventConfirm：确认标记被登记（forge hazard confirm）。由 writeConfirmation 内部追加
	// ——而非 hook 脚本——伪造路径（手写标记文件）至少造不出这条事件，每次合法 confirm 都可审计。
	EventConfirm = "confirm"
)

var eventMu sync.Mutex

// Event records a single hazard-guard event, appended to DataDir/hazards/events.jsonl.
//
// Event 记录一次 hazard-guard 事件，追加写 DataDir/hazards/events.jsonl。
type Event struct {
	Ts          time.Time `json:"ts"`
	Type        string    `json:"type"`        // EventBlock/EventRelease/EventData/EventConfirm
	Fingerprint string    `json:"fingerprint"` // Fingerprint(command)；算不出时为空
	Command     string    `json:"command"`     // 截断的命令串（审计用，maxCommandStore）
}

// AppendEvent appends an event to <DataDir>/hazards/events.jsonl. Ts is stamped by this function,
// Command is truncated to maxCommandStore (consistent with Confirmation) to avoid oversized commands bloating the log.
// Thread-safe: eventMu serializes within the process. Hooks invoke the `forge hazard log` subprocess across multiple processes,
// relying on O_APPEND — POSIX guarantees atomic single-line Write; Windows has no PIPE_BUF guarantee, but hook triggers are low-frequency,
// so interleaving risk is acceptable (audit logs tolerate occasional bad lines; LoadEvents skips corrupted lines).
//
// AppendEvent 追加一条事件到 <DataDir>/hazards/events.jsonl。Ts 由本函数盖时间戳，
// Command 截断到 maxCommandStore（与 Confirmation 一致），避免超长命令撑大日志。
// 线程安全：进程内 eventMu 串行化。hook 是多进程调用 `forge hazard log` 子命令，跨进程
// 靠 O_APPEND——POSIX 下单行 Write 原子；Windows 无 PIPE_BUF 保证，但 hook 触发低频、
// 交错风险可接受（审计日志容忍偶发坏行，LoadEvents 跳过损坏行）。
//
// Failure should not affect the hook main flow — callers (hook scripts) tolerate it with `|| true`; audit failure does not block.
//
// 失败不应影响 hook 主流程——调用方（hook 脚本）用 `|| true` 容错，审计失败不 block。
func AppendEvent(p *forgedata.Project, e Event) error {
	eventMu.Lock()
	defer eventMu.Unlock()

	e.Ts = time.Now()
	e.Command = util.TruncateRunes(e.Command, maxCommandStore)

	path := p.HazardsEventsPath()
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

// LoadEvents reads all events (in-file time order). Returns the parsed partial result when the file does not exist or has corrupted lines.
// Corrupted lines are skipped (no error) — audit logs tolerate occasional line corruption and don't discard the whole file for one bad line.
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

// ConfirmLastBlock registers a confirmation for the NEWEST block event in the event log
// and returns its fingerprint and command. This is the copy-free HITL path
// (`forge hazard confirm --last`): the agent confirms "the command that was just
// blocked" without transcribing a 64-char hex fingerprint or re-quoting the command
// string — both transcription forms are proven distortion sources (2026-07 AgentWorld:
// three hand-copied fingerprints, two corrupt; 2026-08-24 Forge session: confirm of the
// bare command mismatched the hook's fingerprint over the full command line with pipe
// suffix). The event log is written by the hook itself at block time, so its
// fingerprint is authoritative by construction.
//
// Semantics: newest EventBlock with a non-empty Fingerprint wins (block events without
// a fingerprint are unconfirmable anyway); re-confirming an already-confirmed block
// simply renews the window (same as Confirm). Does not check whether the block was
// already released — renewal is harmless and keeps the flow single-step.
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
