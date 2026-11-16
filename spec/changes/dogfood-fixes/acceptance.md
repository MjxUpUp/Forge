# 验收：dogfood-fixes

```bash
# 1. 编译
go build ./...
```

```bash
# 2. next 干净路由 + 评分静默回退
go test ./internal/taskpipeline/ -run 'TestNextDecision|TestLoadScoringConfig|TestBuildEvaluateInput|TestNextHint_SpecRepo' -count=1
go test ./internal/protocol/ -run TestConfigured -count=1
```

```bash
# 3. AGENTS 模板钉住 dogfood 踩到的步骤
go test ./internal/change/ -run TestAgentsProtocol -count=1
```

```bash
# 4. cli.md 与 help 面
go test ./internal/cli/ -run 'TestHelpSurface' -count=1
```

```bash
# 5. 格式守卫
sh scripts/checks/gofmt-clean.sh
```

```bash
# 6. 死代码零
sh scripts/checks/deadcode-zero.sh
```

## 人检

- [ ] 在干净仓库跑 `forge next`，首条命令是 `forge task start`，不是 `forge status`。
- [ ] 契约仓库跑 `task complete` 不再出现 protocol.yml 告警。
