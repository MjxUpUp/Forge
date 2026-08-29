package cli

import (
	"github.com/spf13/cobra"
)

// project.go —— `forge project` 命令组：项目数据跨机器传输与身份对齐
// （project-sync）。
//
//	export  打包项目数据为可搬运 bundle（allowlist 默认拒绝敏感/机器本地文件）
//	import  把 bundle 落地合并到本机（lineage 信任 + 幂等账本）
//	adopt   采纳 repo-born 项目 ID（.forge-project-id），跨机器身份对齐
//
// 身份模型与合并语义在 forgedata / datamerge / projectsync；这些命令只是它们
// 的壳。字符串全用 raw 字面量（Windows 引号腐蚀规则，registry.go 惯例）。

func init() {
	rootCmd.AddCommand(projectCmd)
	projectCmd.AddCommand(projectExportCmd)
	projectCmd.AddCommand(projectImportCmd)
	projectCmd.AddCommand(projectAdoptCmd)
}

var projectCmd = &cobra.Command{
	Use:   `project`,
	Short: `项目数据跨机器导出/导入/身份对齐`,
	Long: `forge project —— 项目数据跨机器同步

数据在用户级 DataDir（~/.forge/projects/<key>/）不随仓库走，跨机器需要显式载体：
  export  把项目记录（tasks/checklog/toollog/sessions/act/stamps/protocol）打包为
          checksummed tar.gz bundle；默认排除敏感与机器本地文件（quarantine 源码
          全文、hazard 命令行、冻结状态、会话锚/sentinel），--include 显式选入
  import  校验并合并 bundle 到本机项目；同 key（同身份 lineage，默认保留结果
          字段）或跨 key（默认剥离外来门禁信号）自动路由；账本保证重复导入幂等
  adopt   生成/采纳 .forge-project-id（committed 身份文件），让同一仓库在任意
          机器/路径推导同一 key —— 双机同步的推荐前置

命令族其余入口：单任务跨机器交接用 forge task export/import；注册表 key
分裂（身份漂移）修复用 forge registry rekey。`,
}
