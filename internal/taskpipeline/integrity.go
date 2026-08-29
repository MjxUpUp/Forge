package taskpipeline

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"

	"github.com/MjxUpUp/Forge/internal/nodeid"
)

// StateIntegrity is the HMAC signature block written by SaveTaskState. Its presence
// with a failing signature marks the file as modified outside forge; its absence
// means legacy pre-signing data (allowed through; the next forge save re-signs).
// Design: docs/design/state-integrity-signing.md — the threat model is the agent
// hand-editing DataDir files (2026-08-29 functional probes: forged ReviewPassed /
// DocReview entries were fully trusted); it is NOT cryptographic defense against an
// adversary who can invoke forge itself (CLI calls are product semantics, governed
// by the gates and audits, not by signatures).
//
// StateIntegrity 是 SaveTaskState 写入的 HMAC 签名块。签名存在且验不过 = 文件在
// forge 之外被改过；不存在 = 签名前的存量数据（放行，下次 forge 保存补签）。
// 设计见 docs/design/state-integrity-signing.md——威胁模型是 agent 手改 DataDir
// 文件（2026-08-29 功能探针：伪造的 ReviewPassed/DocReview 曾被全量采信）；不是
// 对「能调用 forge 本身」的对手的密码学防御（CLI 调用是产品语义，由门禁与审计
// 管，不由签名管）。
type StateIntegrity struct {
	KeyID string `json:"key_id,omitempty"` // nodeID 前缀——签名者标识（本机）
	Alg   string `json:"alg,omitempty"`
	Sig   string `json:"sig,omitempty"` // base64(HMAC-SHA256(canonical JSON, key))
}

// IntegrityBroken reports whether the loaded state failed its signature check.
// Gate-satisfying consumers (complete pre-flight, doc gate, review snapshot) must
// refuse to trust fields from a broken state.
//
// IntegrityBroken 报告加载的状态是否未通过签名校验。满足门禁类的消费方（complete
// pre-flight、doc gate、审查快照）必须拒采信 broken 状态上的字段。
func (s *TaskState) IntegrityBroken() bool { return s != nil && s.integrityBroken }

// integrityKey derives the HMAC key from this node's identity: SHA-256 over the
// node's ed25519 private key bytes. No new key-management surface — nodeid.Load is
// the existing funnel, and the key never leaves the machine (cross-machine trust is
// the bundle-signing channel's domain, not this one).
//
// integrityKey 从本机身份派生 HMAC key：对 node 的 ed25519 私钥字节做 SHA-256。
// 不新增密钥管理面——nodeid.Load 是既有漏斗，key 不出本机（跨机信任是 bundle
// 签名通道的领域，不是这里的）。
func integrityKey() ([]byte, string, error) {
	id, err := nodeid.Load()
	if err != nil || id == nil || id.PrivateKey == "" {
		if err == nil {
			err = errors.New("node identity unavailable")
		}
		return nil, "", err
	}
	priv, err := base64.StdEncoding.DecodeString(id.PrivateKey)
	if err != nil || len(priv) == 0 {
		if err == nil {
			err = errors.New("empty private key")
		}
		return nil, "", err
	}
	h := sha256.Sum256(priv)
	keyID := id.NodeID
	if len(keyID) > 8 {
		keyID = keyID[:8]
	}
	return h[:], keyID, nil
}

// canonicalStateJSON marshals the state with the integrity block zeroed — the exact
// bytes SaveTaskState signed. json.Marshal is deterministic for struct field order,
// and map keys are sorted, so re-marshalling the loaded state reproduces it.
//
// canonicalStateJSON 对 integrity 块置零后 marshal——即 SaveTaskState 所签的确切
// 字节。json.Marshal 对 struct 字段序确定、map 键有序，加载后重新 marshal 可复现。
func canonicalStateJSON(s *TaskState) ([]byte, error) {
	saved := s.Integrity
	s.Integrity = nil
	data, err := json.Marshal(s)
	s.Integrity = saved
	return data, err
}

func signTaskState(s *TaskState) (sig, keyID string, err error) {
	key, kid, err := integrityKey()
	if err != nil {
		return "", "", err
	}
	payload, err := canonicalStateJSON(s)
	if err != nil {
		return "", "", err
	}
	m := hmac.New(sha256.New, key)
	m.Write(payload)
	return base64.StdEncoding.EncodeToString(m.Sum(nil)), kid, nil
}

func verifyTaskState(s *TaskState) (bool, error) {
	if s.Integrity == nil || s.Integrity.Sig == "" {
		return false, errors.New("no signature present")
	}
	key, _, err := integrityKey()
	if err != nil {
		return false, err
	}
	payload, err := canonicalStateJSON(s)
	if err != nil {
		return false, err
	}
	m := hmac.New(sha256.New, key)
	m.Write(payload)
	want, err := base64.StdEncoding.DecodeString(s.Integrity.Sig)
	if err != nil {
		return false, err
	}
	return hmac.Equal(m.Sum(nil), want), nil
}
