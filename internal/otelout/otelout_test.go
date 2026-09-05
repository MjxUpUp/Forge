package otelout

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
)

func mkEntry(check checklog.CheckName, passed bool, detail, session, task string, at time.Time) checklog.Entry {
	e := checklog.Entry{
		Check:      check,
		Passed:     passed,
		Checked:    true,
		Detail:     detail,
		SessionID:  session,
		TaskRef:    task,
		RecordedAt: at,
	}
	return e
}

// TestBuildExport_GoldenShape 钉 OTLP/JSON 四级结构（resource→scope→span→event）
// 与关键属性——versioned mapper 的形状契约，上游消费方按此回归。
func TestBuildExport_GoldenShape(t *testing.T) {
	base := time.Date(2026, 9, 5, 10, 0, 0, 0, time.UTC)
	entries := []checklog.Entry{
		mkEntry(checklog.CheckCheatScan, false, "BLOCKED: 命中 type-suppression 2 处", "sess-1", "task/a", base),
		mkEntry(checklog.CheckTaskVerify, true, "ok", "sess-1", "task/a", base.Add(time.Minute)),
		mkEntry(checklog.CheckTaskGuard, true, "全局行（无 session）", "", "task/b", base.Add(2*time.Minute)),
	}
	var buf bytes.Buffer
	if err := WriteOTLP(&buf, entries, Options{ServiceVersion: "1.51.0-test", ProjectKey: "k123"}); err != nil {
		t.Fatalf("WriteOTLP: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(buf.Bytes(), &raw); err != nil {
		t.Fatalf("输出不是合法 JSON: %v\n%s", err, buf.String())
	}
	rss, ok := raw["resourceSpans"].([]any)
	if !ok || len(rss) != 1 {
		t.Fatalf("resourceSpans 应为 1 个，实际 %v", raw["resourceSpans"])
	}
	rs := rss[0].(map[string]any)
	resAttr := map[string]string{}
	for _, a := range rs["resource"].(map[string]any)["attributes"].([]any) {
		am := a.(map[string]any)
		resAttr[am["key"].(string)] = am["value"].(map[string]any)["stringValue"].(string)
	}
	if resAttr["service.name"] != "forge" || resAttr["service.version"] != "1.51.0-test" || resAttr["forge.project.key"] != "k123" {
		t.Fatalf("resource 属性不符: %v", resAttr)
	}
	sss := rs["scopeSpans"].([]any)
	if len(sss) != 1 {
		t.Fatalf("scopeSpans 应为 1 个")
	}
	ss := sss[0].(map[string]any)
	if ss["scope"].(map[string]any)["name"] != "forge.checklog" {
		t.Fatalf("scope.name 应为 forge.checklog")
	}
	spans := ss["spans"].([]any)
	if len(spans) != 2 {
		t.Fatalf("应有 2 个 span（sess-1 与 task/b），实际 %d", len(spans))
	}
	// 第一个 span：session 分组，2 events，时间窗覆盖两条
	sp1 := spans[0].(map[string]any)
	if sp1["name"] != "forge.session" {
		t.Fatalf("span.name 应为 forge.session")
	}
	if len(sp1["traceId"].(string)) != 32 || len(sp1["spanId"].(string)) != 16 {
		t.Fatalf("traceId/spanId 长度不符: %v/%v", sp1["traceId"], sp1["spanId"])
	}
	evs := sp1["events"].([]any)
	if len(evs) != 2 {
		t.Fatalf("span1 应有 2 events，实际 %d", len(evs))
	}
	ev1 := evs[0].(map[string]any)
	if ev1["name"] != "cheat-scan" {
		t.Fatalf("event.name 应为 check 名，实际 %v", ev1["name"])
	}
	evAttr := map[string]any{}
	for _, a := range ev1["attributes"].([]any) {
		am := a.(map[string]any)
		evAttr[am["key"].(string)] = am["value"].(map[string]any)
	}
	if evAttr["forge.check.passed"].(map[string]any)["boolValue"] != false {
		t.Fatalf("passed 应编码为 boolValue=false")
	}
	if got := evAttr["forge.check.detail"].(map[string]any)["stringValue"].(string); !strings.Contains(got, "type-suppression") {
		t.Fatalf("detail 应保留关键词: %q", got)
	}
	// Source 非零时必带 evidence_source 属性（mkEntry 未设 Source → 本夹具省略，
	// 断言负形态：不存在）。Record 兜底填充的行为由 checklog 包自测覆盖。
	if _, ok := evAttr["forge.check.evidence_source"]; ok {
		t.Fatalf("未设 Source 的条目不应带 evidence_source 属性")
	}
	// 第二个 span：task 分组（无 session）
	sp2 := spans[1].(map[string]any)
	evs2 := sp2["events"].([]any)
	if len(evs2) != 1 || evs2[0].(map[string]any)["name"] != "task-guard" {
		t.Fatalf("span2 应含 1 条 task-guard event")
	}
}

// TestBuildExport_StableTraceIDs 同一分组键重复导出得到相同 trace/span ID。
func TestBuildExport_StableTraceIDs(t *testing.T) {
	base := time.Date(2026, 9, 5, 10, 0, 0, 0, time.UTC)
	entries := []checklog.Entry{mkEntry(checklog.CheckCheatScan, true, "ok", "sess-9", "t/1", base)}
	a := BuildExport(entries, Options{})
	b := BuildExport(entries, Options{})
	sa := a.ResourceSpans[0].ScopeSpans[0].Spans[0]
	sb := b.ResourceSpans[0].ScopeSpans[0].Spans[0]
	if sa.TraceID != sb.TraceID || sa.SpanID != sb.SpanID {
		t.Fatalf("稳定 ID 破坏: %v/%v vs %v/%v", sa.TraceID, sa.SpanID, sb.TraceID, sb.SpanID)
	}
}

// TestBuildExport_EmptyEntries 空输入返回骨架而非空对象——消费方区分"无数据"与"失败"。
func TestBuildExport_EmptyEntries(t *testing.T) {
	out := BuildExport(nil, Options{ServiceVersion: "v"})
	if len(out.ResourceSpans) != 1 {
		t.Fatalf("空输入也应有 ResourceSpans 骨架")
	}
	spans := out.ResourceSpans[0].ScopeSpans[0].Spans
	if spans == nil {
		t.Fatalf("空输入 spans 应为空数组（proto3 JSON repeated 不出 null——严格 OTLP 接收器拒收）")
	}
	if len(spans) != 0 {
		t.Fatalf("空输入 spans 应长度 0，实际 %v", spans)
	}
}

// TestBuildExport_DetailTruncation detail 超 200 rune 截断。
func TestBuildExport_DetailTruncation(t *testing.T) {
	base := time.Date(2026, 9, 5, 10, 0, 0, 0, time.UTC)
	long := strings.Repeat("长", 500)
	out := BuildExport([]checklog.Entry{mkEntry(checklog.CheckCheatScan, true, long, "s", "t", base)}, Options{})
	detail := ""
	for _, a := range out.ResourceSpans[0].ScopeSpans[0].Spans[0].Events[0].Attributes {
		if a.Key == "forge.check.detail" {
			detail = *a.Value.StringValue
		}
	}
	if got := len([]rune(detail)); got > 210 { // 截断上限 + 省略号余量
		t.Fatalf("detail 应被截断到 ~200 rune，实际 %d", got)
	}
}
