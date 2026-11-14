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
