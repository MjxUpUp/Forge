// Package skillintegrate 承载 skill 的 forge 集成知识（forge 侧资产，依赖单向契约的
// 承接面）。skills 零反向依赖契约（CONVENTIONS §13）迁出各 skill 的 forge 集成内容
// （条件块 / references/forge-integration.md / 双路径）后，知识点落在本包内嵌的
// notes/<skill>.md；forge 用户经 `forge skills integration <skill>` 查看，skill-trigger
// 推荐命中时在推荐块内追加一行指针。skills-only 消费方（拷 skills/ 目录、不装 forge）
// 不携带本包内容——这正是契约要的形态：方法论完整在中立库，forge 增强完整在 forge 侧。
//
// Package skillintegrate hosts the forge-side integration knowledge for skills
// (a forge-owned asset — the receiving end of the one-way dependency contract).
// After the zero-reverse-dependency migration (CONVENTIONS §13) extracted each
// skill's forge integration content (conditional blocks /
// references/forge-integration.md / dual-path halves), the knowledge landed in
// the embedded notes/<skill>.md here; forge users read them via
// `forge skills integration <skill>`, and skill-trigger appends a pointer line
// to recommendation blocks. Skills-only consumers (copying the skills/ tree,
// no forge installed) carry none of this — exactly the contract's shape:
// methodology complete in the neutral library, forge enhancements on the
// forge side.
package skillintegrate

import (
	"embed"
	"fmt"
	"slices"
)

// notesFS 内嵌集成笔记（notes/<skill>.md，文件名即 skill 名）。与 skills/ 不同，
// 这里是 forge 专属内容——允许且只含 forge 命令/路径/机制。
//
// notesFS embeds the integration notes (notes/<skill>.md, filename = skill
// name). Unlike skills/, this is forge-only content — forge commands, paths
// and mechanisms are expected and allowed here.
//
//go:embed notes/*.md
var notesFS embed.FS

// Lookup 返回 skill 的 forge 集成笔记内容（无则 ok=false）。
//
// Lookup returns the forge integration note for a skill (ok=false when absent).
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

// Has 报告 skill 是否有集成笔记（渲染层指针用，避免读全文）。
//
// Has reports whether a skill has an integration note (used by the render
// layer's pointer, avoiding a full read).
func Has(skill string) bool {
	_, ok := Lookup(skill)
	return ok
}

// List 返回有集成笔记的 skill 名（字典序）。
//
// List returns the skill names that have integration notes (sorted).
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

// PointerLine 返回 skill-trigger 推荐块里的指针行（无笔记返空串）。文案走
// skilltrigger 的输出契约：事实陈述、非祈使、不含 ASCII 双引号。
//
// PointerLine returns the pointer line for skill-trigger recommendation
// blocks (empty string when no note exists). Wording follows skilltrigger's
// output contract: factual, non-imperative, no ASCII double quotes.
func PointerLine(skill string) string {
	if !Has(skill) {
		return ""
	}
	return fmt.Sprintf("    forge 集成（仅 forge 项目）：forge skills integration %s", skill)
}
