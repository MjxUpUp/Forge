// Package skills 内置 canonical skill 库（go:embed 编译期嵌入），作为无外部源时的 fallback。
// 二进制 +1.5M；发布流程零改动（goreleaser/go build 自动含；npm 包只是下载器）。
package skills

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"
)

// FS 是编译期嵌入的 skill 库虚拟文件系统（路径相对 skills/ 根，正斜杠分隔）。
// `*` 匹配当前目录直接子项（CONVENTIONS.md + 各 canonical skill 目录），目录匹配后递归嵌入全部内容。
// 注：skill 目录数量随分发增减——不在此写死数字，避免注释与实际再次漂移。
// embed 规则：含 build 指令的 .go 文件（本文件 embed.go）被自动排除；但无指令的 .go
// （embed_test.go）会被嵌入，故 ExtractTo 显式跳过 .go。skills/ 已确认无 .git/隐藏文件污染。
//
//go:embed *
var FS embed.FS

// ExtractTo 把内置 skill 库解压到 dir（真实文件系统目录），返回首个遇到的错误。
// install/drift-check/adapters 等需真实路径——embed FS 是虚拟的，无法被 link 指向，
// 故 fallback 时先解压到持久缓存目录，再以该目录作为 canonical 源。
func ExtractTo(dir string) error {
	return fs.WalkDir(FS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// embed FS 路径用正斜杠；写文件系统前转 OS 分隔符
		target := filepath.Join(dir, filepath.FromSlash(path))
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		// 跳过 .go：embed 只排除"含 build 指令的 .go"（embed.go 自身），无指令的
		// embed_test.go 会被 `*` 嵌入。它是包内测试产物，不属于分发资产，不能进入缓存
		// 目录（否则 link/copy 会把它带进用户目标）。
		if filepath.Ext(path) == ".go" {
			return nil
		}
		data, rerr := FS.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		return os.WriteFile(target, data, 0644)
	})
}
