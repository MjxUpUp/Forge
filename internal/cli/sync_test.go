package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/forgedata/forgedatatest"
)

// writeSyncStamp writes DataDir/.sync-version with the given binary version, so
// autoSync can read it past its first early-return guard. Replaces the old
// state.json LastSyncVersion mechanism: the project pipeline (and its
// state.json) was deleted, so autoSync now no-ops via a .sync-version stamp,
// which lives in the user-level DataDir after the user-level-assets refactor.
func writeSyncStamp(t *testing.T, dir, version string) {
	t.Helper()
	dataDir := forgedata.DataDirFor(dir)
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatalf("mkdir DataDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, ".sync-version"), []byte(version), 0644); err != nil {
		t.Fatalf("write .sync-version: %v", err)
	}
}

// hookWritten reports whether autoSync got past its early-return and ran
// WriteHookTemplates (the first sync step), by checking for bash-guard.sh under
// DataDir/hooks/ (user-level-assets: hook reference copies live in DataDir).
// autoSync's later steps (settings/skill generation) may error in a bare temp
// dir, but hook writing happens first and is what the early-return guards.
func hookWritten(t *testing.T, dir string) bool {
	t.Helper()
	_, err := os.Stat(filepath.Join(forgedata.DataDirFor(dir), "hooks", "bash-guard.sh"))
	return err == nil
}

// Dev builds must re-sync on every command even when the .sync-version stamp
// already equals "dev", because embedded templates change between dev builds.
// Regression: .forge/hooks/ was observed stuck at 8 files while embed.go had 15.
func TestAutoSyncDevVersionResyncsWhenEqual(t *testing.T) {
	dir := t.TempDir()
	writeSyncStamp(t, dir, "dev")

	// Ignore error: later sync steps (skill/settings gen) may fail in a bare
	// temp dir, but we only care whether the early-return was taken.
	_ = autoSync(dir, "dev", false)

	if !hookWritten(t, dir) {
		t.Fatal("dev version with stamp=\"dev\" must still resync (early-return should be skipped), but hook was not written")
	}
}

// Non-dev builds must skip resync when the .sync-version stamp already matches —
// this is the normal idempotent behavior for released versions.
func TestAutoSyncReleasedVersionSkipsWhenEqual(t *testing.T) {
	dir := t.TempDir()
	writeSyncStamp(t, dir, "v0.17.0")

	_ = autoSync(dir, "v0.17.0", false)

	if hookWritten(t, dir) {
		t.Fatal("released version with matching stamp should skip resync (early-return), but hook was written")
	}
}

// Non-dev builds must resync when the version differs (upgrade path).
func TestAutoSyncResyncsWhenVersionDiffers(t *testing.T) {
	dir := t.TempDir()
	writeSyncStamp(t, dir, "v0.16.0")

	_ = autoSync(dir, "v0.17.0", false)

	if !hookWritten(t, dir) {
		t.Fatal("version mismatch should trigger resync, but hook was not written")
	}
}

// writeSettingsWithStaleBinding writes a settings.local.json that binds
// task-verify under PostToolUse with a wide matcher — the legacy mis-binding
// that caused runaway hook invocations (100+/session) in real heavy-use
// projects before the generator was fixed to bind task-verify only under Stop.
func writeSettingsWithStaleBinding(t *testing.T, dir string) {
	t.Helper()
	claudeDir := filepath.Join(dir, ".claude")
	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		t.Fatalf("mkdir .claude: %v", err)
	}
	stale := `{
  "hooks": {
    "PostToolUse": [
      {"matcher": "Bash|Read|Glob|Skill|Agent", "hooks": [{"type": "command", "command": "forge hook task-verify"}]}
    ],
    "Stop": [
      {"hooks": [{"type": "command", "command": "forge hook task-verify"}]}
    ]
  }
}`
	if err := os.WriteFile(filepath.Join(claudeDir, "settings.local.json"), []byte(stale), 0644); err != nil {
		t.Fatalf("write settings: %v", err)
	}
}

// A settings file carrying a legacy task-verify PostToolUse binding must force a
// resync even when the .sync-version stamp matches the binary — the stale
// binding has to be regenerated away regardless of version. Regression for the
// DevWorkbench case where a stale settings file persisted across upgrades.
func TestAutoSyncHealsStaleBindingEvenWhenVersionMatches(t *testing.T) {
	dir := t.TempDir()
	writeSyncStamp(t, dir, "v0.17.0")
	writeSettingsWithStaleBinding(t, dir)

	if !settingsHasStaleBinding(dir) {
		t.Fatal("settingsHasStaleBinding should detect task-verify under PostToolUse")
	}

	_ = autoSync(dir, "v0.17.0", false)

	if !hookWritten(t, dir) {
		t.Fatal("stale binding must force resync even when version matches, but hook was not written")
	}
}

// settingsHasStaleBinding must NOT flag a clean settings file (task-verify only
// under Stop) — otherwise every command would pointlessly resync.
func TestSettingsHasStaleBindingFalseForCleanSettings(t *testing.T) {
	dir := t.TempDir()
	writeSyncStamp(t, dir, "v0.17.0")
	// Generate the canonical clean settings via the generator itself.
	writeClaudeSettingsFixture(t, dir)
	if settingsHasStaleBinding(dir) {
		t.Fatal("clean generated settings must not be flagged as stale")
	}
}

// When plugin is installed at user level, autoSync must NOT write project-level
// hooks — the "write then immediately strip" pattern corrupts
// settings.local.json if the process is interrupted between the two steps.
// dedupeProjectLevelIfPlugin (deferred) still cleans up legacy hooks.
func TestAutoSyncSkipsSettingsWriteWhenPluginInstalled(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", home)
	writeForgePluginFixture(t, home)

	dir := t.TempDir()
	writeSyncStamp(t, dir, "v0.16.0") // version mismatch → would trigger full sync

	// Pre-populate settings.local.json with user fields only (no hooks).
	userSettings := `{"env":{"KEY":"val"},"model":"gpt-4"}`
	claudeDir := filepath.Join(dir, ".claude")
	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		t.Fatalf("mkdir .claude: %v", err)
	}
	if err := os.WriteFile(filepath.Join(claudeDir, "settings.local.json"), []byte(userSettings), 0644); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	// autoSync with version mismatch — would normally rewrite host settings.
	_ = autoSync(dir, "v0.17.0", false)

	// Verify: settings.local.json must be untouched (no hooks written).
	data, err := os.ReadFile(filepath.Join(claudeDir, "settings.local.json"))
	if err != nil {
		t.Fatalf("read settings after autoSync: %v", err)
	}
	var parsed map[string]json.RawMessage
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("parse settings: %v", err)
	}
	if _, hasHooks := parsed["hooks"]; hasHooks {
		t.Error("plugin installed: settings writer must not write hooks to settings.local.json")
	}
	if string(parsed["env"]) != `{"KEY":"val"}` {
		t.Errorf("user env field was modified: got %s", string(parsed["env"]))
	}
	if string(parsed["model"]) != `"gpt-4"` {
		t.Errorf("user model field was modified: got %s", string(parsed["model"]))
	}
}

// A missing .sync-version stamp must not silently skip sync — old code returned
// nil here, leaving stale settings unhealed. With the heal-on-bad-state fix, the
// stamp-independent artifacts (hooks + settings) still regenerate so a stale
// binding gets repaired even when no stamp exists.
func TestAutoSyncHealsArtifactsWhenStampMissing(t *testing.T) {
	dir := t.TempDir()
	forgeDir := filepath.Join(dir, ".forge")
	if err := os.MkdirAll(forgeDir, 0755); err != nil {
		t.Fatalf("mkdir .forge: %v", err)
	}
	// Intentionally NO .sync-version — simulates a half-initialized or corrupted project.

	_ = autoSync(dir, "v0.17.0", false)

	if !hookWritten(t, dir) {
		t.Fatal("missing .sync-version must still heal hooks+settings, but hook was not written")
	}
}

// TestMigrateRuntimeResidue_MovesRuntimeToDataDir: after autoSync integrates migrate, runtime state
// residue under .forge/ (archives/tasks/active-task-ref etc.) should move to DataDir, while config (hooks/)
// stays in .forge/. Regression guard: after refactor-data-home, runtime residue accumulated by old versions
// would not auto-migrate, leaving .forge/ piled with hundreds of files after upgrade; autoSync integrating
// migrate moves them to DataDir in one shot. Requires a git project (ProjectFor derives the key).
//
// TestMigrateRuntimeResidue_MovesRuntimeToDataDir：autoSync 集成 migrate 后，.forge/ 下的
// runtime state 残留（归档/tasks/active-task-ref 等）应迁到 DataDir，config（hooks/）留 .forge/。
// 回归守卫：refactor-data-home 后老版本积累的 runtime 残留不会自动迁，升级后 .forge/ 仍堆
// 几百个文件；autoSync 集成 migrate 一次性搬到 DataDir 瘦身。需 git 项目（ProjectFor 派生 key）。
func TestMigrateRuntimeResidue_MovesRuntimeToDataDir(t *testing.T) {
	root, p := forgedatatest.RealProject(t)
	// runtime residue (should migrate)
	//
	// runtime 残留（应迁）
	syncWrite(t, filepath.Join(root, `.forge`, `checklog.jsonl`), `legacy-main`)
	syncWrite(t, filepath.Join(root, `.forge`, `checklog-20260101.jsonl`), `archive`)
	syncMkdir(t, filepath.Join(root, `.forge`, `tasks`))
	syncWrite(t, filepath.Join(root, `.forge`, `tasks`, `feat.json`), `task`)
	syncWrite(t, filepath.Join(root, `.forge`, `active-task-ref`), `feat/legacy`)
	syncWrite(t, filepath.Join(root, `.forge`, `.task-verify-throttle.last`), `ts`)
	// config (should stay)
	//
	// config（应留）
	syncMkdir(t, filepath.Join(root, `.forge`, `hooks`))
	syncWrite(t, filepath.Join(root, `.forge`, `hooks`, `bash-guard.sh`), `hook`)

	migrateRuntimeResidue(root)

	// runtime moves to DataDir
	//
	// runtime 迁到 DataDir
	for _, rel := range []string{
		`checklog.jsonl`, `checklog-20260101.jsonl`,
		filepath.Join(`tasks`, `feat.json`),
		`active-task-ref`, `.task-verify-throttle.last`,
	} {
		if _, err := os.Stat(filepath.Join(p.DataDir, rel)); err != nil {
			t.Errorf(`期望 DataDir/%s 存在（runtime 应迁）：%v`, rel, err)
		}
	}
	// runtime directory disappears from .forge/
	//
	// runtime 目录从 .forge/ 消失
	if _, err := os.Stat(filepath.Join(root, `.forge`, `tasks`)); err == nil {
		t.Errorf(`.forge/tasks 应已迁走`)
	}
	// config stays in .forge/ (allowlist does not migrate config by mistake)
	//
	// config 留 .forge/（白名单不误迁配置）
	if _, err := os.Stat(filepath.Join(root, `.forge`, `hooks`, `bash-guard.sh`)); err != nil {
		t.Errorf(`config hooks/bash-guard.sh 应留 .forge/（白名单边界）`)
	}
}

// TestMigrateRuntimeResidue_PreservesQuarantineOnConflict: under default semantics, when a same-named
// quarantine already exists in DataDir, skip and keep the source — do not introduce RemoveSrcOnConflict,
// to avoid deleting the user code copy isolated by file-sentinel. When .forge/quarantine/<sid>/ and
// DataDir/quarantine/<sid>/ share the same name, the source is kept and not deleted.
//
// TestMigrateRuntimeResidue_PreservesQuarantineOnConflict：default 语义下 DataDir 已有同名
// quarantine 时 skip 保留源——不引入 RemoveSrcOnConflict，避免删 file-sentinel 隔离的用户
// 代码副本。.forge/quarantine/<sid>/ 与 DataDir/quarantine/<sid>/ 同名时，源保留不删。
func TestMigrateRuntimeResidue_PreservesQuarantineOnConflict(t *testing.T) {
	root, p := forgedatatest.RealProject(t)
	sid := `sess-123`
	// Both sides have a quarantine session with the same name (conflict)
	//
	// 双侧都有同名 quarantine session（冲突）
	syncWrite(t, filepath.Join(root, `.forge`, `quarantine`, sid, `src.go`), `project-side`)
	syncWrite(t, filepath.Join(p.DataDir, `quarantine`, sid, `src.go`), `datadir-side`)

	migrateRuntimeResidue(root)

	// DataDir side is not overwritten (datadir-side is kept)
	//
	// DataDir 侧不被覆盖（保留 datadir-side）
	got, err := os.ReadFile(filepath.Join(p.DataDir, `quarantine`, sid, `src.go`))
	if err != nil {
		t.Fatalf(`DataDir quarantine 应存在：%v`, err)
	}
	if string(got) != `datadir-side` {
		t.Errorf(`DataDir quarantine 不应被覆盖，期望 datadir-side，实得 %q`, string(got))
	}
	// .forge/ side quarantine is kept on skip (not deleted — the isolated user code copy must not be lost)
	//
	// .forge/ 侧 quarantine skip 保留（不删——隔离的用户代码副本不可丢）
	if _, err := os.Stat(filepath.Join(root, `.forge`, `quarantine`, sid, `src.go`)); err != nil {
		t.Errorf(`.forge/quarantine 冲突时应 skip 保留（不删隔离数据），实得 err：%v`, err)
	}
}

// TestCleanupLegacyDeadFiles: dead files left after feature removal should be deleted by autoSync, config stays:
//   - pipeline.yml/state.json: dead files after the project-level pipeline was deleted
//   - session-health-*.last: dead stamps after the session-health hook was removed (including session-scoped variants)
//   - protocol.yml stays (live config, allowlist boundary)
//
// TestCleanupLegacyDeadFiles：功能删除后的死文件应被 autoSync 删，config 留：
//   - pipeline.yml/state.json：项目级管道删除后死文件
//   - session-health-*.last：session-health hook 移除后死 stamp（含 session-scoped 变体）
//   - protocol.yml 留（活 config，白名单边界）
func TestCleanupLegacyDeadFiles(t *testing.T) {
	dir := t.TempDir()
	syncMkdir(t, filepath.Join(dir, `.forge`))
	syncWrite(t, filepath.Join(dir, `.forge`, `pipeline.yml`), `dead-pipeline`)
	syncWrite(t, filepath.Join(dir, `.forge`, `state.json`), `dead-state`)
	syncWrite(t, filepath.Join(dir, `.forge`, `session-health-abc.last`), `stamp`)
	syncWrite(t, filepath.Join(dir, `.forge`, `session-health-xyz.last`), `stamp2`)
	syncWrite(t, filepath.Join(dir, `.forge`, `protocol.yml`), `proto`)

	cleanupLegacyDeadFiles(dir)

	for _, dead := range []string{`pipeline.yml`, `state.json`, `session-health-abc.last`, `session-health-xyz.last`} {
		if _, err := os.Stat(filepath.Join(dir, `.forge`, dead)); err == nil {
			t.Errorf(`死文件 .forge/%s 应被删`, dead)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, `.forge`, `protocol.yml`)); err != nil {
		t.Errorf(`config protocol.yml 应留（非死文件，白名单边界）`)
	}
}

// TestAutoSyncMigratesRuntimeOnVersionChange: autoSync triggers migrate on version change, moving .forge/
// runtime residue to DataDir. Verifies step wiring (migrateRuntimeResidue is actually called by autoSync,
// not missed). Other autoSync steps (skill/settings gen) produce stderr warnings when running at a git
// project root, ignored.
//
// TestAutoSyncMigratesRuntimeOnVersionChange：autoSync 版本变化时触发 migrate，把 .forge/
// runtime 残留迁到 DataDir。验证 step 接线（migrateRuntimeResidue 真被 autoSync 调用，而非
// 漏接）。autoSync 其他 step（skill/settings gen）在 git 项目 root 跑产 stderr warning，忽略。
func TestAutoSyncMigratesRuntimeOnVersionChange(t *testing.T) {
	root, p := forgedatatest.RealProject(t)
	// Isolate CLAUDE_CONFIG_DIR so settings writers/dedupe do not hit the real ~/.claude
	//
	// 隔离 CLAUDE_CONFIG_DIR，避免 settings writers/dedupe 命中真实 ~/.claude
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	syncWrite(t, filepath.Join(root, `.forge`, `checklog-20260101.jsonl`), `archive`)
	syncMkdir(t, filepath.Join(root, `.forge`, `tasks`))
	syncWrite(t, filepath.Join(root, `.forge`, `tasks`, `feat.json`), `task`)
	writeSyncStamp(t, root, `v0.16.0`)

	_ = autoSync(root, `v0.17.0`, false)

	if _, err := os.Stat(filepath.Join(p.DataDir, `checklog-20260101.jsonl`)); err != nil {
		t.Errorf(`autoSync 版本变化应触发 migrate，归档应迁 DataDir：%v`, err)
	}
	if _, err := os.Stat(filepath.Join(p.DataDir, `tasks`, `feat.json`)); err != nil {
		t.Errorf(`tasks/feat.json 应迁 DataDir：%v`, err)
	}
}

// TestMigrateRuntimeResidue_NonGitProjectMigratesToDataDir: after the
// user-level-assets refactor, forgedata.ProjectFor is a pure path derivation that
// succeeds for ANY cwd (non-git → PathKey), so migrateRuntimeResidue also migrates
// non-git projects' .forge/ runtime residue to the user-level DataDir — runtime
// state never stays in the project tree. The autoSync call site is rootCmd
// PersistentPreRun (before every forge command); the function has no return value,
// so this verifies no panic + the residue actually moved.
//
// TestMigrateRuntimeResidue_NonGitProjectMigratesToDataDir：user-level-assets 重构后
// forgedata.ProjectFor 是纯路径推导、对任意 cwd 成功（非 git → PathKey），故
// migrateRuntimeResidue 也会把非 git 项目的 .forge/ runtime 残留迁到用户级
// DataDir——runtime state 绝不留项目树。autoSync 调用点是 rootCmd PersistentPreRun
// （每个 forge 命令前）；函数无返回值，故验不 panic + 残留真实迁走。
func TestMigrateRuntimeResidue_NonGitProjectMigratesToDataDir(t *testing.T) {
	dir := t.TempDir() // 无 git init → PathKey 推导
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	syncMkdir(t, filepath.Join(dir, `.forge`))
	syncWrite(t, filepath.Join(dir, `.forge`, `checklog.jsonl`), `residue`)

	migrateRuntimeResidue(dir) // 不应 panic

	// Non-git project: runtime residue migrates to the user-level DataDir (PathKey).
	//
	// 非 git 项目：runtime 残留迁到用户级 DataDir（PathKey）。
	if _, err := os.Stat(filepath.Join(forgedata.DataDirFor(dir), `checklog.jsonl`)); err != nil {
		t.Errorf(`非 git 项目 runtime 残留应迁到 DataDir，实得 err：%v`, err)
	}
}

// TestAutoSyncMigrateIdempotent: the first autoSync (version change) migrates runtime to DataDir,
// the second (equal stamp early-return) leaves DataDir state unchanged. Guards autoSync-layer
// idempotence: future refactors changing the early-return logic will not cause repeated migrate/delete.
//
// TestAutoSyncMigrateIdempotent：第一次 autoSync（version 变化）迁 runtime 到 DataDir，
// 第二次（stamp 相等 early-return）DataDir 状态不变。守 autoSync 层幂等：future 重构改
// early-return 逻辑不致重复迁/删。
func TestAutoSyncMigrateIdempotent(t *testing.T) {
	root, p := forgedatatest.RealProject(t)
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	syncWrite(t, filepath.Join(root, `.forge`, `checklog-20260101.jsonl`), `archive`)
	writeSyncStamp(t, root, `v0.16.0`)

	_ = autoSync(root, `v0.17.0`, false) // 第一次：version 变化，sync + migrate
	migrated := filepath.Join(p.DataDir, `checklog-20260101.jsonl`)
	if _, err := os.Stat(migrated); err != nil {
		t.Fatalf(`第一次 autoSync 应迁归档到 DataDir：%v`, err)
	}

	_ = autoSync(root, `v0.17.0`, false) // 第二次：stamp 已更新为 v0.17.0，early-return no-op

	// Idempotent: archive still present (not repeatedly processed/deleted)
	//
	// 幂等：归档仍在（未被重复处理/删除）
	if _, err := os.Stat(migrated); err != nil {
		t.Errorf(`幂等：第二次 autoSync 后 DataDir 归档应仍在：%v`, err)
	}
}

// ---- helpers ----

func syncMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatalf(`mkdir %s: %v`, path, err)
	}
}

func syncWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf(`mkdir dir %s: %v`, filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf(`write %s: %v`, path, err)
	}
}
