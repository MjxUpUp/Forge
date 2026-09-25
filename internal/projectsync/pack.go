package projectsync

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// PackInput parameterizes Pack.
//
// PackInput 参数化 Pack。
type PackInput struct {
	DataDir      string
	Extra        []string // opt-in sensitive stores (IncludeQuarantine / IncludeHazards)
	Origin       Origin
	ForgeVersion string
	Now          time.Time // ExportedAt stamp (injected for test determinism)
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
	for _, rel := range rels {
		p := filepath.Join(in.DataDir, filepath.FromSlash(rel))
		info, serr := os.Stat(p)
		if serr != nil {
			return nil, fmt.Errorf(`stat %s: %w`, rel, serr)
		}
		sum, herr := fileSHA256(p)
		if herr != nil {
			return nil, fmt.Errorf(`hash %s: %w`, rel, herr)
		}
		m.Files = append(m.Files, FileEntry{Path: rel, SHA256: sum, Size: info.Size()})
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

	for _, fe := range m.Files {
		if err := tw.WriteHeader(&tar.Header{
			Name: `data/` + fe.Path, Mode: 0644, Size: fe.Size,
		}); err != nil {
			return nil, err
		}
		f, ferr := os.Open(filepath.Join(in.DataDir, filepath.FromSlash(fe.Path)))
		if ferr != nil {
			return nil, ferr
		}
		// io.Copy 在 header 声明的 Size 边界上：stat 与读之间缩水的文件产生短写
		// 和损坏 tar——错误在此暴露而非留到导入方。
		_, cerr := io.Copy(tw, f)
		f.Close()
		if cerr != nil {
			return nil, fmt.Errorf(`写入 %s: %w`, fe.Path, cerr)
		}
	}
	return m, nil
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
