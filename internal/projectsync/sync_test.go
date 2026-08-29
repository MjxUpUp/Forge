package projectsync

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// sync_test.go —— 项目 bundle 传输的安全守卫（project-sync）：allowlist 默认拒绝、
// tar 路径安全、sha256 完整性、格式版本守卫、账本幂等。中文字符串用 raw 字面量。

// seedDataDir 种一个含全部已知文件类的 DataDir 形状目录树——可移植与机器本地
// 兼备——allowlist 必须精确挑出可移植子集。
func seedDataDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		// 可移植集（应入 bundle）
		`tasks/feat-x.json`:       `{"task_ref":"feat/x"}`,
		`checklog.jsonl`:          `{"recorded_at":"2026-08-18T10:00:00Z"}`,
		`checklog-20260817.jsonl`: `{"recorded_at":"2026-08-17T10:00:00Z"}`,
		`toollog.jsonl`:           `{"timestamp":"2026-08-18T10:00:00Z"}`,
		`toollog-20260817.jsonl`:  `{"timestamp":"2026-08-17T10:00:00Z"}`,
		`sessions.jsonl`:          `{"started_at":"2026-08-18T09:00:00Z"}`,
		`sessions/sid-1.json`:     `{"session_id":"sid-1"}`,
		`act/conclusions.jsonl`:   `{"completed_at":"2026-08-18T12:00:00Z"}`,
		`stamps/main.stamp`:       `{"diff_hash":"abc"}`,
		`protocol.yml`:            `scope: default`,
		// 机器本地/敏感集（默认绝不入 bundle）
		`tasks/feat-x.lock`:               `lock`,
		`stamps/hook-deploy`:              `1755494400`,
		`hooks/pre-tool.sh`:               `#!/bin/sh`,
		`freeze/state.json`:               `{"paths":["/Users/a/x"]}`,
		`session.json`:                    `{"session_id":"live"}`,
		`active-task-ref-sess42`:          `feat/x`,
		`active-task-ref`:                 `feat/x`,
		`.task-complete-grace-sess42`:     `1755494400`,
		`.resume-stale-sess42`:            `1755494400`,
		`.cold-start-injected-sess42`:     `{}`,
		`.task-verify-throttle.last`:      `1755494400`,
		`.sync-version`:                   `1.35.0`,
		`.migration-meta.json`:            `{"schema_version":1}`,
		`imports.jsonl`:                   `{"bundle_id":"old"}`,
		`.rekey-backup-20260818-000000/x`: `backup`,
		`quarantine/sess1/src.go`:         `package main`,
		`hazards/events.jsonl`:            `{"command":"curl -H token"}`,
		`hazards/fp1.json`:                `{"command":"danger"}`,
	}
	for rel, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// TestExportAllowlist_DefaultDeny：默认导出清单恰好是可移植集——一切机器本地
// sentinel/锚/戳与敏感 store 均被排除；未来的未知文件按构造落在 allowlist 之外。
func TestExportAllowlist_DefaultDeny(t *testing.T) {
	dir := seedDataDir(t)
	rels, err := ExportFiles(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, r := range rels {
		got[r] = true
	}
	want := []string{
		`tasks/feat-x.json`, `checklog.jsonl`, `checklog-20260817.jsonl`,
		`toollog.jsonl`, `toollog-20260817.jsonl`, `sessions.jsonl`,
		`sessions/sid-1.json`, `act/conclusions.jsonl`, `stamps/main.stamp`, `protocol.yml`,
	}
	if len(got) != len(want) {
		t.Errorf(`默认清单应恰为 %d 项，got %d: %v`, len(want), len(got), rels)
	}
	for _, w := range want {
		if !got[w] {
			t.Errorf(`可移植文件 %s 应在清单中（got %v）`, w, rels)
		}
	}
	for _, banned := range []string{
		`tasks/feat-x.lock`, `stamps/hook-deploy`, `hooks/pre-tool.sh`, `freeze/state.json`,
		`session.json`, `active-task-ref-sess42`, `active-task-ref`,
		`.task-complete-grace-sess42`, `.resume-stale-sess42`, `.cold-start-injected-sess42`,
		`.task-verify-throttle.last`, `.sync-version`, `.migration-meta.json`,
		`imports.jsonl`, `.rekey-backup-20260818-000000/x`, `quarantine/sess1/src.go`,
		`hazards/events.jsonl`, `hazards/fp1.json`,
	} {
		if got[banned] {
			t.Errorf(`机器本地/敏感文件 %s 不得进默认清单`, banned)
		}
	}
}

// TestExportAllowlist_ExplicitIncludes：--include quarantine / hazards 才入，且只入这些。
func TestExportAllowlist_ExplicitIncludes(t *testing.T) {
	dir := seedDataDir(t)
	rels, err := ExportFiles(dir, []string{`quarantine`})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range rels {
		if r == `quarantine/sess1/src.go` {
			found = true
		}
		if strings.HasPrefix(r, `hazards/`) {
			t.Errorf(`未 include 的 hazards 不应出现: %s`, r)
		}
	}
	if !found {
		t.Errorf(`include quarantine 后应含 quarantine 文件，got %v`, rels)
	}

	rels, err = ExportFiles(dir, []string{`hazards`})
	if err != nil {
		t.Fatal(err)
	}
	haz, quar := 0, 0
	for _, r := range rels {
		if strings.HasPrefix(r, `hazards/`) {
			haz++
		}
		if strings.HasPrefix(r, `quarantine/`) {
			quar++
		}
	}
	if haz != 2 || quar != 0 {
		t.Errorf(`include hazards 应只含 2 个 hazards 文件，got haz=%d quar=%d: %v`, haz, quar, rels)
	}
}

// TestStripNonAllowlisted_ForgedManifestPayloads：导入侧默认拒绝——可移植集之外
// 的一切从 staging 剥除，含选入型 store（import 没有 --include 门槛可满足）与
// 伪造 manifest 可能列出的机器本地文件。
func TestStripNonAllowlisted_ForgedManifestPayloads(t *testing.T) {
	dir := seedDataDir(t)
	removed, err := StripNonAllowlisted(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, r := range removed {
		got[r] = true
	}
	for _, banned := range []string{
		`imports.jsonl`, `active-task-ref-sess42`, `active-task-ref`, `session.json`,
		`.sync-version`, `hooks/pre-tool.sh`, `freeze/state.json`, `tasks/feat-x.lock`,
		`stamps/hook-deploy`, `.task-complete-grace-sess42`,
		`quarantine/sess1/src.go`, `hazards/events.jsonl`, `hazards/fp1.json`,
	} {
		if !got[banned] {
			t.Errorf(`%s 应被剥除（未在 removed 清单）: %v`, banned, removed)
		}
		if _, serr := os.Stat(filepath.Join(dir, filepath.FromSlash(banned))); !os.IsNotExist(serr) {
			t.Errorf(`%s 剥除后仍存在于 staging`, banned)
		}
	}
	// 可移植集不动
	if _, serr := os.Stat(filepath.Join(dir, `tasks`, `feat-x.json`)); serr != nil {
		t.Errorf(`可移植任务文件不应被剥除: %v`, serr)
	}
	if _, serr := os.Stat(filepath.Join(dir, `protocol.yml`)); serr != nil {
		t.Errorf(`protocol.yml 不应被剥除: %v`, serr)
	}
}

// packFixture 把 seedDataDir 打进内存，返回字节与 manifest。
func packFixture(t *testing.T, extra []string) ([]byte, *Manifest) {
	t.Helper()
	dir := seedDataDir(t)
	var buf bytes.Buffer
	m, err := Pack(PackInput{
		DataDir:      dir,
		Extra:        extra,
		Origin:       Origin{Hostname: `h1`, User: `u1`, Root: `/repo`, Key: `abc123456789`, KeyMode: `path`},
		ForgeVersion: `1.36.0`,
		Now:          time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC),
	}, &buf)
	if err != nil {
		t.Fatalf(`Pack: %v`, err)
	}
	return buf.Bytes(), m
}

// TestPackUnpack_RoundTrip：pack → unpack 在 dest/data/ 下逐字节还原每个文件，
// manifest 完整性字段齐备。
func TestPackUnpack_RoundTrip(t *testing.T) {
	raw, m := packFixture(t, nil)
	dest := t.TempDir()
	got, err := Unpack(bytes.NewReader(raw), dest)
	if err != nil {
		t.Fatalf(`Unpack: %v`, err)
	}
	if got.BundleID != m.BundleID || got.BundleID == `` {
		t.Errorf(`BundleID 应回读一致且非空: %q vs %q`, got.BundleID, m.BundleID)
	}
	if len(got.Files) != len(m.Files) {
		t.Fatalf(`文件数不一致: %d vs %d`, len(got.Files), len(m.Files))
	}
	for _, fe := range got.Files {
		data, err := os.ReadFile(filepath.Join(dest, `data`, filepath.FromSlash(fe.Path)))
		if err != nil {
			t.Fatalf(`回读 %s: %v`, fe.Path, err)
		}
		if int64(len(data)) != fe.Size {
			t.Errorf(`%s 尺寸不一致`, fe.Path)
		}
	}
	// 抽查一个文件内容
	b, _ := os.ReadFile(filepath.Join(dest, `data`, `tasks`, `feat-x.json`))
	if string(b) != `{"task_ref":"feat/x"}` {
		t.Errorf(`内容应逐字节还原，got %s`, b)
	}
}

// buildTar 直接按给定条目构造 tar.gz（绕过 Pack），用于对抗性 fixture。
func buildTar(t *testing.T, entries []struct {
	name string
	body string
	typ  byte
}) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		hdr := &tar.Header{Name: e.name, Mode: 0644, Size: int64(len(e.body)), Typeflag: e.typ}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if e.typ == tar.TypeReg {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// manifestJSONFor 构造一个哈希正确的 manifest.json 正文，让对抗测试每次只变一个变量。
func manifestJSONFor(t *testing.T, formatVersion int, files map[string]string) string {
	t.Helper()
	m := Manifest{FormatVersion: formatVersion, BundleID: `bid-test`}
	for name, body := range files {
		sum := sha256Hex([]byte(body))
		m.Files = append(m.Files, FileEntry{Path: strings.TrimPrefix(name, `data/`), SHA256: sum, Size: int64(len(body))})
	}
	data, err := json.Marshal(&m)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestUnpack_RejectsPathEscape(t *testing.T) {
	cases := []struct {
		name string
		terr string
	}{
		{`相对穿越`, `data/../evil.txt`},
		{`绝对路径`, `data//etc/passwd`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			raw := buildTar(t, []struct {
				name string
				body string
				typ  byte
			}{
				{`manifest.json`, manifestJSONFor(t, 1, nil), tar.TypeReg},
				{c.terr, `x`, tar.TypeReg},
			})
			if _, err := Unpack(bytes.NewReader(raw), t.TempDir()); err == nil {
				t.Errorf(`路径穿越条目 %s 应被拒绝`, c.terr)
			}
		})
	}
}

func TestUnpack_RejectsSymlink(t *testing.T) {
	raw := buildTar(t, []struct {
		name string
		body string
		typ  byte
	}{
		{`manifest.json`, manifestJSONFor(t, 1, nil), tar.TypeReg},
		{`data/link`, `../../evil`, tar.TypeSymlink},
	})
	if _, err := Unpack(bytes.NewReader(raw), t.TempDir()); err == nil {
		t.Error(`symlink 条目应被拒绝`)
	}
}

func TestUnpack_RejectsUnlistedEntry(t *testing.T) {
	raw := buildTar(t, []struct {
		name string
		body string
		typ  byte
	}{
		{`manifest.json`, manifestJSONFor(t, 1, map[string]string{`data/a.txt`: `a`}), tar.TypeReg},
		{`data/a.txt`, `a`, tar.TypeReg},
		{`data/extra.txt`, `surprise`, tar.TypeReg},
	})
	if _, err := Unpack(bytes.NewReader(raw), t.TempDir()); err == nil {
		t.Error(`manifest 未列出的条目应被拒绝（列表外写入）`)
	}
}

func TestUnpack_RejectsTamperedContent(t *testing.T) {
	raw := buildTar(t, []struct {
		name string
		body string
		typ  byte
	}{
		{`manifest.json`, manifestJSONFor(t, 1, map[string]string{`data/a.txt`: `original`}), tar.TypeReg},
		{`data/a.txt`, `tampered!`, tar.TypeReg},
	})
	if _, err := Unpack(bytes.NewReader(raw), t.TempDir()); err == nil {
		t.Error(`内容与 sha256 不符应被拒绝`)
	}
}

func TestUnpack_VersionGuard(t *testing.T) {
	for _, v := range []int{0, 2} {
		t.Run(map[int]string{0: `v0 缺失`, 2: `v2 未来`}[v], func(t *testing.T) {
			raw := buildTar(t, []struct {
				name string
				body string
				typ  byte
			}{
				{`manifest.json`, manifestJSONFor(t, v, nil), tar.TypeReg},
			})
			if _, err := Unpack(bytes.NewReader(raw), t.TempDir()); err == nil {
				t.Errorf(`format_version=%d 应被拒绝（0 与 >当前 双拒）`, v)
			}
		})
	}
}

func TestUnpack_MissingListedEntryFails(t *testing.T) {
	raw := buildTar(t, []struct {
		name string
		body string
		typ  byte
	}{
		{`manifest.json`, manifestJSONFor(t, 1, map[string]string{`data/a.txt`: `a`, `data/b.txt`: `b`}), tar.TypeReg},
		{`data/a.txt`, `a`, tar.TypeReg},
		// b.txt 声明了却缺失
	})
	if _, err := Unpack(bytes.NewReader(raw), t.TempDir()); err == nil {
		t.Error(`manifest 声明但 tar 缺失的条目应报错（不完整 bundle）`)
	}
}

// TestLedger_HasAndAppend: the machine-local import ledger is append-only JSONL with
// tolerant reads (corrupt lines skipped, never fatal).
//
// TestLedger_HasAndAppend：机器本地导入账本是 append-only JSONL，读取容错（坏行
// 跳过、绝不致命）。
func TestLedger_HasAndAppend(t *testing.T) {
	dir := t.TempDir()
	ok, err := HasImportedBundle(dir, `bid-1`)
	if err != nil || ok {
		t.Fatalf(`空账本应 false 无错: ok=%v err=%v`, ok, err)
	}
	rec := ImportRecord{BundleID: `bid-1`, SHA256: `deadbeef`, FromKey: `k1`, ToKey: `k2`, ImportedAt: time.Now()}
	if err := AppendImportRecord(dir, rec); err != nil {
		t.Fatal(err)
	}
	ok, err = HasImportedBundle(dir, `bid-1`)
	if err != nil || !ok {
		t.Fatalf(`追加后应命中: ok=%v err=%v`, ok, err)
	}
	ok, _ = HasImportedBundle(dir, `bid-2`)
	if ok {
		t.Error(`不同 bundle_id 不应命中`)
	}
	// 坏行容错：手工塞一行垃圾再查——依旧可用
	f, err := os.OpenFile(filepath.Join(dir, `imports.jsonl`), os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(f, "not-json\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()
	ok, err = HasImportedBundle(dir, `bid-1`)
	if err != nil || !ok {
		t.Errorf(`坏行不应致命: ok=%v err=%v`, ok, err)
	}
}
