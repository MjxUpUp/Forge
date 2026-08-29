package registry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/MjxUpUp/Forge/internal/forgedata"
)

// audit_test.go —— Audit() 发现守卫（FORGE_DATA_HOME 隔离）。中文字符串用 raw
// 字面量。

// seedRepo 建 git 形状 repo 目录（可 adopt：含 .git 目录）。
func seedRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, `.git`), 0755); err != nil {
		t.Fatal(err)
	}
	return root
}

// writeRegistry 写全局注册表文件。
func writeRegistry(t *testing.T, entries []Entry) {
	t.Helper()
	home, err := forgedata.GlobalHome()
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(File{Projects: entries})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, `projects.json`), append(data, '\n'), 0644); err != nil {
		t.Fatal(err)
	}
}

// kindsOf 抽取发现类型供断言。
func kindsOf(findings []Finding) map[string]Finding {
	out := map[string]Finding{}
	for _, f := range findings {
		out[f.Kind] = f
	}
	return out
}

// TestAudit_CleanWhenConsistent：注册的 repo 数据在派生 key 下且无多余 → 零发现。
func TestAudit_CleanWhenConsistent(t *testing.T) {
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	root := seedRepo(t)
	key, err := forgedata.Key(root)
	if err != nil {
		t.Fatal(err)
	}
	dir := forgedata.RootDir(key)
	if err := os.MkdirAll(filepath.Join(dir, `tasks`), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, `tasks`, `feat-x.json`), []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}
	writeRegistry(t, []Entry{{Path: root, Key: key}})
	if got := Audit(); len(got) != 0 {
		t.Errorf(`一致状态应零发现，got %+v`, got)
	}
}

// TestAudit_KeyDriftAfterIDAdoption：条目仍带路径 key 而派生已是 ID key 且旧目录
// 有载荷 → key-drift。
func TestAudit_KeyDriftAfterIDAdoption(t *testing.T) {
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	root := seedRepo(t)
	pathKey, err := forgedata.KeyFromPath(root)
	if err != nil {
		t.Fatal(err)
	}
	oldDir := forgedata.RootDir(pathKey)
	if err := os.MkdirAll(filepath.Join(oldDir, `tasks`), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oldDir, `tasks`, `feat-a.json`), []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}
	writeRegistry(t, []Entry{{Path: root, Key: pathKey}})

	// adopt ID（数据未迁移的半途状态——audit 的目标场景）
	id := `fpid_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa`
	if err := os.WriteFile(filepath.Join(root, forgedata.ProjectIDFileName), []byte(id+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	findings := kindsOf(Audit())
	fd, ok := findings[AuditKeyDrift]
	if !ok {
		t.Fatalf(`应报 key-drift，got %+v`, findings)
	}
	if fd.Path != root {
		t.Errorf(`key-drift 应指向项目路径，got %s`, fd.Path)
	}
}

// TestAudit_OrphanDataDirIgnoresBackupShells：无注册条目的有载荷 key 目录被标记；
// 只剩 .rekey-backup 壳的不标。
func TestAudit_OrphanDataDirIgnoresBackupShells(t *testing.T) {
	home := t.TempDir()
	t.Setenv(`FORGE_DATA_HOME`, home)

	live := filepath.Join(home, `projects`, `orphan000001`)
	if err := os.MkdirAll(filepath.Join(live, `tasks`), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(live, `tasks`, `x.json`), []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}
	shell := filepath.Join(home, `projects`, `shell00000002`, `.rekey-backup-20260818-000000`, `y.json`)
	if err := os.MkdirAll(filepath.Dir(shell), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(shell, []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}

	findings := kindsOf(Audit())
	if _, ok := findings[AuditOrphanDataDir]; !ok {
		t.Fatalf(`有载荷孤儿目录应被标记，got %+v`, findings)
	}
	// 壳目录不得误报（Finding.Key 定位到 shell00000002 即误报）
	for _, f := range Audit() {
		if f.Kind == AuditOrphanDataDir && f.Key == `shell00000002` {
			t.Error(`纯 .rekey-backup 壳目录不应报 orphan（非活数据）`)
		}
	}
}

// TestAudit_IDCollisionAndInvalidID：两个 repo 共享同一 .forge-project-id →
// id-collision；畸形 ID 文件 → invalid-id。
func TestAudit_IDCollisionAndInvalidID(t *testing.T) {
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	a, b := seedRepo(t), seedRepo(t)
	shared := `fpid_000000000000000000000000000000ff`
	for _, r := range []string{a, b} {
		if err := os.WriteFile(filepath.Join(r, forgedata.ProjectIDFileName), []byte(shared+"\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	bad := seedRepo(t)
	if err := os.WriteFile(filepath.Join(bad, forgedata.ProjectIDFileName), []byte("not-valid\n"), 0644); err != nil {
		t.Fatal(err)
	}
	writeRegistry(t, []Entry{{Path: a}, {Path: b}, {Path: bad}})

	findings := kindsOf(Audit())
	if _, ok := findings[AuditIDCollision]; !ok {
		t.Errorf(`共享 ID 的两个 repo 应报 id-collision，got %+v`, findings)
	}
	if _, ok := findings[AuditInvalidID]; !ok {
		t.Errorf(`畸形 ID 应报 invalid-id，got %+v`, findings)
	}
}
