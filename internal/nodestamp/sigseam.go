package nodestamp

// sigseam.go — 事件签名接缝（2026-09-03，eval traps 首轮实测的 action item：
// Sig v1 恒空 → 伪造 checklog 审计行不可检测）。
//
// 职责：把 nodeid 的 ed25519 Sign/Verify 以 nodestamp 缓存身份为单一出口暴露给
// checklog（checklog 已 import 本包，签名接入零新依赖）。签名失败绝不阻塞记录
// （第一原则：打戳绝不阻塞它依附的事件）——Sig 降级为空，读侧按 unsigned-legacy
// 处理。验签的公钥来源是本机 node.json；跨机行的验签需要公钥注册表
// （node-identity 的 TrustStore TOFU），v1 不做——见 checklog.EntryVerdict 的
// 诚实边界注释。

import "fmt"

// SignPayload signs msg with the cached node private key (base64 signature).
// Errors on a missing/broken identity — the caller degrades, never blocks.
//
// SignPayload 用缓存节点私钥签 msg（base64 签名）。身份缺失/损坏时报错——
// 调用方降级处理，绝不阻塞。
func SignPayload(msg []byte) (string, error) {
	mu.Lock()
	defer mu.Unlock()
	id, err := loadIdentity()
	if err != nil {
		return "", fmt.Errorf("nodestamp: 节点身份不可用，无法签名: %w", err)
	}
	return id.Sign(msg)
}

// LocalIdentity returns the cached node id and public key (base64) for local
// verification. Errors on a missing/broken identity.
//
// LocalIdentity 返回缓存节点 id 与公钥（base64），供本机验签。身份缺失/损坏时
// 报错。
func LocalIdentity() (nodeID, pubB64 string, err error) {
	mu.Lock()
	defer mu.Unlock()
	id, err := loadIdentity()
	if err != nil {
		return "", "", fmt.Errorf("nodestamp: 节点身份不可用: %w", err)
	}
	return id.NodeID, id.PublicKey, nil
}
