package projectsync

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/MjxUpUp/Forge/internal/util"
)

// PackInput parameterizes Pack.
//
// PackInput 参数化 Pack。
type PackInput struct {
	DataDir      string
	Extra        []string // opt-in sensitive stores (IncludeQuarantine / IncludeHazards)
	Origin       Origin
	ForgeVersion string
	// Redact 在打包时把每个文本文件过一遍 util.Redact（本机主目录/用户名/
	// hostname 与已知 secret 形态 → 占位符）。bundle 会被推到远端或交给他人，
	// 而 checklog/toollog 里天然带绝对路径与编辑片段。
	Redact bool
	Now    time.Time // ExportedAt stamp (injected for test determinism)
}

// bundleEntry 是一个待写入 tar 的条目：源路径可能与 DataDir 不同（脱敏时会
// 先落一份临时副本），Size/SHA256 一律以实际写出的字节为准。
type bundleEntry struct {
	rel  string
	path string
	size int64
	sum  string
	tmp  bool
}

// Pack writes the bundle (manifest.json + data/<rel>) as a gzip tar stream to w and returns the manifest it wrote.
//
// Pack 把 bundle（manifest.json + data/<rel>）以 gzip tar 流写到 w，返回写出的
// manifest。每个文件两遍：先 hash 后流式写——bundle 载荷（rotated 日志）可能很
// 大，除 manifest 外不整体驻内存。文件排序保证 bundle 确定。
func Pack(in PackInput, w io.Writer) (*Manifest, error) {
	rels, err := ExportFiles(in.DataDir, in.Extra)
	if err != nil {
		return nil, err
	}
	bundleID, err := NewBundleID()
	if err != nil {
		return nil, err
	}
	m := &Manifest{
		FormatVersion: FormatVersion,
		ForgeVersion:  in.ForgeVersion,
		BundleID:      bundleID,
		ExportedAt:    in.Now,
		Origin:        in.Origin,
		Includes:      in.Extra,
	}
	if in.Redact {
		m.Origin = RedactedOrigin(in.Origin)
	}

	entries := make([]bundleEntry, 0, len(rels))
	for _, rel := range rels {
		e, err := prepareEntry(in.DataDir, rel, in.Redact)
		if err != nil {
			return nil, err
		}
		entries = append(entries, e)
		if e.tmp {
			defer os.Remove(e.path)
		}
		m.Files = append(m.Files, FileEntry{Path: e.rel, SHA256: e.sum, Size: e.size})
	}

	gz := gzip.NewWriter(w)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()

	manifestBody, merr := marshalManifest(m)
	if merr != nil {
		return nil, merr
	}
	if err := tw.WriteHeader(&tar.Header{
		Name: `manifest.json`, Mode: 0644, Size: int64(len(manifestBody)),
	}); err != nil {
		return nil, err
	}
	if _, err := tw.Write(manifestBody); err != nil {
		return nil, err
	}

	for _, fe := range entries {
		if err := tw.WriteHeader(&tar.Header{
			Name: `data/` + fe.rel, Mode: 0644, Size: fe.size,
		}); err != nil {
			return nil, err
		}
		f, ferr := os.Open(fe.path)
		if ferr != nil {
			return nil, ferr
		}
		// io.Copy 在 header 声明的 Size 边界上：stat 与读之间缩水的文件产生短写
		// 和损坏 tar——错误在此暴露而非留到导入方。
		_, cerr := io.Copy(tw, f)
		f.Close()
		if cerr != nil {
			return nil, fmt.Errorf(`写入 %s: %w`, fe.rel, cerr)
		}
	}
	return m, nil
}

// prepareEntry 计算一个 bundle 条目的实际字节大小与哈希。Redact 时先把文件流式
// 脱敏到临时文件（日志可能很大，不整体驻内存），manifest 的 Size/SHA256 因此
// 描述的是脱敏后的载荷——导入方按 manifest 校验，必须与 tar 内字节一致。
func prepareEntry(dataDir, rel string, redact bool) (bundleEntry, error) {
	src := filepath.Join(dataDir, filepath.FromSlash(rel))
	if !redact {
		info, err := os.Stat(src)
		if err != nil {
			return bundleEntry{}, fmt.Errorf(`stat %s: %w`, rel, err)
		}
		sum, err := fileSHA256(src)
		if err != nil {
			return bundleEntry{}, fmt.Errorf(`hash %s: %w`, rel, err)
		}
		return bundleEntry{rel: rel, path: src, size: info.Size(), sum: sum}, nil
	}

	tmp, err := os.CreateTemp(``, `.forge-pack-*.tmp`)
	if err != nil {
		return bundleEntry{}, err
	}
	tmpName := tmp.Name()
	if err := scrubFile(src, tmp); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return bundleEntry{}, fmt.Errorf(`脱敏 %s: %w`, rel, err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return bundleEntry{}, err
	}
	info, err := os.Stat(tmpName)
	if err != nil {
		os.Remove(tmpName)
		return bundleEntry{}, err
	}
	sum, err := fileSHA256(tmpName)
	if err != nil {
		os.Remove(tmpName)
		return bundleEntry{}, err
	}
	return bundleEntry{rel: rel, path: tmpName, size: info.Size(), sum: sum, tmp: true}, nil
}

// scrubFile 按行把 src 脱敏写入 dst。按行处理保住 JSONL 的结构，也避免把
// 超大日志整体读进内存；超长行由 bufio.Reader 的 ReadString 兜住（不设
// Scanner 的 token 上限）。
func scrubFile(src string, dst io.Writer) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 64*1024)
	for {
		line, rerr := r.ReadString('\n')
		if line != `` {
			if _, werr := io.WriteString(dst, util.Redact(line)); werr != nil {
				return werr
			}
		}
		if rerr == io.EOF {
			return nil
		}
		if rerr != nil {
			return rerr
		}
	}
}

// RedactedOrigin 把源机器身份字段替换为占位符。Key/KeyMode/ProjectID 保留：
// 它们是对端判定 lineage 的依据，且不携带用户名或绝对路径。
func RedactedOrigin(o Origin) Origin {
	o.Hostname = `<host>`
	o.User = `<user>`
	if o.Root != `` {
		o.Root = `<home>`
	}
	return o
}

// fileSHA256 把文件流过 sha256（大日志不整体驻内存）。
func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return ``, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ``, err
	}
	return fmt.Sprintf(`%x`, h.Sum(nil)), nil
}
