package clitask

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/taskpipeline"
)

// TestApproveDigest 钉住 W5.1 + W2.3/W2.4 的 CLI 链路：登记 design → 触点写入 PlanScope；
// --approve 先打印一屏摘要（TL;DR/高危步骤/Open Questions/变化）并记录 --checked；改内容
// 重登记后再审批，摘要点名变化章节；--check 报出不存在的引用。
func TestApproveDigest(t *testing.T) {
	dir, taskRef := setupChainTask(t, "version: 1\nstages:\n  - name: design\n    mode: human\n")
	v1 := strings.Join([]string{
		"## TL;DR",
		"采用增量快照，不改存储格式。",
		"## 设计",
		"### 触点",
		"- `internal/a/a.go`",
		"## 风险与高危步骤",
		"- 删除旧索引（不可逆）",
		"## Open Questions",
		"- Q1 快照保留几天？",
		"",
	}, "\n")
	src := filepath.Join(dir, "design-src.md")
	if err := os.WriteFile(src, []byte(v1), 0o644); err != nil {
		t.Fatal(err)
	}
	captureStdout(t, func() {
		if err := runArtifactSet("design", src, false); err != nil {
			t.Fatalf("set: %v", err)
		}
	})
	st, err := taskpipeline.LoadTaskState(dir, taskRef)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(st.PlanScope, []string{"internal/a/a.go"}) || st.PlanScopeSource != taskpipeline.PlanScopeSourceDesign {
		t.Fatalf("登记 design 应把触点写入 PlanScope: %v / %q", st.PlanScope, st.PlanScopeSource)
	}

	out := captureStdout(t, func() {
		if err := runArtifactApprove("design", "reviewer", []string{"高危步骤", "接口契约"}); err != nil {
			t.Fatalf("approve: %v", err)
		}
	})
	for _, frag := range []string{"审批摘要：design", "采用增量快照", "删除旧索引（不可逆）", "Q1 快照保留几天？", "首次审批", "已审批 design"} {
		if !strings.Contains(out, frag) {
			t.Errorf("摘要缺 %q:\n%s", frag, out)
		}
	}
	st, _ = taskpipeline.LoadTaskState(dir, taskRef)
	if got := st.ArtifactApprovals["design"].Checked; !reflect.DeepEqual(got, []string{"高危步骤", "接口契约"}) {
		t.Errorf("Checked 应落审批记录: %v", got)
	}

	// 改风险章节后重登记 → 再审批：摘要点名变化章节。
	v2 := strings.Replace(v1, "- 删除旧索引（不可逆）", "- 删除旧索引（不可逆）\n- 回填历史数据（耗时 2h）", 1)
	if err := os.WriteFile(src, []byte(v2), 0o644); err != nil {
		t.Fatal(err)
	}
	captureStdout(t, func() {
		if err := runArtifactSet("design", src, false); err != nil {
			t.Fatalf("re-set: %v", err)
		}
	})
	out = captureStdout(t, func() {
		if err := runArtifactApprove("design", "reviewer", nil); err != nil {
			t.Fatalf("re-approve: %v", err)
		}
	})
	if !strings.Contains(out, "+1 / -0 行；涉及章节：风险与高危步骤") {
		t.Errorf("再审批摘要应点名变化章节:\n%s", out)
	}
	if !strings.Contains(out, "--checked") {
		t.Errorf("未声明审过内容时应提示 --checked:\n%s", out)
	}

	// --check：已登记 design 的触点文件不存在 → 报 path。
	out = captureStdout(t, func() {
		if err := runArtifactCheck(""); err != nil {
			t.Fatalf("check: %v", err)
		}
	})
	if !strings.Contains(out, "[path] internal/a/a.go") {
		t.Errorf("--check 应报不存在的触点文件:\n%s", out)
	}
	if err := os.MkdirAll(filepath.Join(dir, "internal", "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "internal", "a", "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out = captureStdout(t, func() {
		if err := runArtifactCheck(""); err != nil {
			t.Fatalf("check: %v", err)
		}
	})
	if !strings.Contains(out, "方案事实核查通过") {
		t.Errorf("文件补上后应通过:\n%s", out)
	}
}
