package datamerge

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/taskpipeline"
)

// merge_test.go —— Options 在 legacy rekey 语义之外的新能力守卫（legacy 语义由
// cli/registry_rekey_test.go 端到端钉住）。中文字符串用 raw 字面量。

// seedDir 在 dir 下写 rel→content 文件。
func seedDir(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

// logLine 造一条带时间戳的 JSONL 行。
func logLine(ts, v string) string {
	return `{"recorded_at":"` + ts + `","detail":"` + v + `"}`
}

// taskJSON 序列化一个最小真实 TaskState。
func taskJSON(ref string, decisions []taskpipeline.Decision) string {
	s := taskpipeline.TaskState{TaskRef: ref, Decisions: decisions}
	data, _ := json.Marshal(&s)
	return string(data)
}

// TestIsTaskFile_SlashContract：Dirs 内的 rel 路径在分类前归一为斜杠形态
// （filepath.ToSlash）——isTaskFile 的契约。Windows 上 filepath.Rel 产出反斜杠；
// 不做归一所有 tasks/*.json 都会跌进普通搬移/跳过路径。此测试钉住调用方现在
// 保证的分类器契约。
func TestIsTaskFile_SlashContract(t *testing.T) {
	cases := map[string]bool{
		`tasks/feat-x.json`:     true,
		`tasks/sub/feat-x.json`: true,
		`tasks/feat-x.lock`:     false, // per-task 锁残留不是状态
		`tasks/feat-x.jsonl`:    false,
		`act/conclusions.jsonl`: false,
		`tasksx/feat-x.json`:    false, // 前缀必须含斜杠边界
		`tasks\feat-x.json`:     false, // 反斜杠输入必须拒——调用方（Dirs）负责先 ToSlash 归一，本契约显式钉住
	}
	for rel, want := range cases {
		if got := isTaskFile(rel); got != want {
			t.Errorf(`isTaskFile(%q) = %v, want %v`, rel, got, want)
		}
	}
}

// TestDirs_DedupExactLinesIdempotent：DedupExactLines 下同一源 JSONL 合并两次，
// 第一次合并后目标字节不变（第二次把重复提供的行全部去重）。这是
// A→B→A→B 重导入保证。
func TestDirs_DedupExactLinesIdempotent(t *testing.T) {
	from := t.TempDir()
	to := t.TempDir()
	src := logLine(`2026-08-18T10:00:00Z`, `e1`) + "\n" + logLine(`2026-08-18T10:02:00Z`, `e2`) + "\n"
	seedDir(t, from, map[string]string{`checklog.jsonl`: src})
	seedDir(t, to, map[string]string{`checklog.jsonl`: logLine(`2026-08-18T09:59:00Z`, `e0`) + "\n"})

	// 第一轮：from 是一次性源；用 NoFromBackup + 复制 from，保持 from 可重放
	fromCopy1 := t.TempDir()
	seedDir(t, fromCopy1, map[string]string{`checklog.jsonl`: src})
	if _, err := Dirs(fromCopy1, to, Options{DedupExactLines: true, NoFromBackup: true}); err != nil {
		t.Fatalf(`第一轮合并: %v`, err)
	}
	after1, err := os.ReadFile(filepath.Join(to, `checklog.jsonl`))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(after1), "\n"); got != 3 {
		t.Fatalf(`第一轮应 3 行，got %d: %s`, got, after1)
	}

	// 第二轮：同样的源再合并一次 → 字节不变（全量重叠导出不产生重复事件）
	fromCopy2 := t.TempDir()
	seedDir(t, fromCopy2, map[string]string{`checklog.jsonl`: src})
	if _, err := Dirs(fromCopy2, to, Options{DedupExactLines: true, NoFromBackup: true}); err != nil {
		t.Fatalf(`第二轮合并: %v`, err)
	}
	after2, err := os.ReadFile(filepath.Join(to, `checklog.jsonl`))
	if err != nil {
		t.Fatal(err)
	}
	if string(after1) != string(after2) {
		t.Errorf(`重复导入应字节不变：`+"\n"+`一轮=%s`+"\n"+`二轮=%s`, after1, after2)
	}
}

// TestDirs_TaskUnionMergesStates：TaskUnion 下同 ref 任务冲突做状态合并（决策并
// 集、受信时门禁愈合走 MergeTaskStateSync），而非逐字保 to 侧文件。
func TestDirs_TaskUnionMergesStates(t *testing.T) {
	from := t.TempDir()
	to := t.TempDir()
	seedDir(t, from, map[string]string{
		`tasks/feat-x.json`: taskJSON(`feat/x`, []taskpipeline.Decision{{ID: `d-remote`, Content: `remote`}}),
	})
	seedDir(t, to, map[string]string{
		`tasks/feat-x.json`: taskJSON(`feat/x`, []taskpipeline.Decision{{ID: `d-local`, Content: `local`}}),
	})
	actions, err := Dirs(from, to, Options{TaskPolicy: TaskUnion, TrustResults: true})
	if err != nil {
		t.Fatal(err)
	}
	merged := false
	for _, a := range actions {
		if strings.Contains(a, `merge-task`) {
			merged = true
		}
	}
	if !merged {
		t.Fatalf(`应产生 merge-task 动作，actions=%v`, actions)
	}
	data, err := os.ReadFile(filepath.Join(to, `tasks`, `feat-x.json`))
	if err != nil {
		t.Fatal(err)
	}
	var s taskpipeline.TaskState
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf(`合并产物应是合法 TaskState: %v`+"\n"+`%s`, err, data)
	}
	if len(s.Decisions) != 2 {
		t.Errorf(`决策应并集为 2 条（local+remote），got %+v`, s.Decisions)
	}
}

// TestDirs_TaskUnionRefusesGarbageTask：TaskUnion 下不可解析为 TaskState 的
// tasks/*.json 不被采纳（绝不逐字进 tasks/）。
func TestDirs_TaskUnionRefusesGarbageTask(t *testing.T) {
	from := t.TempDir()
	to := t.TempDir()
	seedDir(t, from, map[string]string{
		`tasks/garbage.json`: `{"ref":"x","side":"from"}`, // 缺 task_ref 的非 TaskState
	})
	_, err := Dirs(from, to, Options{TaskPolicy: TaskUnion, TrustResults: true})
	if err != nil {
		t.Fatalf(`垃圾任务应被跳过而非让合并失败: %v`, err)
	}
	if _, serr := os.Stat(filepath.Join(to, `tasks`, `garbage.json`)); !os.IsNotExist(serr) {
		t.Errorf(`垃圾任务文件不应进入 to/tasks/`)
	}
}

// TestDirs_TaskSkip：TaskSkip 完全不动 tasks/*.json（不搬也不并），留给调用方自理。
func TestDirs_TaskSkip(t *testing.T) {
	from := t.TempDir()
	to := t.TempDir()
	seedDir(t, from, map[string]string{
		`tasks/feat-x.json`: taskJSON(`feat/x`, nil),
		`act/conclusions.jsonl`: `{"task_ref":"feat/x","completed_at":"` +
			time.Now().Format(time.RFC3339) + `","grade":"A"}` + "\n",
	})
	seedDir(t, to, map[string]string{
		`act/conclusions.jsonl`: `{"task_ref":"feat/old","completed_at":"2026-08-17T00:00:00Z","grade":"B"}` + "\n",
	})
	// 与 project import 的实际调用同参（MergeConclusions 开——import 侧专属）。
	if _, err := Dirs(from, to, Options{TaskPolicy: TaskSkip, MergeConclusions: true, NoFromBackup: true}); err != nil {
		t.Fatal(err)
	}
	if _, serr := os.Stat(filepath.Join(to, `tasks`, `feat-x.json`)); !os.IsNotExist(serr) {
		t.Error(`TaskSkip 不应搬移任务文件`)
	}
	// conclusions.jsonl 按时间戳合并（import 路径经 MergeConclusions 进合并集）
	data, err := os.ReadFile(filepath.Join(to, `act`, `conclusions.jsonl`))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(data), "\n"); got != 2 {
		t.Errorf(`conclusions.jsonl 应合并为 2 行，got %d: %s`, got, data)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if !strings.Contains(lines[0], `feat/old`) {
		t.Errorf(`按时间戳有序：旧行应在前，got %s`, lines[0])
	}
}

// TestDirs_ConclusionsGatedByOption：legacy rekey（零值 Options）把
// act/conclusions.jsonl 当普通文件（冲突保 to 侧——抽包前的精确行为）；
// 仅 MergeConclusions 才让它进时间戳合并。守住「rekey 语义零变化」契约。
func TestDirs_ConclusionsGatedByOption(t *testing.T) {
	from := t.TempDir()
	to := t.TempDir()
	seedDir(t, from, map[string]string{`act/conclusions.jsonl`: `{"task_ref":"a","completed_at":"2026-08-18T12:00:00Z","grade":"A"}` + "\n"})
	seedDir(t, to, map[string]string{`act/conclusions.jsonl`: `{"task_ref":"b","completed_at":"2026-08-17T00:00:00Z","grade":"B"}` + "\n"})

	// 零值 Options（rekey 路径）：冲突保 to 侧，from 的 A 结论不进
	if _, err := Dirs(from, to, Options{}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(to, `act`, `conclusions.jsonl`))
	if strings.Contains(string(data), `"task_ref":"a"`) {
		t.Errorf(`零值 Options 下 conclusions 冲突应保 to 侧（rekey 零变化），got %s`, data)
	}

	// MergeConclusions（import 路径）：时间戳有序合并，两条都在
	from2 := t.TempDir()
	seedDir(t, from2, map[string]string{`act/conclusions.jsonl`: `{"task_ref":"a","completed_at":"2026-08-18T12:00:00Z","grade":"A"}` + "\n"})
	if _, err := Dirs(from2, to, Options{MergeConclusions: true, DedupExactLines: true, NoFromBackup: true}); err != nil {
		t.Fatal(err)
	}
	data2, _ := os.ReadFile(filepath.Join(to, `act`, `conclusions.jsonl`))
	if !strings.Contains(string(data2), `"task_ref":"a"`) || !strings.Contains(string(data2), `"task_ref":"b"`) {
		t.Errorf(`MergeConclusions 下两条结论都应在且有序，got %s`, data2)
	}
	if strings.Count(string(data2), "\n") != 2 {
		t.Errorf(`合并后应恰 2 行，got %s`, data2)
	}
}

// TestDirs_NoFromBackupRemovesFromDir：NoFromBackup 合并后删除 from 目录，而不在
// 活的 to 目录里留 .rekey-backup。
func TestDirs_NoFromBackupRemovesFromDir(t *testing.T) {
	from := t.TempDir()
	to := t.TempDir()
	seedDir(t, from, map[string]string{
		`sessions/s1.json`: `{"session_id":"s1"}`,
		`protocol.yml`:     `side: from`,
	})
	seedDir(t, to, map[string]string{`protocol.yml`: `side: to`})
	if _, err := Dirs(from, to, Options{NoFromBackup: true}); err != nil {
		t.Fatal(err)
	}
	if _, serr := os.Stat(from); !os.IsNotExist(serr) {
		t.Error(`NoFromBackup 后 from 目录应被删除`)
	}
	backups, _ := filepath.Glob(filepath.Join(to, `.rekey-backup-*`))
	if len(backups) != 0 {
		t.Errorf(`NoFromBackup 不应留备份目录: %v`, backups)
	}
	// 无冲突文件仍并入
	if _, serr := os.Stat(filepath.Join(to, `sessions`, `s1.json`)); serr != nil {
		t.Errorf(`sessions/s1.json 应并入: %v`, serr)
	}
}

// writeBigFile 写一个 size 字节的伪随机内容文件（异或位移生成，不可压缩——
// 内容校验不能被「碰巧全零也相等」糊弄）。
func writeBigFile(t *testing.T, path string, size int64, seed uint64) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	w := bufio.NewWriterSize(f, 1<<16)
	buf := make([]byte, 1<<16)
	state := seed
	for written := int64(0); written < size; {
		n := int64(len(buf))
		if remaining := size - written; remaining < n {
			n = remaining
		}
		for i := range buf {
			state ^= state << 13
			state ^= state >> 7
			state ^= state << 17
			buf[i] = byte(state)
		}
		if _, err := w.Write(buf[:n]); err != nil {
			t.Fatal(err)
		}
		written += n
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
}

// fileDigest 是内容一致性校验的基准真值（SHA-256，比逐字节比对省内存——对流式
// 路径的测试本身也不该整读大文件）。
func fileDigest(t *testing.T, path string) string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// TestMoveFileStreamingFallbackContentPreserved 钉流式跨设备回退：10MB 大文件经
// copy+remove 路径搬移后内容逐位一致、源文件删除、dst 目录无 .tmp-* 残留。
// rename 失败注入用「dst 父目录尚不存在」实现（两平台 rename 到缺失父目录必败
// → 必走 fallback；旧实现的 AtomicWrite 同样先 MkdirAll，故该用例重构前后都过，
// 兼作前后等价钉）。
func TestMoveFileStreamingFallbackContentPreserved(t *testing.T) {
	from := t.TempDir()
	to := t.TempDir()
	src := filepath.Join(from, "bundle-payload.bin")
	writeBigFile(t, src, 10<<20, 0x9E3779B97F4A7C15)
	want := fileDigest(t, src)

	// dst 的父目录刻意不存在：首轮 os.Rename 必失败，fallback 路径确定性执行。
	dst := filepath.Join(to, "tasks", "nested", "payload-copy.bin")
	if err := MoveFile(src, dst); err != nil {
		t.Fatalf("MoveFile fallback 失败: %v", err)
	}
	if got := fileDigest(t, dst); got != want {
		t.Fatalf("10MB 内容不一致：got %s want %s（流式拷贝损坏数据）", got, want)
	}
	if _, serr := os.Stat(src); !os.IsNotExist(serr) {
		t.Error("fallback 成功后源文件应被删除")
	}
	residue, _ := filepath.Glob(filepath.Join(filepath.Dir(dst), ".tmp-*"))
	if len(residue) != 0 {
		t.Errorf("dst 目录不应残留 temp 文件: %v", residue)
	}
	if info, err := os.Stat(dst); err != nil {
		t.Fatal(err)
	} else if info.Size() != 10<<20 {
		t.Errorf("dst 大小 = %d, want %d", info.Size(), 10<<20)
	}
}

// TestMoveFileStreamingHelperNoResidueOnError 直接钉 helper 的清理契约：拷贝源
// 消失（open 后即删——Windows 上已打开文件仍可删）不足以让 io.Copy 失败的话，
// 退而求其次钉「目标不可写的报错路径不残留 temp」。用 dst 父目录是一个**文件**
// 制造 MkdirAll 失败：helper 必须报错且 to 下无 .tmp-*。
func TestMoveFileStreamingHelperNoResidueOnError(t *testing.T) {
	from := t.TempDir()
	to := t.TempDir()
	src := filepath.Join(from, "small.bin")
	if err := os.WriteFile(src, []byte("xyz"), 0644); err != nil {
		t.Fatal(err)
	}
	// 把 dst 的父目录占成一个文件：MkdirAll 必败。
	blocker := filepath.Join(to, "blocked")
	if err := os.WriteFile(blocker, []byte("not a dir"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := copyFileAtomic(src, filepath.Join(blocker, "dst.bin")); err == nil {
		t.Fatal("父目录被文件占据时 helper 必须报错")
	}
	residue, _ := filepath.Glob(filepath.Join(to, ".tmp-*"))
	if len(residue) != 0 {
		t.Errorf("失败路径不应残留 temp 文件: %v", residue)
	}
	// 源文件不动（MoveFile 层才负责 remove src；helper 只管拷贝）。
	if _, serr := os.Stat(src); os.IsNotExist(serr) {
		t.Error("helper 失败时源文件必须原样保留")
	}
}

// TestMoveFileRenamePathUnchanged 钉同卷快速路径：rename 成功时绝不走流式拷贝
// （dst 内容正确即可——rename 语义天然保内容）。
func TestMoveFileRenamePathUnchanged(t *testing.T) {
	from := t.TempDir()
	to := t.TempDir()
	src := filepath.Join(from, "plain.json")
	if err := os.WriteFile(src, []byte(`{"a":1}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := MoveFile(src, filepath.Join(to, "plain.json")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(to, "plain.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"a":1}` {
		t.Fatalf("rename 路径内容不符: %q", data)
	}
}
