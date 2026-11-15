# 验收判据：入口路由与阶段机

change: `entry-routing`

这里的每个 ```bash 围栏是一条可执行判据，`forge change verify` 会按出现顺序编号（#1、#2……）并依次实跑。`tasks.md` 里引用的就是这个序号。

- 不要用 `&&` 串联；多步就多行。
- 围栏内以 `set -eo pipefail` 执行；管道、后台、再包一层 shell 都会被 verify 拒绝。

#1 编译：

```bash
go build ./...
```

#2 next 决策表（四象限 + 未声明仓库不变）：

```bash
go test ./internal/taskpipeline/ -run 'TestNextDecision' -count=1
```

#3 task start 守卫与 `--kind task`：

```bash
go test ./internal/clitask/ -run 'TestTaskStart_RequiresChangeOrKindTask|TestTaskStart_ExplicitKindTaskAllowed' -count=1
```

#4 阶段推导与 clarify 记录：

```bash
go test ./internal/change/ -run 'TestStage|TestClarifyRecord|TestUnboundTasks' -count=1
```

#5 e2e 真实路由：

```bash
go test ./internal/e2e/ -run 'TestEntryRoutingChangeFirst|TestChangeStageBlocksEarlyTask' -count=1
```

#6 死代码零：

```bash
sh scripts/checks/deadcode-zero.sh
```

#7 文档与模板守卫：

```bash
go test ./internal/cli/ -run 'TestAgentsProtocol|TestHelpSurface|TestCliReference' -count=1
```