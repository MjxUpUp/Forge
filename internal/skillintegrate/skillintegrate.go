// Package skillintegrate hosts the forge-side integration knowledge for skills.
//
// Package skillintegrate 承载 skill 的 forge 集成知识（forge 侧资产，依赖单向契约的
// 承接面）。skills 零反向依赖契约（CONVENTIONS §13）迁出各 skill 的 forge 集成内容
// （条件块 / references/forge-integration.md / 双路径）后，知识点落在本包内嵌的
// notes/<skill>.md；forge 用户经 `forge skills integration <skill>` 查看，skill-trigger
// 推荐命中时在推荐块内追加一行指针。skills-only 消费方（拷 skills/ 目录、不装 forge）
// 不携带本包内容——这正是契约要的形态：方法论完整在中立库，forge 增强完整在 forge 侧。
package skillintegrate

import (
	"embed"
	"fmt"
	"slices"
)

// notesFS 内嵌集成笔记（notes/<skill>.md，文件名即 skill 名）。与 skills/ 不同，
// 这里是 forge 专属内容——允许且只含 forge 命令/路径/机制。
//
//go:embed notes/*.md
var notesFS embed.FS

// Lookup returns the forge integration note for a skill (ok=false when absent).
//
// Lookup 返回 skill 的 forge 集成笔记内容（无则 ok=false）。
func Lookup(skill string) (string, bool) {
	if skill == "" {
		return "", false
	}
	b, err := notesFS.ReadFile("notes/" + skill + ".md")
	if err != nil {
		return "", false
	}
	return string(b), true
}

// Has reports whether a skill has an integration note.
//
// Has 报告 skill 是否有集成笔记（渲染层指针用，避免读全文）。
func Has(skill string) bool {
	_, ok := Lookup(skill)
	return ok
}

// List returns the skill names that have integration notes (sorted).
//
// List 返回有集成笔记的 skill 名（字典序）。
func List() []string {
	entries, err := notesFS.ReadDir("notes")
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if len(name) > 3 && name[len(name)-3:] == ".md" {
			out = append(out, name[:len(name)-3])
		}
	}
	slices.Sort(out)
	return out
}

// PointerLine returns the pointer line for skill-trigger recommendation blocks.
//
// PointerLine 返回 skill-trigger 推荐块里的指针行（无笔记返空串）。文案走
// skilltrigger 的输出契约：事实陈述、非祈使、不含 ASCII 双引号。
func PointerLine(skill string) string {
	if !Has(skill) {
		return ""
	}
	return fmt.Sprintf("    forge 集成（仅 forge 项目）：forge skills integration %s", skill)
}
