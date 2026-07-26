package cli

import (
	"fmt"
	"os"

	"github.com/MjxUpUp/Forge/internal/skillscanonical"
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
