package taskpipeline

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// TestReqHygieneDesignStage 钉住 W2.2：design 产物进卫生扫描（歧义标记带 stage 前缀、
// NEEDS CLARIFICATION 标记），Open Questions 未关闭项逐条报出、已关闭项不报。
func TestReqHygieneDesignStage(t *testing.T) {
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	root := t.TempDir()
	state := &TaskState{TaskRef: "feat/design-hygiene"}
	design := strings.Join([]string{
		"## TL;DR",
		"采用 A，[NEEDS CLARIFICATION: 鉴权方式]",
		"## Open Questions",
		"- Q1 缓存失效策略？",
		"- [x] Q2 已经确认",
		"- Q3 已决：走方案 B",
		"- ~~Q4 作废~~",
		"## 验证与回滚",
		"`go test ./...`",
	}, "\n")
	ref, err := WriteArtifact(root, state.TaskRef, "design", design)
	if err != nil {
		t.Fatal(err)
	}
	state.SpecArtifacts = map[string]ArtifactRef{"design": ref}

	findings := CheckRequirementsHygiene(root, state)
	var ambiguity, open []string
	for _, f := range findings {
		switch f.Category {
		case "ambiguity":
			ambiguity = append(ambiguity, f.Detail)
		case "open-question":
			open = append(open, f.Detail)
		}
	}
	if len(ambiguity) != 1 || !strings.HasPrefix(ambiguity[0], "design: ") || !strings.Contains(ambiguity[0], "NEEDS CLARIFICATION") {
		t.Errorf("design 歧义标记应带 stage 前缀并识别 NEEDS CLARIFICATION, got %v", ambiguity)
	}
	if len(open) != 1 {
		t.Fatalf("只有 Q1 未关闭, got %v", open)
	}
	// 行号相对落盘产物（WriteArtifact 会加 frontmatter）——回读核对该行确是 Q1。
	stored, err := os.ReadFile(artifactAbsPath(root, ref))
	if err != nil {
		t.Fatal(err)
	}
	var lineNo int
	if _, err := fmt.Sscanf(open[0][strings.Index(open[0], "行 ")+len("行 "):], "%d", &lineNo); err != nil {
		t.Fatalf("finding 应带行号: %q", open[0])
	}
	if lines := strings.Split(string(stored), "\n"); lineNo < 1 || lineNo > len(lines) || !strings.Contains(lines[lineNo-1], "Q1") {
		t.Errorf("行 %d 应指向 Q1 未关闭项", lineNo)
	}

	t.Run("只有 design 产物也会接线扫描（不再只认 spec）", func(t *testing.T) {
		if _, has := state.SpecArtifacts["spec"]; has {
			t.Fatal("前提：本用例无 spec 产物")
		}
		adviseReqHygiene(root, state)
		if !findCheckEntry(t, root, state.TaskRef, CheckNameReqHygiene) {
			t.Fatal("仅登记 design 时也应落 req-hygiene 条目")
		}
	})
}

// TestOpenQuestionsPlaceholdersAndFences 钉住审查轮修复：「无/None」占位项不算未决
// 问题；围栏内的 # 行不结束 Open Questions 一节。
func TestOpenQuestionsPlaceholdersAndFences(t *testing.T) {
	if got := scanOpenQuestions("## Open Questions\n- 无\n- None\n- N/A\n"); len(got) != 0 {
		t.Errorf("占位项不应报: %v", got)
	}
	text := "## Open Questions\n```sh\n# 示例注释\n```\n- Q1 未决\n"
	if got := scanOpenQuestions(text); len(got) != 1 {
		t.Errorf("围栏内 # 不应结束本节，Q1 应被报出: %v", got)
	}
}
