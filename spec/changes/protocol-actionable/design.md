# 协议可执行化：design

## 改动面

| 文件 | 改动 |
| --- | --- |
| `internal/change/templates/agents-protocol.md` | 顶部新增「开始之前（硬规则）」；`spec/forge.yaml` 条目删掉 hooks 执法叙述；新增「Task 流程（门禁在命令里）」；「快速通道」补一句 C 模式（不走 forge）的边界 |
| `internal/change/agents_protocol_test.go`（新） | 钉模板的可执行性与无残留：含「先跑 `forge next`」、含「## Task 流程」、不含 `hazard-guard`/`hooks: on` |
| `scripts/checks/protocol-actionable.sh`（新） | 同上，从「产物文件」视角再钉一遍，防止只在测试里生效 |
| `scripts/checks/migration-doc.sh`（新） | README 必须含「从 1.84 及更早版本升级」节与 `FORGE:START` 清理命令 |
| `README.md` | 新增「从 1.84 及更早版本升级」节：旧注入、旧 hook 配置、旧 plugin/skills 的备份与清理、自检 |
| `AGENTS.md`（仓库自身） | 由 `forge change agents` 用新模板重新同步托管块 |

## 协议文本要点（为什么这么写）

1. **硬规则放最前、用命令动词。** 说明文放后面当参考；第一屏的决定性句子是
   「任何代码改动之前，先跑 `forge next`，只执行它给出的下一步」。
2. **点明「门禁是命令，不是 hook」。** 这是 1.85 后最容易误解的一点：没有后台拦截，
   所以 agent 必须自己跑 `task-implement`/`task-verify`/`task complete`；不跑就等于没门禁。
3. **给出口，别只给禁止。** `forge task wild "<说明>"`（一次性小改申报）与
   「这次不用走 forge」（不建 change）保留为显式出口，避免 agent 因「必须全套」而绕过整条流程。
4. **Task 流程独立成段。** change 流程讲产物，Task 流程讲门禁与证据；之前只有前者，
   agent 自然只写文档不跑门禁。

## 迁移说明要点

- 先备份（`~/.claude/CLAUDE.md`、`~/.codex/AGENTS.md`、`settings.json`、`installed_plugins.json`）。
- 用 `grep -rl "FORGE:START"` 定位旧注入；整段删除或整文件删除。
- 清 `~/.codex/hooks.json` 的 forge 条目、`~/.claude/settings.json` 里 forge hook 条目、
  旧 plugin 目录、`~/.forge/skills-cache` 与指向它的符号链接。
- 自检：仓库内 `forge status` 正常；用户目录 `grep -rl "forge hook"` 为空。

## 附带的清理

`spec/changes/cmd-surface-convergence/` 在归档后又因 amend 重新落回工作树，与
`spec/changes/archive/20261114-cmd-surface-convergence/` 重复，导致 `forge change check`
把它当作未归档 change 并报「证据已失效」。本次一并删除这份重复目录，避免每个分支
都继承一条假红。

## 风险与取舍

- **措辞过强**：可能让 agent 对一行文案也跑全套门禁 → 用「代码改动」限定，并保留
  `wild` 与「不走 forge」两个显式出口；周界由团队约定，不写成机器阈值。
- **迁移命令是破坏性的**：只写在 README，不内置成命令；强调先备份、逐条执行。
- **模板与仓库 AGENTS.md 可能漂移**：本次提交后用 `forge change agents` 同步，
  现有文档守卫测试会检查托管块与模板一致。