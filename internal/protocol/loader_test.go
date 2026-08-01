package protocol

import (
	"os"
	"path/filepath"
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
