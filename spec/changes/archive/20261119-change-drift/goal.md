# Goal：spec 漂移检测（第 6 项）

## Objective

让「change.md 说改这些，实际改了那些」可见：`forge change drift <id>` 对照 change.md「## 改动」点名的文件与自该 change 建立以来的实际改动（已提交 + 未提交），列出「改了但没声明」与「声明了但没改」。

## Out of Scope

- 不阻断任何门禁：多改/漏改是正常的，问题是偏差没人看见。
- 不解析自由文本语义（只认改动段里的反引号路径）。
- 不自动改写 change.md。

## Check

- 声明过的文件被改、未声明文件也被改 → 输出把未声明项列在「改了但没声明」。
- 声明了但没改 → 列在「声明了但没改」。
- `--json` 输出 `{"declared":…,"changed":…,"undeclared":…,"untouched":…}`。
