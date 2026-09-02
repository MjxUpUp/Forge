// Package harnessdetect detects foreign AI-coding-agent harnesses in a project
// root (Project Policy Layer P4).
//
// Package harnessdetect 检测项目根里的外来 AI 编码 harness（Project Policy
// Layer P4）。
//
// 设计（docs/design/project-policy-layer.md）：高置信目录级信号
// 命中时 forge 默认让位（declined, by=foreign-harness），显式 forge on 可恢复。
// 判定纪律：宁可漏判（漏判代价 = 多问一次），不可误判（误判 = 错误接管自有
// harness 的项目，用户感知不可逆）。信号表是单一真相源；新增信号必须同步本表与测试。
package harnessdetect

import (
	"os"
	"path/filepath"
	"strings"
)

// detectDirNonEmpty 报告 root/<sub> 存在且含至少一个条目。
func detectDirNonEmpty(root, sub string) bool {
	entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(sub)))
	return err == nil && len(entries) > 0
}

// detectJSONHasKey 轻量探测：root/<sub> 是 JSON 且文本含 "key"（含引号形态）。
// 刻意不做完整解析——信号判定只要"该键大概率存在"，误报方向已由高置信目录
// 形态约束（.claude/settings.json 是 Claude Code 专属路径）。
func detectJSONHasKey(root, sub, key string) bool {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(sub)))
	if err != nil {
		return false
	}
	return strings.Contains(string(data), `"`+key+`"`)
}

// Detect returns the first high-confidence foreign-harness signal in root, or
// ok=false when the project shows no own-harness intent. Signal order is the
// table; first hit wins.
//
// Detect 返回 root 内第一个高置信外来 harness 信号；无信号时 ok=false。
// 顺序即信号表，首个命中生效。
func Detect(root string) (signal string, hit bool) {
	// 信号表：目录/文件形态 → 人读信号名。全部为"自有 harness 意图"的强形态：
	// spec-kit 的标记目录、项目自建的 slash commands、settings.json 的 hooks/
	// permissions 接线（别的工具在 Claude Code 里接管 hook 槽位）、Cursor 规则集。
	type sig struct {
		match func(root string) bool
		desc  string
	}
	table := []sig{
		{func(r string) bool { return dirExists(filepath.Join(r, `.specify`)) }, `.specify/（spec-kit）`},
		{func(r string) bool { return detectDirNonEmpty(r, `.claude/commands`) }, `.claude/commands/（自带 slash commands）`},
		{func(r string) bool {
			return fileExists(filepath.Join(r, `.claude`, `settings.json`)) && detectJSONHasKey(r, `.claude/settings.json`, `hooks`)
		}, `.claude/settings.json 含 hooks（自有 harness 接线）`},
		{func(r string) bool {
			return fileExists(filepath.Join(r, `.claude`, `settings.json`)) && detectJSONHasKey(r, `.claude/settings.json`, `permissions`)
		}, `.claude/settings.json 含 permissions（自有权限接线）`},
		{func(r string) bool { return detectDirNonEmpty(r, `.cursor/rules`) }, `.cursor/rules/（Cursor 规则集）`},
	}
	for _, s := range table {
		if s.match(root) {
			return s.desc, true
		}
	}
	return ``, false
}

// dirExists / fileExists 是 os.Stat 的存在性薄封装（表内谓词共用）。
func dirExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}
