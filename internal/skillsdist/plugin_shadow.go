package skillsdist

// plugin_shadow.go —— 插件同名遮蔽检测（design-stage-hardening 后续项）：核心 skill 库
// 收回的 skill（如 2026-10 收回的 design-audit / design-artifact-standards）在用户
// 尚未升级的扩展插件缓存里仍有旧副本，Claude Code 会同时加载两份。drift-check 只看
// canonical 与分发目标，看不到插件缓存——本文件补这一视图。只读、advisory。
//
// plugin_shadow.go — detects stale same-name skills in installed Claude Code plugins
// (e.g. an un-upgraded forge-design plugin still shipping skills that moved back
// into the core library). Read-only, advisory.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// corePluginName is the forge plugin itself — its bundled skills ARE the core
// library, not a shadow.
//
// corePluginName 是 forge 插件本身——它打包的 skill 就是核心库，不算遮蔽。
const corePluginName = "forge"

// PluginShadow is one core skill name also shipped by another installed plugin.
//
// PluginShadow 是另一个已安装插件里也带着的一个核心 skill 名。
type PluginShadow struct {
	Skill  string `json:"skill"`
	Plugin string `json:"plugin"` // installed_plugins.json 的键，如 forge-design@forge
	Path   string `json:"path"`   // 该插件里这份 skill 的目录
}

// installedPluginsFile is the schema-v2 shape of Claude Code's plugin registry.
type installedPluginsFile struct {
	Plugins map[string][]struct {
		InstallPath string `json:"installPath"`
	} `json:"plugins"`
}

// PluginShadows lists core skill names that other installed Claude Code plugins
// also ship, reading <claudeHome>/plugins/installed_plugins.json. A missing
// registry means no plugins (nil, nil); a malformed one is an error.
//
// PluginShadows 读 <claudeHome>/plugins/installed_plugins.json，列出其他已安装
// Claude Code 插件里也带着的核心 skill 名。注册表不存在 = 没装插件（nil, nil）；
// 格式损坏返回 error。
func PluginShadows(claudeHome string, coreNames []string) ([]PluginShadow, error) {
	if claudeHome == "" {
		return nil, nil
	}
	data, err := os.ReadFile(filepath.Join(claudeHome, "plugins", "installed_plugins.json"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var reg installedPluginsFile
	if err := json.Unmarshal(data, &reg); err != nil {
		return nil, fmt.Errorf("installed_plugins.json 解析失败: %w", err)
	}
	core := make(map[string]bool, len(coreNames))
	for _, n := range coreNames {
		core[n] = true
	}
	var out []PluginShadow
	seen := map[string]bool{}
	for key, installs := range reg.Plugins {
		if name, _, _ := strings.Cut(key, "@"); name == corePluginName {
			continue
		}
		for _, inst := range installs {
			if inst.InstallPath == "" {
				continue
			}
			entries, err := os.ReadDir(filepath.Join(inst.InstallPath, "skills"))
			if err != nil {
				continue // 插件不带 skills/ 或目录已被清理
			}
			for _, e := range entries {
				if !e.IsDir() || !core[e.Name()] {
					continue
				}
				dir := filepath.Join(inst.InstallPath, "skills", e.Name())
				if _, err := os.Stat(filepath.Join(dir, "SKILL.md")); err != nil {
					continue
				}
				id := key + "\x00" + e.Name()
				if seen[id] {
					continue // 同一插件 user/project 两种 scope 指向同一份
				}
				seen[id] = true
				out = append(out, PluginShadow{Skill: e.Name(), Plugin: key, Path: dir})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Skill != out[j].Skill {
			return out[i].Skill < out[j].Skill
		}
		return out[i].Plugin < out[j].Plugin
	})
	return out, nil
}
