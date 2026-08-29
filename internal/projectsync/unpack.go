package projectsync

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// MaxBundleBytes is the total-payload ceiling Unpack enforces (2 GiB): the
// per-entry size is bounded by the manifest, but a forged manifest could declare
// a huge total and stream gigabytes to disk (a filler DoS against /tmp). Cheap
// bound; legitimate project bundles are orders of magnitude below it.
//
// MaxBundleBytes 是 Unpack 强制的总载荷上限（2 GiB）：单条目尺寸被 manifest 约束，
// 但伪造的 manifest 可声明巨大总量向磁盘流数据（对 /tmp 的填充 DoS）。廉价上限；
// 合法项目 bundle 低于它若干数量级。
const MaxBundleBytes = 2 << 30

// Unpack reads a bundle stream, validates it against its own manifest, and writes
// the payloads under <destDir>/data/. Security posture (mirrors cli/update.go
// extractBinary): regular files only (symlink/hardlink headers rejected), no
// absolute paths, no `..` traversal, every tar entry must be listed in the manifest
// and every listed entry must be present, format version double-guarded (0 and
// >FormatVersion both refused), per-file sha256+size verified while streaming.
// destDir must be outside FORGE_DATA_HOME (the caller owns that decision — staging
// must not be discoverable by DataDir scanners).
//
// Unpack 读取 bundle 流，对照其 manifest 校验，把载荷写到 <destDir>/data/ 下。
// 安全姿态（镜像 cli/update.go extractBinary）：仅普通文件（拒绝 symlink/hardlink
// header）、拒绝绝对路径、拒绝 `..` 穿越、tar 条目必须在 manifest 列表内且列表
// 条目必须齐全、格式版本双拒（0 与 >FormatVersion）、流式校验逐文件 sha256+size。
// destDir 必须在 FORGE_DATA_HOME 之外（该决策归调用方——staging 不能被 DataDir
// 扫描器发现）。
func Unpack(r io.Reader, destDir string) (*Manifest, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf(`bundle 不是合法 gzip 流: %w`, err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)

	// First entry MUST be manifest.json — the guard needs the list before any
	// payload lands.
	//
	// 首条目必须是 manifest.json——守卫需要先有列表再落任何载荷。
	hdr, err := tr.Next()
	if err != nil {
		return nil, fmt.Errorf(`bundle 为空或损坏: %w`, err)
	}
	if hdr.Name != `manifest.json` || hdr.Typeflag != tar.TypeReg {
		return nil, fmt.Errorf(`bundle 首条目应为 manifest.json，实得 %q (type %d)`, hdr.Name, hdr.Typeflag)
	}
	manifestBody, err := io.ReadAll(tr)
	if err != nil {
		return nil, fmt.Errorf(`读 manifest 失败: %w`, err)
	}
	var m Manifest
	if err := json.Unmarshal(manifestBody, &m); err != nil {
		return nil, fmt.Errorf(`manifest 不是合法 JSON（确认是 forge project export 产物）: %w`, err)
	}
	if m.FormatVersion == 0 || m.FormatVersion > FormatVersion {
		return nil, fmt.Errorf(`bundle format_version=%d 不被支持（本机支持 %d）；确认是 forge project export 产出，或升级 Forge 后再导入`, m.FormatVersion, FormatVersion)
	}
	if m.BundleID == `` {
		return nil, fmt.Errorf(`manifest 缺 bundle_id（文件损坏或非 Forge bundle）`)
	}
	listed := make(map[string]FileEntry, len(m.Files))
	var total int64
	for _, fe := range m.Files {
		if fe.Path == `` || fe.SHA256 == `` {
			return nil, fmt.Errorf(`manifest 文件条目不完整（path/sha256 缺失）`)
		}
		if fe.Size < 0 {
			return nil, fmt.Errorf(`manifest 条目 %s 尺寸非法（%d）`, fe.Path, fe.Size)
		}
		total += fe.Size
		if total > MaxBundleBytes {
			return nil, fmt.Errorf(`bundle 声明总量 %d 超上限 %d（拒绝落地）`, total, MaxBundleBytes)
		}
		listed[fe.Path] = fe
	}

	dataDir := filepath.Join(destDir, `data`)
	seen := make(map[string]bool, len(listed))
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf(`读 bundle 条目失败: %w`, err)
		}
		if hdr.Typeflag != tar.TypeReg {
			return nil, fmt.Errorf(`bundle 条目 %q 非普通文件（type %d）——拒绝 symlink/hardlink`, hdr.Name, hdr.Typeflag)
		}
		rel := strings.TrimPrefix(hdr.Name, `data/`)
		if hdr.Name == rel { // no data/ prefix at all
			return nil, fmt.Errorf(`bundle 条目 %q 缺 data/ 前缀`, hdr.Name)
		}
		fe, ok := listed[rel]
		if !ok {
			return nil, fmt.Errorf(`bundle 条目 %q 不在 manifest 列表内——拒绝列表外写入`, rel)
		}
		if hdr.Size != fe.Size {
			return nil, fmt.Errorf(`%s 尺寸与 manifest 不符（tar=%d manifest=%d）`, rel, hdr.Size, fe.Size)
		}

		dst := filepath.Join(dataDir, filepath.FromSlash(rel))
		if !safeJoin(dataDir, rel) {
			return nil, fmt.Errorf(`bundle 条目路径不安全: %q`, rel)
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
			return nil, err
		}
		sum, werr := verifyAndWrite(tr, dst, hdr.Size)
		if werr != nil {
			return nil, fmt.Errorf(`落盘 %s 失败: %w`, rel, werr)
		}
		if sum != fe.SHA256 {
			// The partial file is garbage — remove it so a retry doesn't find a
			// half-verified artifact.
			//
			// 半个文件是垃圾——删除，使重试不会发现半验证产物。
			os.Remove(dst)
			return nil, fmt.Errorf(`%s 内容校验失败（sha256 不符，bundle 可能被篡改或损坏）`, rel)
		}
		seen[rel] = true
	}
	for path := range listed {
		if !seen[path] {
			return nil, fmt.Errorf(`manifest 声明的 %s 在 bundle 中缺失（不完整 bundle）`, path)
		}
	}
	return &m, nil
}

// safeJoin reports whether slash-path rel, joined under base, stays inside base
// (no leading slash, no `..` segment, no drive/UNC shape on Windows).
//
// safeJoin 判断斜杠路径 rel 拼进 base 后是否仍在 base 内（无前导斜杠、无 `..`
// 段、无 Windows 盘符/UNC 形态）。
func safeJoin(base, rel string) bool {
	if rel == `` || strings.HasPrefix(rel, `/`) || strings.HasPrefix(rel, `\`) {
		return false
	}
	clean := filepath.FromSlash(rel)
	for _, seg := range strings.Split(clean, string(filepath.Separator)) {
		if seg == `..` || seg == `` {
			return false
		}
		// On Windows a colon in a segment is either a drive letter (C:) or an NTFS
		// alternate data stream (file.json:stream) — the ADS form would let a bundle
		// write content INTO a hidden stream of an allowlisted-looking file, invisible
		// to WalkDir/StripNonAllowlisted, then ride the file-level move into the live
		// DataDir. Reject ANY colon, both shapes.
		//
		// Windows 下段内冒号要么是盘符（C:）要么是 NTFS 备用数据流
		//（file.json:stream）——ADS 形态会把内容写进看似合法文件的隐藏流，
		// WalkDir/StripNonAllowlisted 不可见，随后随文件级 move 混进活 DataDir。
		// 两种形态一并拒绝。
		if strings.ContainsRune(seg, ':') {
			return false
		}
	}
	joined := filepath.Join(base, clean)
	return strings.HasPrefix(joined, base+string(filepath.Separator))
}

// verifyAndWrite streams exactly size bytes to dst while hashing; the caller
// compares the returned hex sha256 against the manifest entry.
//
// verifyAndWrite 把恰好 size 字节流到 dst 并同时 hash；调用方将返回的 hex sha256
// 与 manifest 条目比对。
func verifyAndWrite(r io.Reader, dst string, size int64) (string, error) {
	f, err := os.Create(dst)
	if err != nil {
		return ``, err
	}
	h := sha256.New()
	// Bound the copy at the declared size: a lying header cannot make us read past
	// the entry into the next one.
	//
	// 拷贝以声明尺寸为界：撒谎的 header 不能让我们读过头闯进下一条目。
	if _, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(r, size)); err != nil {
		f.Close()
		os.Remove(dst)
		return ``, err
	}
	if err := f.Close(); err != nil {
		return ``, err
	}
	return fmt.Sprintf(`%x`, h.Sum(nil)), nil
}

// marshalManifest serializes the manifest (stable shape, no HTML escaping of +/-
// characters in hashes).
//
// marshalManifest 序列化 manifest（形状稳定，不转义 hash 里的 +/- 字符）。
func marshalManifest(m *Manifest) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	if err := enc.Encode(m); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
