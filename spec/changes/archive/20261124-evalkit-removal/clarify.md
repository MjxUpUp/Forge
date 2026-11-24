# 澄清：边界与非目标

change: `evalkit-removal`

## 边界（做什么 / 不做什么）

边界：删除 forge review llm 判官臂与 internal/evalkit 整包；撤掉只为它存在的 FORGE_EVAL_PREFLIGHT 逃生舱；重钉 compat 快照并修 docs/reference/cli.md；evals/forge/judge-samples 只注历史不删数据。非目标：不改 review pass/gate/status 语义与门禁判定；不改 CHANGELOG/ADR/docs/archive 的历史记录；不处理 compat checks 面其余 eval-* 历史名。人确认引用 m04120（用户就 evalkit 可达性回「改」）。

## 非目标

- 不改 `review pass/gate/status` 的行为、参数与门禁判定；
- 不删 `evals/` 下的历史判分数据（只加「历史」注记，不伪造可复现性）；
- 不改 CHANGELOG、ADR、`docs/archive/` 里的历史记录（它们记录的是当时的事实）；
- 不处理 compat checks 面其余 eval-* 历史名（属另一股残余死面，不在本条范围）。

人确认：2026-11-24T03:54:18Z
