package skillgen

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	forgeSectionStart = "<!-- FORGE:START -->"
	forgeSectionEnd   = "<!-- FORGE:END -->"
)

// GenerateClaudeMD creates or updates .claude/CLAUDE.md with a Forge-managed
// quality protocol section. If the file already exists, only the marked section
// is replaced — user content is preserved.
func GenerateClaudeMD(projectDir string) error {
	claudeDir := filepath.Join(projectDir, ".claude")
	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		return fmt.Errorf("failed to create .claude dir: %w", err)
	}

	path := filepath.Join(claudeDir, "CLAUDE.md")

	forgeSection := buildForgeSection()

	// Read existing file if present
	existing, err := os.ReadFile(path)
	if err == nil && len(existing) > 0 {
		// Update only the Forge section
		updated := replaceForgeSection(string(existing), forgeSection)
		return os.WriteFile(path, []byte(updated), 0644)
	}

	// Create new file with just the Forge section
	return os.WriteFile(path, []byte(forgeSection), 0644)
}

func buildForgeSection() string {
	var sb strings.Builder
	sb.WriteString(forgeSectionStart + "\n\n")
	sb.WriteString("# Forge 质量协议\n\n")
	sb.WriteString("本项目使用 Forge 进行质量保障。请遵守以下规则：\n\n")
	sb.WriteString("1. **修改前先说意图** — 告诉用户你打算改什么、为什么改\n")
	sb.WriteString("2. **编译必须通过** — 每次修改后确认编译通过（auto-compile hook 自动检查）\n")
	sb.WriteString("3. **不弱化断言** — 不删除 t.Fatal、assert! 等断言（assertion-check hook 自动检查）\n")
	sb.WriteString("4. **测试伴随变更** — 新代码有对应测试\n")
	sb.WriteString("5. **提交前确认** — commit 信息描述变更内容和原因\n")
	sb.WriteString("6. **结束前验证** — 会话结束前运行测试确认无破坏\n\n")
	sb.WriteString("使用 `/forge-pipeline` 运行项目级管道。\n")
	sb.WriteString("使用 `/forge-quality` 查看完整质量协议。\n\n")
	sb.WriteString(forgeSectionEnd + "\n")
	return sb.String()
}

// replaceForgeSection replaces the content between FORGE:START and FORGE:END
// markers, preserving everything outside the markers.
func replaceForgeSection(content, newSection string) string {
	startIdx := strings.Index(content, forgeSectionStart)
	endIdx := strings.Index(content, forgeSectionEnd)

	if startIdx == -1 || endIdx == -1 || endIdx <= startIdx {
		// No markers found — append the section
		return content + "\n" + newSection
	}

	// Replace between markers
	before := content[:startIdx]
	after := content[endIdx+len(forgeSectionEnd):]

	// newSection ends with "\n" from forgeSectionEnd+"\n", trim it
	// so we control the exact spacing between markers and after-content
	section := strings.TrimRight(newSection, "\n")

	result := before + section + "\n"

	// Clean up leading whitespace from after-content
	after = strings.TrimLeft(after, "\n")
	if after != "" {
		result += "\n" + after
	}
	return result
}
