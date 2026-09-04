package checklog

// verify.go — 审计行签名与验签（eval traps 首轮实测 action item：v1 Sig 恒空，
// 伪造 checklog 行不可检测——2026-09-03 起补闭环）。
//
// 写侧：Record 在 marshal 前对 canonical 事件字节（Stamp.Sig 清零、其余字段含
// NodeID/Seq/TsHLC 全量入签）签名；签名失败降级为空 Sig（第一原则：打戳绝不
// 阻塞事件），读侧按 legacy 处理。
//
// 读侧：EntryVerdict 三态裁决 + 诚实边界——
//   - Sig 空          → UnsignedLegacy（v1.1 前的历史行/降级写出，不判伪造）
//   - NodeID 非本机   → ForeignNode（v1 只有本机公钥；跨机验签需公钥注册表
//     ——node-identity TrustStore 的 TOFU 职责，本包不越界。攻击者可把行伪装
//     成他机 node_id 绕过本机验证——v1 已知边界，披露卡已列）
//   - 本机行验签失败  → Forged（Sig 非空且与事件字节不匹配——正是 eval traps
//     植入的伪造形态）

import (
	"encoding/json"

	"github.com/MjxUpUp/Forge/internal/nodestamp"
	"github.com/MjxUpUp/Forge/internal/nodeid"
)

// EntryVerdict is the three-state outcome of auditing one entry's signature.
//
// EntryVerdict 是一条条目签名的三态裁决。
type EntryVerdict string

const (
	// VerdictValid: signed by this node and the signature matches the event bytes.
	//
	// VerdictValid：本机签名且与事件字节匹配。
	VerdictValid EntryVerdict = "valid"
	// VerdictUnsignedLegacy: no signature (pre-signing rows / degraded writes) —
	// the vast majority of history; never judged forged.
	//
	// VerdictUnsignedLegacy：无签名（签名机制前/降级写出的行）——历史主体，
	// 绝不判伪造。
	VerdictUnsignedLegacy EntryVerdict = "unsigned-legacy"
	// VerdictForeignNode: signed-looking row attributed to another node — local
	// verification is impossible without a key registry (v1 boundary).
	//
	// VerdictForeignNode：归属他机的带签行——无公钥注册表则本机无法验（v1 边界）。
	VerdictForeignNode EntryVerdict = "foreign-node"
	// VerdictForged: a non-empty signature that does NOT verify against the event
	// bytes on a locally-attributable row — the planted-forgery shape.
	//
	// VerdictForged：可归属本机的行上，非空签名与事件字节不匹配——被植入伪造
	// 的形态。
	VerdictForged EntryVerdict = "forged"
)

// canonicalEventBytes marshals the entry with Stamp.Sig zeroed — the exact
// bytes Record signed. All other Stamp fields (NodeID/Seq/TsHLC) are inside the
// signature, so tampering with any of them breaks verification. Go's encoding/
// json sorts map keys, making the marshal deterministic across processes.
//
// canonicalEventBytes marshal 条目并把 Stamp.Sig 清零——即 Record 签过的字节。
// Stamp 其余字段（NodeID/Seq/TsHLC）都在签名内，篡改任一即验签失败。Go 的
// encoding/json 对 map 键排序，跨进程 marshal 确定。
func canonicalEventBytes(e *Entry) ([]byte, error) {
	cp := *e
	cp.Sig = ""
	return json.Marshal(&cp)
}

// signEntry signs the entry's canonical bytes into Stamp.Sig (degrades to
// empty on any failure — recording must never block).
//
// signEntry 对条目 canonical 字节签名并写入 Stamp.Sig（任何失败降级为空——
// 记录绝不阻塞）。
func signEntry(e *Entry) {
	if e.Sig != "" {
		return // 导入路径携带的源节点签名——保留原样
	}
	msg, err := canonicalEventBytes(e)
	if err != nil {
		return
	}
	sig, err := nodestamp.SignPayload(msg)
	if err != nil {
		return // 降级：空 Sig，读侧按 legacy 处理
	}
	e.Sig = sig
}

// AuditEntry audits one entry against the local node identity.
//
// AuditEntry 用本机节点身份裁决一条条目。
func AuditEntry(e *Entry) EntryVerdict {
	if e == nil || e.Sig == "" {
		return VerdictUnsignedLegacy
	}
	localID, localPub, err := nodestamp.LocalIdentity()
	if err != nil {
		return VerdictUnsignedLegacy // 身份不可用：无法裁决，按 legacy（fail-open）
	}
	// 归属他机的行（node_id 非空且非本机）无本机公钥可验——v1 边界，不误判。
	if e.NodeID != "" && e.NodeID != localID {
		return VerdictForeignNode
	}
	msg, err := canonicalEventBytes(e)
	if err != nil {
		return VerdictForged
	}
	if nodeid.Verify(localPub, msg, e.Sig) {
		return VerdictValid
	}
	return VerdictForged
}
