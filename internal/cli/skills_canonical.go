package cli

import (
	"fmt"
	"os"

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
