// Package skillsdist 曾经负责把 canonical skill 库分发到各 AI 工具的宿主目录
// （~/.claude/skills、~/.codex/skills …）并跟踪分发漂移。阶段 D 按 ADR-0002
// 删除宿主层后，这里只剩一个只读能力：列出 canonical 库里的 skill 名。
// 分发/安装/漂移检查都不再存在——质量能力只在 forge task gate / forge change
// verify 进程内执行，不再往用户级目录写任何东西。
package skillsdist

import (
	"os"
	"path/filepath"
	"slices"
)

// ListSkills returns the names of all direct subdirectories under canonical that contain a SKILL.md (excludes skill-routing/, CONVENTIONS.md, etc.).
//
// ListSkills 返回 canonical 下所有含 SKILL.md 的直接子目录名（排除 skill-routing/、CONVENTIONS.md 等）。
// os.Stat（跟随 symlink）而不是 DirEntry.IsDir：历史 canonical 库里有 link/junction
// 指向真实目录的 skill，用 Lstat 语义会漏掉；环由 Stat 报错兜住（跳过）。
func ListSkills(canonical string) ([]string, error) {
	entries, err := os.ReadDir(canonical)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		name := e.Name()
		dir := filepath.Join(canonical, name)
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, "SKILL.md")); err == nil {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names, nil
}
