package forgedata

// migrate_internal_test.go — internal guards for migrateOne/statExists error classification.
// NUL in a path makes os.Stat fail with EINVAL (not IsNotExist) deterministically on both
// Windows and Unix, giving a portable "non-NotExist stat error" fixture.
//
// migrate_internal_test.go —— migrateOne/statExists 错误分类的内部守卫。
// 路径含 NUL 让 os.Stat 在 Windows/Unix 上确定性失败为 EINVAL（非 IsNotExist），
// 提供跨平台的「非 NotExist stat 错误」夹具。

import (
	"os"
	"path/filepath"
	"testing"
)

// TestStatExists_Classifies: exists → (true,nil); missing → (false,nil); invalid path (NUL) →
// real error, never collapsed into "does not exist".
//
// TestStatExists_Classifies：存在 → (true,nil)；缺失 → (false,nil)；非法路径（NUL）→
// 真实 error，绝不被吞成「不存在」。
func TestStatExists_Classifies(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, `f`)
	if err := os.WriteFile(existing, []byte(`x`), 0644); err != nil {
		t.Fatal(err)
	}

	if ok, err := statExists(existing); err != nil || !ok {
		t.Errorf(`existing: got (%v, %v), want (true, nil)`, ok, err)
	}
	if ok, err := statExists(filepath.Join(dir, `missing`)); err != nil || ok {
		t.Errorf(`missing: got (%v, %v), want (false, nil)`, ok, err)
	}
	if ok, err := statExists(dir + string(filepath.Separator) + "bad\x00path"); err == nil || ok {
		t.Errorf(`NUL path: got (%v, %v), want (false, real error)`, ok, err)
	}
}

// TestMigrateOne_StatErrorNotTreatedAsAbsent pins the fix: a non-NotExist dst stat error aborts
// with an error in BOTH dry-run and real modes, and the source is left untouched (previously the
// error was read as "dst absent", so real mode could move src into an unverified dst and dry-run
// would report a phantom migration).
//
// TestMigrateOne_StatErrorNotTreatedAsAbsent 钉死修复：dst stat 的非 NotExist 错误在
// dry-run 与实跑两种模式下都必须报错中止，且源文件不动（旧实现把错误读成「dst 不存在」，
// 实跑可能把 src 移进未验证的 dst，dry-run 会报告虚假迁移）。
func TestMigrateOne_StatErrorNotTreatedAsAbsent(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, `src`)
	if err := os.WriteFile(src, []byte(`data`), 0644); err != nil {
		t.Fatal(err)
	}
	badDst := filepath.Join(dir, "bad\x00dst")

	for _, dryRun := range []bool{false, true} {
		moved, err := migrateOne(src, badDst, MigrateOptions{DryRun: dryRun})
		if err == nil {
			t.Errorf(`dryRun=%v: expect error for invalid dst, got nil (moved=%v)`, dryRun, moved)
		}
		if moved {
			t.Errorf(`dryRun=%v: must not report moved on stat error`, dryRun)
		}
	}
	// src untouched in both modes.
	//
	// 两种模式下 src 均未被改动
	if data, err := os.ReadFile(src); err != nil || string(data) != `data` {
		t.Errorf(`src must stay untouched, got %q, err=%v`, string(data), err)
	}
}
