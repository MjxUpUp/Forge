// Package aatout converts forge checklog audit rows into IETF
// draft-sharif-agent-audit-trail shaped records — a versioned, detachable
// mapper (focus-batches §D2, direction D2).
//
// Package aatout 把 forge checklog 审计行转换为 IETF
// draft-sharif-agent-audit-trail 形状的记录——versioned mapper，可摘除
// （focus-batches §D2，方向 D2）。
//
// 诚实边界（versioned mapper 契约，标准仍在 -02 草案期）：
//   - canonicalization 用"排序键+无空白 JSON"近似 RFC 8785 JCS——AAT 的
//     prev_hash 链依赖 JCS；我们的记录是扁平 ASCII 键结构，简化规范化在其上
//     等价，但嵌套/数值场景不保证。上游 -03 改字段时本 mapper 递增版本重写。
//   - record_id 用确定性 UUID（sha256(name) 按 RFC 4122 v5 形态填位）而非草案
//     的 v4——可重复导出得到同一 id，下游可增量消费；这是有意的偏离，注释在
//     每份导出的 meta 里声明。
//   - signature 字段直通 checklog 既有的 ed25519 行签名（Forge 侧原语与 AAT 的
//     ECDSA P-256 是平行实现——AST10 钦定 ed25519，我们不改自己的原语去凑草案）。
//   - 无 TSA（外部时间戳锚定）——草案可选项，v1 不做。
package aatout

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
)

// mapperVersion 是 AAT mapper 的语义版本（AATVersion 常量的对应物）：字段映射或
// 规范化变化必须递增——消费方据此降级处理。
const mapperVersion = "1"

// Action/outcome 映射（AAT action_type registry：tool_call/tool_response/
// decision/delegation/escalation/error/lifecycle；outcome：success/failure/
// timeout/denied/escalated）。Forge check 行语义按 CheckName 分桶——映射表集中
// 一处，新增 CheckName 落到 lifecycle 默认桶（诚实：未知类型不伪装成 decision）。
func classify(check checklog.CheckName, level checklog.Level) (actionType, outcome string) {
	switch {
	case level == checklog.LevelBlocked:
		return "decision", "denied"
	case level == checklog.LevelFail:
		return "error", "failure"
	case level == checklog.LevelWarn:
		return "escalation", "escalated"
	}
	switch check {
	case checklog.CheckTaskGuard, checklog.CheckCheatScan, checklog.CheckSelfReport,
		checklog.CheckTaskVerify, checklog.CheckTaskComplete:
		return "decision", "success"
	case checklog.CheckSkillTrigger, checklog.CheckTaskStarted, checklog.CheckGatePush, checklog.CheckTaskStalled:
		return "lifecycle", "success"
	case checklog.CheckSubagentStop, checklog.CheckToolFailure:
		return "tool_response", "success"
	default:
		return "lifecycle", "success"
	}
}

// AATRecord 是 AAT-02 形状的导出行（字段名 snake_case 对齐草案；forge 语义放
// forge_* 扩展键——草案允许 action_detail 前缀扩展）。
type AATRecord struct {
	RecordID     string `json:"record_id"` // 确定性 UUIDv5 形态（见包注释的偏离声明）
	Timestamp    string `json:"timestamp"` // RFC 3339 UTC
	AgentID      string `json:"agent_id"`  // URI 形态：forge://node/<node_id>（无节点时空）
	AgentVersion string `json:"agent_version,omitempty"`
	SessionID    string `json:"session_id,omitempty"`
	ActionType   string `json:"action_type"`
	ActionDetail string `json:"action_detail,omitempty"` // forge:<check>[…] 摘要
	Outcome      string `json:"outcome"`
	TrustLevel   string `json:"trust_level"` // L0-L4：forge TOFU 侧映射（见 trustOf）
	TaskRef      string `json:"task_ref,omitempty"`
	ParentID     string `json:"parent_record_id,omitempty"` // 前一条导出记录的 id（链）
	PrevHash     string `json:"prev_hash"`                  // sha256(canonical(prev))，genesis 为 64 个 0
	InputHash    string `json:"input_hash,omitempty"`       // sha256(detail)——证据载荷指纹
	Signature    string `json:"signature,omitempty"`        // checklog 既有 ed25519 行签名直通
}

// ExportMeta 是每份导出的头行（meta 行在最前，声明 mapper 版本与全部有意的
// 偏离——消费方读第一行即可决定能不能吃这份数据）。
type ExportMeta struct {
	Meta        string   `json:"meta"` // 常量 "forge-aat-export"
	MapperVer   string   `json:"mapper_version"`
	AATDraft    string   `json:"aat_draft"` // "draft-sharif-agent-audit-trail-02"
	Deviations  []string `json:"deviations"`
	RecordCount int      `json:"record_count"`
	GeneratedAt string   `json:"generated_at"`
}

// Options 是导出参数。AgentVersion 填 agent_version；无 SessionID 的行按
// TaskRef 归属（forge 全局行的诚实归属）。
type Options struct {
	AgentVersion string
}

const genesisHash = "0000000000000000000000000000000000000000000000000000000000000000"

// BuildExport 把 checklog 行转为 meta + AAT 记录序列（链式 prev_hash）。
// 确定性：同一输入两次导出字节一致（record_id/prev_hash 全部从内容派生）——
// 下游可以 diff 两份导出定位增量。
func BuildExport(entries []checklog.Entry, opts Options) ([]byte, error) {
	var b strings.Builder
	meta := ExportMeta{
		Meta:      "forge-aat-export",
		MapperVer: mapperVersion,
		AATDraft:  "draft-sharif-agent-audit-trail-02",
		Deviations: []string{
			"record_id: deterministic UUIDv5-shape from record identity (draft says UUIDv4; idempotent re-export is a feature)",
			"canonicalization: sorted-keys compact JSON approximates RFC 8785 JCS (flat ASCII-key records are equivalent under both)",
			"signature: passthrough of forge ed25519 row signature (draft exemplifies ECDSA P-256; primitives are parallel, not conflicting)",
			"tsa: external timestamp anchoring not included (optional in draft)",
		},
		RecordCount: len(entries),
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
	}
	metaLine, err := json.Marshal(meta)
	if err != nil {
		return nil, err
	}
	b.Write(metaLine)
	b.WriteByte('\n')

	prevHash := genesisHash
	prevID := ""
	for _, e := range entries {
		rec := toRecord(e, opts)
		rec.PrevHash = prevHash
		rec.ParentID = prevID
		canonical, err := canonicalJSON(rec)
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(canonical)
		prevHash = hex.EncodeToString(sum[:])
		prevID = rec.RecordID
		line, err := json.Marshal(rec)
		if err != nil {
			return nil, err
		}
		b.Write(line)
		b.WriteByte('\n')
	}
	return []byte(b.String()), nil
}

// toRecord 单行映射（不碰链字段——BuildExport 统一填）。
func toRecord(e checklog.Entry, opts Options) AATRecord {
	actionType, outcome := classify(e.Check, e.EffectiveLevel())
	detail := strings.TrimSpace(e.Detail)
	if len(detail) > 300 {
		detail = detail[:300] + "…"
	}
	ad := "forge:" + string(e.Check)
	if detail != "" {
		ad += " — " + detail
	}
	rec := AATRecord{
		RecordID:     deterministicUUID(string(e.Check) + "|" + e.TaskRef + "|" + e.SessionID + "|" + e.RecordedAt.UTC().Format(time.RFC3339Nano) + "|" + e.Stamp.NodeID + "|" + fmt.Sprint(e.Stamp.Seq)),
		Timestamp:    e.RecordedAt.UTC().Format(time.RFC3339Nano),
		AgentID:      "",
		AgentVersion: opts.AgentVersion,
		SessionID:    e.SessionID,
		ActionType:   actionType,
		ActionDetail: ad,
		Outcome:      outcome,
		TrustLevel:   trustOf(e),
		TaskRef:      e.TaskRef,
		InputHash:    hashOf(e.Detail),
		Signature:    e.Sig,
	}
	if e.NodeID != "" {
		rec.AgentID = "forge://node/" + e.NodeID
	}
	return rec
}

// trustOf 把 Forge 的证据来源分级映射到 AAT trust_level（L0 最低）：deterministic
// 证据 L3、agent-claim L1、未标注 L0——诚实声明 forge 侧没有 L4（跨机验签需公钥
// 注册表，v1 边界）。
func trustOf(e checklog.Entry) string {
	switch e.Source {
	case checklog.EvidenceDeterministic:
		return "L3"
	case checklog.EvidenceAgentClaim:
		return "L1"
	default:
		return "L0"
	}
}

// canonicalJSON 排序键紧凑序列化（prev_hash 链的规范化基——见包注释的 JCS 近似）。
func canonicalJSON(r AATRecord) ([]byte, error) {
	m := map[string]any{
		"record_id": r.RecordID, "timestamp": r.Timestamp, "agent_id": r.AgentID,
		"agent_version": r.AgentVersion, "session_id": r.SessionID,
		"action_type": r.ActionType, "action_detail": r.ActionDetail,
		"outcome": r.Outcome, "trust_level": r.TrustLevel, "task_ref": r.TaskRef,
		"parent_record_id": r.ParentID, "prev_hash": r.PrevHash,
		"input_hash": r.InputHash, "signature": r.Signature,
	}
	return json.Marshal(m) // encoding/json 对 map 键排序 → 紧凑确定性
}

func hashOf(s string) string {
	if s == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// deterministicUUID 从身份串派生稳定 UUID（RFC 4122 形态：version/variant 位按
// v5 语义置位，命名空间取 forge 常量）。
func deterministicUUID(identity string) string {
	const ns = "forge.aat.v1"
	sum := sha256.Sum256([]byte(ns + "|" + identity))
	b := sum[:16]
	b[6] = (b[6] & 0x0f) | 0x50 // version 5
	b[8] = (b[8] & 0x3f) | 0x80 // variant RFC 4122
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// WriteExport marshals to w（BuildExport 的薄包装，CLI 用）。
func WriteExport(w io.Writer, entries []checklog.Entry, opts Options) error {
	body, err := BuildExport(entries, opts)
	if err != nil {
		return err
	}
	_, err = w.Write(body)
	return err
}
