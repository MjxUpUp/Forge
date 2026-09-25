# W0.2 Spec：batch 单入口分派本体

## 问题

同 matcher 组内每个 hook 一条 `forge hook <name>` 接线，宿主逐条拉起进程——
一次 Bash 调用在 PreToolUse/Bash 组拉 4 个 forge，SessionStart 一次拉 6 个。

## 方案

`forge hook batch --event E [--matcher M]`（internal/hookdispatch/hook_batch.go）：
一次进程按名册顺序递归实跑组内全部 hook——os.Stdin 换载同一 payload、stdout
逐 hook 捕获。语义与逐条拉起对齐：

- 首个 block（*HookBlockError）原样透传，外层 cobra 映射 exit 2 + 正确 JSON，
  后续 hook 不再执行（宿主「阻断即停」语义相同）；
- 全 allow：claude 形态 additionalContext 合并为单次注入（空保持静默），
  非 JSON 输出退化为文本拼接；
- 名册现查 ForgeHookSpecForProfile(ActiveProfile())——与档位（W0.3）同源，
  内层 RunHook 的档位门二次兜底；
- 接线变换 ForgeHookWiring()：多 hook matcher 组收敛为一条 batch 条目
  （%q 引号保证 matcher 的 `|` shell 安全），单 hook matcher 保持原条目。

## 已知边界

windsurf 名册硬编码、未批量化——出口档位过滤已生效，其镜像比对用 per-hook
spec fixture（translator_test.go 显式变体）。batch 接线已覆盖全部 claude 系
写渠（用户级 settings、插件载荷、codex/zcode/cursor/kimi/codebuddy/reasonix），
镜像测试比较器已同步迁移到 batch 条目断言。

## 验收

- TestForgeHookWiring_BatchTransform：事件/matcher 数不变、条目数下降、
  batch 命令含事件与 matcher。
- batch 分派单测：TestRunHook_ProfileGateSkipsDroppedHook /
  TestRunHook_ProfileGateKeptHookStillRuns（档位门两向）+
  TestPreToolUseBash_LatencyBudget（耗时预算，实测 mean 102ms）。
- 全仓 build/vet/gofmt/staticcheck/deadcode 零故障，go test 63 包零失败。
