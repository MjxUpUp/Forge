// Package projectsync is the cross-machine project-data transport (project-sync):
// allowlist-selected bundle packing/unpacking with a checksummed manifest, and the
// machine-local import ledger. The identity layer (repo-born project ID) and the
// merge semantics (datamerge) live in their own packages; this package only moves
// bytes safely.
//
// Bundle layout (tar.gz):
//
//	manifest.json          — first entry, format-guarded, lists every file + sha256
//	data/<rel>             — file payloads at their DataDir-relative paths
//
// Trust model: sha256 protects against corruption, NOT malice (no signature). The
// execution safety line is elsewhere — verify-acceptance --trust-foreign and the
// import-time lineage check (same key ⇒ same developer's other machine).
//
// Chinese strings use raw string literals (Windows quote-corruption rule).
//
// Package projectsync 是项目数据跨机器传输层（project-sync）：allowlist 圈定的
// bundle 打包/解包 + 校验和 manifest + 机器本地导入账本。身份层（repo-born
// project ID）与合并语义（datamerge）在各自包里；本包只负责安全搬运字节。
//
// Bundle 布局（tar.gz）：
//
//	manifest.json          — 首条目，版本守卫，列出每个文件 + sha256
//	data/<rel>             — 文件载荷，按 DataDir 相对路径摆放
//
// 信任模型：sha256 防损坏不防恶意（无签名）。执行安全线在别处——
// verify-acceptance --trust-foreign 与导入时 lineage 判定（同 key ⇒ 同一开发者的
// 另一台机器）。
package projectsync

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"
)

// FormatVersion is the bundle format version. Import refuses a manifest whose
// version is 0 (missing/hand-edited) or greater than this (future format) — the same
// forward-compatibility guard task_port.go applies to task bundles: a future format
// must never be silently mis-parsed as the current one.
//
// FormatVersion 是 bundle 格式版本。import 拒绝版本为 0（缺失/手改）或大于此值
// （未来格式）的 manifest——与 task_port.go 对 task bundle 的前向兼容守卫同款：
// 未来格式绝不能被静默误解析为当前格式。
const FormatVersion = 1

// Manifest is the bundle envelope. Every file entry is checksummed; Origin carries
// the source machine's identity derivation so the importer can decide lineage
// (same key ⇒ trusted sync) without any network round-trip.
//
// Manifest 是 bundle 信封。每个文件条目带校验和；Origin 携带源机器的身份推导，
// 使导入方无需任何网络往返即可判定 lineage（同 key ⇒ 受信同步）。
type Manifest struct {
	FormatVersion int         `json:"format_version"`
	ForgeVersion  string      `json:"forge_version,omitempty"`
	BundleID      string      `json:"bundle_id"`
	ExportedAt    time.Time   `json:"exported_at"`
	Origin        Origin      `json:"origin"`
	Files         []FileEntry `json:"files"`
	// Includes records the extra includes used at export (quarantine/hazards), so a
	// consumer can tell a sensitive bundle from a default one.
	//
	// Includes 记录导出时的额外包含（quarantine/hazards），使消费方能区分敏感
	// bundle 与默认 bundle。
	Includes []string `json:"includes,omitempty"`
}

// Origin is the source-machine provenance block.
//
// Origin 是源机器溯源块。
type Origin struct {
	Hostname  string `json:"hostname,omitempty"`
	User      string `json:"user,omitempty"`
	Root      string `json:"root,omitempty"`
	Key       string `json:"key"`
	KeyMode   string `json:"key_mode"` // path | id
	ProjectID string `json:"project_id,omitempty"`
}

// FileEntry is one bundled file with its integrity fields.
//
// FileEntry 是一个被打包的文件及其完整性字段。
type FileEntry struct {
	Path   string `json:"path"` // DataDir-relative, slash-separated
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// NewBundleID generates a random bundle id (16 bytes hex). Bundle ids need
// uniqueness, not secrecy.
//
// NewBundleID 生成随机 bundle id（16 字节 hex）。bundle id 需要唯一性，不需要保密性。
func NewBundleID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return ``, fmt.Errorf(`生成 bundle id 失败: %w`, err)
	}
	return hex.EncodeToString(b[:]), nil
}

// sha256Hex returns the hex sha256 of b.
//
// sha256Hex 返回 b 的 sha256 hex。
func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
