package nodeid

// trust.go — the node trust store (docs/design/node-identity.md §3): known peers
// with their public keys and trust profile, plus the require-signed toggle that
// flips the machine into the TEAM profile (signature enforcement on bundle import).
//
// Trust establishment is TOFU-by-explicit-command: `forge trust add` shows the
// fingerprint and the human confirms out-of-band (SSH known_hosts / Syncthing
// introduction precedent). The store lives at ~/.forge/trust.json (0600,
// FORGE_DATA_HOME aware), NEVER travels in bundles (allowlist default-deny).
//
// trust.go —— 节点信任 store（docs/design/node-identity.md §3）：已知对端及其公钥
// 与信任 profile，外加把本机切入团队档（bundle 导入验签强制）的 require-signed
// 开关。信任建立是显式命令式 TOFU：`forge trust add` 展示指纹、人带外确认（SSH
// known_hosts / Syncthing introduction 先例）。store 在 ~/.forge/trust.json
// （0600，FORGE_DATA_HOME 感知），永不随 bundle 旅行（allowlist 默认拒绝）。

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/MjxUpUp/Forge/internal/forgedata"
)

// TrustedPeer is one known node.
//
// TrustedPeer 是一个已知节点。
type TrustedPeer struct {
	PublicKey string    `json:"public_key"` // base64 ed25519
	Label     string    `json:"label,omitempty"`
	Profile   string    `json:"profile"` // personal | team（v1 展示语义；强制力度由 RequireSigned 承担）
	AddedAt   time.Time `json:"added_at"`
}

// TrustStore is ~/.forge/trust.json.
//
// TrustStore 即 ~/.forge/trust.json。
type TrustStore struct {
	Peers         map[string]TrustedPeer `json:"peers"`
	RequireSigned bool                   `json:"require_signed"` // 团队档总开关
}

// BundleSig is the sidecar signature block (bundle.tar.gz.sig) over the bundle's
// whole-file sha256 hex digest.
//
// BundleSig 是 sidecar 签名块（bundle.tar.gz.sig），覆盖 bundle 整文件 sha256 hex。
type BundleSig struct {
	NodeID    string `json:"node_id"`
	PublicKey string `json:"public_key"` // 声明的签名公钥（与 trust store 不一致时 store 胜）
	Sig       string `json:"sig"`        // base64 ed25519 over digest bytes
}

// SigVerdict is the bundle-signature verification outcome.
//
// SigVerdict 是 bundle 签名验证结果。
type SigVerdict int

const (
	SigMissing       SigVerdict = iota // 无 sidecar
	SigVerified                        // 已知对端 + 验签通过
	SigUnknownSigner                   // 有签名但签名者不在 trust store
	SigInvalid                         // 验签失败/公钥不一致（任何 profile 都拒绝）
	SigRejected                        // 团队档（RequireSigned）下的缺失/未知签名者
)

func (v SigVerdict) String() string {
	switch v {
	case SigMissing:
		return `missing`
	case SigVerified:
		return `verified`
	case SigUnknownSigner:
		return `unknown-signer`
	case SigInvalid:
		return `invalid`
	case SigRejected:
		return `rejected`
	}
	return `?`
}

// NewTrustStore returns an empty store (personal profile: RequireSigned=false).
//
// NewTrustStore 返回空 store（个人档：RequireSigned=false）。
func NewTrustStore() *TrustStore {
	return &TrustStore{Peers: map[string]TrustedPeer{}}
}

func trustPath() (string, error) {
	home, err := forgedata.GlobalHome()
	if err != nil {
		return ``, err
	}
	return filepath.Join(home, `trust.json`), nil
}

// LoadTrustStore reads the store; a missing file is an empty store (not an error).
//
// LoadTrustStore 读 store；文件缺失返回空 store（不是错误）。
func LoadTrustStore() (*TrustStore, error) {
	p, err := trustPath()
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return NewTrustStore(), nil
	}
	if err != nil {
		return nil, err
	}
	var ts TrustStore
	if err := json.Unmarshal(raw, &ts); err != nil {
		return nil, fmt.Errorf(`trust.json 损坏: %w`, err)
	}
	if ts.Peers == nil {
		ts.Peers = map[string]TrustedPeer{}
	}
	return &ts, nil
}

// SaveTrustStore persists atomically with 0600 perms.
//
// SaveTrustStore 原子落盘，权限 0600。
func SaveTrustStore(ts *TrustStore) error {
	p, err := trustPath()
	if err != nil {
		return err
	}
	raw, err := json.MarshalIndent(ts, ``, `  `)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), `trust-*.json.tmp`)
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(0600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, p)
}

// Add registers a peer (TOFU). NodeID must be shape-valid and consistent with the
// given public key — a mismatch means someone is being handed a wrong key.
//
// Add 登记对端（TOFU）。NodeID 必须形态合法且与给定公钥一致——不一致意味着有人
// 拿到了错的钥匙。
func (ts *TrustStore) Add(nodeID, pubB64, label, profile string) error {
	if !ValidNodeID(nodeID) {
		return fmt.Errorf(`非法 node_id %q`, nodeID)
	}
	pub, err := decodePub(pubB64)
	if err != nil {
		return err
	}
	if got := DeriveNodeID(pub); got != nodeID {
		return fmt.Errorf(`node_id %q 与公钥不匹配（推导得 %q）——拒绝登记`, nodeID, got)
	}
	if profile != `personal` && profile != `team` {
		return fmt.Errorf(`profile 必须是 personal|team`)
	}
	ts.Peers[nodeID] = TrustedPeer{PublicKey: pubB64, Label: label, Profile: profile, AddedAt: time.Now().UTC()}
	return nil
}

// Remove unregisters a peer.
//
// Remove 注销对端。
func (ts *TrustStore) Remove(nodeID string) error {
	if _, ok := ts.Peers[nodeID]; !ok {
		return fmt.Errorf(`节点 %q 不在 trust store`, nodeID)
	}
	delete(ts.Peers, nodeID)
	return nil
}

// Peer looks up a node.
//
// Peer 查询节点。
func (ts *TrustStore) Peer(nodeID string) (TrustedPeer, bool) {
	p, ok := ts.Peers[nodeID]
	return p, ok
}

// VerifyBundleSig applies the verdict matrix (see trust_test.go for the pinned
// matrix). The store's copy of the public key is authoritative — the key in the sig
// block is a self-claim and is cross-checked against it.
//
// VerifyBundleSig 应用判定矩阵（矩阵由 trust_test.go 钉死）。store 里的公钥是
// 权威——sig 块里的公钥是自声明，与它对验。
func (ts *TrustStore) VerifyBundleSig(digestHex string, sig *BundleSig) SigVerdict {
	if sig == nil {
		if ts.RequireSigned {
			return SigRejected
		}
		return SigMissing
	}
	peer, ok := ts.Peers[sig.NodeID]
	if !ok {
		if ts.RequireSigned {
			return SigRejected
		}
		return SigUnknownSigner
	}
	if sig.PublicKey != peer.PublicKey {
		return SigInvalid // sig 块自声明公钥与 store 不符——拒绝
	}
	if !Verify(peer.PublicKey, []byte(digestHex), sig.Sig) {
		return SigInvalid
	}
	return SigVerified
}

// decodePub decodes a base64 ed25519 public key.
//
// decodePub 解码 base64 ed25519 公钥。
func decodePub(pubB64 string) ([]byte, error) {
	id := &Identity{PublicKey: pubB64}
	pub, err := id.PublicKeyBytes()
	if err != nil {
		return nil, err
	}
	return pub, nil
}
