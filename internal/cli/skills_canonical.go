package cli

import (
	"fmt"
	"os"

	"github.com/MjxUpUp/Forge/internal/skillscanonical"
)

// resolveCanonical 解析 skill 库源目录（真实文件系统路径）与是否外部真实源。
// 优先级：--canonical flag > $FORGE_SKILLS_CANONICAL > 内置 embed 库。
// env/embed 两段下沉到 skillscanonical（与 mcpserver 共用），flag 优先级留在 cli 层。
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
