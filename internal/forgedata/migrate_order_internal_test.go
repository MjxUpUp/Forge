package forgedata

// migrate_order_internal_test.go — internal (package forgedata) guard pinning the runtimeDirs
// invariants that the 2026-08-15 trust-boundary fix depends on. External tests can only observe
// behavior; these two contracts are about the declaration itself, so they live next to it.
//
// migrate_order_internal_test.go —— 内部（package forgedata）守卫，钉死 2026-08-15 信任边界
// 修复所依赖的 runtimeDirs 不变量。外部测试只能观察行为；这两条契约关乎声明本身，故放在
// 声明旁边。

import "testing"

// TestRuntimeDirs_TasksLast pins the ordering trust invariant: "tasks" MUST be the LAST entry of
// runtimeDirs. The caller (forge migrate / autoSync) sanitizes foreign gate signals off promoted
// task files only when the tasks move actually completed (tasks ∈ Moved); with tasks earlier in
// the list, a later entry's failure would strand already-promoted, unsanitized task files in the
// trusted DataDir (a re-run then skips the move — skip never sanitizes). A silent reorder of this
// slice would reopen the trust hole with no behavioral test failing, so the declaration itself is
// pinned here.
//
// TestRuntimeDirs_TasksLast 钉死顺序信任不变量："tasks" 必须是 runtimeDirs 的最后一条。
// 调用方（forge migrate / autoSync）只在 tasks 迁移实际完成（tasks ∈ Moved）时对提升的
// task 文件清洗外来门禁信号；tasks 排前面时，更晚条目失败会把已提升、未清洗的 task 文件
// 搁浅在受信 DataDir（重跑会 skip 该次迁移——skip 永不清洗）。静默重排这个 slice 会重开
// 信任缺口而没有任何行为测试失败，故在此直接钉住声明。
func TestRuntimeDirs_TasksLast(t *testing.T) {
	if len(runtimeDirs) == 0 {
		t.Fatal(`runtimeDirs 不应为空（白名单迁移依赖它）`)
	}
	if last := runtimeDirs[len(runtimeDirs)-1]; last != `tasks` {
		t.Errorf(`runtimeDirs 最后一条必须是 "tasks"（信任边界顺序不变量），实得 %q（全表 %v）`, last, runtimeDirs)
	}
}

// TestRuntimeDirs_TrustAnchorsExcluded pins the whitelist exclusion: stamps and hazards are
// repo-committable trust anchors (fingerprints precomputable offline by a hostile repo) and must
// never appear in runtimeDirs — see TestMigrateProject_NeverMigratesStamps/_Hazards for the
// behavioral side.
//
// TestRuntimeDirs_TrustAnchorsExcluded 钉死白名单排除：stamps 与 hazards 是可提交进 repo 的
// 信任锚（敌意 repo 可离线预计算指纹），绝不能出现在 runtimeDirs——行为侧见
// TestMigrateProject_NeverMigratesStamps/_Hazards。
func TestRuntimeDirs_TrustAnchorsExcluded(t *testing.T) {
	names := append(append([]string{}, runtimeDirs...), runtimeFiles...)
	for _, name := range names {
		if name == `stamps` || name == `hazards` {
			t.Errorf(`%q 不得出现在迁移白名单（repo 可提交的信任锚，不是 runtime state），白名单=%v`, name, names)
		}
	}
}
