package evalkit

// golden_harvest_test.go — L3 harvest 的守卫：canonical 目录零写入（golden 只进
// 人工策展红线）+ 投影形态 + append-only（已有候选不覆盖）。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
)

func TestHarvestCandidates_CanonicalZeroWrite(t *testing.T) {
	root := t.TempDir()
	tasksDir := filepath.Join(forgedata.DataDirFor(root), "tasks")
	if err := os.MkdirAll(tasksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	st := &taskpipeline.TaskState{TaskRef: "feat/harvest", Acceptance: taskpipeline.ParseAcceptance([]string{`go version :: go version`})}
	st.CompletedAt = &now
	if err := taskpipeline.SaveTaskState(root, st); err != nil {
		t.Fatal(err)
	}
	goldenDir := filepath.Join(root, "evals", "forge", "golden")
	if err := os.MkdirAll(goldenDir, 0o755); err != nil {
		t.Fatal(err)
	}
	canary := filepath.Join(goldenDir, "MANIFEST.sha256")
	if err := os.WriteFile(canary, []byte("pinned"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := HarvestCandidates(root, time.Time{})
	if err != nil {
		t.Fatalf(`HarvestCandidates: %v`, err)
	}
	if res.Tasks != 1 || res.Written != 1 {
		t.Fatalf(`应 1 任务 1 候选，got %+v`, res)
	}
	// canonical 零写入：manifest 与 canonical 目录内容不变；候选只落 candidates/。
	got, _ := os.ReadFile(canary)
	if string(got) != "pinned" {
		t.Error(`canonical 目录（MANIFEST 等）绝不可被 harvest 写入`)
	}
	cand, err := os.ReadFile(filepath.Join(goldenDir, "candidates", "cand-feat-harvest-0.yaml"))
	if err != nil {
		t.Fatalf(`候选未落盘: %v`, err)
	}
	if !strings.Contains(string(cand), "HARVEST CANDIDATE") || !strings.Contains(string(cand), "verify-acceptance") {
		t.Errorf(`候选骨架缺头部/门禁字段: %s`, cand[:120])
	}
	// append-only：重跑不覆盖（内容不变——计数 Written=0）。
	res2, err := HarvestCandidates(root, time.Time{})
	if err != nil || res2.Written != 0 {
		t.Errorf(`重复 harvest 应零新增（append-only），got %+v err=%v`, res2, err)
	}
	// --since 过滤：晚于任务完成时间的 since → 跳过。
	res3, err := HarvestCandidates(root, now.Add(time.Hour))
	if err != nil || res3.SkippedOld != 1 {
		t.Errorf(`since 晚于完成时间应跳过，got %+v err=%v`, res3, err)
	}
}

// TestTrapCase_CheatPatternTypes 钉住陷阱类型枚举扩容（traps 扩容批）：cheat-scan
// 七模式族经目录加载合法、拼错类型加载拒绝——枚举是策展契约，静默放宽会稀释类型语义。
func TestTrapCase_CheatPatternTypes(t *testing.T) {
	dir := t.TempDir()
	write := func(name, typ string) {
		body := "id: " + name + "\ntype: " + typ + "\ndescription: d\nprobe_argv: [x]\ndetect_any: [exit_nonzero]\n"
		if err := os.WriteFile(filepath.Join(dir, name+".yaml"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("t-ok", "path-assumption")
	if _, err := LoadTrapDir(dir); err != nil {
		t.Errorf(`cheat-scan 模式类型应合法: %v`, err)
	}
	dir2 := t.TempDir()
	body := "id: t-bogus\ntype: bogus-pattern\ndescription: d\nprobe_argv: [x]\ndetect_any: [exit_nonzero]\n"
	if err := os.WriteFile(filepath.Join(dir2, "t-bogus.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadTrapDir(dir2); err == nil {
		t.Error(`未知陷阱类型应被加载器拒绝`)
	}
}
