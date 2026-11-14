package change

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const completeGoal = `# 示例：目标契约

## Objective

让 change 有第五份产物。

## Why

因为目标不该只活在会话里。

## Constraints

必须兼容没有 goal.md 的老 change。

## Out of Scope

不做法务审批流。

## Check

acceptance #1 通过。
`

func TestParseGoal_Complete(t *testing.T) {
	g, err := ParseGoal(completeGoal)
	if err != nil {
		t.Fatalf("ParseGoal: %v", err)
	}
	if len(g.Sections) != 5 {
		t.Fatalf("sections = %d, want 5", len(g.Sections))
	}
	if g.Sections[0].Key != "objective" || !strings.Contains(g.Sections[0].Body, "第五份产物") {
		t.Fatalf("objective section = %+v", g.Sections[0])
	}
}

func TestParseGoal_MissingSection(t *testing.T) {
	partial := strings.Replace(completeGoal, "## Out of Scope\n\n不做法务审批流。\n", "", 1)
	_, err := ParseGoal(partial)
	if err == nil {
		t.Fatal("缺 Out of Scope 应当报错")
	}
	if !strings.Contains(err.Error(), "Out of Scope") {
		t.Fatalf("错误应点名缺段: %v", err)
	}
}

func TestParseGoal_ScaffoldMarkerIsIncomplete(t *testing.T) {
	scaffold := strings.Replace(completeGoal, "不做法务审批流。", "<!-- forge:fill：写 Out of Scope -->", 1)
	if _, err := ParseGoal(scaffold); err == nil {
		t.Fatal("残留 forge:fill 标记应视为未完成")
	}
}

func TestParseGoal_HeadingAliases(t *testing.T) {
	aliased := strings.NewReplacer(
		"## Objective", "## 目标",
		"## Why", "## 原因",
		"## Constraints", "## 约束",
		"## Out of Scope", "## 范围外",
		"## Check", "## 检查",
	).Replace(completeGoal)
	if _, err := ParseGoal(aliased); err != nil {
		t.Fatalf("中文标题应等价: %v", err)
	}
}

func TestLoadGoal_MissingFile(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "spec", "changes", "demo"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, _, _, err := LoadGoal(root, "demo")
	if err == nil || !strings.Contains(err.Error(), "goal.md") {
		t.Fatalf("缺 goal.md 应点名文件: %v", err)
	}
}

func TestGoalHash_Stable(t *testing.T) {
	a := GoalHash([]byte(completeGoal))
	b := GoalHash([]byte(completeGoal))
	if a != b || len(a) != 64 {
		t.Fatalf("hash = %q / %q", a, b)
	}
	if GoalHash([]byte(completeGoal+"x")) == a {
		t.Fatal("内容变化后 hash 必须变化")
	}
}

// TestDocProblems_LegacyChangeWithoutGoalAllowed 钉住老 change 兼容：没有 goal.md
// 的 change 仍能通过产物完整性检查（引入 goal 时不能把所有既有 change 变红）。
func TestDocProblems_LegacyChangeWithoutGoalAllowed(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		FileProposal:   "# 提案\n\n做点事。\n",
		FileDesign:     "# 设计\n\n按提案做。\n",
		FileTasks:      "# 任务\n\n- [ ] 1. 跑（判据：acceptance #1）\n",
		FileAcceptance: "# 验收\n\n```bash\ntrue\n```\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if problems := DocProblems(dir, false); len(problems) != 0 {
		t.Fatalf("没有 goal.md 的老 change 应放行，got %v", problems)
	}
}

// TestDocProblems_IncompleteGoalRejected 钉住「存在但没填完」必须拦：goal.md 生成后
// 留着 forge:fill 标记，verify 不能当它不存在。
func TestDocProblems_IncompleteGoalRejected(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		FileGoal:       "# 目标\n\n## Objective\n\n<!-- forge:fill：写目标 -->\n",
		FileProposal:   "# 提案\n\n做点事。\n",
		FileDesign:     "# 设计\n\n按提案做。\n",
		FileTasks:      "# 任务\n\n- [ ] 1. 跑（判据：acceptance #1）\n",
		FileAcceptance: "# 验收\n\n```bash\ntrue\n```\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	problems := DocProblems(dir, false)
	if len(problems) == 0 || !strings.Contains(strings.Join(problems, "；"), "goal.md") {
		t.Fatalf("未填完的 goal.md 必须被拦并点名，got %v", problems)
	}
}

// TestDocProblems_NewChangeCannotDropGoal 钉住 Code Review P2-1：新 change 由
// change new 创建（带 .goal-required 标记），把 goal.md 删掉不能冒充老 change。
func TestDocProblems_NewChangeCannotDropGoal(t *testing.T) {
	root := newGitRepo(t)
	if _, err := Init(root, "mini"); err != nil {
		t.Fatal(err)
	}
	dir, err := New(root, "dropper", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, GoalRequiredFile)); err != nil {
		t.Fatalf("change new 应留下 %s 标记: %v", GoalRequiredFile, err)
	}
	if err := os.Remove(filepath.Join(dir, FileGoal)); err != nil {
		t.Fatal(err)
	}
	probs := DocProblems(dir, false)
	joined := strings.Join(probs, "\n")
	if !strings.Contains(joined, "goal.md") {
		t.Fatalf("删掉 goal.md 的新 change 必须被 verify 拦下，problems=%v", probs)
	}
}

// TestDocProblems_LegacyChangeWithoutGoalStillAllowed 是老 change 的兼容承诺：
// 没有 .goal-required 标记、也没有 goal.md 时不报 goal 问题。
func TestDocProblems_LegacyChangeWithoutGoalStillAllowed(t *testing.T) {
	root := newGitRepo(t)
	if _, err := Init(root, "mini"); err != nil {
		t.Fatal(err)
	}
	dir, err := New(root, "legacy", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, GoalRequiredFile)); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, FileGoal)); err != nil {
		t.Fatal(err)
	}
	for _, p := range DocProblems(dir, false) {
		if strings.Contains(p, "goal.md") {
			t.Fatalf("无标记的老 change 不该因缺 goal.md 被判问题: %v", p)
		}
	}
}

// TestGoalTemplateMatchesParser 钉住模板与解析器的联动（Code Review P2-3）：
// 模板标题拼错/少一段/多一段，所有 change new 生成的 goal.md 都会永远填不完。
// 这里同时断言标题集合一致、填完标记后可解析。
func TestGoalTemplateMatchesParser(t *testing.T) {
	var heads []string
	for _, line := range strings.Split(tmplGoal, "\n") {
		if strings.HasPrefix(line, "## ") {
			heads = append(heads, strings.TrimSpace(strings.TrimPrefix(line, "## ")))
		}
	}
	if len(heads) != len(goalSectionSpecs) {
		t.Fatalf("模板有 %d 个二级标题，解析器要求 %d 段: %v", len(heads), len(goalSectionSpecs), heads)
	}
	for i, spec := range goalSectionSpecs {
		if heads[i] != spec.Title {
			t.Errorf("第 %d 段标题 = %q，want %q（顺序也要一致）", i+1, heads[i], spec.Title)
		}
	}

	var lines []string
	for _, line := range strings.Split(tmplGoal, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), scaffoldMarker) {
			lines = append(lines, "内容：把这一段写清楚。")
			continue
		}
		lines = append(lines, line)
	}
	filled := strings.Join(lines, "\n")
	filled = strings.ReplaceAll(filled, "{{id}}", "demo")
	filled = strings.ReplaceAll(filled, "{{title}}", "演示")
	g, err := ParseGoal(filled)
	if err != nil {
		t.Fatalf("按模板填完后必须可解析: %v\n%s", err, filled)
	}
	if len(g.Sections) != len(goalSectionSpecs) {
		t.Fatalf("解析出 %d 段，want %d", len(g.Sections), len(goalSectionSpecs))
	}
}
