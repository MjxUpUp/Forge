package taskpipeline

// segmenter.go — 监控分段工具（focus-batches §1b，方向 B 的硬前置）：Classifier
// Context Rot（arXiv 2605.12366）实证监控用 LLM 在长轨迹上对危险动作漏检率升高
// 2×-30×，唯一验证过的缓解是分段判定 + 周期性重注入。本文件把该约束做成确定性
// 工具：任何下游 judge/取证消费（forge trace --window / transcript-forensics 流程）
// 取输入时按窗口切片，每窗头部重注入守卫摘要——禁止全量轨迹塞给 LLM 判定环节。
//
// forge 侧原则的另一面（见 focus-batches 设计约束）：监控依据 checklog/toollog
// 签名台账而非模型上下文记忆——台账是确定性载体，本工具只是消费侧的取数纪律。

import (
	"strings"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/toolusage"
)

// SegmentWindow 是一个监控窗口：头部守卫重注入行 + 该窗的事件行。
type SegmentWindow struct {
	Header string   // 周期性重注入的守卫摘要（论文唯一验证过的缓解形态）
	Lines  []string // 窗口内事件行（checklog/toollog 渲染后的文本）
}

// SegmentEvents 把 checklog+toollog 事件按字符预算切成监控窗口。budgetChars 是
// 每窗事件行的字符上限（头部不计入——重注入是固定成本）；header 是周期性重注入
// 文本（调用方给守卫摘要/活跃约束；空串时置默认提示）。事件按时间序交错
// （checklog 与 toollog 各自有序，合并后仍稳定：同刻按 checklog 先、toollog 后，
// 避免同秒乱序抖动）。budget<=0 返回单窗（调用方自证不需要分段时的退化形态）。
func SegmentEvents(entries []checklog.Entry, calls []toolusage.ToolCall, budgetChars int, header string) []SegmentWindow {
	if header == "" {
		header = "[monitor-guard] 按窗口分段判定；守卫规则与活跃约束以最新重注入为准（Context Rot 缓解，arXiv 2605.12366）"
	}
	type line struct {
		ts   int64
		text string
	}
	var lines []line
	for _, e := range entries {
		lines = append(lines, line{e.RecordedAt.UnixNano(), renderCheck(e)})
	}
	for _, c := range calls {
		lines = append(lines, line{c.Timestamp.UnixNano(), renderCall(c)})
	}
	// 稳定插入排序（事件量级数百~数千，且两路输入各自有序——插入排序在此输入
	// 分布下接近线性，避免为一次性工具引 sort.Slice 的不必要泛型开销）。
	for i := 1; i < len(lines); i++ {
		for j := i; j > 0 && lines[j].ts < lines[j-1].ts; j-- {
			lines[j], lines[j-1] = lines[j-1], lines[j]
		}
	}
	var out []SegmentWindow
	cur := SegmentWindow{Header: header}
	size := 0
	flush := func() {
		if len(cur.Lines) > 0 {
			out = append(out, cur)
			cur = SegmentWindow{Header: header}
			size = 0
		}
	}
	for _, l := range lines {
		if budgetChars > 0 && size+len(l.text) > budgetChars && len(cur.Lines) > 0 {
			flush()
		}
		cur.Lines = append(cur.Lines, l.text)
		size += len(l.text)
	}
	flush()
	if len(out) == 0 {
		out = append(out, cur) // 空输入也给一个空窗（头部仍在——消费方知道管道活着）
	}
	return out
}

func renderCheck(e checklog.Entry) string {
	var b strings.Builder
	b.WriteString("[")
	b.WriteString(e.RecordedAt.Format("01-02 15:04:05"))
	b.WriteString("][check:")
	b.WriteString(string(e.Check))
	b.WriteString("] ")
	if !e.Passed {
		b.WriteString("FAIL ")
	}
	b.WriteString(strings.TrimSpace(e.Detail))
	return b.String()
}

func renderCall(c toolusage.ToolCall) string {
	var b strings.Builder
	b.WriteString("[")
	b.WriteString(c.Timestamp.Format("01-02 15:04:05"))
	b.WriteString("][tool:")
	b.WriteString(c.ToolName)
	b.WriteString("] ")
	in := strings.TrimSpace(c.ToolInput)
	if in == "" {
		in = "(no input)"
	}
	b.WriteString(in)
	return b.String()
}
