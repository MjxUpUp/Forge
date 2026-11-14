# 验收判据：goal 契约

change: `task-goal-contract`

verify 按出现顺序编号（#1、#2……），`tasks.md` 引用的就是这个序号。

```bash
go build ./...
```

```bash
go test ./internal/change/ -run 'TestParseGoal|TestLoadGoal|TestGoalHash|TestDocProblems' -count=1
```

```bash
go test ./internal/taskpipeline/ -run TestCheckGoalContract -count=1
```

```bash
go test ./internal/cli/ -run 'TestChangeNew_WritesGoalContract|TestChangeNewFast' -count=1
```

```bash
go test ./internal/clitask/ -run 'TestResolveGoalContract|TestTaskStart_RegistersGoalContractFlags' -count=1
```

```bash
grep -q "五份模板" docs/reference/cli.md
```

```bash
grep -q "goal.md" README.md
```

```bash
sh scripts/checks/deadcode-zero.sh
```

## 人工检查项（可选）

- [ ] `forge change status task-goal-contract` 里 goal 契约显示「五段已填」。