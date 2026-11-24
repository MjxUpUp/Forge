# change：evalkit-removal

## 改动

1. `internal/cli/review.go`：删除 `reviewLLMCmd`、`runReviewLLM`、`llmDispatchScaffold` 与 `init()` 里的注册/flag；随之删除只服务它的 `encoding/json`、`time`、`projectroot` 与 `internal/evalkit` import。
2. 删除 `internal/cli/review_llm_test.go`（三路径测试随命令消失）。
3. 删除 `internal/evalkit` 整包（card/decompose/env/escapeaudit/golden/golden_harvest/helpers/hookwire/judgeaudit/metrics/oracle/preflight/report/resume/rotate/runner/stats/telemetry/traps/wedgedrill 及测试）。
4. `internal/compat/compat.go` 的 `EscapeEnvs` 去掉 `FORGE_EVAL_PREFLIGHT`（其常量只存在于被删包）；`internal/compat/compat_test.go` 的 `rosterExtraSources` 去掉对应条目（守卫会校验文件存在与字面量在场）。
5. `docs/reference/cli.md` 去掉 `forge review llm` 行；`evals/forge/judge-samples/README.md` 顶部注明该采样协议的命令已移除（数据保留为历史）。
6. `compat.snapshot.json` 重钉：命令面 87 → 86（少 `forge review llm`），逃生舱 9 → 8（少 `FORGE_EVAL_PREFLIGHT`）。

## 影响

- 命令面少一条子命令（`forge review llm`）——它是 core-only 之后仍可达的 eval 时代入口，删除属破坏性收窄，但该命令从 `forge review` 的 help 里消失后不再误导 agent。
- 逃生舱少一条——`FORGE_EVAL_PREFLIGHT` 只被已删的 evalkit preflight 读取，留着是死开关。
- `docs/reference/cli.md` 由快照重新生成；`forge docs closure --strict` 必须保持 drift 0。
- 不改任何门禁语义：review pass 仍是 task-complete 的硬前置。

## 验证

`acceptance.md` 五条：构建、判官臂消失守卫（先红后绿）、compat 包测试、能力面漂移 0、cli/change/taskpipeline 定向测试。
