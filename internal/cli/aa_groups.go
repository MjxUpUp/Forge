package cli

// 命令族温和分组：命令路径全不变，仅让 `forge --help` 按职能分组展示。
//
// 为什么用 GroupID 而非归并路径：20 个顶层命令的路径已写进 README、CLAUDE.md、
// session-retrospective skill、MCP 文档与用户脚本——改路径要同步 5+ 处且破坏向后兼容。
// cobra 的 Command.Group 只改 help 展示，零迁移成本。
//
// 顺序敏感：cobra 在 AddCommand 时校验 "GroupID 已注册"（command.go:1208），未注册会 panic。
// 各命令在自身文件的 init() 里 rootCmd.AddCommand（按文件名字母序执行）；本文件名 aa_groups.go
// 排在所有命令文件之前，故本 init 最先执行——AddGroup 必先于任何 AddCommand。包级 var（各 xxxCmd）
// 在所有 init 之前初始化（Go 规范），故此处设 GroupID 时命令变量已构造完成。
//
// help/completion 是 cobra 自动生成的辅助命令，留默认（不分组，显示在末尾）。
import "github.com/spf13/cobra"

func init() {
	rootCmd.AddGroup(
		&cobra.Group{ID: "lifecycle", Title: "项目生命周期"},
		&cobra.Group{ID: "pipeline", Title: "项目管道"},
		&cobra.Group{ID: "quality", Title: "任务质量"},
		&cobra.Group{ID: "governance", Title: "经验与治理"},
		&cobra.Group{ID: "integrate", Title: "集成与安全"},
	)

	// 项目生命周期：项目级低频管理
	initCmd.GroupID = "lifecycle"
	syncCmd.GroupID = "lifecycle"
	updateCmd.GroupID = "lifecycle"
	snapshotCmd.GroupID = "lifecycle"

	// 项目管道：项目级门禁与状态（status 是主入口）
	statusCmd.GroupID = "pipeline"
	gateCmd.GroupID = "pipeline"
	verifyCmd.GroupID = "pipeline"
	validateCmd.GroupID = "pipeline"

	// 任务质量：任务管道 + 质量观测（trace/act/review/health 是看数据，看板会进一步聚合）
	taskCmd.GroupID = "quality"
	traceCmd.GroupID = "quality"
	actCmd.GroupID = "quality"
	reviewCmd.GroupID = "quality"
	healthCmd.GroupID = "quality"
	dashboardCmd.GroupID = "quality"

	// 经验与治理：经验闭环 + skill 治理
	experienceCmd.GroupID = "governance"
	knowledgeCmd.GroupID = "governance"
	skillsCmd.GroupID = "governance"

	// 集成与安全：agent 接口 + 拦截 + 内部 hook 分发
	mcpCmd.GroupID = "integrate"
	hazardCmd.GroupID = "integrate"
	hookCmd.GroupID = "integrate"
	cloneCmd.GroupID = "integrate"
}
