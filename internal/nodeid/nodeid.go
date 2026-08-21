// Package nodeid is the machine identity store: one ed25519 keypair per machine at
// ~/.forge/node.json (FORGE_DATA_HOME overridable), node_id = public-key fingerprint
// (`fnode_<32hex>` = sha256(pub)[:16] hex). Identity IS the public key, so signature
// verification doubles as identity proof — no separate "who owns this id" challenge.
//
// Design decisions and rationale live in docs/design/node-identity.md; comments here
// keep only local rationale.
//
// Trust boundary: the private key never leaves this machine (0600, user-level home,
// outside every project DataDir so bundles can never carry it — projectsync packs
// DataDir only). rotation_chain is a RESERVED format field: v1 always serializes as
// [] and no rotate command exists yet, but every writer must preserve the field so
// key rotation later is a chain append, not an identity break.
//
// Package nodeid 是机器身份 store：每台机器一对 ed25519 密钥，落在
// ~/.forge/node.json（FORGE_DATA_HOME 可覆盖），node_id = 公钥指纹
// （`fnode_<32hex>` = sha256(公钥) 前 16 字节 hex）。身份即公钥，验签即身份证明。
//
// 设计决策与依据见 docs/design/node-identity.md；注释只留局部 rationale。
package nodeid

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/forgedata"
)

// Identity is one machine's node identity as persisted at ~/.forge/node.json.
//
// Identity 是一台机器的节点身份，持久化于 ~/.forge/node.json。
type Identity struct {
	NodeID        string         `json:"node_id"`     // fnode_<32hex>，公钥指纹
	PublicKey     string         `json:"public_key"`  // base64 ed25519 公钥
	PrivateKey    string         `json:"private_key"` // base64 ed25519 私钥（0600，永不外泄）
	CreatedAt     time.Time      `json:"created_at"`
	RotationChain []RotationLink `json:"rotation_chain"` // 预留格式，v1 恒 []
}

// RotationLink is one reserved key-rotation record: the NEW public key signed by the
// OLD private key, so a trust store can accept identity continuity across rotation.
// v1 writes none; the field exists so adding rotation never changes the schema.
//
// RotationLink 是预留的密钥轮换记录：新公钥 + 旧私钥签名，trust store 验链后接受
// 身份延续。v1 不产生任何记录；字段先存在，加轮换时 schema 不变。
type RotationLink struct {
	PublicKey string    `json:"public_key"` // 新公钥 base64
	Sig       string    `json:"sig"`        // 旧私钥对新公钥字节的签名 base64
	RotatedAt time.Time `json:"rotated_at"`
}

// ValidNodeID reports whether s matches fnode_<32 lowercase hex>. Hand-rolled
// prefix+length+hex loop (no regexp), matching the tight-allowlist style of
// forgedata.ReadProjectID — attacker-controlled node ids must be shape-checked
// before use.
//
// ValidNodeID 报告 s 是否符合 fnode_<32 小写 hex>。手写 prefix+长度+hex 循环
// （不用 regexp），与 forgedata.ReadProjectID 的紧 allowlist 风格一致——攻击者
// 可控的 node id 必须先过形态检查再使用。
func ValidNodeID(s string) bool {
	const prefix = `fnode_`
	if len(s) != len(prefix)+32 || !strings.HasPrefix(s, prefix) {
		return false
	}
	for _, c := range s[len(prefix):] {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// DeriveNodeID derives the fingerprint node id from a public key:
// "fnode_" + hex(sha256(pub)[:16]).
//
// DeriveNodeID 从公钥推导指纹 node id："fnode_" + hex(sha256(pub)[:16])。
func DeriveNodeID(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	return `fnode_` + hex.EncodeToString(sum[:16])
}

// Path returns the identity file path (~/.forge/node.json, FORGE_DATA_HOME aware).
//
// Path 返回身份文件路径（~/.forge/node.json，FORGE_DATA_HOME 感知）。
func Path() (string, error) {
	home, err := forgedata.GlobalHome()
	if err != nil {
		return ``, err
	}
	return filepath.Join(home, `node.json`), nil
}

// Generate creates a fresh identity in memory (not persisted).
//
// Generate 在内存中生成新身份（不落盘）。
func Generate() (*Identity, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf(`generate ed25519: %w`, err)
	}
	return &Identity{
		NodeID:        DeriveNodeID(pub),
		PublicKey:     base64.StdEncoding.EncodeToString(pub),
		PrivateKey:    base64.StdEncoding.EncodeToString(priv),
		CreatedAt:     time.Now().UTC(),
		RotationChain: []RotationLink{},
	}, nil
}

// Load reads the persisted identity and verifies node_id == derive(public_key).
// A missing file is an error; use LoadOrCreate to generate on first run.
//
// Load 读取已持久化的身份并校验 node_id == derive(public_key)。
// 文件缺失是错误；首跑生成走 LoadOrCreate。
func Load() (*Identity, error) {
	p, err := Path()
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		return nil, fmt.Errorf(`read node identity: %w`, err)
	}
	var id Identity
	if err := json.Unmarshal(raw, &id); err != nil {
		return nil, fmt.Errorf(`parse node identity: %w`, err)
	}
	if err := id.CheckConsistent(); err != nil {
		return nil, err
	}
	// Defense in depth: a node.json copied in with loose perms (0644 from a naive cp)
	// stays loose forever if Load never rewrites — tighten on read.
	//
	// 防御纵深：宽松权限拷入的 node.json（naive cp 带来 0644）若 Load 永不重写
	// 就一直松着——读时收紧。
	if fi, statErr := os.Stat(p); statErr == nil && fi.Mode().Perm() != 0600 {
		_ = os.Chmod(p, 0600)
	}
	return &id, nil
}

// LoadOrCreate loads the identity, generating and persisting one (0600) if absent.
//
// LoadOrCreate 加载身份；缺失时生成并以 0600 落盘。
func LoadOrCreate() (*Identity, error) {
	id, err := Load()
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	id, err = Generate()
	if err != nil {
		return nil, err
	}
	if err := id.Save(); err != nil {
		return nil, err
	}
	return id, nil
}

// Save persists the identity atomically with 0600 perms.
//
// Save 原子落盘身份，权限 0600。
func (id *Identity) Save() error {
	p, err := Path()
	if err != nil {
		return err
	}
	if err := id.CheckConsistent(); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(id, ``, `  `)
	if err != nil {
		return fmt.Errorf(`marshal node identity: %w`, err)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), `node-*.json.tmp`)
	if err != nil {
		return fmt.Errorf(`mktemp node identity: %w`, err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op after successful rename
	if err := tmp.Chmod(0600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf(`chmod node identity: %w`, err)
	}
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return fmt.Errorf(`write node identity: %w`, err)
	}
	// fsync before rename: a crash window must never surface a truncated node.json
	// (identity loss → node_id change → machine attribution silently forks).
	//
	// rename 前 fsync：崩溃窗口绝不露出半截 node.json（身份丢失 → node_id 变更 →
	// 机器归因静默分叉）。
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf(`fsync node identity: %w`, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf(`close node identity: %w`, err)
	}
	if err := os.Rename(tmpName, p); err != nil {
		return fmt.Errorf(`rename node identity: %w`, err)
	}
	return nil
}

// CheckConsistent verifies node_id derives from the stored public key and the key
// pair matches — a tampered or corrupted node.json must fail loud, not silently
// mis-attribute events to a wrong machine.
//
// CheckConsistent 校验 node_id 由所存公钥推导且密钥对匹配——被篡改/损坏的
// node.json 必须响亮失败，不能静默把事件归错机器。
func (id *Identity) CheckConsistent() error {
	if !ValidNodeID(id.NodeID) {
		return fmt.Errorf(`invalid node_id %q`, id.NodeID)
	}
	pub, err := id.PublicKeyBytes()
	if err != nil {
		return err
	}
	if got := DeriveNodeID(pub); got != id.NodeID {
		return fmt.Errorf(`node_id %q inconsistent with public key (derive %q)`, id.NodeID, got)
	}
	priv, err := id.privateKeyBytes()
	if err != nil {
		return err
	}
	if !priv.Public().(ed25519.PublicKey).Equal(pub) {
		return errors.New(`private key does not match public key`)
	}
	// v1 contract: rotation_chain always serializes as [] — a foreign/buggy writer
	// persisting null must fail loud here, not leak null into `node show` output.
	//
	// v1 契约：rotation_chain 恒序列化为 []——外来/缺陷写者落盘 null 必须在此响亮
	// 失败，不让 null 漏进 `node show` 输出。
	if id.RotationChain == nil {
		return errors.New(`rotation_chain must be [] (never null)`)
	}
	return nil
}

// PublicKeyBytes decodes the base64 public key.
//
// PublicKeyBytes 解码 base64 公钥。
func (id *Identity) PublicKeyBytes() (ed25519.PublicKey, error) {
	pub, err := base64.StdEncoding.DecodeString(id.PublicKey)
	if err != nil {
		return nil, fmt.Errorf(`decode public key: %w`, err)
	}
	if len(pub) != ed25519.PublicKeySize {
		return nil, fmt.Errorf(`public key length %d, want %d`, len(pub), ed25519.PublicKeySize)
	}
	return ed25519.PublicKey(pub), nil
}

// privateKeyBytes decodes the base64 private key.
//
// privateKeyBytes 解码 base64 私钥。
func (id *Identity) privateKeyBytes() (ed25519.PrivateKey, error) {
	priv, err := base64.StdEncoding.DecodeString(id.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf(`decode private key: %w`, err)
	}
	if len(priv) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf(`private key length %d, want %d`, len(priv), ed25519.PrivateKeySize)
	}
	return ed25519.PrivateKey(priv), nil
}

// Sign signs msg with the node private key, returning base64.
//
// Sign 用节点私钥签名 msg，返回 base64。
func (id *Identity) Sign(msg []byte) (string, error) {
	priv, err := id.privateKeyBytes()
	if err != nil {
		return ``, err
	}
	return base64.StdEncoding.EncodeToString(ed25519.Sign(priv, msg)), nil
}

// Verify checks a base64 signature against a base64 public key and message.
// Malformed inputs return false (verification is a boolean verdict, not an error).
//
// Verify 校验 base64 签名与 base64 公钥、消息。畸形输入返回 false
// （验签是布尔判定，不是错误）。
func Verify(pubB64 string, msg []byte, sigB64 string) bool {
	pub, err := base64.StdEncoding.DecodeString(pubB64)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return false
	}
	sig, err := base64.StdEncoding.DecodeString(sigB64)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return false
	}
	return ed25519.Verify(ed25519.PublicKey(pub), msg, sig)
}
