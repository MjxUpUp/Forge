package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/registry"
)

// writeRekeyFixture 种分裂身份数据目录对的一侧。
func writeRekeyFixture(t *testing.T, dir string, files map[string]string) {
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

// checklogLine 造一条带指定 RFC3339 时间戳的 checklog JSONL 行。
func checklogLine(ts, detail string) string {
	return `{"check":"task-guard","passed":true,"checked":true,"tool_name":"Edit","detail":"` + detail + `","recorded_at":"` + ts + `"}`
}

// TestRegistryRekey is the FORGE_DATA_HOME-isolated e2e for `forge registry rekey`: two key data dirs with interleaved-timestamp JSONL logs, conflicting and unique tasks, a live-session anchor, and a protocol.yml conflict.
//
// TestRegistryRekey 是 `forge registry rekey` 的 FORGE_DATA_HOME 隔离 e2e：两个
// key 数据目录、时间戳交错的 JSONL 日志、冲突与独有任务、活会话锚文件、
// protocol.yml 冲突。断言：合并日志按时间戳有序且事件不丢、tasks 取并集且冲突
// 保留 to 侧、dry-run 不落盘、from 目录以备份形式存活（不删除）、注册表移除
// from key 条目。
func TestRegistryRekey(t *testing.T) {
	home := t.TempDir()
	t.Setenv(`FORGE_DATA_HOME`, home)
	fromKey, toKey := `aaaa1111bbbb`, `cccc2222dddd`
	fromDir := forgedata.RootDir(fromKey)
	toDir := forgedata.RootDir(toKey)

	writeRekeyFixture(t, fromDir, map[string]string{
		`tasks/task-a.json`:      `{"ref":"task-a","side":"from"}`,
		`tasks/task-b.json`:      `{"ref":"task-b","side":"from"}`,
		`checklog.jsonl`:         checklogLine(`2026-08-18T10:00:00Z`, `from-1`) + "\n" + checklogLine(`2026-08-18T10:02:00Z`, `from-2`) + "\n",
		`sessions/s1.json`:       `{"session":"s1"}`,
		`protocol.yml`:           `side: from`,
		`active-task-ref-sess42`: `fix/x`,
	})
	writeRekeyFixture(t, toDir, map[string]string{
		`tasks/task-b.json`: `{"ref":"task-b","side":"to"}`,
		`checklog.jsonl`: checklogLine(`2026-08-18T09:59:00Z`, `to-1`) + "\n" +
			checklogLine(`2026-08-18T10:01:00Z`, `to-2`) + "\n" +
			checklogLine(`2026-08-18T10:03:00Z`, `to-3`) + "\n",
		`protocol.yml`: `side: to`,
	})

	// 注册表：from/to key 的活项目目录（存活 = 路径存在）。
	liveFrom := mkLiveForgeDir(t)
	liveTo := mkLiveForgeDir(t)
	regData, err := json.Marshal(registry.File{Projects: []registry.Entry{
		{Path: liveFrom, Key: fromKey},
		{Path: liveTo, Key: toKey},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, `projects.json`), append(regData, '\n'), 0644); err != nil {
		t.Fatal(err)
	}

	setFlags := func(dryRun bool) {
		for name, v := range map[string]string{`from`: fromKey, `to`: toKey} {
			if err := registryRekeyCmd.Flags().Set(name, v); err != nil {
				t.Fatal(err)
			}
		}
		dr := `false`
		if dryRun {
			dr = `true`
		}
		if err := registryRekeyCmd.Flags().Set(`dry-run`, dr); err != nil {
			t.Fatal(err)
		}
	}

	// --- dry-run: plans everything, touches nothing ---
	var dryBuf bytes.Buffer
	registryRekeyCmd.SetOut(&dryBuf)
	setFlags(true)
	if err := runRegistryRekey(registryRekeyCmd, nil); err != nil {
		t.Fatalf(`dry-run RunE: %v`, err)
	}
	if !strings.Contains(dryBuf.String(), `merge`) || !strings.Contains(dryBuf.String(), `dry-run`) {
		t.Errorf(`dry-run 输出应列出 merge 计划并标注 dry-run: %s`, dryBuf.String())
	}
	if _, err := os.Stat(fromDir); err != nil {
		t.Errorf(`dry-run 后 from 目录应仍在: %v`, err)
	}
	if data, _ := os.ReadFile(filepath.Join(toDir, `checklog.jsonl`)); strings.Count(string(data), "\n") != 3 {
		t.Errorf(`dry-run 后 to 侧 checklog 应不变（3 行），实际: %q`, string(data))
	}

	// --- real run ---
	var buf bytes.Buffer
	registryRekeyCmd.SetOut(&buf)
	setFlags(false)
	if err := runRegistryRekey(registryRekeyCmd, nil); err != nil {
		t.Fatalf(`RunE: %v`, err)
	}

	// JSONL 合并：5 个事件、按时间戳有序、不丢。
	merged, err := os.ReadFile(filepath.Join(toDir, `checklog.jsonl`))
	if err != nil {
		t.Fatal(err)
	}
	var details []string
	var prev string
	for _, line := range strings.Split(strings.TrimSpace(string(merged)), "\n") {
		var e struct {
			RecordedAt string `json:"recorded_at"`
			Detail     string `json:"detail"`
		}
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf(`合并行非法 JSON: %q: %v`, line, err)
		}
		if prev != `` && e.RecordedAt < prev {
			t.Errorf(`合并后时间戳乱序: %q 在 %q 之后`, e.RecordedAt, prev)
		}
		prev = e.RecordedAt
		details = append(details, e.Detail)
	}
	if len(details) != 5 {
		t.Fatalf(`合并后应有 5 个事件，实际 %d: %v`, len(details), details)
	}
	wantOrder := []string{`to-1`, `from-1`, `to-2`, `from-2`, `to-3`}
	for i, w := range wantOrder {
		if details[i] != w {
			t.Errorf(`合并顺序[%d]=%q，期望 %q（全序 %v）`, i, details[i], w, details)
		}
	}

	// tasks 并集；冲突保留 to 侧。
	if _, err := os.Stat(filepath.Join(toDir, `tasks`, `task-a.json`)); err != nil {
		t.Errorf(`task-a 应并入 to 侧: %v`, err)
	}
	if data, _ := os.ReadFile(filepath.Join(toDir, `tasks`, `task-b.json`)); !strings.Contains(string(data), `"side":"to"`) {
		t.Errorf(`task-b 冲突应保留 to 侧，实际: %s`, string(data))
	}

	// 子目录并集 + 锚文件已搬（to 侧原本没有）+ protocol 冲突保留 to 侧。
	if _, err := os.Stat(filepath.Join(toDir, `sessions`, `s1.json`)); err != nil {
		t.Errorf(`sessions/s1.json 应并入: %v`, err)
	}
	if data, err := os.ReadFile(filepath.Join(toDir, `active-task-ref-sess42`)); err != nil || string(data) != `fix/x` {
		t.Errorf(`锚文件应只搬不并: data=%q err=%v`, string(data), err)
	}
	if data, _ := os.ReadFile(filepath.Join(toDir, `protocol.yml`)); string(data) != `side: to` {
		t.Errorf(`protocol.yml 冲突应保留 to 侧，实际: %q`, string(data))
	}

	// from 目录以 to 内备份形式存活（不删除），冲突的 from 侧文件随之保留。
	if _, err := os.Stat(fromDir); !os.IsNotExist(err) {
		t.Errorf(`from 目录原位应已不存在（已移入备份）`)
	}
	backups, err := filepath.Glob(filepath.Join(toDir, `.rekey-backup-*`))
	if err != nil || len(backups) != 1 {
		t.Fatalf(`应有 1 个备份目录: %v err=%v`, backups, err)
	}
	if data, err := os.ReadFile(filepath.Join(backups[0], `protocol.yml`)); err != nil || string(data) != `side: from` {
		t.Errorf(`备份中应保留 from 侧 protocol.yml: data=%q err=%v`, string(data), err)
	}
	if data, err := os.ReadFile(filepath.Join(backups[0], `tasks`, `task-b.json`)); err != nil || !strings.Contains(string(data), `"side":"from"`) {
		t.Errorf(`备份中应保留 from 侧 task-b: data=%q err=%v`, string(data), err)
	}

	// 注册表：from key 条目移除，to key 保留。
	regAfter, err := os.ReadFile(filepath.Join(home, `projects.json`))
	if err != nil {
		t.Fatal(err)
	}
	var f registry.File
	if err := json.Unmarshal(regAfter, &f); err != nil {
		t.Fatal(err)
	}
	if len(f.Projects) != 1 || f.Projects[0].Key != toKey {
		t.Errorf(`注册表应只剩 to key 条目: %+v`, f.Projects)
	}
}
