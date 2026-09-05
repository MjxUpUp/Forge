// Package otelout converts forge checklog audit rows into OTLP/JSON export
// requests, so audit evidence can flow into existing SIEM/APM pipelines that
// natively ingest OpenTelemetry (focus-batches.md §1a, direction D1).
//
// Package otelout 把 forge checklog 审计行转换为 OTLP/JSON 导出请求，让审计证据
// 流入原生支持 OpenTelemetry 的既有 SIEM/APM 管线（focus-batches.md §1a，方向 D1）。
//
// 设计约束（versioned mapper，可摘除）：
//   - 零第三方依赖：不引入 otel SDK，手写 OTLP/JSON 1.1 形状——上游 IETF AAT /
//     OTel GenAI semconv 仍在早期（-02 草案 / 无 stable 属性），本包是导出器而非
//     内核存储格式，标准演进时改这里即可（调研 dive_04 卡位判断）。
//   - 映射模型：Resource（service.name=forge）→ Scope（forge.checklog）→
//     Span（每 session 一条，name=forge.session）→ Events（每条 checklog 一条，
//     name=Check 名）。GenAI semconv 尚无"门禁裁决"语义事件，语义挂 forge.*
//     命名空间占位，待上游演进后对齐——不伪称 gen_ai.* 语义。
//   - traceId/spanId 由分组键（session→task→fallback）稳定散列派生：同一会话
//     重复导出得到同一 trace，下游可增量消费。
package otelout

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/util"
)

// Options carries exporter inputs that vary per invocation.
//
// Options 携带每次导出变化的输入。ServiceVersion 进 Resource 的
// service.version；ProjectKey 是 forgedata 项目键（可空）。
type Options struct {
	ServiceVersion string
	ProjectKey     string
}

// detailLimit 是 event 属性里 forge.check.detail 的截断上限（rune）。OTel 属性
// 不设硬上限但 SIEM 索引普遍按 1KB 级截断——送全文只会让下游静默截，不如在
// 导出侧诚实截断（截断即信息：detail 完整文本永远在 checklog 本地）。
const detailLimit = 200

// OTLP/JSON 值编码（keyValue.value 的 oneof 面，本包只用到三种）。
type attributeValue struct {
	StringValue *string `json:"stringValue,omitempty"`
	BoolValue   *bool   `json:"boolValue,omitempty"`
	IntValue    *string `json:"intValue,omitempty"` // OTLP/JSON 要求 int64 用十进制字符串
}

type attribute struct {
	Key   string         `json:"key"`
	Value attributeValue `json:"value"`
}

func strAttr(k, v string) attribute {
	return attribute{Key: k, Value: attributeValue{StringValue: &v}}
}

func boolAttr(k string, v bool) attribute {
	return attribute{Key: k, Value: attributeValue{BoolValue: &v}}
}

func intAttr(k string, v int) attribute {
	s := fmt.Sprintf("%d", v)
	return attribute{Key: k, Value: attributeValue{IntValue: &s}}
}

type otlpEvent struct {
	TimeUnixNano string      `json:"timeUnixNano"`
	Name         string      `json:"name"`
	Attributes   []attribute `json:"attributes,omitempty"`
}

type otlpSpan struct {
	TraceID           string      `json:"traceId"`
	SpanID            string      `json:"spanId"`
	Name              string      `json:"name"`
	StartTimeUnixNano string      `json:"startTimeUnixNano"`
	EndTimeUnixNano   string      `json:"endTimeUnixNano"`
	Attributes        []attribute `json:"attributes,omitempty"`
	Events            []otlpEvent `json:"events,omitempty"`
}

type otlpScopeSpans struct {
	Scope struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"scope"`
	Spans []otlpSpan `json:"spans"`
}

type otlpResourceSpans struct {
	Resource struct {
		Attributes []attribute `json:"attributes"`
	} `json:"resource"`
	ScopeSpans []otlpScopeSpans `json:"scopeSpans"`
}

// Export is the OTLP/JSON export request body (a "TracesData" in OTLP shape).
//
// Export 是 OTLP/JSON 导出请求体（OTLP 形状的 TracesData）。
type Export struct {
	ResourceSpans []otlpResourceSpans `json:"resourceSpans"`
}

// mapperVersion 是 scope.version——下游可据此识别映射协议版本（versioned mapper
// 契约：映射语义变化必须递增，消费方按版本降级处理）。
const mapperVersion = "1"

// groupKey 决定一条 entry 归属哪个 trace：SessionID 优先（并发会话隔离），
// 空 session 用 TaskRef（hook 侧全局行），两者皆空归 "global" 桶。
func groupKey(e checklog.Entry) string {
	if e.SessionID != "" {
		return "session:" + e.SessionID
	}
	if e.TaskRef != "" {
		return "task:" + e.TaskRef
	}
	return "global"
}

// stableHexID 把 key 散列成 n 字节的 OTel ID（hex 编码，2n 字符）。traceId=16
// 字节、spanId=8 字节——sha256 前缀即满足唯一性与稳定性，无需密码学随机。
func stableHexID(key string, n int) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:n])
}

// BuildExport groups entries into one span per session/task and returns the
// OTLP/JSON export body.
//
// BuildExport 把 entries 按 session/task 分组（每 group 一条 span），返回
// OTLP/JSON 导出体。entries 为空时返回带空 spans 的骨架（Resource 仍在，
// 消费方能区分"导出成功但无数据"与"导出失败"）。
func BuildExport(entries []checklog.Entry, opts Options) Export {
	var rs otlpResourceSpans
	rs.Resource.Attributes = []attribute{
		strAttr("service.name", "forge"),
		strAttr("service.version", opts.ServiceVersion),
	}
	if opts.ProjectKey != "" {
		rs.Resource.Attributes = append(rs.Resource.Attributes, strAttr("forge.project.key", opts.ProjectKey))
	}

	var order []string
	groups := map[string]*otlpSpan{}
	for i := range entries {
		e := &entries[i]
		k := groupKey(*e)
		sp, ok := groups[k]
		if !ok {
			sp = &otlpSpan{
				TraceID:           stableHexID(k, 16),
				SpanID:            stableHexID(k+"|span", 8),
				Name:              "forge.session",
				Attributes:        []attribute{strAttr("forge.group.key", k)},
				StartTimeUnixNano: nano(e.RecordedAt),
				EndTimeUnixNano:   nano(e.RecordedAt),
			}
			groups[k] = sp
			order = append(order, k)
		}
		ev := otlpEvent{
			TimeUnixNano: nano(e.RecordedAt),
			Name:         string(e.Check),
			Attributes: []attribute{
				strAttr("forge.check.name", string(e.Check)),
				boolAttr("forge.check.passed", e.Passed),
				boolAttr("forge.check.checked", e.Checked),
				strAttr("forge.check.level", string(e.EffectiveLevel())),
			},
		}
		if e.Source != "" {
			ev.Attributes = append(ev.Attributes, strAttr("forge.check.evidence_source", string(e.Source)))
		}
		if e.TaskRef != "" {
			ev.Attributes = append(ev.Attributes, strAttr("forge.check.task_ref", e.TaskRef))
		}
		if e.NodeID != "" {
			ev.Attributes = append(ev.Attributes, strAttr("forge.check.node_id", e.NodeID))
		}
		if d := strings.TrimSpace(e.Detail); d != "" {
			ev.Attributes = append(ev.Attributes, strAttr("forge.check.detail", util.TruncateRunes(d, detailLimit)))
		}
		sp.Events = append(sp.Events, ev)
		// span 时间窗随成员扩张（After 守卫保证乱序输入时不倒退）。
		if end := parseNano(sp.EndTimeUnixNano); e.RecordedAt.After(end) {
			sp.EndTimeUnixNano = nano(e.RecordedAt)
		}
		if start := parseNano(sp.StartTimeUnixNano); e.RecordedAt.Before(start) {
			sp.StartTimeUnixNano = nano(e.RecordedAt)
		}
	}

	var ss otlpScopeSpans
	ss.Scope.Name = "forge.checklog"
	ss.Scope.Version = mapperVersion
	ss.Spans = []otlpSpan{} // 空也序列化为 []——proto3 JSON repeated 不出 null，严格 OTLP 接收器拒收 null
	for _, k := range order {
		sp := groups[k]
		sp.Attributes = append(sp.Attributes, intAttr("forge.entries", len(sp.Events)))
		ss.Spans = append(ss.Spans, *sp)
	}
	rs.ScopeSpans = []otlpScopeSpans{ss}
	return Export{ResourceSpans: []otlpResourceSpans{rs}}
}

// WriteOTLP marshals the export body (indented JSON) to w.
//
// WriteOTLP 把导出体（缩进 JSON）写入 w。
func WriteOTLP(w io.Writer, entries []checklog.Entry, opts Options) error {
	body, err := json.MarshalIndent(BuildExport(entries, opts), "", "  ")
	if err != nil {
		return err
	}
	_, err = w.Write(append(body, '\n'))
	return err
}

// nano 把时间转为 OTLP 的十进制纳秒字符串。
func nano(t time.Time) string { return fmt.Sprintf("%d", t.UnixNano()) }

// parseNano 是 nano 的逆（空/非法返回零值）。仅用于 span 时间窗扩张的比较，
// 不进入导出数据面。
func parseNano(s string) time.Time {
	var n int64
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil {
		return time.Time{}
	}
	return time.Unix(0, n)
}
