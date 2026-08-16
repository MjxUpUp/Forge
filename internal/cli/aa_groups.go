package cli

// Gentle grouping of the command family: command paths are entirely unchanged; only `forge --help` is grouped by function.
//
// Why GroupID instead of path consolidation: the paths of the 20 top-level commands are already written into README, CLAUDE.md,
// session-retrospective skill, MCP docs, and user scripts — changing paths would require syncing 5+ places and break backward compatibility.
// cobra's Command.Group only changes help display, at zero migration cost.
//
// Order-sensitive: cobra validates "GroupID is registered" at AddCommand time (command.go:1208); unregistered → panic.
// Each command runs rootCmd.AddCommand in its own file's init() (executed in filename-alphabetical order); this file aa_groups.go
// sorts before all command files, so this init runs first — AddGroup must precede any AddCommand. Package-level vars (each xxxCmd)
// are initialized before any init (Go spec), so when GroupID is set here the command variables are already constructed.
//
// help/completion are cobra-auto-generated auxiliary commands, left as default (no group, shown at the end).
//
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

	// Project lifecycle: project-level, low-frequency management.
	//
	// 项目生命周期：项目级低频管理
	initCmd.GroupID = "lifecycle"
	syncCmd.GroupID = "lifecycle"
	updateCmd.GroupID = "lifecycle"
	// init-suggest hook prompt-state management (semantic extension of init).
	//
	suggestCmd.GroupID = "lifecycle" // init-suggest hook 的提示状态管理（init 的语义延伸）
	// One-click uninstall (npm binary + init-suggest markers).
	//
	uninstallCmd.GroupID = "lifecycle" // 一键反装（npm binary + init-suggest markers）
	// Legacy .forge runtime state → DataDir migration (upgrade path).
	//
	migrateCmd.GroupID = "lifecycle" // 旧 .forge runtime state → DataDir 迁移（升级路径）
	// Global project registry cleanup (cleanup counterpart to init's self-registration; backticks guard against Windows quote corruption).
	//
	registryCmd.GroupID = `lifecycle` // 全局项目注册表清理（init 自登记的对应清理入口；反引号防 Windows 引号腐蚀）

	// Project pipeline: project-level state (status is the main entry).
	//
	// 项目管道：项目级状态（status 是主入口）
	statusCmd.GroupID = "pipeline"
	verifyCmd.GroupID = "pipeline"

	// Task quality: task pipeline + quality observation (trace/act/review/health read data; the dashboard aggregates further).
	//
	// 任务质量：任务管道 + 质量观测（trace/act/review/health 是看数据，看板会进一步聚合）
	taskCmd.GroupID = "quality"
	traceCmd.GroupID = "quality"
	actCmd.GroupID = "quality"
	reviewCmd.GroupID = "quality"
	healthCmd.GroupID = "quality"
	dashboardCmd.GroupID = "quality"

	// Skill governance (the experience/knowledge loop has been removed).
	//
	// skill 治理（experience/knowledge 经验闭环已移除）
	skillsCmd.GroupID = "governance"

	// Integration & security: agent interface + interception + internal hook dispatch + multi-host plugin marketplace.
	//
	// 集成与安全：agent 接口 + 拦截 + 内部 hook 分发 + 多 host plugin marketplace
	hazardCmd.GroupID = "integrate"
	freezeCmd.GroupID = "integrate"
	hookCmd.GroupID = "integrate"
	cloneCmd.GroupID = "integrate"
	pluginCmd.GroupID = "integrate"
	// Cross-agent environment consistency audit (read-only; multi-host wiring + version drift).
	//
	// 跨 agent 环境一致性审计（只读；多 host 接线 + 版本漂移）
	doctorCmd.GroupID = "integrate"
	// hook bash computes the DataDir (Hidden, not in the help list).
	//
	dataDirCmd.GroupID = "integrate" // hook bash 算 DataDir 用（Hidden，不进 help 列表）
}
