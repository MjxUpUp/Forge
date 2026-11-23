# 澄清：边界与非目标

change: `residual-hardening`

## 边界（做什么 / 不做什么）

用户 m03366「把这些剩下的风险也全都处理」= 对上一轮 core-cleanup 验收列出的 5 条残留风险的授权。边界：①删除 act.Disposition 整套 ack 死面（含 forgedata.ActDispositionsPath、health.SummarizeAtAcked），ack 通路随 forge act retro-done 一起消失；②死提示守卫从 fmt 实参扩到非测试 Go 源码全部字符串字面量 + internal/**/templates/*.md，豁免用 benign 词或 deadhint:allow；③criteria 空匹配判定补 no test files 与「-v 下无 === RUN」结构性证据；④testledger 跳过判定把 switch/循环分支算条件、识别 helper 间接跳过；⑤skills/skills-forge 内容目录加守卫（目录⇄SKILL.md⇄frontmatter name）。非目标：不改门禁语义、不加顶层命令、不动 core-only 删除面、不改变异判定。

## 非目标

- 不改门禁语义（task/change 两道门禁的判定口径、退出码来源、证据封印规则一律不动）。
- 不新增顶层命令，不改 `forge` 命令面（compat 快照除 `internal/act` 删除导致的载荷项变化外应无差异）。
- 不重写 core-only 已删的命令组，不恢复任何已删能力。
- 不动 git 历史、不删 ADR / CHANGELOG / `spec/changes/archive`。
- 不改变异算子与 mutation 判定口径。

人确认：2026-11-23T17:53:52Z
