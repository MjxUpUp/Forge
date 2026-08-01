package protocol

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/forgedata"
)

// TestSaveLoadRoundTrip verifies Save persists a loadable protocol.yml: with no
// project-level override, Save writes the user-level DataDir copy (creating the dir
// via util.AtomicWrite) and Load reads back an equivalent Protocol.
//
// TestSaveLoadRoundTrip 验证 Save 落盘的 protocol.yml 可加载：无项目级覆盖时
// Save 写用户级 DataDir 副本（经 util.AtomicWrite 自建目录），Load 读回等价 Protocol。
func TestSaveLoadRoundTrip(t *testing.T) {
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	dir := t.TempDir()
	want := DefaultProtocol()

	if err := Save(dir, want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(filepath.Join(forgedata.DataDirFor(dir), "protocol.yml")); err != nil {
		t.Fatalf("protocol.yml must exist in DataDir after Save: %v", err)
	}
	// Zero-project-write: Save must NOT create a project-level .forge/.
	//
	// 零项目写入：Save 不得创建项目级 .forge/。
	if _, err := os.Stat(filepath.Join(dir, ".forge")); !os.IsNotExist(err) {
		t.Fatalf("Save must not create project-level .forge/ (zero-project-write)")
	}

	got, err := Load(dir)
	if err != nil {
		t.Fatalf("Load after Save: %v", err)
	}
	if got.Version != want.Version {
		t.Errorf("Version = %q, want %q", got.Version, want.Version)
	}
	if len(got.Standards) != len(want.Standards) {
		t.Fatalf("Standards len = %d, want %d", len(got.Standards), len(want.Standards))
	}
	for i := range want.Standards {
		if got.Standards[i] != want.Standards[i] {
			t.Errorf("Standards[%d] = %+v, want %+v", i, got.Standards[i], want.Standards[i])
		}
	}
	if len(got.SessionRules) != len(want.SessionRules) {
		t.Fatalf("SessionRules len = %d, want %d", len(got.SessionRules), len(want.SessionRules))
	}
	for i := range want.SessionRules {
		if got.SessionRules[i] != want.SessionRules[i] {
			t.Errorf("SessionRules[%d] = %+v, want %+v", i, got.SessionRules[i], want.SessionRules[i])
		}
	}
}

// TestProjectLevelOverride pins the two-layer contract: when <dir>/.forge/protocol.yml
// exists (team-shared override, `forge init --project`), Load reads IT — not the
// DataDir copy — and Save writes back to it.
//
// TestProjectLevelOverride 钉死双层契约：<dir>/.forge/protocol.yml 存在时
// （团队共享覆盖，`forge init --project`），Load 读它而非 DataDir 副本，
// Save 也写回它。
func TestProjectLevelOverride(t *testing.T) {
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	dir := t.TempDir()

	// DataDir copy with default version; project-level override with a custom version.
	//
	// DataDir 副本用默认 version；项目级覆盖用自定义 version。
	if err := Save(dir, DefaultProtocol()); err != nil {
		t.Fatalf("Save DataDir copy: %v", err)
	}
	override := DefaultProtocol()
	override.Version = "team-override"
	if err := SaveProjectLevel(dir, override); err != nil {
		t.Fatalf("SaveProjectLevel: %v", err)
	}

	got, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Version != "team-override" {
		t.Errorf("Load 应读项目级覆盖 version=team-override，实得 %q", got.Version)
	}

	// Save must write back to the override (project-level), leaving the DataDir copy alone.
	//
	// Save 必须写回覆盖层（项目级），不动 DataDir 副本。
	override.Version = "team-override-v2"
	if err := Save(dir, override); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got2, err := Load(dir)
	if err != nil {
		t.Fatalf("Load after Save: %v", err)
	}
	if got2.Version != "team-override-v2" {
		t.Errorf("Save 应写回项目级覆盖，Load version 实得 %q", got2.Version)
	}
}

// TestSaveLoad_FailFastWhenDataDirUnresolvable pins the silent-mislocation fix: when
// the DataDir cannot be resolved (FORGE_DATA_HOME unset and os.UserHomeDir failing),
// Save/Load must return an explicit error instead of writing a bare "protocol.yml"
// into the process cwd.
//
// TestSaveLoad_FailFastWhenDataDirUnresolvable 钉死静默错位修复：DataDir 无法解析
// （FORGE_DATA_HOME 未设且 os.UserHomeDir 失败）时，Save/Load 必须显式报错，
// 而不是把裸 "protocol.yml" 写进进程 cwd。
func TestSaveLoad_FailFastWhenDataDirUnresolvable(t *testing.T) {
	t.Setenv(`FORGE_DATA_HOME`, ``)
	// Break os.UserHomeDir on every platform: unix reads HOME; Windows reads
	// USERPROFILE, then HOMEDRIVE+HOMEPATH.
	//
	// 全平台破坏 os.UserHomeDir：unix 读 HOME；Windows 读 USERPROFILE，再读
	// HOMEDRIVE+HOMEPATH。
	t.Setenv(`HOME`, ``)
	t.Setenv(`USERPROFILE`, ``)
	t.Setenv(`HOMEDRIVE`, ``)
	t.Setenv(`HOMEPATH`, ``)

	dir := t.TempDir()
	if err := Save(dir, DefaultProtocol()); err == nil {
		t.Fatal("Save 在 DataDir 不可解析时必须报错（拒绝写相对路径），实得 nil")
	}
	if _, err := Load(dir); err == nil {
		t.Fatal("Load 在 DataDir 不可解析时必须报错，实得 nil")
	}
	if _, err := os.Stat(`protocol.yml`); err == nil {
		t.Fatal("不得在进程 cwd 写入相对路径 protocol.yml")
	}
}

// TestEnsureDefault_CreatesWhenMissing: no protocol.yml anywhere → the DataDir copy
// is created from defaults and loads cleanly.
//
// TestEnsureDefault_CreatesWhenMissing：两处都无 protocol.yml → 从默认值创建
// DataDir 副本且可加载。
func TestEnsureDefault_CreatesWhenMissing(t *testing.T) {
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	dir := t.TempDir()

	if err := EnsureDefault(dir); err != nil {
		t.Fatalf("EnsureDefault: %v", err)
	}
	if _, err := os.Stat(filepath.Join(forgedata.DataDirFor(dir), "protocol.yml")); err != nil {
		t.Fatalf("EnsureDefault 应创建 DataDir 副本: %v", err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatalf("Load after EnsureDefault: %v", err)
	}
	if got.Scoring == nil {
		t.Error("EnsureDefault 写入的默认 protocol 应含 scoring 配置")
	}
	// Idempotent: a second run keeps the file (no rewrite, no error).
	//
	// 幂等：第二次跑保留文件（不重写、不报错）。
	if err := EnsureDefault(dir); err != nil {
		t.Fatalf("EnsureDefault 第二次: %v", err)
	}
}

// TestEnsureDefault_KeepsValidFile: an existing valid protocol.yml (user-edited) is
// never touched.
//
// TestEnsureDefault_KeepsValidFile：已存在的合法 protocol.yml（用户改过）绝不动。
func TestEnsureDefault_KeepsValidFile(t *testing.T) {
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	dir := t.TempDir()

	custom := DefaultProtocol()
	custom.Version = "user-edited"
	if err := Save(dir, custom); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := EnsureDefault(dir); err != nil {
		t.Fatalf("EnsureDefault: %v", err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Version != "user-edited" {
		t.Errorf("合法用户文件应保留: version = %q, want user-edited", got.Version)
	}
}

// TestEnsureDefault_CorruptBackedAside pins the corrupt-file fix: a protocol.yml the
// user broke (YAML parse error) is renamed aside to protocol.yml.corrupt-<ts> before
// defaults are written — never silently overwritten.
//
// TestEnsureDefault_CorruptBackedAside 钉死损坏文件修复：用户改坏的 protocol.yml
// （YAML 解析失败）先改名备份为 protocol.yml.corrupt-<ts> 再写默认值——绝不静默
// 覆盖。
func TestEnsureDefault_CorruptBackedAside(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv(`FORGE_DATA_HOME`, dataHome)
	dir := t.TempDir()

	dd := forgedata.DataDirFor(dir)
	if err := os.MkdirAll(dd, 0755); err != nil {
		t.Fatal(err)
	}
	bad := []byte("version: [unclosed\n  : : :")
	path := filepath.Join(dd, "protocol.yml")
	if err := os.WriteFile(path, bad, 0644); err != nil {
		t.Fatal(err)
	}

	if err := EnsureDefault(dir); err != nil {
		t.Fatalf("EnsureDefault: %v", err)
	}

	// The corrupt file must be backed aside with its original content.
	//
	// 损坏文件必须带原内容备份到一边。
	entries, err := os.ReadDir(dd)
	if err != nil {
		t.Fatal(err)
	}
	var backup string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), `protocol.yml.corrupt-`) {
			backup = e.Name()
			break
		}
	}
	if backup == "" {
		t.Fatal("损坏的 protocol.yml 应备份为 protocol.yml.corrupt-<ts>，未找到")
	}
	got, err := os.ReadFile(filepath.Join(dd, backup))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(bad) {
		t.Errorf("备份内容 = %q, want 原损坏内容 %q", got, bad)
	}

	// A fresh default is in place and loads.
	//
	// 新默认值就位且可加载。
	p, err := Load(dir)
	if err != nil {
		t.Fatalf("Load after EnsureDefault: %v", err)
	}
	if p.Scoring == nil {
		t.Error("重建的默认 protocol 应含 scoring 配置")
	}
}

// TestSave_AtomicNoTempLeftover verifies Save leaves no temp files behind in the
// DataDir — AtomicWrite renames its temp file over the target, so a stray .tmp-*
// file would indicate the write path is not atomic.
//
// TestSave_AtomicNoTempLeftover 验证 Save 不在 DataDir 留临时文件——AtomicWrite
// 把临时文件 rename 覆盖目标，残留 .tmp-* 说明写路径不再原子。
func TestSave_AtomicNoTempLeftover(t *testing.T) {
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	dir := t.TempDir()
	if err := Save(dir, DefaultProtocol()); err != nil {
		t.Fatalf("Save: %v", err)
	}
	entries, err := os.ReadDir(forgedata.DataDirFor(dir))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if e.Name() != "protocol.yml" {
			t.Errorf("unexpected file left in DataDir: %s", e.Name())
		}
	}
}
