package cli

// cleanup_test.go — guards for migrateProjectProtocol after the user-level-assets
// refactor: a default-equal project protocol.yml migrates to the DataDir copy;
// customized files (changed fields, unknown fields, unverifiable DataDir copy) are
// never deleted.
//
// cleanup_test.go —— user-level-assets 重构后 migrateProjectProtocol 的守卫：
// 与默认相等的项目 protocol.yml 迁到 DataDir 副本；改过的文件（字段被改、含未知
// 字段、DataDir 副本无法验证）绝不删。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/protocol"
)

// writeProjectProtocol writes content as <dir>/.forge/protocol.yml.
//
// writeProjectProtocol 把 content 写成 <dir>/.forge/protocol.yml。
func writeProjectProtocol(t *testing.T, dir, content string) string {
	t.Helper()
	p := filepath.Join(dir, ".forge", "protocol.yml")
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

// defaultProtocolYAML marshals the default protocol (the bytes an untouched
// forge-written protocol.yml carries).
//
// defaultProtocolYAML marshal 默认 protocol（未被用户碰过的 forge 写入的
// protocol.yml 内容）。
func defaultProtocolYAML(t *testing.T) string {
	t.Helper()
	b, err := yaml.Marshal(protocol.DefaultProtocol())
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestMigrateProjectProtocol_DefaultMigrates: a protocol.yml semantically equal to
// the default (never user-edited) moves to the DataDir copy; the project-level file
// is removed.
//
// TestMigrateProjectProtocol_DefaultMigrates：与默认语义相等（用户没改过）的
// protocol.yml 迁到 DataDir 副本；项目级文件被删。
func TestMigrateProjectProtocol_DefaultMigrates(t *testing.T) {
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	dir := t.TempDir()
	pp := writeProjectProtocol(t, dir, defaultProtocolYAML(t))

	migrateProjectProtocol(dir)

	if _, err := os.Stat(pp); !os.IsNotExist(err) {
		t.Errorf("默认 protocol.yml 应被迁走（项目级文件删除）")
	}
	ddCopy := filepath.Join(forgedata.DataDirFor(dir), "protocol.yml")
	if _, err := os.Stat(ddCopy); err != nil {
		t.Errorf("DataDir 副本应被创建: %v", err)
	}
}

// TestMigrateProjectProtocol_CustomizedKept: a file with a changed field is
// user-modified — kept as the team-shared override layer.
//
// TestMigrateProjectProtocol_CustomizedKept：字段被改的文件是用户改过的——保留为
// 团队共享覆盖层。
func TestMigrateProjectProtocol_CustomizedKept(t *testing.T) {
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	dir := t.TempDir()
	custom := strings.Replace(defaultProtocolYAML(t), `version: "1.0"`, `version: "team-v2"`, 1)
	if custom == defaultProtocolYAML(t) {
		custom = "version: team-v2\n" + defaultProtocolYAML(t)
	}
	pp := writeProjectProtocol(t, dir, custom)

	migrateProjectProtocol(dir)

	if _, err := os.Stat(pp); err != nil {
		t.Errorf("用户改过的 protocol.yml 必须保留: %v", err)
	}
}

// TestMigrateProjectProtocol_UnknownFieldKept pins the strict-decode fix: a file
// carrying fields unknown to Protocol is user content — a lenient unmarshal would
// silently drop them on re-marshal, misjudge the file as default, and delete it.
//
// TestMigrateProjectProtocol_UnknownFieldKept 钉死严格解码修复：含 Protocol 未知
// 字段的文件是用户内容——宽松 unmarshal 会在重 marshal 时静默丢字段，把文件误判
// 成默认而删掉。
func TestMigrateProjectProtocol_UnknownFieldKept(t *testing.T) {
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	dir := t.TempDir()
	pp := writeProjectProtocol(t, dir, defaultProtocolYAML(t)+"\nmy_custom_extension: true\n")

	migrateProjectProtocol(dir)

	if _, err := os.Stat(pp); err != nil {
		t.Errorf("含未知字段的 protocol.yml 必须保留（视为用户改过）: %v", err)
	}
}

// TestMigrateProjectProtocol_UnverifiableCopyKeepsProjectFile pins the stat-error
// fix: when the DataDir copy's existence cannot be verified, the project-level file
// must NOT be deleted. FORGE_DATA_HOME pointing at a regular FILE makes every stat
// under it fail (ENOTDIR on unix; ERROR_PATH_NOT_FOUND on Windows, which Go maps to
// IsNotExist — in that case SaveDataDir fails instead) — either way the outcome must
// be "project file kept".
//
// TestMigrateProjectProtocol_UnverifiableCopyKeepsProjectFile 钉死 stat 错误修复：
// DataDir 副本存在性无法验证时，项目级文件不得删除。FORGE_DATA_HOME 指向一个普通
// 文件让其下所有 stat 失败（unix 上 ENOTDIR；Windows 上 ERROR_PATH_NOT_FOUND 被
// Go 映射为 IsNotExist——此时由 SaveDataDir 失败兜底）——两条路径的结果都必须是
// 「项目文件保留」。
func TestMigrateProjectProtocol_UnverifiableCopyKeepsProjectFile(t *testing.T) {
	fakeHome := filepath.Join(t.TempDir(), "home-is-a-file")
	if err := os.WriteFile(fakeHome, []byte("not a dir"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FORGE_DATA_HOME", fakeHome)
	dir := t.TempDir()
	pp := writeProjectProtocol(t, dir, defaultProtocolYAML(t))

	migrateProjectProtocol(dir)

	if _, err := os.Stat(pp); err != nil {
		t.Errorf("DataDir 副本未验证时项目文件必须保留: %v", err)
	}
}

// TestStripProjectLevelForgeAssets_Reasonix pins the reasonix cleanup boundary:
// the forge-written .reasonix/skills/forge-quality/ skill is stripped (and the
// now-empty .reasonix/skills/ removed), while .reasonix/ itself — the agent's
// session data — is never touched.
//
// TestStripProjectLevelForgeAssets_Reasonix 钉死 reasonix 清理边界：forge 写入的
// .reasonix/skills/forge-quality/ skill 被剥除（空掉的 .reasonix/skills/ 一并删），
// 而 .reasonix/ 本身——agent 的会话数据——绝不被碰。
func TestStripProjectLevelForgeAssets_Reasonix(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, ".reasonix", "skills", "forge-quality")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# forge-quality"), 0644); err != nil {
		t.Fatal(err)
	}
	sessionData := filepath.Join(dir, ".reasonix", "desktop-topic-titles.json")
	if err := os.WriteFile(sessionData, []byte(`{"k":"v"}`), 0644); err != nil {
		t.Fatal(err)
	}

	stripProjectLevelForgeAssets(dir)

	if _, err := os.Stat(skillDir); !os.IsNotExist(err) {
		t.Errorf("forge-written reasonix skill 应被剥除，实得 stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".reasonix", "skills")); !os.IsNotExist(err) {
		t.Errorf("空掉的 .reasonix/skills 应被移除，实得 stat err=%v", err)
	}
	if _, err := os.Stat(sessionData); err != nil {
		t.Errorf(".reasonix/ 会话数据必须保留: %v", err)
	}
}
