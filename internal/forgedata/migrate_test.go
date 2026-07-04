package forgedata_test

// migrate_test.go —— MigrateProject 行为守卫。中文字符串 raw string 规避 Windows 引号腐蚀。
// 用 forgedatatest.RealProject 拿真实 *Project（git + .forge + FORGE_DATA_HOME 隔离），
// 因 MigrateProject 调 p.Ensure() + 依赖 ConfigDir/DataDir 双根真实可写。

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/forgedata/forgedatatest"
)

// TestMigrateProject_MovesRuntimeState_KeepsConfig：.forge/ 含 runtime（tasks/
// checklog/active-task-ref/throttle + 归档/session 变体）+ config（state.json/
// hooks/pipeline.yml），migrate 后 runtime 在 DataDir、config 留 .forge/。
// 钉死白名单边界——既不漏迁 runtime，也不误迁配置。
func TestMigrateProject_MovesRuntimeState_KeepsConfig(t *testing.T) {
	root, p := forgedatatest.RealProject(t)
	// runtime state（应迁）
	mkDir(t, filepath.Join(root, `.forge`, `tasks`))
	mkFile(t, filepath.Join(root, `.forge`, `tasks`, `feat.json`), `task`)
	mkFile(t, filepath.Join(root, `.forge`, `checklog.jsonl`), `main`)
	mkFile(t, filepath.Join(root, `.forge`, `checklog-20260101.jsonl`), `archive`)
	mkFile(t, filepath.Join(root, `.forge`, `toollog.jsonl`), `tl`)
	mkFile(t, filepath.Join(root, `.forge`, `active-task-ref`), `feat/legacy`)
	mkFile(t, filepath.Join(root, `.forge`, `active-task-ref-sid123`), `feat/session`)
	mkFile(t, filepath.Join(root, `.forge`, `.task-verify-throttle.last`), `ts`)
	// config（应留）
	mkFile(t, filepath.Join(root, `.forge`, `state.json`), `state`)
	mkDir(t, filepath.Join(root, `.forge`, `hooks`))
	mkFile(t, filepath.Join(root, `.forge`, `hooks`, `auto-compile.sh`), `hook`)
	mkFile(t, filepath.Join(root, `.forge`, `pipeline.yml`), `pipeline`)

	res, err := forgedata.MigrateProject(p, forgedata.MigrateOptions{})
	if err != nil {
		t.Fatalf(`MigrateProject: %v`, err)
	}
	// runtime 迁到 DataDir
	for _, rel := range []string{
		filepath.Join(`tasks`, `feat.json`),
		`checklog.jsonl`, `checklog-20260101.jsonl`, `toollog.jsonl`,
		`active-task-ref`, `active-task-ref-sid123`,
		`.task-verify-throttle.last`,
	} {
		assertExists(t, filepath.Join(p.DataDir, rel), `DataDir/`+rel)
	}
	// runtime 从 .forge/ 消失
	for _, rel := range []string{
		filepath.Join(`.forge`, `tasks`),
		filepath.Join(`.forge`, `checklog.jsonl`),
		filepath.Join(`.forge`, `active-task-ref`),
		filepath.Join(`.forge`, `.task-verify-throttle.last`),
	} {
		assertNotExists(t, filepath.Join(root, rel), root+`/`+rel)
	}
	// config 留 .forge/
	for _, rel := range []string{
		filepath.Join(`.forge`, `state.json`),
		filepath.Join(`.forge`, `hooks`, `auto-compile.sh`),
		filepath.Join(`.forge`, `pipeline.yml`),
	} {
		assertExists(t, filepath.Join(root, rel), root+`/`+rel)
	}
	if len(res.Moved) == 0 {
		t.Errorf(`期望 Moved 非空，实得 %+v`, res)
	}
	// Left 应含配置（state.json/hooks/pipeline.yml），不含 runtime
	if !contains(res.Left, `state.json`) {
		t.Errorf(`Left 应含 state.json（配置保留），Left=%v`, res.Left)
	}
	if contains(res.Left, `checklog.jsonl`) {
		t.Errorf(`Left 不应含 checklog.jsonl（已迁），Left=%v`, res.Left)
	}
}

// TestMigrateProject_Idempotent：跑两次，第二次 Moved 空（runtime 已在 DataDir）。
func TestMigrateProject_Idempotent(t *testing.T) {
	root, p := forgedatatest.RealProject(t)
	mkFile(t, filepath.Join(root, `.forge`, `checklog.jsonl`), `x`)
	if _, err := forgedata.MigrateProject(p, forgedata.MigrateOptions{}); err != nil {
		t.Fatalf(`first migrate: %v`, err)
	}
	res, err := forgedata.MigrateProject(p, forgedata.MigrateOptions{})
	if err != nil {
		t.Fatalf(`second migrate: %v`, err)
	}
	if len(res.Moved) != 0 {
		t.Errorf(`第二次 Moved 应空（幂等），实得 %v`, res.Moved)
	}
}

// TestMigrateProject_DryRun：--dry-run 报告将迁移但不实际移动（源仍在 .forge/，DataDir 无）。
func TestMigrateProject_DryRun(t *testing.T) {
	root, p := forgedatatest.RealProject(t)
	mkFile(t, filepath.Join(root, `.forge`, `checklog.jsonl`), `x`)
	res, err := forgedata.MigrateProject(p, forgedata.MigrateOptions{DryRun: true})
	if err != nil {
		t.Fatalf(`MigrateProject dry-run: %v`, err)
	}
	if !contains(res.Moved, `checklog.jsonl`) {
		t.Errorf(`dry-run 应报告 checklog.jsonl 将迁移，Moved=%v`, res.Moved)
	}
	// 源仍在（未执行）
	assertExists(t, filepath.Join(root, `.forge`, `checklog.jsonl`), `源文件`)
	// DataDir 没有
	assertNotExists(t, filepath.Join(p.DataDir, `checklog.jsonl`), `DataDir 目标`)
	// DryRun 不填 Left（剩余无意义）
	if len(res.Left) != 0 {
		t.Errorf(`DryRun Left 应空，实得 %v`, res.Left)
	}
}

// TestMigrateProject_SkipExisting：dst 已有 + 非 force → skip，源保留 dst 不覆盖。
func TestMigrateProject_SkipExisting(t *testing.T) {
	root, p := forgedatatest.RealProject(t)
	mkFile(t, filepath.Join(root, `.forge`, `checklog.jsonl`), `src`)
	mkFile(t, filepath.Join(p.DataDir, `checklog.jsonl`), `dst`)
	res, err := forgedata.MigrateProject(p, forgedata.MigrateOptions{})
	if err != nil {
		t.Fatalf(`MigrateProject: %v`, err)
	}
	if !contains(res.Skipped, `checklog.jsonl`) {
		t.Errorf(`期望 checklog.jsonl 被 skip，Skipped=%v`, res.Skipped)
	}
	assertExists(t, filepath.Join(root, `.forge`, `checklog.jsonl`), `源应保留`)
	got := readStr(t, filepath.Join(p.DataDir, `checklog.jsonl`))
	if got != `dst` {
		t.Errorf(`dst 应保留原内容，实得 %q`, got)
	}
}

// TestMigrateProject_ForceOverwrite：dst 已有 + force → 覆盖为源，源迁走。
func TestMigrateProject_ForceOverwrite(t *testing.T) {
	root, p := forgedatatest.RealProject(t)
	mkFile(t, filepath.Join(root, `.forge`, `checklog.jsonl`), `src`)
	mkFile(t, filepath.Join(p.DataDir, `checklog.jsonl`), `dst`)
	if _, err := forgedata.MigrateProject(p, forgedata.MigrateOptions{Force: true}); err != nil {
		t.Fatalf(`MigrateProject force: %v`, err)
	}
	got := readStr(t, filepath.Join(p.DataDir, `checklog.jsonl`))
	if got != `src` {
		t.Errorf(`force 应覆盖 dst 为 src，实得 %q`, got)
	}
	assertNotExists(t, filepath.Join(root, `.forge`, `checklog.jsonl`), `源应迁走`)
}

// TestMigrateProject_DirTreeCopied：tasks/ 含嵌套子目录，migrate 后 DataDir/tasks
// 完整复制（验 copyTree 递归 + Rename 整树）。Windows 跨盘时走 copyTree fallback。
func TestMigrateProject_DirTreeCopied(t *testing.T) {
	root, p := forgedatatest.RealProject(t)
	mkDir(t, filepath.Join(root, `.forge`, `gates`, `task-implement`))
	mkFile(t, filepath.Join(root, `.forge`, `gates`, `task-implement`, `status.json`), `passed`)
	mkFile(t, filepath.Join(root, `.forge`, `gates`, `task-implement`, `report.txt`), `r`)
	if _, err := forgedata.MigrateProject(p, forgedata.MigrateOptions{}); err != nil {
		t.Fatalf(`MigrateProject: %v`, err)
	}
	assertExists(t, filepath.Join(p.DataDir, `gates`, `task-implement`, `status.json`), `嵌套 status.json`)
	assertExists(t, filepath.Join(p.DataDir, `gates`, `task-implement`, `report.txt`), `嵌套 report.txt`)
	got := readStr(t, filepath.Join(p.DataDir, `gates`, `task-implement`, `status.json`))
	if got != `passed` {
		t.Errorf(`嵌套文件内容应保留，实得 %q`, got)
	}
}

// TestMigrateProject_DryRunForceKeepsDst：M1 回归守卫——dry-run+force+dst 已存在时，
// 报告"将覆盖"但不实际删 dst（dry-run 不动文件契约）。防 migrateOne 把 DryRun 判断
// 放到 RemoveAll(dst) 之后，致 dry-run+force 误删 DataDir 已有数据。
func TestMigrateProject_DryRunForceKeepsDst(t *testing.T) {
	root, p := forgedatatest.RealProject(t)
	mkFile(t, filepath.Join(root, `.forge`, `checklog.jsonl`), `src`)
	mkFile(t, filepath.Join(p.DataDir, `checklog.jsonl`), `dst`)
	res, err := forgedata.MigrateProject(p, forgedata.MigrateOptions{DryRun: true, Force: true})
	if err != nil {
		t.Fatalf(`MigrateProject dry-run+force: %v`, err)
	}
	if !contains(res.Moved, `checklog.jsonl`) {
		t.Errorf(`dry-run+force 应报告 checklog.jsonl 将覆盖，Moved=%v`, res.Moved)
	}
	// dst 仍存在且内容不变（dry-run 不删 dst）
	got := readStr(t, filepath.Join(p.DataDir, `checklog.jsonl`))
	if got != `dst` {
		t.Errorf(`dry-run 不应动 dst，期望 "dst"，实得 %q`, got)
	}
	// src 仍存在（dry-run 不动 src）
	assertExists(t, filepath.Join(root, `.forge`, `checklog.jsonl`), `源文件`)
}

// ---- helpers ----

func mkDir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatalf(`mkdir %s: %v`, path, err)
	}
}

func mkFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf(`mkdir dir %s: %v`, filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf(`write %s: %v`, path, err)
	}
}

func assertExists(t *testing.T, path, label string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Errorf(`期望存在 %s（%s）: %v`, label, path, err)
	}
}

func assertNotExists(t *testing.T, path, label string) {
	t.Helper()
	if _, err := os.Stat(path); err == nil {
		t.Errorf(`期望不存在 %s（%s），但存在`, label, path)
	}
}

func readStr(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf(`read %s: %v`, path, err)
	}
	return string(b)
}

func contains(slice []string, s string) bool {
	for _, x := range slice {
		if x == s {
			return true
		}
	}
	return false
}
