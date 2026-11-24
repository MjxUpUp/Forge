# goal：evalkit-removal

## Objective

删除 `forge review llm` 判官臂与 `internal/evalkit` 整包（35 个文件），让评测时代的 LLM 判官代码不再从 `cmd/forge` 可达；同时撤掉只为它存在的 `FORGE_EVAL_PREFLIGHT` 逃生舱与命令面条目，并重钉 compat 快照。

完成状态可观察：`forge review llm` 报 unknown command；`go list -deps ./cmd/forge` 不含 `internal/evalkit`；`forge compat report` / 快照里命令面少一条、逃生舱少一条；`forge docs closure --strict` 漂移 0。

## Out of Scope

- 不改 `review pass/gate/status` 的行为、参数与门禁判定；
- 不删 `evals/` 下的历史判分数据（只加「历史」注记，不伪造可复现性）；
- 不改 CHANGELOG、ADR、`docs/archive/` 里的历史记录（它们记录的是当时的事实）；
- 不处理 compat checks 面其余 eval-* 历史名（属另一股残余死面，不在本条范围）。

## Check

见 `acceptance.md` 五条围栏：构建、判官臂消失守卫、compat 面、能力面漂移、相关包测试。
