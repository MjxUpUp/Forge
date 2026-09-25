# R1-R18 校验规则文本定义

`forge skills validate` 的规范校验规则（exit code：0=全部通过，2=存在规范失败）。
**真相源是代码**：`internal/skillsqa/registry.go`（`AuditSkill`；R1-R11 对齐 SkillsHub registry.py，R12-R18 为 forge 本地扩展）。
本文件是文本镜像，两边漂移时以代码为准；改规则先改代码再同步本文件。

## 硬规则（违反 = issue，validate exit 2）

| 规则 | 定义 |
|------|------|
| R1 | name 必须 kebab-case：`^[a-z][a-z0-9-]*$` 全匹配 |
| R2 | name 必须与目录名（skill id）一致 |
| R3 | frontmatter 顶层字段白名单：`name` / `description` / `license` / `allowed-tools` / `metadata` / `compatibility` / `version` / `requires`（防字段 typo） |
| R4 | description 长度 ≥80 字符（按字符数计，非字节）；>1024 字符超 Anthropic skill 规范上限（硬）；>500 偏长（advisory，建议精简到 what+when） |
| R5 | description 必须含 `Use when`（大小写不敏感子串） |
| R6 | description 必须含 `SKIP`（大小写不敏感子串） |
| R7 | `metadata.pattern` 必填且合法。合法原子值：`tool-wrapper` / `generator` / `reviewer` / `inversion` / `pipeline` / `gate` / `routing` / `fallback` / `reference`（经验/踩坑记录型，2026-08 新增）；支持 `+` 组合（如 `pipeline + gate`），每段都须合法 |
| R8 | SKILL.md ≤500 行（超了拆 references） |
| R9 | 正文须含高信号内容，命中任一关键词即过：`decision tree` / `决策树` / `post-generation` / `自查` / `review` / `gotcha` / `易错` / `checklist` / `检查清单` / `red flag` / `rationaliz` / `红旗` / `借口` |
| R11（硬部分） | `references/` 平铺 ≤1 层——文件直接放 references/ 下，不允许子目录 |
| R13 | SKILL.md 正文（不含 frontmatter）≤500 行（2026-08 新增；R8 对齐 Python 计全文行，R13 显式化正文口径，body >500 时两者同报） |
| R14 | frontmatter 必填 `name` 与 `description`（2026-08 新增；description ≤1024 字符上限由 R4 覆盖不重复报） |

## Advisory（不阻断 Pass，但应修）

| 规则 | 定义 |
|------|------|
| R4（上限部分） | description >500 字符：偏长 advisory |
| R10（CSO） | description 不应总结 body 工作流——命中工作流总结词（`完整工作流` / `完整流程` / `全流程` / `完整协议` / `完整编排` / `全链路` / `全工序`）报 advisory。否则模型照 description 行动、跳过 SKILL.md 正文 |
| R11（软部分） | >100 行的 markdown reference 建议带 ToC（认 `## 目录` / `## Contents` / `## Table of Contents`） |
| R12 | `metadata.triggers` 声明校验（实验字段，不写合法；写了则校验）：合法 JSON；`event` ∈ `UserPromptSubmit` / `PreToolUse` / `PostToolUse` / `Stop` / `SessionStart`；`keywords` 或 `when` 至少一；`when` ∈ `source_changed_uncommitted` / `test_command_failed` / `coding_intent` / `task_active_no_review` / `skill_file_touched`；`match` 仅对 PreToolUse/PostToolUse 有意义 |
| R15 | 正文 ALL-CAPS 命令式词（`ALWAYS`/`NEVER`/`MUST`，整词、仅全大写计）合计 >5 次：提醒改「指令+原因」写法（2026-08 新增） |
| R16 | `references/` 下 >300 行文件需 ToC（2026-08 新增；markdown 由 R11 以 >100 行更低门槛先行覆盖，不重复报，实际增量是非 markdown 参考文件） |
| R17 | `evals/evals.json` 存在时校验 schema：对象含 `trigger_cases` 数组，每项 `{query: string, should_trigger: boolean}`（2026-08 新增；文件不存在则跳过） |
| R18 | skill 目录零 forge 反向依赖（硬 issue）：SKILL.md 正文与全部内容文件（decisions.md/evals 除外）不得含 forge CLI 调用、`~/.forge/` 路径、`$FORGE_*` 变量、forge-integration.md 指针；命中即失败。存量豁免表 `R18Grandfathered` 已于 2026-08 迁移后清空（集成知识迁至 forge 侧 `forge skills integration`），`metadata.requires_forge: "true"` 的 forge 原生 skill 跳过（CONVENTIONS §13） |

### triggers 匹配语义陷阱（R12 查不出，写 keywords 前必读）

R12 只校验「keywords 或 when 至少需一」，**查不出「配了 keywords 却永远不会触发」**。keywords 的匹配源只有：prompt、`ToolInput["command"]`（经 sanitizeCommand 剥离 heredoc body——守卫类关键词写在 heredoc 内容里不会命中）、`ToolOutput` 的 stdout/stderr/output（`internal/skilltrigger` sourceText，命中优先级同此序）。推论：

- **工具门禁类 trigger（`PreToolUse match Agent|Task` 等）不要配 keywords**：Agent/Task 这类工具的 ToolInput 没有 `command` 键，PreToolUse 时刻还没有 ToolOutput，多数宿主的 PreToolUse 载荷也不带 prompt——keywords 三个匹配源全空 = 静默禁用。这类 trigger 用合法 `when`（如 `source_changed_uncommitted`）表达触发条件。
- keywords 适合「用户输入/ shell 命令文本里会出现的词」（UserPromptSubmit、Bash 类 PreToolUse）；when 适合「环境状态条件」（工作区有未提交源码、测试失败等）。
- ToC 锚点按 GitHub slugger 规则：标点（含 em-dash「—」）删除、空格转连字符——「A — B」的锚点是 `--` 双连字符，写 ToC 链接时逐条核对。

## 配套

- 安全审查（21 条规则 + 加权评分）：`forge skills audit`，实现同包（audit.py 语义对齐 + forge 本地 DC-8/DC-9/DC-10 供应链执行向量），不属于 R1-R18 规范契约。
- 防注水扫描：`references/skill-anti-degradation-check.sh`（见 SKILL.md「防注水自检」节指针的拆分文件 references/anti-degradation.md）。
