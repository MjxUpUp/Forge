# 澄清：边界与非目标

change: `gate-hardening`

## 边界（做什么 / 不做什么）

边界：① -run 无匹配（no tests to run）判负；② 行为类任务缺 --accept-red 则 task-complete BLOCKED，豁免须显式 flag+理由；③ review pass 必须声明 --criteria-reviewed。非目标：不做 --reaffirm/--criteria-by、不改 eval suite、不改 change 侧判据语法判官、不加顶层命令、不做 hook

## 非目标

- 不实现 `--reaffirm`（判据逾期失效）与 `--criteria-by`（判据作者独立）——留到 B2 批。
- 不改 eval suite 的阻断范围，不改 change 侧判据语法判官（internal/change/judge.go）。
- 不新增顶层命令；不改两道门禁的名称与顺序。
- 不把任何检查做成后台 hook：仍然是命令跑才生效。

人确认：2026-11-23T11:09:47Z
