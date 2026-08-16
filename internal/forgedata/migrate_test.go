package forgedata_test

// migrate_test.go — MigrateProject behavior guards. Chinese strings use raw string literals to avoid Windows quote corruption.
// Uses forgedatatest.RealProject to obtain a real *Project (git + .forge + FORGE_DATA_HOME isolation),
// because MigrateProject calls p.Ensure() + depends on ConfigDir/DataDir dual roots being real and writable.
//
// migrate_test.go —— MigrateProject 行为守卫。中文字符串 raw string 规避 Windows 引号腐蚀。
// 用 forgedatatest.RealProject 拿真实 *Project（git + .forge + FORGE_DATA_HOME 隔离），
// 因 MigrateProject 调 p.Ensure() + 依赖 ConfigDir/DataDir 双根真实可写。

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/forgedata/forgedatatest"
)

// TestMigrateProject_MovesRuntimeState_KeepsConfig: .forge/ contains runtime (tasks/
// checklog/active-task-ref/throttle + archive/session variants) + config (hooks/ etc.),
// after migrate runtime lands in DataDir while config stays under .forge/. Pins the whitelist boundary — neither missing
// runtime nor wrongly migrating non-runtime entries. Note: state.json/pipeline.yml are dead files of the removed project-level pipeline
// (not in the migrate whitelist so left under .forge/, but autoSync cleanupLegacyDeadFiles deletes them on upgrade
// ); hooks are live config (migrate/autoSync both keep them). This test pins the migrate boundary: non-runtime
// entries (dead files + live config) all stay under .forge/ after migrate.
//
// TestMigrateProject_MovesRuntimeState_KeepsConfig：.forge/ 含 runtime（tasks/
// checklog/active-task-ref/throttle + 归档/session 变体）+ config（hooks/等），
// migrate 后 runtime 在 DataDir、config 留 .forge/。钉死白名单边界——既不漏迁
// runtime，也不误迁非 runtime 条目。注：state.json/pipeline.yml 是已删项目级管道的
// 死文件（migrate 不在白名单故留 .forge/，但 autoSync 升级时 cleanupLegacyDeadFiles
// 会删）；hooks 是活 config（migrate/autoSync 都留）。本测试钉 migrate 边界：非 runtime
// 条目（死文件 + 活 config）migrate 都留 .forge/。
func TestMigrateProject_MovesRuntimeState_KeepsConfig(t *testing.T) {
	root, p := forgedatatest.RealProject(t)
	// runtime state (should migrate).
	//
	// runtime state（应迁）
	mkDir(t, filepath.Join(root, `.forge`, `tasks`))
	mkFile(t, filepath.Join(root, `.forge`, `tasks`, `feat.json`), `task`)
	mkFile(t, filepath.Join(root, `.forge`, `checklog.jsonl`), `main`)
	mkFile(t, filepath.Join(root, `.forge`, `checklog-20260101.jsonl`), `archive`)
	mkFile(t, filepath.Join(root, `.forge`, `toollog.jsonl`), `tl`)
	mkFile(t, filepath.Join(root, `.forge`, `active-task-ref`), `feat/legacy`)
	mkFile(t, filepath.Join(root, `.forge`, `active-task-ref-sid123`), `feat/session`)
	mkFile(t, filepath.Join(root, `.forge`, `.task-verify-throttle.last`), `ts`)
	// config (should stay).
	//
	// config（应留）
	mkFile(t, filepath.Join(root, `.forge`, `state.json`), `state`)
	mkDir(t, filepath.Join(root, `.forge`, `hooks`))
	mkFile(t, filepath.Join(root, `.forge`, `hooks`, `auto-compile.sh`), `hook`)
	mkFile(t, filepath.Join(root, `.forge`, `pipeline.yml`), `pipeline`)

	res, err := forgedata.MigrateProject(p, forgedata.MigrateOptions{})
	if err != nil {
		t.Fatalf(`MigrateProject: %v`, err)
	}
	// runtime migrated to DataDir.
	//
	// runtime 迁到 DataDir
	for _, rel := range []string{
		filepath.Join(`tasks`, `feat.json`),
		`checklog.jsonl`, `checklog-20260101.jsonl`, `toollog.jsonl`,
		`active-task-ref`, `active-task-ref-sid123`,
		`.task-verify-throttle.last`,
	} {
		assertExists(t, filepath.Join(p.DataDir, rel), `DataDir/`+rel)
	}
	// runtime disappears from .forge/.
	//
	// runtime 从 .forge/ 消失
	for _, rel := range []string{
		filepath.Join(`.forge`, `tasks`),
		filepath.Join(`.forge`, `checklog.jsonl`),
		filepath.Join(`.forge`, `active-task-ref`),
		filepath.Join(`.forge`, `.task-verify-throttle.last`),
	} {
		assertNotExists(t, filepath.Join(root, rel), root+`/`+rel)
	}
	// config stays under .forge/.
	//
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
	// Left should contain non-runtime entries (state.json/pipeline.yml dead files + hooks live config), not runtime entries.
	//
	// Left 应含非 runtime 条目（state.json/pipeline.yml 死文件 + hooks 活 config），不含 runtime
	if !slices.Contains(res.Left, `state.json`) {
		t.Errorf(`Left 应含 state.json（死文件，migrate 不动），Left=%v`, res.Left)
	}
	if slices.Contains(res.Left, `checklog.jsonl`) {
		t.Errorf(`Left 不应含 checklog.jsonl（已迁），Left=%v`, res.Left)
	}
}

// TestMigrateProject_Idempotent: run twice, second time Moved is empty (runtime already in DataDir).
//
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

// TestMigrateProject_DryRun: --dry-run reports planned migration but does not actually move (source stays in .forge/, DataDir has nothing).
//
// TestMigrateProject_DryRun：--dry-run 报告将迁移但不实际移动（源仍在 .forge/，DataDir 无）。
func TestMigrateProject_DryRun(t *testing.T) {
	root, p := forgedatatest.RealProject(t)
	mkFile(t, filepath.Join(root, `.forge`, `checklog.jsonl`), `x`)
	res, err := forgedata.MigrateProject(p, forgedata.MigrateOptions{DryRun: true})
	if err != nil {
		t.Fatalf(`MigrateProject dry-run: %v`, err)
	}
	if !slices.Contains(res.Moved, `checklog.jsonl`) {
		t.Errorf(`dry-run 应报告 checklog.jsonl 将迁移，Moved=%v`, res.Moved)
	}
	// source still present (not executed).
	//
	// 源仍在（未执行）
	assertExists(t, filepath.Join(root, `.forge`, `checklog.jsonl`), `源文件`)
	// DataDir has nothing.
	//
	// DataDir 没有
	assertNotExists(t, filepath.Join(p.DataDir, `checklog.jsonl`), `DataDir 目标`)
	// DryRun does not populate Left (residual is meaningless).
	//
	// DryRun 不填 Left（剩余无意义）
	if len(res.Left) != 0 {
		t.Errorf(`DryRun Left 应空，实得 %v`, res.Left)
	}
}

// TestMigrateProject_SkipExisting: dst already exists + non-force → skip, source kept and dst not overwritten.
//
// TestMigrateProject_SkipExisting：dst 已有 + 非 force → skip，源保留 dst 不覆盖。
func TestMigrateProject_SkipExisting(t *testing.T) {
	root, p := forgedatatest.RealProject(t)
	mkFile(t, filepath.Join(root, `.forge`, `checklog.jsonl`), `src`)
	mkFile(t, filepath.Join(p.DataDir, `checklog.jsonl`), `dst`)
	res, err := forgedata.MigrateProject(p, forgedata.MigrateOptions{})
	if err != nil {
		t.Fatalf(`MigrateProject: %v`, err)
	}
	if !slices.Contains(res.Skipped, `checklog.jsonl`) {
		t.Errorf(`期望 checklog.jsonl 被 skip，Skipped=%v`, res.Skipped)
	}
	assertExists(t, filepath.Join(root, `.forge`, `checklog.jsonl`), `源应保留`)
	got := readStr(t, filepath.Join(p.DataDir, `checklog.jsonl`))
	if got != `dst` {
		t.Errorf(`dst 应保留原内容，实得 %q`, got)
	}
}

// TestMigrateProject_ForceOverwrite: dst already exists + force → overwrite with source, source migrated away.
//
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

// TestMigrateProject_DirTreeCopied: tasks/ contains nested subdirectories, after migrate DataDir/tasks is
// fully copied (validates copyTree recursion + Rename whole tree). Windows cross-drive falls back to copyTree.
//
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

// TestMigrateProject_DryRunForceKeepsDst: M1 regression guard — dry-run+force+dst exists,
// reports "will overwrite" but does not actually delete dst (dry-run no-touch contract). Prevents migrateOne from putting the DryRun check
// after RemoveAll(dst), causing dry-run+force to wrongly delete existing DataDir data.
//
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
	if !slices.Contains(res.Moved, `checklog.jsonl`) {
		t.Errorf(`dry-run+force 应报告 checklog.jsonl 将覆盖，Moved=%v`, res.Moved)
	}
	// dst still exists with unchanged content (dry-run does not delete dst).
	//
	// dst 仍存在且内容不变（dry-run 不删 dst）
	got := readStr(t, filepath.Join(p.DataDir, `checklog.jsonl`))
	if got != `dst` {
		t.Errorf(`dry-run 不应动 dst，期望 "dst"，实得 %q`, got)
	}
	// src still exists (dry-run does not touch src).
	//
	// src 仍存在（dry-run 不动 src）
	assertExists(t, filepath.Join(root, `.forge`, `checklog.jsonl`), `源文件`)
}

// TestMigrateProject_StatErrorAborts pins the src-side fix: a non-NotExist os.Stat error on a
// whitelist entry (here via a NUL in ConfigDir — EINVAL on Windows and Unix alike) must abort
// with a real error instead of being silently treated as "entry absent" and vanishing from the
// report.
//
// TestMigrateProject_StatErrorAborts 钉死源端修复：白名单条目 os.Stat 的非 NotExist 错误
// （这里用 ConfigDir 含 NUL 构造——Windows/Unix 均为 EINVAL）必须显式报错中止，而非被
// 静默当「不存在」从报告中凭空消失。
func TestMigrateProject_StatErrorAborts(t *testing.T) {
	p := &forgedata.Project{
		Key:       `k`,
		DataDir:   t.TempDir(),
		ConfigDir: t.TempDir() + string(filepath.Separator) + "bad\x00config",
	}
	_, err := forgedata.MigrateProject(p, forgedata.MigrateOptions{})
	if err == nil {
		t.Fatal(`ConfigDir 含非法路径时应报错中止，实得 nil`)
	}
}

// TestMigrateProject_ConfigDirEqualsDataDir_NeverDeletes pins the data-loss fix: for a
// zero-project-write project (ConfigDir == DataDir) every whitelist entry has src == dst,
// so --force would RemoveAll(dst) — deleting the user's live tasks/checklog — before the
// move. MigrateProject must return an empty result immediately and leave every byte
// untouched, in force, non-force, and dry-run modes alike.
//
// TestMigrateProject_ConfigDirEqualsDataDir_NeverDeletes 钉死数据丢失修复：零项目写入
// 项目（ConfigDir == DataDir）每条白名单 src == dst，--force 会先 RemoveAll(dst)——
// 删掉用户活的 tasks/checklog——再移动。MigrateProject 必须立即返回空结果且一字节
// 不动，force / 非 force / dry-run 三种模式皆然。
func TestMigrateProject_ConfigDirEqualsDataDir_NeverDeletes(t *testing.T) {
	dd := t.TempDir()
	p := &forgedata.Project{Key: `k`, DataDir: dd, ConfigDir: dd}
	mkFile(t, filepath.Join(dd, `tasks`, `feat.json`), `task`)
	mkFile(t, filepath.Join(dd, `checklog.jsonl`), `main`)
	mkFile(t, filepath.Join(dd, `active-task-ref`), `feat/x`)

	for _, opts := range []forgedata.MigrateOptions{
		{Force: true},
		{},
		{DryRun: true, Force: true},
	} {
		res, err := forgedata.MigrateProject(p, opts)
		if err != nil {
			t.Fatalf(`MigrateProject(opts=%+v): %v`, opts, err)
		}
		if len(res.Moved) != 0 || len(res.Skipped) != 0 || len(res.Left) != 0 {
			t.Errorf(`opts=%+v: 结果应为全空（无残留可迁），实得 %+v`, opts, res)
		}
		// Data must survive every mode — --force above all.
		//
		// 数据必须在每种模式下存活——尤其 --force。
		assertExists(t, filepath.Join(dd, `tasks`, `feat.json`), `tasks/feat.json`)
		assertExists(t, filepath.Join(dd, `checklog.jsonl`), `checklog.jsonl`)
		assertExists(t, filepath.Join(dd, `active-task-ref`), `active-task-ref`)
	}
}

// TestMigrateProject_NeverMigratesStamps pins the 2026-08-15 trust-boundary fix: .forge/stamps
// is repo-committable attacker-authorable content — a cloned malicious repo could ship stamps that
// forge would treat as local trust anchors once promoted into the user-level DataDir. stamps must
// stay project-local under .forge/ and never land in DataDir, in every migrate mode (incl. --force).
//
// TestMigrateProject_NeverMigratesStamps 钉住 2026-08-15 信任边界修复：.forge/stamps 是可提交
// 进 repo 的攻击者可书写内容——clone 一个恶意仓库即可带上被 forge 当本机信任锚的 stamps（一旦
// 被提升进用户级 DataDir）。stamps 必须保持项目局部于 .forge/，任何 migrate 模式（含 --force）
// 都不得落进 DataDir。
func TestMigrateProject_NeverMigratesStamps(t *testing.T) {
	root, p := forgedatatest.RealProject(t)
	mkDir(t, filepath.Join(root, `.forge`, `stamps`))
	mkFile(t, filepath.Join(root, `.forge`, `stamps`, `review.passed`), `forged`)

	for _, opts := range []forgedata.MigrateOptions{
		{},
		{Force: true},
		{DryRun: true, Force: true},
	} {
		res, err := forgedata.MigrateProject(p, opts)
		if err != nil {
			t.Fatalf(`MigrateProject(opts=%+v): %v`, opts, err)
		}
		for _, m := range res.Moved {
			if m == `stamps` {
				t.Errorf(`opts=%+v: stamps 不得出现在 Moved（repo 可提交内容不是 runtime 信任锚）`, opts)
			}
		}
		assertNotExists(t, filepath.Join(p.DataDir, `stamps`), `DataDir/stamps`)
	}
	// stamps stays project-local in every mode — --force never deletes it either.
	//
	// 任何模式下 stamps 保持项目局部——--force 也不得删它。
	assertExists(t, filepath.Join(root, `.forge`, `stamps`, `review.passed`), `.forge/stamps/review.passed`)
}

// TestMigrateProject_NeverMigratesHazards pins the 2026-08-15 trust-boundary fix: .forge/hazards
// holds hazard-confirmations forge treats as local trust anchors (a repo that controls the
// hazard-triggering command can precompute the fingerprint offline and ship the confirmation).
// hazards must stay project-local under .forge/ and never land in DataDir, in every migrate mode
// (incl. --force) — same rationale as stamps above.
//
// TestMigrateProject_NeverMigratesHazards 钉住 2026-08-15 信任边界修复：.forge/hazards 存的是
// forge 当本机信任锚的 hazard 确认（控制高危命令的 repo 可离线预算指纹并随仓库携带确认）。
// hazards 必须保持项目局部于 .forge/，任何 migrate 模式（含 --force）都不得落进 DataDir
// ——与上方 stamps 同理。
func TestMigrateProject_NeverMigratesHazards(t *testing.T) {
	root, p := forgedatatest.RealProject(t)
	mkDir(t, filepath.Join(root, `.forge`, `hazards`))
	mkFile(t, filepath.Join(root, `.forge`, `hazards`, `cmd-fingerprint`), `forged`)

	for _, opts := range []forgedata.MigrateOptions{
		{},
		{Force: true},
		{DryRun: true, Force: true},
	} {
		res, err := forgedata.MigrateProject(p, opts)
		if err != nil {
			t.Fatalf(`MigrateProject(opts=%+v): %v`, opts, err)
		}
		for _, m := range res.Moved {
			if m == `hazards` {
				t.Errorf(`opts=%+v: hazards 不得出现在 Moved（repo 可预计算的信任锚不是 runtime）`, opts)
			}
		}
		assertNotExists(t, filepath.Join(p.DataDir, `hazards`), `DataDir/hazards`)
	}
	// hazards stays project-local in every mode — --force never deletes it either.
	//
	// 任何模式下 hazards 保持项目局部——--force 也不得删它。
	assertExists(t, filepath.Join(root, `.forge`, `hazards`, `cmd-fingerprint`), `.forge/hazards/cmd-fingerprint`)
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
