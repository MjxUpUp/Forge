# 目标：独立评审发现的文档事实修正

## Objective

用第二个模型（claude 的 key 走 chat/completions）对 core-only 之后重写的文档做独立评审，逐条核实评审发现，把确认为真的文档事实错误改掉；评审里的误报（例如把仍存在的 `--project` no-op、`forge change gate`、`## 📦 安装` 标题当作缺陷）不照做，但要在交付说明里写清为什么是误报。

## Out of Scope

- 不改代码行为，不新增/删除命令；只改文档措辞与事实陈述。
- 不追究评审提到的「evalkit 仍可从 review.go 到达」这类代码面残留（属另一批）。
- 不重写整篇文档，只做定点修正。

## Check

- `forge docs closure --strict` 漂移 0。
- `forge docs lint` 对改动文档 0 硬失败。
- 文档中不再出现与现状冲突的事实：门禁两道、验收入口两个、评分已删、宿主层 1.85 移除、compat 承诺自 3.0.1 起。
