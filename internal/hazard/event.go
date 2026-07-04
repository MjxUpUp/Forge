package hazard

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/MjxUpUp/Forge/internal/forgedata"
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
	// EventBlock：hazard-guard 拦截高危命令（未确认，等待 HITL）。
	EventBlock = "block"
	// EventRelease：因 forge hazard confirm 登记（5min 窗口内）而放行。
	EventRelease = "release"
	// EventData：context classification 判定危险串仅在引号内（数据，非执行）而放行，
	// 如 grep "rm -rf" / git commit -m "fix rm -rf bug"。
	EventData = "data"
)

var eventMu sync.Mutex

// Event 记录一次 hazard-guard 事件，追加写 .forge/hazards/events.jsonl。
type Event struct {
	Ts          time.Time `json:"ts"`
	Type        string    `json:"type"`        // EventBlock/EventRelease/EventData
	Fingerprint string    `json:"fingerprint"` // Fingerprint(command)；算不出时为空
	Command     string    `json:"command"`     // 截断的命令串（审计用，maxCommandStore）
}

// AppendEvent 追加一条事件到 <DataDir>/hazards/events.jsonl。Ts 由本函数盖时间戳，
// Command 截断到 maxCommandStore（与 Confirmation 一致），避免超长命令撑大日志。
// 线程安全：进程内 eventMu 串行化。hook 是多进程调用 `forge hazard log` 子命令，跨进程
// 靠 O_APPEND——POSIX 下单行 Write 原子；Windows 无 PIPE_BUF 保证，但 hook 触发低频、
// 交错风险可接受（审计日志容忍偶发坏行，LoadEvents 跳过损坏行）。
//
// 失败不应影响 hook 主流程——调用方（hook 脚本）用 `|| true` 容错，审计失败不 block。
func AppendEvent(p *forgedata.Project, e Event) error {
	eventMu.Lock()
	defer eventMu.Unlock()

	e.Ts = time.Now()
	e.Command = truncate(e.Command, maxCommandStore)

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
			continue // 跳过损坏行
		}
		events = append(events, e)
	}
	return events, scanner.Err()
}

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
