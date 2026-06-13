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
	sb.WriteString("## 基本规则\n\n")
	sb.WriteString("1. **修改前先说意图** — 告诉用户你打算改什么、为什么改\n")
	sb.WriteString("2. **编译必须通过** — 每次修改后确认编译通过（auto-compile hook 自动检查）\n")
	sb.WriteString("3. **不弱化断言** — 不删除 t.Fatal、assert! 等断言（assertion-check hook 自动检查）\n")
	sb.WriteString("4. **测试伴随变更** — 新代码有对应测试\n")
	sb.WriteString("5. **提交前确认** — commit 信息描述变更内容和原因\n")
	sb.WriteString("6. **结束前验证** — 会话结束前运行测试确认无破坏\n\n")

	// Task workflow — the most critical operational guidance to prevent agents
	// from hitting task-guard/bash-guard blocks without knowing what to do.
	sb.WriteString("## Task 工作流（必读）\n\n")
	sb.WriteString("**非平凡变更（>10 行）前必须启动 Forge 任务**，否则 Write/Edit/Bash 写入会被 hook 拦截。纯文档、单行 typo 修复、版本号 bump 除外。\n\n")
	sb.WriteString("### 启动任务\n\n")
	sb.WriteString("```bash\n")
	sb.WriteString("# 在 master/main 上：创建新分支 + 启动任务\n")
	sb.WriteString("forge task start --ref feat/xxx --title \"描述\" --branch\n")
	sb.WriteString("\n")
	sb.WriteString("# 已在 feature 分支上：不加 --branch（--branch 仅在 main/master 可用）\n")
	sb.WriteString("forge task start --ref fix/xxx --title \"描述\"\n")
	sb.WriteString("```\n\n")

	sb.WriteString("### 门禁顺序（必须按序推进，所有命令带 `--ref <ref>`）\n\n")
	sb.WriteString("1. `task-understand` — 描述意图和范围后运行\n")
	sb.WriteString("2. `task-design` — 描述设计方案后运行（需 ≥1 分钟真实工作 + ≥1 次工具调用）\n")
	sb.WriteString("3. `task-implement` — 代码写完、编译通过后运行（自动检查编译+断言+代码变更）\n")
	sb.WriteString("4. `task-verify` — 测试通过后运行\n")
	sb.WriteString("5. `task-complete` — E2E 验证通过后运行（`forge task gate task-complete --ref <ref>`）\n\n")
	sb.WriteString("每个门禁命令：`forge task gate <id> --ref <ref>`\n\n")
	sb.WriteString("门禁全通过后运行 `forge task complete --ref <ref>` 触发评分。\n\n")

	sb.WriteString("### 安全机制\n\n")
	sb.WriteString("- **task-guard**：拦截无任务的 Write/Edit 源码操作\n")
	sb.WriteString("- **bash-guard**：拦截无任务的 Bash 写文件命令（writeFile、cat >、sed -i 等）\n")
	sb.WriteString("- **file-sentinel**：检测 Bash 执行后的未授权文件变更并自动 revert\n")
	sb.WriteString("- **自保护**：`.forge/*` 和 `.claude/settings*` 不能被直接修改，只能通过 `forge` 命令操作\n\n")

	sb.WriteString("### 常见错误\n\n")
	sb.WriteString("| 错误信息 | 原因 | 解决方法 |\n")
	sb.WriteString("|----------|------|----------|\n")
	sb.WriteString("| Write/Edit denied by task-guard | 无活跃任务 | 先 `forge task start --ref <type>/<name>` |\n")
	sb.WriteString("| Bash denied by bash-guard | Bash 含写文件操作且无任务 | 先启动任务 |\n")
	sb.WriteString("| insufficient work activity | 门禁间工具调用 <1 次 | 用 Read/Grep/Glob 探索代码 |\n")
	sb.WriteString("| HEAD not moved | task-implement 前没有新提交 | 先写代码并 `git commit` |\n")
	sb.WriteString("| --branch on non-main | `--branch` 只在 master/main 可用 | 已在 feature 分支时去掉 `--branch` |\n")
	sb.WriteString("| task already exists | 任务已启动 | 用 `forge task status --ref <ref>` 查看 |\n")
	sb.WriteString("| Reverted by file-sentinel | Bash 写了源码但无任务 | 先启动任务，或用 Write/Edit 工具 |\n\n")

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
