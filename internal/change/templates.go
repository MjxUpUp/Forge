package change

import _ "embed"

// 模板是内嵌的：`forge change new` 在没有任何网络的机器上也要能产出骨架。
// 想改措辞直接改这些 .md，重新编译即生效——不需要在运行时找模板目录。
//
//go:embed templates/proposal.md
var tmplProposal string

//go:embed templates/design.md
var tmplDesign string

//go:embed templates/tasks.md
var tmplTasks string

//go:embed templates/acceptance.md
var tmplAcceptance string

// goal.md 是第五份产物（task-goal-contract）：五段式目标契约，进 git 可评审。
//
//go:embed templates/goal.md
var tmplGoal string

// 名字刻意不叫 agents.md：Windows 文件系统大小写不敏感，落地后就是 AGENTS.md，
// 会让这个模板在被 agents 读到之前先当成仓库内指令生效。
//
//go:embed templates/agents-protocol.md
var tmplAgents string
