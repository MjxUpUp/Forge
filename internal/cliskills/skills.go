package cliskills

import (
	"github.com/spf13/cobra"
)

// VersionFn returns the forge binary's version string; the cli registrar
// injects the provider at command-tree assembly.
//
// VersionFn 返回 forge 二进制版本号，由 cli 侧注册器注入 provider。cliskills
// 不 import cli（cli 注册本包的命令，反向会成环），版本经此 seam 取用。
// 刻意是惰性闭包而非 init 期按值拷贝：版本经 SetVersion 在 main() 里才落
// rootCmd.Version（ldflags 注入），init 期拷贝恒得空串（docsconsistency.
// RegisterVersion 同款时序论证，见 internal/cli/root.go）。
var VersionFn func() string

// version 兜底：未注册 provider（单测直接调包内函数）时取 "dev"，避免空串
// 污染 embed 缓存版本标记（空串标记会让缓存与真实版本永不等值，触发无谓
// 的全量重建）。
func version() string {
	if VersionFn == nil {
		return "dev"
	}
	return VersionFn()
}

// Root is the `forge skills` parent command.
//
// Root 是 `forge skills` 父命令。自带 PersistentPreRunE 覆盖 root 的——
// 保留 update 检查，跳过 findProjectRoot+autoSync，让 skill 分发在非 forge 项目
// （用户全局目录）也能跑，不要求当前目录是 forge 项目（PersistentPreRunE 由
// cli 注册器注入，见 internal/cli/skills_register.go）。
var Root = &cobra.Command{
	Use:   "skills",
	Short: "Skill 库管理与分发",
	Long: `forge skills — 管理 canonical skill 库并分发到各 AI 工具。

子命令清单见下方 "Available Commands"（cobra 自动派生，与注册保持一致）。

canonical 源优先级：--canonical > $FORGE_SKILLS_CANONICAL > 内置 embed 库。`,
}

// skillsCanonicalFlag 是 --canonical flag 的值（覆盖 env 与内置 embed 库）。
var skillsCanonicalFlag string

func init() {
	Root.PersistentFlags().StringVar(&skillsCanonicalFlag, "canonical", "",
		"skill 库源目录（覆盖 $FORGE_SKILLS_CANONICAL 与内置 embed 库）")
}
