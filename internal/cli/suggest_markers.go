package cli

// suggest_markers.go — init-suggest hook 的 per-project 提示标记的读写助手
//（2026-09 死代码清扫：`forge suggest` 命令族已删——decline/reset 与 forge off/on
// 完全重复、status 无使用证据；本文件保留 off/on 双写垫片消费的 marker 助手，
// 标记值的单一真相源仍在 init-suggest hook（bash））。
//
// 标记存储与 init-suggest hook 同一目录（~/.forge/.init-suggested/<tag>）。tag 由
// hookdispatch.SuggestTagFor 产出（按 git root 键控——调用方 off.go 传 root，
// 与 hook 的 FORGE_CWD_TAG 同一函数，F1 修复：原按 cwd 键控，子目录 decline
// 写错 tag 致永久静默失效）。
//
// 中文字符串用 raw string（反引号）包裹，规避 Windows 输入引号腐蚀。
// 标记存储与 init-suggest hook 同一目录（~/.forge/.init-suggested/<tag>）。tag 由
// hookdispatch.SuggestTagFor 产出（按 git root 键控——调用方 off.go 传 root；
// cwd）——与 hook 的 FORGE_CWD_TAG 一致，确保与 hook 读写同一标记（F1 修复：
// 原按 cwd 键控，子目录 decline 写错 tag 致永久静默失效）。
//
// 中文字符串用 raw string（反引号）包裹，规避 Windows 输入引号腐蚀。

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/MjxUpUp/Forge/internal/forgedata"
)

// suggestStateDir 是 init-suggest hook 的 per-project 提示标记目录，与 hook 脚本的
// ${FORGE_DATA_HOME:-$HOME/.forge}/.init-suggested 同路径。refactor-data-home commit E：
// 统一走 forgedata.GlobalHome()（FORGE_DATA_HOME 优先），与 hook bash 一致。
func suggestStateDir() string {
	home, err := forgedata.GlobalHome()
	if err != nil {
		// 降级分支：仅在 UserHomeDir 失败（无 HOME）时到达。保留旧行为的相对
		// ".forge"（标记读写会在 cwd 下解析该相对路径，行为与收敛前逐字一致）。
		home = ".forge"
	}
	return filepath.Join(home, ".init-suggested")
}

// writeSuggestMarker 写 legacy 提示标记（declined/suggested）。off.go 的双写垫片
// 消费——标记值的单一真相源在 init-suggest hook（bash）。
func writeSuggestMarker(tag, value string) error {
	dir := suggestStateDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf(`create suggest state dir: %w`, err)
	}
	if err := os.WriteFile(filepath.Join(dir, tag), []byte(value), 0644); err != nil {
		return fmt.Errorf(`write suggest marker: %w`, err)
	}
	return nil
}

// removeSuggestMarker 清除 legacy 提示标记；不存在不算错（幂等）。
func removeSuggestMarker(tag string) error {
	if err := os.Remove(filepath.Join(suggestStateDir(), tag)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf(`clear suggest marker: %w`, err)
	}
	return nil
}

// baseName 是 filepath.Base 的盘根安全包装：basename 退化为裸分隔符/点/空时返回 ""，
// 让调用方回退到全路径（off/on 的项目名显示用；盘根/空 basename 时 filepath.Base
// 对 "E:\" / "/" 返裸分隔符，见 memory windows-go-bash-pitfalls）。
func baseName(path string) string {
	b := filepath.Base(path)
	if b == string(filepath.Separator) || b == "." || b == "" {
		return ""
	}
	return b
}
