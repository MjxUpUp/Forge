# 协议可执行化：design

## 改动面

| 文件 | 改动 |
| --- | --- |
| `internal/change/templates/agents-protocol.md` | 顶部新增「开始之前（硬规则）」；`spec/forge.yaml` 条目改成「hooks 仅兼容保留、1.85 起没有 hook 执法层」；新增「Task 流程（门禁在命令里）」；「快速通道」后的「不建 change」提成独立小节；逃生舱区分验证类与节奏类并点名 flag |
| `internal/change/agents_protocol_test.go`（新） | 钉模板的可执行性与无残留：含「先跑 `forge next`」、含「## Task 流程」、不含 `hazard-guard`/`hooks: on` |
| `scripts/checks/protocol-actionable.sh`（新） | 同上，从「产物文件」视角再钉一遍，防止只在测试里生效 |
| `scripts/checks/migration-doc.sh`（新） | README 必须含「从 1.84 及更早版本升级」节与 `FORGE:START` 清理命令 |
| `README.md` | 新增「从 1.84 及更早版本升级」节：旧版写入落点表（claude/codex/windsurf/kimi）、逐个判断的备份、按文件编辑而非整删的 hook 清理、插件登记、悬空符号链接、自检命令 |
| `internal/change/change.go`、`internal/specroot/specroot.go` | 两处 `hooks` 字段注释与模板对齐：兼容保留、不再驱动任何执法 |
| `spec/changes/cmd-surface-convergence/`（删） | 与 `archive/20261114-cmd-surface-convergence/` 重复（归档后 amend 又落回），导致 `change check` 假红 |
| `spec/changes/protocol-actionable/**`、evidence | 本次 change 自己的五份产物、`.goal-required` 与 verify 证据 |
| `AGENTS.md`（仓库自身） | 由 `forge change agents` 用新模板重新同步托管块；该文件在 `.gitignore` 里，不入库，因此不会出现在 diff 中 |

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

- 先备份，且**逐个判断文件是否存在**（不再用 `cp … 2>/dev/null` 静默吞错）；清单含
  `installed_plugins.json` 与 codex 的 `config.toml`。
- 旧注入落在四个宿主：`~/.claude/CLAUDE.md`、`~/.codex/AGENTS.md`、
  `~/.codeium/windsurf/memories/global_rules.md`、`~/.kimi-code/config.toml`；
  `grep -l "FORGE:START"` 只查这四个入口文件，不递归扫 `~/.claude`（会把历史会话
  jsonl 里的字样误报出来）。
- hook 配置**只删条目、不删文件**：codex 的 `hooks.json` 里用户自己的 hook 要保留，
  `config.toml` 的 `[features] hooks` 要回滚；claude `settings.json` 同理。
- skills 分发：先 `find … -maxdepth 1 -type l -delete` 删指向缓存目录的链接，再删
  `~/.forge/skills-cache`；否则 Windows 上会留下悬空 junction。插件还要清
  `installed_plugins.json` 的 forge 条目。
- 自检四条：四个入口无 `FORGE:START`、两个 hook 配置无 `forge hook`、缓存目录不存在、
  skills 目录无悬空链接；再在仓库里跑 `forge status` 看契约。

## 附带的清理

`spec/changes/cmd-surface-convergence/` 在归档后又因 amend 重新落回工作树，与
`spec/changes/archive/20261114-cmd-surface-convergence/` 重复，导致 `forge change check`
把它当作未归档 change 并报「证据已失效」。本次一并删除这份重复目录，避免每个分支
都继承一条假红。

## 风险与取舍

- **措辞过强**：可能让 agent 对一行文案也跑全套门禁 → 用「代码改动」限定，并保留
  `wild` 与「不走 forge」两个显式出口；周界由团队约定，不写成机器阈值。
- **迁移命令是破坏性的**：只写在 README，不内置成命令；强调先备份、逐条执行。
- **模板与仓库 AGENTS.md 可能漂移**：`AGENTS.md` 在 `.gitignore` 里、不入库，没有 CI
  守卫；同步后人工核对：`diff <(sed -n '/forge:begin/,/forge:end/p' AGENTS.md) internal/change/templates/agents-protocol.md`
  （只允许尾部换行差）。