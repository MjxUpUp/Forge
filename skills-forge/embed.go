// Package skillsforge ships the forge-native skill library (go:embed at compile
// time): skills that document forge's own machinery (skill-evolution /
// skill-routing / skill-authoring-standard, all `metadata.requires_forge: "true"`).
// Separate from the neutral skills/ tree per the zero-reverse-dependency contract
// (skills/CONVENTIONS.md §13): skills-only consumers copy skills/ and never see
// these; the forge binary embeds BOTH trees and merges them into the same cache
// dir (~/.forge/skills-cache/embedded), so single-root consumers (ListSkills,
// skilltrigger.LoadAll, install, adapters) keep seeing the full set unchanged.
// The forge plugin (plugins/forge/skills) also ships both trees.
//
// Package skillsforge 内置 forge 原生 skill 库（go:embed 编译期嵌入）：描述 forge
// 自身机制的 skill（skill-evolution / skill-routing / skill-authoring-standard，
// 均带 `metadata.requires_forge: "true"`）。按零反向依赖契约（skills/CONVENTIONS.md
// §13）与中立的 skills/ 树分离：skills-only 消费方拷 skills/ 永远看不到这些；forge
// 二进制同时嵌入两棵树并合并解压到同一缓存目录（~/.forge/skills-cache/embedded），
// 单根消费者（ListSkills、skilltrigger.LoadAll、install、adapters）无需改动即可
// 继续看到全量集。forge 插件（plugins/forge/skills）也同时分发两棵树。
package skillsforge

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"
)

// FS is the compile-time-embedded forge-native skill library (paths relative to
// the skills-forge/ root). Every skill here must carry
// `metadata.requires_forge: "true"` (enforced by TestSkillsForge_AllMarked in
// skillsqa) — an unmarked skill in the forge-only zone is a misplacement.
//
// FS 是编译期嵌入的 forge 原生 skill 库虚拟文件系统（路径相对 skills-forge/ 根）。
// 此处每个 skill 必须带 `metadata.requires_forge: "true"`（skillsqa 的
// TestSkillsForge_AllMarked 守卫）——forge 专区里出现未标记的 skill 就是放错位置。
//
//go:embed *
var FS embed.FS

// ExtractTo extracts the forge-native overlay into dir WITHOUT clearing it (the
// neutral skills.FS extraction owns the cache lifecycle; this is an additive
// overlay with its own version marker). Top-level dirs present in THIS overlay
// version are removed first so edits converge; skills REMOVED from the overlay
// are cleaned up by the neutral layer's full rebuild (both markers share the
// version string, so a version switch wipes the whole cache). Returns the first
// error encountered.
//
// ExtractTo 把 forge 原生覆盖层解压到 dir，不清空目录（缓存生命周期归中性
// skills.FS 的解压管理；本覆盖层增量写入并自带版本标记）。先删除**本版本覆盖层
// 持有的**顶层目录使编辑收敛；从覆盖层**移除**的 skill 由中立层的整目录重建清理
//（两个标记共用同一 version 字符串，版本切换必整清缓存）。返回首个遇到的错误。
func ExtractTo(dir string) error {
	// Converge first: remove top-level dirs (skills) that this overlay owns, so a
	// renamed/removed forge-native skill does not linger from an older overlay.
	// 先收敛：删掉覆盖层持有的顶层目录（skill 目录），改名/删除的 forge 原生 skill
	// 不会从旧覆盖层残留。
	entries, err := fs.ReadDir(FS, ".")
	if err != nil {
		return err
	}
	var topDirs []string
	for _, e := range entries {
		if e.IsDir() {
			topDirs = append(topDirs, e.Name())
		}
	}
	for _, d := range topDirs {
		if err := os.RemoveAll(filepath.Join(dir, d)); err != nil {
			return err
		}
	}
	return fs.WalkDir(FS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == "." {
			return nil
		}
		// Same rule as skills/embed.go: .go files without build directives (e.g.
		// a future embed_test.go) do get embedded — never extract them into the cache.
		//
		// 与 skills/embed.go 同一规则：无 build 指令的 .go 文件（如未来加的
		// embed_test.go）确实会被嵌入——绝不解压进缓存。
		if filepath.Ext(path) == ".go" {
			return nil
		}
		target := filepath.Join(dir, filepath.FromSlash(path))
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		data, rerr := FS.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		return os.WriteFile(target, data, 0644)
	})
}
