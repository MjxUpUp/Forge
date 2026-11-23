# 澄清：边界与非目标

change: `core-only`

## 边界（做什么 / 不做什么）

边界：只保留 init/on/off/version/next/status/change/task/review/docs/impact/test/compat；删除 config/conventions/harness/migrate/node/policy/project/registry/skill/update/workspace/worktree/dashboard/health/trace/enforcement/skills/bridge/trust/act/eval/gate/verify 及其 cli 文件、失去引用的 internal 包与测试；删 18 个无生产者检查名、artifactchain 与 loopedge 的 chain.yaml 读取、testcoverage 兜底、docs/plans+surveys+自述已删/已被取代的 design。非目标：不改核心命令语义与门禁判定、不重写剩余实现、不删 docs/adr 与 CHANGELOG 与 spec/changes/archive、不动 git 历史。人确认来源：用户在清理选项里明确选「A+B+C+D：旧命令组也砍」（连同前一条 m02464 全项目清理要求）。

## 非目标

- 不改 change/task/review/docs/impact/test/compat 的核心语义与门禁判定。
- 不改 3.0 已定的评分/证据/红先绿机制。
- 不重写剩余命令的实现，只删与修编译/测试连带。
- 不删 docs/adr、CHANGELOG.md、spec/changes/archive（决策与变更记录，不是残留）。
- 不动 git 历史；删掉的内容在历史里仍可取回。

人确认：2026-11-23T16:34:21Z
