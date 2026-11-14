package change

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// FileGoal 是 goal 契约的产物文件名：change 的第五份产物，随 git 走。
const FileGoal = "goal.md"

// GoalSection 是 goal 契约的必填段落。五段对应「交代目标/原因/约束/范围外/检查」：
// 缺任何一段，agent 都能把「做完了」解释成另一件事。
type GoalSection struct {
	Key   string `json:"key"`
	Title string `json:"title"`
	Body  string `json:"body"`
}

// Goal 是 goal.md 的结构化视图（不缓存，每次从文件解析）。
type Goal struct {
	Sections []GoalSection `json:"sections"`
}

// goalSectionSpecs 是段落键与标题的单一真相源；顺序即渲染顺序。
var goalSectionSpecs = []GoalSection{
	{Key: "objective", Title: "Objective"},
	{Key: "why", Title: "Why"},
	{Key: "constraints", Title: "Constraints"},
	{Key: "out_of_scope", Title: "Out of Scope"},
	{Key: "check", Title: "Check"},
}

// GoalSectionKeys 返回必填段落键（供 CLI/文档展示）。
func GoalSectionKeys() []string {
	out := make([]string, 0, len(goalSectionSpecs))
	for _, s := range goalSectionSpecs {
		out = append(out, s.Key)
	}
	return out
}

// renderGoalTemplate 生成带 forge:fill 标记的 goal.md 骨架。
func renderGoalTemplate(id, title string) string {
	out := strings.ReplaceAll(tmplGoal, "{{title}}", title)
	out = strings.ReplaceAll(out, "{{id}}", id)
	return out
}

// ParseGoal 解析 goal.md：按二级标题取五段，标题大小写与空格不敏感
// （"Out of Scope"/"OutOfScope" 等价）。缺段/空段/残留 forge:fill 标记都算未完成。
func ParseGoal(content string) (*Goal, error) {
	sections := map[string]string{}
	var current string
	var buf []string
	flush := func() {
		if current != "" {
			sections[current] = strings.TrimSpace(strings.Join(buf, "\n"))
		}
		buf = nil
	}
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "## ") {
			flush()
			current = normalizeGoalHeading(strings.TrimSpace(strings.TrimPrefix(line, "## ")))
			continue
		}
		if current != "" {
			buf = append(buf, line)
		}
	}
	flush()

	g := &Goal{}
	var missing []string
	for _, spec := range goalSectionSpecs {
		body, ok := sections[spec.Key]
		if !ok || body == "" {
			missing = append(missing, spec.Title)
			continue
		}
		if strings.Contains(body, scaffoldMarker) {
			missing = append(missing, spec.Title+"（还留着 forge:fill 标记）")
			continue
		}
		g.Sections = append(g.Sections, GoalSection{Key: spec.Key, Title: spec.Title, Body: body})
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("goal 契约未完成：缺 %s（五段：%s）", strings.Join(missing, "、"), strings.Join(GoalSectionKeys(), " / "))
	}
	return g, nil
}

func normalizeGoalHeading(h string) string {
	switch strings.ToLower(strings.ReplaceAll(strings.TrimSpace(h), " ", "")) {
	case "objective", "目标":
		return "objective"
	case "why", "原因", "为什么":
		return "why"
	case "constraints", "约束":
		return "constraints"
	case "outofscope", "范围外", "不在范围":
		return "out_of_scope"
	case "check", "检查", "验收":
		return "check"
	}
	return ""
}

// GoalPath 返回 change 的 goal.md 路径。
func GoalPath(root, id string) string { return filepath.Join(ChangeDir(root, id), FileGoal) }

// LoadGoal 读取并解析 change 的 goal.md，返回契约、原始字节的 sha256 与仓库相对路径。
func LoadGoal(root, id string) (*Goal, string, string, error) {
	path := GoalPath(root, id)
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, "", "", fmt.Errorf("change %q 没有 goal.md（第五份产物）：先补 %s", id, relTo(root, path))
		}
		return nil, "", "", fmt.Errorf("读取 %s: %w", relTo(root, path), err)
	}
	g, err := ParseGoal(string(raw))
	if err != nil {
		return nil, "", "", fmt.Errorf("%s：%w", relTo(root, path), err)
	}
	sum := sha256.Sum256(raw)
	return g, hex.EncodeToString(sum[:]), relTo(root, path), nil
}

// GoalHash 返回任意 goal 文件内容的 sha256（小写十六进制）。
func GoalHash(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}
