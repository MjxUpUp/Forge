# Goal：skill 资本化（第 5 项）

## Objective

让一次 retro 的教训变成下次任务开始时会出现的东西：`forge skill new <id>` 把「做什么 / 何时用 / 根因与做法 / 证据」落成 `skills/<id>/SKILL.md`，frontmatter 带 name 与 description（description 内联 Use when），正文是最小可执行版本。

## Out of Scope

- 不自动从会话文本里「总结」教训（挑教训是判断，不是模板填空）。
- 不生成 triggers（触发词接线属宿主决策，需人确认）。
- 不改既有 skills 的 QA 规则。

## Check

- `forge skill new demo --description … --when … --lesson …` 在临时仓库写出 `skills/demo/SKILL.md`，含 name/description 与三个正文小节。
- 缺 description/when/lesson、id 含 `../` 或大写、`--dir` 越界时拒绝且不落盘。
- 已存在且未加 `--force` 时拒绝覆盖。
