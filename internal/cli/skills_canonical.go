package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/MjxUpUp/Forge/internal/skillscanonical"
	"github.com/MjxUpUp/Forge/internal/skillsfm"
)

// resolveCanonical resolves the skill library source directory (real filesystem path) and whether it is an external real source.
// Priority: --canonical flag > $FORGE_SKILLS_CANONICAL > built-in embed library.
// The env/embed branches are pushed down to skillscanonical (shared by internal packages); flag priority stays in the cli layer.
// Returns (dir, isExternal, err). isExternal=false means it comes from the embed-extracted cache directory.
//
// resolveCanonical 解析 skill 库源目录（真实文件系统路径）与是否外部真实源。
// 优先级：--canonical flag > $FORGE_SKILLS_CANONICAL > 内置 embed 库。
// env/embed 两段下沉到 skillscanonical（供内部包共用），flag 优先级留在 cli 层。
// 返回 (dir, isExternal, err)。isExternal=false 表示来自 embed 解压的缓存目录。
func resolveCanonical() (string, bool, error) {
	if skillsCanonicalFlag != "" {
		if _, err := os.Stat(skillsCanonicalFlag); err != nil {
			return "", false, fmt.Errorf("--canonical 路径不存在: %s", skillsCanonicalFlag)
		}
		return skillsCanonicalFlag, true, nil
	}
	return skillscanonical.Resolve(rootCmd.Version)
}

// detectRepoSkillsDir returns <project-root>/skills when it looks like a real canonical
// skill tree, else "". The check is two-part: the CONVENTIONS.md marker every canonical
// tree carries (the same marker EnsureEmbeddedCache uses to recognize an extracted
// snapshot) AND at least one <skill>/SKILL.md inside — a bare CONVENTIONS.md (or a
// skills/ dir created for some other purpose) is not a skill tree and must fall through
// to the fail-loud embed-cache error instead of being misdetected. Mutating skill
// commands (decide) use it to default to the repo's canonical tree instead of the embed
// cache when forge runs inside a skills-bearing checkout (the Forge repo itself being
// the recurring case). Resolution goes through the project root (registry-based), so
// invoking from a subdirectory works too.
//
// detectRepoSkillsDir 在项目根带真实 canonical skill 树时返回 <项目根>/skills，否则
// 返 ""。校验两段：canonical 树必带的 CONVENTIONS.md 标记（EnsureEmbeddedCache 识别
// 解压快照用的同一标记），且树内至少一个 <skill>/SKILL.md——光杆 CONVENTIONS.md
// （或为别的用途建的 skills/ 目录）不是 skill 树，必须落到 embed 缓存的响亮报错而
// 非被误判。变更类 skill 命令（decide）用它把默认写入目标从 embed 缓存改成仓库
// canonical 树（Forge 本仓是高频场景）。经项目根解析（注册表），子目录调用同样生效。
func detectRepoSkillsDir() string {
	root, err := findProjectRoot()
	if err != nil {
		return ""
	}
	dir := filepath.Join(root, "skills")
	if _, err := os.Stat(filepath.Join(dir, "CONVENTIONS.md")); err != nil {
		return ""
	}
	if matches, err := filepath.Glob(filepath.Join(dir, "*", "SKILL.md")); err != nil || len(matches) == 0 {
		return ""
	}
	return dir
}

// requireValidSkillName rejects --skill values that are not plain skill names
// (empty, ".", "..", or containing path separators). The name is joined into
// filesystem paths by the skillsdecisions/skillseval stores — an unchecked
// "../../x" would traverse out of the skill directory (path traversal defense
// at the cli boundary; skillsfm.IsValidSkillName is the shared validator).
//
// requireValidSkillName 拒绝非纯 skill 名的 --skill 值（空、"."、".." 或含路径
// 分隔符）。skill 名会被 skillsdecisions/skillseval store 拼进文件系统路径——
// 不校验的 "../../x" 会遍历出 skill 目录（cli 边界的路径遍历防御；
// skillsfm.IsValidSkillName 是共享校验器）。
func requireValidSkillName(name string) error {
	if !skillsfm.IsValidSkillName(name) {
		return fmt.Errorf("非法 skill 名 %q（不得为空、\".\"/\"..\" 或含路径分隔符）", name)
	}
	return nil
}
