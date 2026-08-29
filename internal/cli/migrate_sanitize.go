package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/MjxUpUp/Forge/internal/util"
)

// migrate_sanitize.go —— 迁移后信任边界清洗（2026-08-15 审查，V5）。
//
// .forge/ 是可提交进 repo 的，从中提升进用户级 DataDir 的任何 task state 都是攻击者可书写的
// 内容：clone 一个恶意仓库即可带上 .forge/tasks/*.json，内含 CompletedAt + 已通过门禁 History +
// ReviewPassed + 验收 Passed——进 DataDir 后即被当作本机信任状态（仅 CompletedAt 一项就会关掉
// 所有 CompletedAt==nil 守卫的硬检查并让 complete 门禁自动通过）。MigrateProject 逐字搬文件；
// 本清洗 pass 在搬完后立即剥离外来门禁信号，复用与 task import 共用的单一真相源
// taskpipeline.StripForeignGateSignals。forgedata 不能自带此逻辑：taskpipeline import 了
// forgedata，反向依赖会是 import cycle。

// sanitizeMigratedTasks 在 `tasks` 从 .forge/ 迁入后，原位剥离项目 DataDir/tasks 下所有 task
// 文件的外来门禁信号。返回清洗的任务数。任何失败都是硬错误（fail-closed）：迁移已经发生，
// 清洗失败却让 hostile 文件留在受信 DataDir、`forge migrate` 以 0 退出——正是 2026-08-15 审查
// 点名的退出码契约违背（清洗不得 fail-open）。
//
// 文件原位处理（读原始 JSON → 剥离 → 原子写回同一路径），不走 ListTaskStates/SaveTaskState：
// SaveTaskState 用 SanitizeRef(TaskRef) 重推文件名，hostile 文件 xyz.json 内带 task_ref
// "feat/evil" 会被改写成 feat-evil.json 而原文件的未清洗字节残留（两个 ref 碰撞时还会静默
// 坍缩成一个）。原位处理同时覆盖 ListTaskStates 会因文件名/ref 不匹配而跳过的文件。
//
// 何时清洗 DataDir 的「全部」任务是正确的？MigrateProject 整树搬 tasks 目录：DataDir 已有
// tasks 目录时 move 被 SKIP（无内容进入，无需清洗）；只有 DataDir/tasks 不存在时整树落地
// ——此时其下每个 task 文件都来自可提交的 .forge/。故「tasks ∈ Moved」（非 dry-run）⟹
// DataDir 内全部任务都是外来源。清洗就是 StripForeignGateSignals 的契约：保留 spec/溯源，
// 剥离信任与控制流信号。
func sanitizeMigratedTasks(root string) (int, error) {
	tasksDir := filepath.Join(forgedata.DataDirFor(root), "tasks")
	entries, err := os.ReadDir(tasksDir)
	if err != nil {
		return 0, fmt.Errorf("sanitize: read migrated tasks dir: %w", err)
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		path := filepath.Join(tasksDir, e.Name())
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return n, fmt.Errorf("sanitize: read %s: %w", e.Name(), rerr)
		}
		var s taskpipeline.TaskState
		if uerr := json.Unmarshal(data, &s); uerr != nil {
			// 畸形文件在此不可跳过：本目录内所有文件都是本次从未信 .forge/ 落地的（见上方
			// 「何时清洗全部正确」论证），留下任何未检字节就是 fail-open。
			return n, fmt.Errorf("sanitize: unmarshal %s: %w", e.Name(), uerr)
		}
		taskpipeline.StripForeignGateSignals(&s)
		out, merr := json.MarshalIndent(&s, "", "  ")
		if merr != nil {
			return n, fmt.Errorf("sanitize: marshal %s: %w", e.Name(), merr)
		}
		if werr := util.AtomicWrite(path, out, 0644); werr != nil {
			return n, fmt.Errorf("sanitize: write %s: %w", e.Name(), werr)
		}
		n++
	}
	return n, nil
}

// migratedTasksMoved 报告本次迁移是否实际把 tasks 目录落进了 DataDir（即
// sanitizeMigratedTasks 的触发条件）。DryRun 不移动，永不触发。部分结果同样算：tasks 已
// 落地后更晚的条目失败时，MigrateProject 会连同 error 一起返回部分 MigrationResult——
// 调用方仍须清洗。
func migratedTasksMoved(moved []string) bool {
	for _, m := range moved {
		if m == "tasks" {
			return true
		}
	}
	return false
}

// sanitizePendingMarker 是 DataDir 点文件，记录「外来 task 文件已落地但其清洗 pass 失败」
// ——让一次失败的清洗不至于变成永久：失败后 tasks 目录已在 DataDir，重跑 migrate/autoSync
// 会 SKIP 该次迁移（dst 已存在），清洗触发条件（tasks ∈ Moved）永不再燃。之后的
// forge migrate / autoSync 见标记即重试；成功清除。
//
// 权衡（已接受）：若用户在失败与重试之间在同一 DataDir 里起了真正本机的任务，重试会把
// 它们一并清洗。窗口天然罕见（清洗失败 = IO 故障），且剥掉本机门禁信号可恢复（重跑门禁），
// 而把攻击者可书写的门禁信号留在受信 DataDir 里存活不可恢复。
const sanitizePendingMarker = `.foreign-task-sanitize-pending`

// sanitizeAfterMigration 是两个调用方（forge migrate + autoSync）共用的唯一可重试入口：
// 本次 run 落地了 tasks 目录、或上次清洗留下了 pending 标记时执行清洗。返回（清洗数, error）。
func sanitizeAfterMigration(root string, moved []string) (int, error) {
	dataDir := forgedata.DataDirFor(root)
	marker := filepath.Join(dataDir, sanitizePendingMarker)
	should := migratedTasksMoved(moved)
	if !should {
		if _, err := os.Stat(marker); err == nil {
			should = true // retry a previously failed sanitize（重试上次失败的清洗）
		}
	}
	if !should {
		return 0, nil
	}
	n, err := sanitizeMigratedTasks(root)
	if err != nil {
		// 留下/刷新标记，让下次 migrate/autoSync 重试。
		_ = os.MkdirAll(dataDir, 0755)
		_ = os.WriteFile(marker, []byte(`foreign task files landed; sanitize failed; retry pending`), 0644)
		return n, err
	}
	// 成功清除标记（忽略删除错误——残留标记只会在将来多触发一次幂等清洗；清洗对已干净
	// 文件本身是 no-op）。
	_ = os.Remove(marker)
	return n, nil
}
