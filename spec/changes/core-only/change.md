# change：core-only

## 改动

- 删除旧时代 CLI 命令组及其 cli 文件与测试（见 goal 白名单之外的 19 组）。
- 删除由此失去引用的 internal 包（registry/projectsync/worktree/nodeid/nodestamp/trust/bridgeverify/dashboard/
  health/evalsuite/act/…），保留仍被核心命令使用的基础包（forgedata/projectroot/checklog/change/taskpipeline/…）。
- 删除 checklog 中 18 个无生产者的检查名常量；`task-verify` 改名为 `task-gate`，读取旧记录时兼容 `task-verify`。
- 摘掉 internal/artifactchain 与 internal/taskpipeline/loopedge.go 的 chain.yaml 读取（loop 预算取默认值）。
- 摘掉 internal/taskpipeline/testcoverage.go 的 task-complete 兜底及其测试。
- 删除 docs/plans、docs/surveys，以及 docs/design 下自述「已删除/已被取代」的文档；修掉剩余文档里的死链。
- 重新生成 compat.snapshot.json、AGENTS.md 命令面、docs/reference/cli.md 相应行。

## 影响

- 命令面从 200 降到约 60；compat 快照与 golden 测试需要重钉。
- 旧用户脚本里调用被删命令会直接 unknown command（预期：这是刻意的破坏性清理）。
- 核心包测试是被删命令的测试之外的全部常规测试；一次性删除约 60 个 cli 测试文件。

## 验证

acceptance.md 的 5 条围栏；另跑 go test ./... -count=1 与 forge docs closure --strict 作为人工确认。
