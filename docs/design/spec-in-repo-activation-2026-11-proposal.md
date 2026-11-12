# 把激活权放回仓库：spec 层 + 可选验证器

status: superseded by docs/adr/0002-repo-takeover-drop-host-layer.md（2026-11-12）
注：本文件保留了当时的九份行业材料摘录与取舍过程，结论以 ADR-0002 为准。
触发：用户提出「非 forge 项目也被无脑执行 hook/检查，噪音、污染、浪费 token」，并希望把企业内部
的 spec 规范合并进来，让流程「每阶段有可审计产物、换机器换 agent 也能跑、不必安装插件」。

## 0. 结论先行

1. 不整体照搬 LoopSpec / OpenSpec / fleet-cli。借它们的**产物形状**（目录、编号、digest 批准、
   归档），不借它们的**执行编排**。LoopSpec 仓 981 个被跟踪文件里 806 个（82%）是工作流产物，
   fleet-cli 的 openspec 只有 27 个文件——说明这套方法的本体极轻，重的是被它带进仓库的账本。
2. Forge 的定位改为：**仓库里有一份 spec，Forge 才存在；没有就一个字节都别花。**
3. hooks 从「安装即接管」改为「仓库声明才接线」，单入口 batch 保留。
4. spec 层是纯文件、零运行时、随 git 走；Forge 二进制只是它可选的验证器。没有 Forge 的 agent
   读 `AGENTS.md` 的 20 行协议也能执行；Forge 负责的是「判定」而不是「携带流程」。

## 1. 现状：三个成本中心（本机实测）

### 1.1 接线在用户级，不在项目级

`internal/hooks/settings.go` 的 `GenerateUserSettings` 把钩子写进 `~/.claude/settings.json`
（user-level，机器上每个项目都读）；codex / cursor / kimi 各自还有一份插件或 hooks.json。
`forge init` 自 v1.22 起「零项目写入，全部资产在用户级」——这条设计让换机器必须重新 init，
也让非成员仓库无条件付成本。

### 1.2 每次工具调用的固定税

`internal/hooks/settings.go:80 ForgeHookSpec()`：standard 档 35 条目 / 8 事件（`profile.go:7`），
其中 `Write|Edit` 上 Pre 6 条（freeze-guard / task-guard / assertion-check / read-before-edit /
skill-trigger / conventions-write）、Post 5 条（auto-compile / workflow-test-guard / skill-trigger /
test-nudge / doc-lint），Bash 上 Pre 5 条 + Post 3 条。

W0.2 的 `forge hook batch`（`internal/hookdispatch/hook_batch.go`）已把一组收敛成一次进程拉起，
codex/cursor/kimi 接线里是 `forge hook batch --event ... --matcher ...`。本机实测（Windows，
C:\...\Temp\notforge，非 forge 项目）：

| 调用 | 实测 |
|---|---|
| `forge hook task-guard`（单条） | 0.10–0.12 s |
| `forge hook batch --event PreToolUse --matcher "Write\|Edit"` | 0.12–0.13 s |
| 直达二进制 `...\npm-global\forge` | 0.10–0.13 s（不是 cmd shim 的锅） |

→ 一次编辑 2 次拉起 ≈ 0.25 s，一次 Bash 2 次 ≈ 0.25 s，**非成员仓库同样支付**。
`hookdispatch/hook.go:502` 在 `projectroot.Find()` 失败时静默放行——放行是对的，但税已经交了。

### 1.3 上下文污染

`internal/hooks/embed_task.go:197 InitSuggestHook` 是 **global SessionStart**（注释自陈
「全局 hook：在非 forge 项目正是要发现它们」）：在任意非 forge git 仓库首次开会话时，向 agent
注入「建议 git init + forge init」的 additionalContext，并额外 spawn 一次 `forge policy state`。
这才是「污染 + 浪费 token」的直接来源——forge 在别人的项目里主动开口说话。
`skill-scan` / `mcp-scan` 同类全局扫描（`hookdispatch/hook.go:496` 注释：global hook 在任何
project 都相关），每次会话都要扫 `$HOME/.claude/skills`。

## 2. 为什么会这样：状态放错了地方

| | 能力在哪 | 状态在哪 | 换机器 | 换 agent | 非成员项目 |
|---|---|---|---|---|---|
| Forge 现状 | 二进制 + 宿主接线 | `~/.forge`（projects.json / task state / checklog） | 重新 init | 重装插件 | 白付成本 + 被搭话 |
| LoopSpec / OpenSpec | 协议 + 目录 | 从文件系统推导 | git clone 即可 | 读文件即可 | 完全不参与 |
| 你企业那套 spec 规范 | 人 + 文档模板 | 每阶段产物 | 带走产物即可 | 读产物即可 | — |

所以问题不是「自动化太自动」，而是**「是否参与」的判据放在了安装态**。把它换成仓库态，
三个抱怨会一起消失，而且不需要牺牲门禁强度。

## 3. 目标形态：三层，激活在仓库

```
L0 契约层（纯文件，零运行时，随 git 走）
   spec/forge.yaml                      ← 激活声明 + 档位 + gate 顺序
   spec/changes/<id>/{proposal,design,tasks,acceptance}.md
   spec/changes/<id>/evidence/          ← 运行证据，绑 diff
   spec/specs/                          ← 归档后的稳定规格
L1 执行协议（repo-owned 说明）
   AGENTS.md 里 20 行状态机：proposal→design→tasks→evidence→archive
L2 验证器（CLI，可选）
   forge change new|plan|apply|verify|archive
L3 会话门禁（hook，按需）
   只有 spec/forge.yaml 存在才接线/才运行；batch 仍是单入口
```

L0+L1 是「换 agent 也能跑」的部分；L2 是「判定」；L3 是「在会话里提前拦」。三者解耦：
没有 L2/L3，流程仍可执行，只是没有机器判定；没有 L0，L2/L3 一秒都不运行。

## 4. 目录形状

```
spec/
  forge.yaml                 # version, profile: mini|full, gates: [build,test,acceptance]
  changes/
    2026-11-order-export/
      proposal.md            # 为什么做 / 范围 / 非目标 / 影响面
      design.md              # 怎么做 / 接口 / 数据 / 风险
      tasks.md               # 可勾选，一条一验证点
      acceptance.md          # 判据：可执行命令 + 期望输出 + 人工判定项（分离标注）
      plan.digest            # 本次改动文件白名单 + 方案摘要的 SHA-256（人按 digest 批准）
      evidence/
        build.log
        test-01.json
        acceptance-A3.png
  specs/                     # archive 后沉淀的稳定规格（下游只读）
```

三条与 OpenSpec/LoopSpec 的**刻意差异**：

1. `acceptance.md` 必须是**可执行**判据（命令 + 期望、字段级断言），不是「验收标准」清单。
   依据：飞猪 p3 的假成功案例——`rowCount=1` 但 status 期望 ENABLE 实际 DRAFT；以及 p2
   张忻正「代码写完不算完，证据齐了才算完」「客户端 0 直接合，因为拿不出用户看见了的证据」。
2. `evidence/` 与 diff 绑定并 `sealed`（何伟 p1 的 `journey-result-EV-001.json` 带 `sealed:true`）。
   证据不是截图堆，是「这次 diff → 这条判据 → 这份输出」的闭包。
3. 批准 = 对 `plan.digest` 的一次人工签字，摘要一屏（LoopSpec 同款；Forge 已有
   `internal/taskpipeline/approvaldigest.go`，注释引 CHI 2025：人对 AI 计划的信任校准度只有 0.50，
   过细的计划反而增加负担）。

## 5. 与九份材料的对齐（取什么 / 不取什么）

| 材料 | 取 | 不取 |
|---|---|---|
| 何伟 p1 一人三端 | 双循环分层（主仓共同 spec / 子仓端内 plan）、编号制、证据目录、sealed | 全套 .qoder skills 依赖（那是 Qoder 私有能力） |
| 张忻正 p2 bilibili | 「只数能直接合的 MR」的度量观、四份证据、契约先行 | 六个 Agent 的组织形态（他们自己砍到只剩 Code Agent） |
| 李桐馨 p3 飞猪测试 | 结构化进结构化出、字段级断言、回流只回写已确认结论 | 阶段一那种「逐环节 AI 提效」（时间只是从执行搬到确认） |
| 王浩男 p7 交付大脑 | 上下文共享清单、共识按版本保存、失败子任务续做 | 超级流程的编排器（重，且绑内部平台） |
| 许晓斌 p9 阿里 | 验证环境与生产差距、SWE-Milestone 93.2% vs 21.1% | 自建 benchmark 基础设施 |
| 王政达 p5 GUI | Case→DSL→无 LLM 回放→证据包 | GUI 自动化本身 |
| 王泽锋 p6 柯南 | 轻 Agent 重上下文、Skills=原子能力+结束点、L1–L3 事实/L4 推测分层、只读工具面 | 告警域的产品形态 |
| 拓海 p8 专才 Agent | 单一 JSON 入口、交接产物约定、改动范围检查 | 节点级拆解的全套 |

横向结论：**九份材料没有一份把「流程」做成插件**。它们都把流程做成产物（spec / plan / DSL /
node_spec.json / 证据包），把能力做成 CLI 或 skill。Forge 现在恰好相反。

## 6. 落地三步

**P0 止血（约 3 天，可独立发布）**
- `init-suggest` 改为 opt-in（`forge config suggest=on` 才注入），默认不在非成员项目开口。
- hook 入口顺序改为：读 `spec/forge.yaml` 标记（一次 stat）→ 不存在直接 exit 0，不读 registry、
  不 spawn `forge policy state`。
- 非成员仓库：SessionStart 注入 0 字节、spawn 次数降到 0–2。
- 度量：非成员仓库一次 Edit 的进程数与注入字节数，写成 guard test。

**P1 spec 层（约 2 周）**
- `forge change new|plan|apply|verify|archive`；状态从文件推导，不新增进度库。
- `AGENTS.md` 协议段（20 行）+ 一个 `spec/` 模板；验证「无插件、纯读文件」的 agent 能跑通。
- 3 个真实 change 对照：有 spec / 无 spec 的返工次数与 token 消耗。

**P2 收口**
- evidence sealed、archive 到 `spec/specs/`、`forge change import <dir>` 把你企业那套规范映射进来。
- hook 只读化（不写项目文件）、项目级接线（`.claude/settings.json` 随仓库走）+ `forge trust` 签名。

## 7. 反例与风险

- **spec 不能变成更多文档**。82.3% 的项目历史上出现过文档与代码不一致；AGENTS.md 类上下文文件
  不普遍提高成功率、推理成本平均 +20%。若 `acceptance.md` 不能执行、`evidence/` 不绑 diff，
  这层就是新的验证债。
- **gate 不能全自动放行**。SWE-Milestone 里独立任务 93.2%、连续演化只有 21.1%——跨阶段连续性
  仍要人把关；`plan.digest` 的人工签字是这条的落点。
- **项目级接线 = 仓库里的命令会被别人执行**。需要 `forge trust` 签名与首次确认，别把供应链风险
  换成便利。
- **迁移成本**：企业规范若已有一套目录与审批流，importer 要做映射而不是另起一套；否则两套并存，
  漂移立刻发生。

## 8. 待拍板

1. 企业那套 spec 规范的实际目录/字段/审批环节（决定 importer 的形状）。
2. 默认档：`mini`（只用 CLI 门禁，不接 hook）还是 `full`（接 hook）？
3. 是否接受「spec 目录提交进仓库 + 项目级接线」这两个前提。