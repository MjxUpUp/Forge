package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/registry"
	"github.com/MjxUpUp/Forge/internal/taskcontext"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/spf13/cobra"
)

// project_sync_test.go —— `forge project adopt/export/import` 的进程内 RunE 测试
// （project-sync），FORGE_DATA_HOME 隔离。中文字符串用 raw 字面量。

// newSyncMachine 建一个 git 形状 repo（带 legacy .forge 目录让 findProjectRoot 能
// 发现）并返回根路径。由调用方自行 chdir/env 切换。
func newSyncMachine(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, `.git`), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, `.forge`), 0755); err != nil {
		t.Fatal(err)
	}
	return root
}

// seedTaskState 经 SaveTaskState 写真实 TaskState（生产路径：SanitizeRef 折叠的
// 文件名、MarshalIndent 格式），使 fixture 落在 forge 自己放它的确切位置。须在
// 目标「机器」env 内调用。
func seedTaskState(t *testing.T, root, ref string, mutate func(*taskpipeline.TaskState)) {
	t.Helper()
	s := &taskpipeline.TaskState{TaskRef: ref, Branch: ref, Source: `explicit`}
	if mutate != nil {
		mutate(s)
	}
	if err := taskpipeline.SaveTaskState(root, s); err != nil {
		t.Fatal(err)
	}
}

// withMachine 以 cwd=root、FORGE_DATA_HOME=home 跑 fn（「机器」切换）。
func withMachine(t *testing.T, root, home string, fn func()) {
	t.Helper()
	oldWd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Setenv(`FORGE_DATA_HOME`, home)
	defer func() {
		if err := os.Chdir(oldWd); err != nil {
			t.Fatal(err)
		}
	}()
	fn()
}

// runAdopt / runExport / runImport 以 flag 集驱动 cobra RunE 本体。
func runAdopt(t *testing.T, flags map[string]string) (string, error) {
	t.Helper()
	for k, v := range flags {
		if err := projectAdoptCmd.Flags().Set(k, v); err != nil {
			t.Fatal(err)
		}
	}
	var buf bytes.Buffer
	projectAdoptCmd.SetOut(&buf)
	err := runProjectAdopt(projectAdoptCmd, nil)
	return buf.String(), err
}

func runExport(t *testing.T, flags map[string]string) (string, error) {
	t.Helper()
	for k, v := range flags {
		if err := projectExportCmd.Flags().Set(k, v); err != nil {
			t.Fatal(err)
		}
	}
	var buf bytes.Buffer
	projectExportCmd.SetOut(&buf)
	err := runProjectExport(projectExportCmd, nil)
	return buf.String(), err
}

func runImport(t *testing.T, flags map[string]string, args ...string) (string, error) {
	t.Helper()
	for k, v := range flags {
		if err := projectImportCmd.Flags().Set(k, v); err != nil {
			t.Fatal(err)
		}
	}
	var buf bytes.Buffer
	projectImportCmd.SetOut(&buf)
	err := runProjectImport(projectImportCmd, args)
	return buf.String(), err
}

// resetProjectCmdFlags 在测试间重置共享命令对象上的持久 cobra flag 值。
func resetProjectCmdFlags(t *testing.T) {
	t.Helper()
	type cmdFlags struct {
		cmd   *cobra.Command
		flags map[string]string
	}
	for _, c := range []cmdFlags{
		{projectAdoptCmd, map[string]string{`dry-run`: `false`, `regenerate`: `false`}},
		{projectExportCmd, map[string]string{`out`: ``, `include`: ``}},
		{projectImportCmd, map[string]string{`dry-run`: `false`, `untrusted`: `false`, `trust-foreign`: `false`, `force`: `false`, `adopt-id`: `false`}},
	} {
		for k, v := range c.flags {
			if err := c.cmd.Flags().Set(k, v); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// TestProjectAdopt_MigratesDataAndFlipsID：adopt 把路径 key 的 DataDir 迁到 ID
// key，在主根写 ID 文件，并同步注册表（数据最终落在新 key 下）。
func TestProjectAdopt_MigratesDataAndFlipsID(t *testing.T) {
	resetProjectCmdFlags(t)
	home := t.TempDir()
	root := newSyncMachine(t)

	withMachine(t, root, home, func() {
		pathKey, err := forgedata.KeyFromPath(root)
		if err != nil {
			t.Fatal(err)
		}
		oldDir := forgedata.RootDir(pathKey)
		seedTaskState(t, root, `feat/a`, nil)
		if err := os.WriteFile(filepath.Join(oldDir, `checklog.jsonl`), []byte(`{"recorded_at":"2026-08-18T10:00:00Z","check":"task-guard"}`+"\n"), 0644); err != nil {
			t.Fatal(err)
		}
		// 注册表登记旧 key（adopt 应同步走）
		if err := registry.Add(root); err != nil {
			t.Fatal(err)
		}

		out, aerr := runAdopt(t, map[string]string{`dry-run`: `false`})
		if aerr != nil {
			t.Fatalf(`adopt: %v`+"\n"+`%s`, aerr, out)
		}

		id, rerr := forgedata.ReadProjectID(root)
		if rerr != nil {
			t.Fatalf(`ID 文件应写入主根: %v`, rerr)
		}
		newKey := forgedata.IDKey(id)
		if got, _ := forgedata.Key(root); got != newKey {
			t.Errorf(`adopt 后 Key 应为 ID key %s，got %s`, newKey, got)
		}
		// 断言走生产读取路径：SaveTaskState/LoadTaskState 都经 SanitizeRef 折叠
		// （feat/a → feat-a.json），按折叠名查文件或直接 LoadTaskState。
		if _, lerr := taskpipeline.LoadTaskState(root, `feat/a`); lerr != nil {
			t.Errorf(`任务应随迁移落到新 key（LoadTaskState 经新身份解析）: %v`, lerr)
		}
		if _, serr := os.Stat(filepath.Join(forgedata.RootDir(newKey), `checklog.jsonl`)); serr != nil {
			t.Errorf(`checklog 应迁到新 key: %v`, serr)
		}
		// 幂等：重跑打印现状退出，无新迁移
		out2, aerr2 := runAdopt(t, map[string]string{})
		if aerr2 != nil {
			t.Fatalf(`重复 adopt 应幂等: %v`, aerr2)
		}
		if !strings.Contains(out2, `已启用项目 ID`) {
			t.Errorf(`重复 adopt 应打印现状，got %s`, out2)
		}
	})
}

// TestProjectAdopt_DryRunTouchesNothing：--dry-run 列动作，不写 ID 文件、不迁数据。
func TestProjectAdopt_DryRunTouchesNothing(t *testing.T) {
	resetProjectCmdFlags(t)
	home := t.TempDir()
	root := newSyncMachine(t)
	withMachine(t, root, home, func() {
		pathKey, _ := forgedata.KeyFromPath(root)
		seedTaskState(t, root, `feat/b`, nil)
		out, err := runAdopt(t, map[string]string{`dry-run`: `true`})
		if err != nil {
			t.Fatalf(`dry-run adopt: %v`, err)
		}
		if !strings.Contains(out, `dry-run`) {
			t.Errorf(`应标注 dry-run: %s`, out)
		}
		if _, serr := os.Stat(filepath.Join(root, forgedata.ProjectIDFileName)); !os.IsNotExist(serr) {
			t.Error(`dry-run 不得写 ID 文件`)
		}
		if _, serr := os.Stat(filepath.Join(forgedata.RootDir(pathKey), `tasks`, taskcontext.SanitizeRef(`feat/b`)+`.json`)); serr != nil {
			t.Errorf(`dry-run 不得迁移数据: %v`, serr)
		}
	})
}

// TestProjectExportImport_SameKeyRoundTrip：两台机器带同一项目 ID（双机标准姿
// 态）→ 同 key → 受信导入保留完成块；同 bundle 重导入被账本跳过；重叠导出不产
// 生重复 jsonl 行。
func TestProjectExportImport_SameKeyRoundTrip(t *testing.T) {
	resetProjectCmdFlags(t)
	home := t.TempDir()
	machineA := newSyncMachine(t)
	machineB := newSyncMachine(t)
	id := `fpid_0123456789abcdef0123456789abcdef`
	for _, m := range []string{machineA, machineB} {
		if err := os.WriteFile(filepath.Join(m, forgedata.ProjectIDFileName), []byte(id+"\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	key := forgedata.IDKey(id)

	// 机器 A：完成任务 + 证据，导出。dataDir 必须在 withMachine 内解析——
	// RootDir 读 FORGE_DATA_HOME，env 外解析会指到真实 home（测试污染）。
	var bundlePath string
	withMachine(t, machineA, home, func() {
		dataDir := forgedata.RootDir(key)
		done := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
		seedTaskState(t, machineA, `feat/sync`, func(s *taskpipeline.TaskState) {
			s.CompletedAt = &done
			s.ReviewPassed = true
			s.History = []taskpipeline.TaskGateResult{{Gate: `task-implement`, Passed: true}, {Gate: `task-verify`, Passed: true}, {Gate: `task-complete`, Passed: true}}
		})
		if err := os.WriteFile(filepath.Join(dataDir, `checklog.jsonl`), []byte(`{"recorded_at":"2026-08-18T11:00:00Z","check":"task-guard","detail":"e1"}`+"\n"), 0644); err != nil {
			t.Fatal(err)
		}
		bundlePath = filepath.Join(t.TempDir(), `a.tar.gz`)
		out, err := runExport(t, map[string]string{`out`: bundlePath})
		if err != nil {
			t.Fatalf(`export: %v`+"\n"+`%s`, err, out)
		}
	})

	// 机器 B：导入（同 key → 受信）。
	withMachine(t, machineB, home, func() {
		dataDir := forgedata.RootDir(key)
		out, err := runImport(t, map[string]string{}, bundlePath)
		if err != nil {
			t.Fatalf(`import: %v`+"\n"+`%s`, err, out)
		}
		if !strings.Contains(out, `受信`) {
			t.Errorf(`同 key 应判定受信: %s`, out)
		}
		got, lerr := taskpipeline.LoadTaskState(machineB, `feat/sync`)
		if lerr != nil {
			t.Fatalf(`B 侧应能加载任务: %v`, lerr)
		}
		if got.CompletedAt == nil || !got.ReviewPassed {
			t.Errorf(`受信导入应保留完成块（CompletedAt/ReviewPassed），got %+v`, got)
		}
		// 账本跳过：同 bundle 再导 → 命中即跳过
		out2, err2 := runImport(t, map[string]string{}, bundlePath)
		if err2 != nil {
			t.Fatalf(`二次 import: %v`, err2)
		}
		if !strings.Contains(out2, `已导入过`) {
			t.Errorf(`账本应跳过重复 bundle: %s`, out2)
		}

		// 重叠导出不重复：A 不新增数据，再次全量导出 → --force 导入 → 行数不变
		var bundle2 = filepath.Join(t.TempDir(), `a2.tar.gz`)
		out3, err3 := runExport(t, map[string]string{`out`: bundle2})
		if err3 != nil {
			t.Fatalf(`二次 export: %v`+"\n"+`%s`, err3, out3)
		}
		out4, err4 := runImport(t, map[string]string{`force`: `true`}, bundle2)
		if err4 != nil {
			t.Fatalf(`force import: %v`+"\n"+`%s`, err4, out4)
		}
		data, rerr := os.ReadFile(filepath.Join(dataDir, `checklog.jsonl`))
		if rerr != nil {
			t.Fatal(rerr)
		}
		if n := strings.Count(string(data), `"detail":"e1"`); n != 1 {
			t.Errorf(`重叠导出重导入不得产生重复行（e1 出现 %d 次）:`+"\n"+`%s`, n, data)
		}
	})
}

// TestProjectImport_KeyMismatchStripsByDefault：来自另一台机器的路径身份 bundle
// （key 不同）默认剥离外来门禁/完成信号；--trust-foreign 显式放行保留。
func TestProjectImport_KeyMismatchStripsByDefault(t *testing.T) {
	resetProjectCmdFlags(t)
	home := t.TempDir()
	machineA := newSyncMachine(t) // 路径身份（无 ID）
	machineB := newSyncMachine(t) // 路径身份，不同路径 → 不同 key

	var bundlePath string
	withMachine(t, machineA, home, func() {
		done := time.Now()
		seedTaskState(t, machineA, `feat/x`, func(s *taskpipeline.TaskState) {
			s.CompletedAt = &done
			s.History = []taskpipeline.TaskGateResult{{Gate: `task-implement`, Passed: true}}
		})
		bundlePath = filepath.Join(t.TempDir(), `x.tar.gz`)
		if out, err := runExport(t, map[string]string{`out`: bundlePath}); err != nil {
			t.Fatalf(`export: %v`+"\n"+`%s`, err, out)
		}
	})

	withMachine(t, machineB, home, func() {
		out, err := runImport(t, map[string]string{}, bundlePath)
		if err != nil {
			t.Fatalf(`import: %v`+"\n"+`%s`, err, out)
		}
		if !strings.Contains(out, `不可信`) {
			t.Errorf(`key 不匹配应判定不可信: %s`, out)
		}
		got, lerr := taskpipeline.LoadTaskState(machineB, `feat/x`)
		if lerr != nil {
			t.Fatalf(`加载任务: %v`, lerr)
		}
		if got.CompletedAt != nil {
			t.Errorf(`默认剥离应清 CompletedAt，got %v`, got.CompletedAt)
		}
		for _, h := range got.History {
			if h.Passed {
				t.Errorf(`默认剥离应剔除外来 Passed 门禁: %+v`, got.History)
			}
		}

		// --trust-foreign：显式放行（换新 bundle 避开账本跳过）
		bundle2 := filepath.Join(t.TempDir(), `x2.tar.gz`)
		withMachine(t, machineA, home, func() {
			if out, err := runExport(t, map[string]string{`out`: bundle2}); err != nil {
				t.Fatalf(`export2: %v`+"\n"+`%s`, err, out)
			}
		})
		out2, err2 := runImport(t, map[string]string{`trust-foreign`: `true`}, bundle2)
		if err2 != nil {
			t.Fatalf(`trust-foreign import: %v`+"\n"+`%s`, err2, out2)
		}
		got2, _ := taskpipeline.LoadTaskState(machineB, `feat/x`)
		if got2.CompletedAt == nil {
			t.Error(`--trust-foreign 应保留完成块`)
		}
	})
}

// TestProjectImport_IDBundleRefusesThenAdopts：ID 身份项目的 bundle 落到路径身份
// 机器默认拒绝；--adopt-id 采纳 bundle 的 ID（迁移本机数据）后按受信同 key 继续。
func TestProjectImport_IDBundleRefusesThenAdopts(t *testing.T) {
	resetProjectCmdFlags(t)
	home := t.TempDir()
	machineA := newSyncMachine(t)
	machineB := newSyncMachine(t)
	id := `fpid_ffffffffffffffffffffffffffffffff`
	if err := os.WriteFile(filepath.Join(machineA, forgedata.ProjectIDFileName), []byte(id+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	idKey := forgedata.IDKey(id)
	done := time.Date(2026, 8, 18, 13, 0, 0, 0, time.UTC)

	var bundlePath string
	withMachine(t, machineA, home, func() {
		seedTaskState(t, machineA, `feat/id`, func(s *taskpipeline.TaskState) {
			s.CompletedAt = &done
		})
		bundlePath = filepath.Join(t.TempDir(), `id.tar.gz`)
		if out, err := runExport(t, map[string]string{`out`: bundlePath}); err != nil {
			t.Fatalf(`export: %v`+"\n"+`%s`, err, out)
		}
	})

	withMachine(t, machineB, home, func() {
		// 本机已有路径身份数据（应被 adopt-id 迁移）
		seedTaskState(t, machineB, `feat/local-b`, nil)

		_, err := runImport(t, map[string]string{}, bundlePath)
		if err == nil || !strings.Contains(err.Error(), `forge project adopt`) {
			t.Fatalf(`默认应拒绝并给 adopt 指引，got err=%v`, err)
		}

		out, err := runImport(t, map[string]string{`adopt-id`: `true`}, bundlePath)
		if err != nil {
			t.Fatalf(`--adopt-id import: %v`+"\n"+`%s`, err, out)
		}
		if got, _ := forgedata.Key(machineB); got != idKey {
			t.Errorf(`adopt-id 后本机 key 应为 ID key，got %s`, got)
		}
		// 本机旧数据已随采纳迁移（SanitizeRef 折叠名）
		if _, serr := os.Stat(filepath.Join(forgedata.RootDir(idKey), `tasks`, taskcontext.SanitizeRef(`feat/local-b`)+`.json`)); serr != nil {
			t.Errorf(`本机旧任务应迁移到 ID key: %v`, serr)
		}
		got, lerr := taskpipeline.LoadTaskState(machineB, `feat/id`)
		if lerr != nil || got.CompletedAt == nil {
			t.Errorf(`采纳后同 key 受信导入应保留完成块: %v %+v`, lerr, got)
		}
	})
}

// TestProjectImport_RefCollisionNeverClobbers：bundle 任务的 TaskRef 折叠命中一个
// 已存在但 ref 不同的本地任务文件（SanitizeRef 碰撞：feat:x 与 feat/x 共享
// feat-x.json）时必须跳过、绝不覆盖本地文件——LoadTaskState 的串号错误不得被
// 误读成「本机无此任务」（审查 blocker #1）。
func TestProjectImport_RefCollisionNeverClobbers(t *testing.T) {
	resetProjectCmdFlags(t)
	home := t.TempDir()
	machineA := newSyncMachine(t)
	machineB := newSyncMachine(t)
	id := `fpid_0123456789abcdef0123456789abcdef`
	for _, m := range []string{machineA, machineB} {
		if err := os.WriteFile(filepath.Join(m, forgedata.ProjectIDFileName), []byte(id+"\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	var bundlePath string
	withMachine(t, machineA, home, func() {
		// A 机任务 ref = feat:x（SanitizeRef 折叠为 feat-x.json，与 feat/x 同文件名）
		seedTaskState(t, machineA, `feat:collision-x`, nil)
		bundlePath = filepath.Join(t.TempDir(), `c.tar.gz`)
		if out, err := runExport(t, map[string]string{`out`: bundlePath}); err != nil {
			t.Fatalf(`export: %v`+"\n"+`%s`, err, out)
		}
	})
	withMachine(t, machineB, home, func() {
		// B 机已有 ref = feat/collision-x（同折叠文件名 feat-x.json）
		seedTaskState(t, machineB, `feat/collision-x`, func(s *taskpipeline.TaskState) {
			s.Goal = `LOCAL MUST SURVIVE`
		})
		out, err := runImport(t, map[string]string{}, bundlePath)
		if err != nil {
			t.Fatalf(`import: %v`+"\n"+`%s`, err, out)
		}
		got, lerr := taskpipeline.LoadTaskState(machineB, `feat/collision-x`)
		if lerr != nil {
			t.Fatalf(`本地任务应原样可读: %v`, lerr)
		}
		if got.Goal != `LOCAL MUST SURVIVE` {
			t.Fatalf(`ref 碰撞时本地任务被覆盖（Goal=%q）——blocker 回归`, got.Goal)
		}
		if !strings.Contains(out, `拒绝覆盖`) {
			t.Errorf(`碰撞应产生 skip 警告: %s`, out)
		}
	})
}

// TestProjectImport_UntrustedStripsSameKey：--untrusted 对同 key（lineage 受信）
// 路径也强制完整剥离——CompletedAt/Passed 历史/Score 清空。
func TestProjectImport_UntrustedStripsSameKey(t *testing.T) {
	resetProjectCmdFlags(t)
	// 真双机隔离（两个 home）：同 key 但各自 DataDir——否则 A 的 seed 对 B 直接
	// 可见，import 走 merge 分支（local 权威），「新增任务被 strip」无从测起。
	homeA, homeB := t.TempDir(), t.TempDir()
	machineA := newSyncMachine(t)
	machineB := newSyncMachine(t)
	id := `fpid_0123456789abcdef0123456789abcdee`
	for _, m := range []string{machineA, machineB} {
		if err := os.WriteFile(filepath.Join(m, forgedata.ProjectIDFileName), []byte(id+"\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	var bundlePath string
	withMachine(t, machineA, homeA, func() {
		done := time.Now()
		seedTaskState(t, machineA, `feat/untrusted`, func(s *taskpipeline.TaskState) {
			s.CompletedAt = &done
			s.History = []taskpipeline.TaskGateResult{{Gate: `task-implement`, Passed: true}}
		})
		bundlePath = filepath.Join(t.TempDir(), `u.tar.gz`)
		if out, err := runExport(t, map[string]string{`out`: bundlePath}); err != nil {
			t.Fatalf(`export: %v`+"\n"+`%s`, err, out)
		}
	})
	withMachine(t, machineB, homeB, func() {
		out, err := runImport(t, map[string]string{`untrusted`: `true`}, bundlePath)
		if err != nil {
			t.Fatalf(`import: %v`+"\n"+`%s`, err, out)
		}
		if !strings.Contains(out, `不可信`) {
			t.Errorf(`--untrusted 应判不可信: %s`, out)
		}
		got, lerr := taskpipeline.LoadTaskState(machineB, `feat/untrusted`)
		if lerr != nil {
			t.Fatal(lerr)
		}
		if got.CompletedAt != nil {
			t.Errorf(`--untrusted 同 key 也应剥 CompletedAt, got %v`+"\n"+`import-out=%s`, got.CompletedAt, out)
		}
		for _, h := range got.History {
			if h.Passed {
				t.Errorf(`--untrusted 应剔除 Passed 历史: %+v`, got.History)
			}
		}
	})
}

// TestProjectImport_TrustedMarksAcceptanceForeign：受信同 key 路径保留结果字段，
// 但外来验收 Run 命令仍打标记——verify-acceptance 的执行闸对外来可执行字符串必须
// 保持武装（审查 major #3：2026-08-15 的命令执行向量不得从 sync 路径回潮）。
func TestProjectImport_TrustedMarksAcceptanceForeign(t *testing.T) {
	resetProjectCmdFlags(t)
	homeA, homeB := t.TempDir(), t.TempDir() // 真双机隔离，理由同上
	machineA := newSyncMachine(t)
	machineB := newSyncMachine(t)
	id := `fpid_0123456789abcdef0123456789abcdfd`
	for _, m := range []string{machineA, machineB} {
		if err := os.WriteFile(filepath.Join(m, forgedata.ProjectIDFileName), []byte(id+"\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	var bundlePath string
	withMachine(t, machineA, homeA, func() {
		done := time.Now()
		seedTaskState(t, machineA, `feat/acc-foreign`, func(s *taskpipeline.TaskState) {
			s.CompletedAt = &done
			s.Acceptance = []taskpipeline.AcceptanceCriterion{{Run: `go test ./...`, Expected: `pass`}}
		})
		bundlePath = filepath.Join(t.TempDir(), `af.tar.gz`)
		if out, err := runExport(t, map[string]string{`out`: bundlePath}); err != nil {
			t.Fatalf(`export: %v`+"\n"+`%s`, err, out)
		}
	})
	withMachine(t, machineB, homeB, func() {
		if out, err := runImport(t, map[string]string{}, bundlePath); err != nil {
			t.Fatalf(`import: %v`+"\n"+`%s`, err, out)
		}
		got, lerr := taskpipeline.LoadTaskState(machineB, `feat/acc-foreign`)
		if lerr != nil {
			t.Fatal(lerr)
		}
		if got.CompletedAt == nil {
			t.Error(`受信路径应保留 CompletedAt`)
		}
		if len(got.Acceptance) != 1 {
			t.Fatalf(`验收 spec 应保留, got %+v`, got.Acceptance)
		}
		if !got.AcceptanceForeign {
			t.Error(`受信路径也必须置 AcceptanceForeign——外来 Run 命令的执行闸不得解除（--trust-foreign 是逃生口）`)
		}
	})
}
