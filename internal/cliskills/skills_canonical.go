package cliskills

import (
	"fmt"
	"github.com/MjxUpUp/Forge/internal/projectroot"
	"os"
	"path/filepath"

	"github.com/MjxUpUp/Forge/internal/skillscanonical"
	"github.com/MjxUpUp/Forge/internal/skillsfm"
)

// resolveCanonical 解析 skill 库源目录（真实文件系统路径）与是否外部真实源。
// 优先级：--canonical flag > $FORGE_SKILLS_CANONICAL > 内置 embed 库。
// env/embed 两段下沉到 skillscanonical（供内部包共用），flag 优先级留在 cli 层。
// 返回 (dir, isExternal, err)。isExternal=false 表示来自 embed 解压的缓存目录。
// ResolveCanonical resolves the canonical skill-library dir: --canonical flag >
// $FORGE_SKILLS_CANONICAL > embedded library cache.
//
// ResolveCanonical 解析 canonical skill 库目录：--canonical flag >
// $FORGE_SKILLS_CANONICAL > 内置 embed 库缓存。
func ResolveCanonical() (string, bool, error) {
	if skillsCanonicalFlag != "" {
		if _, err := os.Stat(skillsCanonicalFlag); err != nil {
			return "", false, fmt.Errorf("--canonical 路径不存在: %s", skillsCanonicalFlag)
		}
		return skillsCanonicalFlag, true, nil
	}
	return skillscanonical.Resolve(Version)
}

// detectRepoSkillsDir 在项目根带真实 canonical skill 树时返回 <项目根>/skills，否则
// 返 ""。校验两段：canonical 树必带的 CONVENTIONS.md 标记（EnsureEmbeddedCache 识别
// 解压快照用的同一标记），且树内至少一个 <skill>/SKILL.md——光杆 CONVENTIONS.md
// （或为别的用途建的 skills/ 目录）不是 skill 树，必须落到 embed 缓存的响亮报错而
// 非被误判。变更类 skill 命令（decide）用它把默认写入目标从 embed 缓存改成仓库
// canonical 树（Forge 本仓是高频场景）。经项目根解析（注册表），子目录调用同样生效。
func detectRepoSkillsDir() string {
	root, err := projectroot.Find()
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

// detectRepoForgeSkillsDir 在项目根存在真实 forge 原生 skill 树（至少一个
// <skill>/SKILL.md——本专区无 CONVENTIONS.md 标记；它按定义就是 forge 专属内容）
// 时返回 <项目根>/skills-forge。2026-08 迁移后 requires_forge skill 的家：
// decide 对这些名字回退到它，仓库内写入落在真实源而非可再生成的 embed 缓存
// （与 detectRepoSkillsDir 的立项理由同一危害类）。
func detectRepoForgeSkillsDir() string {
	root, err := projectroot.Find()
	if err != nil {
		return ""
	}
	dir := filepath.Join(root, "skills-forge")
	matches, err := filepath.Glob(filepath.Join(dir, "*", "SKILL.md"))
	if err != nil || len(matches) == 0 {
		return ""
	}
	return dir
}

// repoSkillWriteDir 解析具名 skill 数据（decisions.md 等）的仓库内写入目标：
// skill 住 skills-forge/ 就用它（forge 原生，2026-08 迁移后），否则用存在的中立
// skills/ 树。两者皆无（仓库外）返回 ""。调用方须以「解析非来自显式
// --canonical/env 源」为前提——用户意图优先。中立回退不要求 skill 存在于该树：
// 拼错的名字应在读前写层（LoadDecisions / VerifyDecision）拿到明确报错，而不是
// 静默落回可再生成的缓存。
func repoSkillWriteDir(skill string) string {
	if skill == "" {
		return ""
	}
	if d := detectRepoForgeSkillsDir(); d != "" {
		if _, err := os.Stat(filepath.Join(d, skill)); err == nil {
			return d
		}
	}
	return detectRepoSkillsDir()
}

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
