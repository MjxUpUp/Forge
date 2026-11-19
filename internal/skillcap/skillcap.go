// Package skillcap 把「这次踩过的坑」沉淀成一份可被宿主加载的 SKILL.md。
//
// 定位（调研结论的 P2「skill 资本化」）：失败教训只有落成 skill 才会在下次任务
// 开始时真正回到模型眼前；写在会话里、写在 retro 里都会随会话消失。本包只做
// 一件事——给定 id、描述、适用时机、教训与证据，渲染并写入 skills/<id>/SKILL.md。
//
// 刻意不做的事：不自动从 retro 文本里「总结」教训（那是模型该干的判断），不覆盖
// 已有 skill（除非显式 --force），不生成 triggers（触发器属于宿主接线决策）。
package skillcap

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Draft 是一份待落盘的 skill 草稿。
//
// Draft 只承载「已经想清楚的内容」：Description 一句话说明做什么，WhenToUse 写明
// 什么时候该用（宿主按这段做路由），Lesson 是这次踩坑的根因与做法，Evidence 指向
// 证据（commit/测试/门禁记录）。
type Draft struct {
	ID          string
	Description string
	WhenToUse   string
	Lesson      string
	Evidence    string
}

var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,63}$`)

// Validate 拒绝会写到 skills/ 之外或覆盖同名 skill 的输入。
//
// id 限定为小写字母/数字/连字符：它同时是目录名与 frontmatter 的 name，允许大写或
// 点号会让 Windows/macOS 上出现「同 id 不同目录」的分叉。
func (d Draft) Validate() error {
	if !idPattern.MatchString(d.ID) {
		return fmt.Errorf("skill id %q 不合法：只用小写字母、数字与连字符（2-64 字符）", d.ID)
	}
	if strings.TrimSpace(d.Description) == "" {
		return fmt.Errorf("skill %s 缺 description：一句话说明它做什么（宿主靠它路由）", d.ID)
	}
	if strings.TrimSpace(d.WhenToUse) == "" {
		return fmt.Errorf("skill %s 缺 when-to-use：写清什么时机该用它", d.ID)
	}
	if strings.TrimSpace(d.Lesson) == "" {
		return fmt.Errorf("skill %s 缺 lesson：把这次的根因与做法写下来，否则不叫资本化", d.ID)
	}
	return nil
}

// Render 渲染 SKILL.md 的完整内容。
//
// frontmatter 只写 name/description 与最小 metadata：description 里内联 Use when /
// SKIP，是为了让宿主只读一行就能判断要不要加载（与既有 skills 同款约定）。
func Render(d Draft) (string, error) {
	if err := d.Validate(); err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "---\nname: %s\n", d.ID)
	fmt.Fprintf(&b, "description: %q\n", strings.TrimSpace(d.Description))
	fmt.Fprintf(&b, "metadata:\n  pattern: procedure\n  domain: capitalization\n  source: retro\n")
	b.WriteString("---\n\n")
	fmt.Fprintf(&b, "# %s\n\n", strings.ReplaceAll(d.ID, "-", " "))
	b.WriteString("## When to use\n\n")
	b.WriteString(strings.TrimSpace(d.WhenToUse))
	b.WriteString("\n\n## Procedure\n\n")
	b.WriteString(strings.TrimSpace(d.Lesson))
	b.WriteString("\n")
	if ev := strings.TrimSpace(d.Evidence); ev != "" {
		b.WriteString("\n## Evidence\n\n")
		b.WriteString(ev)
		b.WriteString("\n")
	}
	b.WriteString("\n## Notes\n\n这份 skill 由 `forge skill new` 从一次 retro 结论生成；写法是最小可执行版本，遇到反例请直接改本文件并保留证据链接。\n")
	return b.String(), nil
}

// Write 把草稿写成 <root>/<skillsDir>/<id>/SKILL.md，返回写入路径。
//
// 已存在时返回错误而不是静默覆盖：skill 是长期资产，覆盖一次就可能把别人的修订
// 抹掉；确实要重写时调用方传 force。
func Write(root, skillsDir string, d Draft, force bool) (string, error) {
	content, err := Render(d)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(skillsDir) == "" {
		skillsDir = "skills"
	}
	dir := filepath.Join(root, skillsDir, d.ID)
	// 双重保险：Validate 已限死 id 字符集，这里再确认最终目录仍在 skills 之下
	// （防 skillsDir 本身被传成 "../x"）。
	// skillsDir 也必须留在 root 内：它来自 flag，传 ../outside 时相对 base 仍然「在
	// base 之下」，只比 base 与 dir 是抓不到的。
	base := filepath.Join(root, skillsDir)
	if rel, err := filepath.Rel(root, base); err != nil || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("skill 路径越界：%s 不在仓库 %s 内", skillsDir, root)
	}
	if rel, err := filepath.Rel(base, dir); err != nil || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("skill 路径越界：%s", dir)
	}
	path := filepath.Join(dir, "SKILL.md")
	if _, err := os.Stat(path); err == nil && !force {
		return "", fmt.Errorf("%s 已存在；确实要重写请加 --force（覆盖会丢掉既有修订）", path)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", err
	}
	return path, nil
}
