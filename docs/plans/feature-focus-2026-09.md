# 功能聚焦决策：砍什么、聚焦什么（2026-09）

- 依据：发展方向深度调研（2026-09-05，~/.forge/research/forge-direction-20260905-0956/report.md，P0/P1 结论）、skills 价值审计（docs/skills-value-audit-2026-08-02.md）、代码普查（docs/surveys/2026-09-code-census.md，债务已清偿）、本机 dogfooding 使用数据（checklog 533 条 / 31 种 check / 53 skill 中 16 个有触发记录 / 26 个任务）。
- 决策口径：**聚焦 = 投入加大；拆包 = 移出核心分发但不删除；冻结 = 维护模式不新增能力；砍掉 = 移除代码与命令。**
- 判据（按序）：① 是否落在"跨宿主证据与签核层"新定位上；② 是否属于调研确认的深水资产（6-12 个月无原生对标）；③ 使用实证（dogfooding 频次 / blocked 实际发生）；④ 维护成本与重复度（审计已知问题）。

---

## 一、聚焦清单（投入加大，对应调研 P0/P1）

| # | 聚焦对象 | 对应方向 | 现状证据 | 下一批动作 |
|---|---|---|---|---|
| F1 | **任务门禁管线**（task start→implement→verify→complete + gates + 三段工件 + forge next/wild） | A/B 底座 | task-verify 69 次、task-complete 25 次、plan-first 24 次；vNext P1-P3 已落地 | 门禁终裁迁移 git/PR 收口（pre-push/PR check + checklog 按 commit 归档） |
| F2 | **反作弊扫描族**（cheat-scan 38 / test-capability-scan 38 / assertion-check 37 / unused-scan 38 / verify-acceptance 14） | B（P0） | 全部高频在用；恒真断言检测 1.50 刚补 | P0 学术转化包：held-out gap 门禁、自报-checklog 一致性门禁、safe-halt 语义、监控分段前置 |
| F3 | **checklog 签名审计 + 节点身份**（checklog/nodestamp/nodeid/trust） | D1（P0） | 伪造检测 1.50 刚落地；ed25519 被 OWASP AST10 钦定为同款原语 | OTel GenAI exporter（checklog→spans/events）；IETF AAT / OWASP ACS versioned mapper |
| F4 | **hooks 纵深 22 个**（重定位为"证据生产者"） | A | read-before-edit 与 hazard-guard 各实际 blocked 22 次——唯一真正开枪的两道门 | repo-level 分发双通道（云 agent 兼容）；hook 结论统一写 checklog 随 git 上行；hook 脚本依赖自包含 |
| F5 | **多 agent 分派 V2 + worktree 多任务** | C（P1） | Symphony 27k stars 验证同构赛道；Forge 台账持久化/人机认领/依赖门禁三点领先 | issue-tracker 双向镜像；always-on 治理包（成本熔断/停滞检测/input-required 人审点） |
| F6 | **forge eval 双轨** | E（P1） | 1.50 刚落地，方法论与 2026 前沿同向 | harness 披露清单化；judge-audit 升级 MVVP 四项协议；报告纳入证据包 |
| F7 | **宿主适配层**（hostcap/agentbridge/harnessdetect/project policy） | 战略底座 | 12 宿主是"模型中立"卖点（Anthropic 封锁第三方 harness 后的差异化） | 国产宿主（Kimi/CodeBuddy/ZCode）深度适配（NVDB 后替代潮靶心）；季度对照平台路线图审计留白清单 |
| F8 | **Pulse 看板 + conventions 档案** | D/A 观测面 | dashboard eval 事件 1.50 已接；conventions 是 AGENTS.md 生态缺的验证层 | conventions 指纹结论写回/校验 AGENTS.md；Pulse 消费 OTel 导出 |

## 二、处置清单

### 2.1 拆包（移出核心分发 → 独立 skill pack，不删除）【已执行 2026-09-05：12 个 skill 已 `git mv` 至 `plugins/forge-design/skills/`（含 plugin 壳与 marketplace 条目），核心 `skills/` 缩至通用集】

前端/设计族 **12 个 skill**：frontend-feature-development、frontend-stack-selection、frontend-aesthetics-execution、frontend-code-review、ai-generated-ui-review、ai-ui-generation-workflow、design-system-workflow、design-system-migration、design-review-snapshot、design-artifact-standards、design-audit、ui-iteration-feedback-loop。（均已迁移 ↑）

- 理由：① dogfooding **零触发**（Go CLI 项目天然不命中，但同理——核心包不应为少数场景常驻 12 个 skill 的路由负担）；② 价值审计证实族内重复严重（frontend-code-review ↔ ai-generated-ui-review 重复 3.5/6 维度、动效选型表两处、token 铁律 4 处）；③ 与"证据与签核层"定位无关；④ 调研 I10：Agent Skills 开放标准 + marketplace 是分发主通道——拆包恰好让设计族按官方规范流通（frontmatter 对齐 + forge.* metadata）。
- 处置：`forge plugin pack` 机制已具备 → 建 `forge-design-pack`（或独立仓库）；core `skills/` 只留通用验证/流程/编排/检索；plugin 分发两棵树的机制不变。prototype-confirmation 视拆包后引用情况决定去留（其纪律部分通用）。

### 2.2 瘦身/合并（内容级砍除，skill 名保留或并入）

| 对象 | 审计判决 | 聚焦语境处置 |
|---|---|---|
| maintainability-and-readability（298 行） | 合并：唯一真增量 §2.7 双语注释规范 | 【已合并 2026-08-09（commit f09e524）：§2.7 已入 code-review-gate/references/review-checklist.md「注释规范」节，目录已删】执行合并：§2.7 并入 code-review-gate，其余废弃 |
| system-architecture（审计口径 245 行）/ backend-development（审计口径 203 行）/ resilience-and-observability（审计口径 274 行；执行时点 275） | 改进（大幅瘦身）：70%+ 教科书对强模型零增量 | 【已瘦身 2026-09-05：245→128 / 203→168（前次已瘦身大半）/ 274→137 行】瘦到"决策点+反模式+Gotchas"密度（各 ~100-120 行），或降级为 code-review-gate 的 references |
| secure-coding（审计口径 277 行；执行时点 307） | 改进：基线停在 OWASP 2021、零 agent 时代威胁 | 【已重写 2026-09-05：307→150 行，OWASP 2025 变化要点 + Agent/LLM 威胁节（prompt injection/MCP tool poisoning/skill 供应链，Snyk ToxicSkills 3,984 扫描 1,467 缺陷/76 恶意）+ 压缩清单】重写时**只保留** agent/LLM 威胁节（prompt injection/MCP/skill 供应链）——这才是"模型已内化知识"之外的真增量，且与方向 B 同频 |
| design-system-migration / integration-test-architecture / reverse-engineering-patterns | 单一项目实录 60-90% / 承诺与交付落差 | 前者【已随设计族拆包至 forge-design】；后两者案例下沉 references、正文压缩（未在本批执行） |

### 2.3 冻结（维护模式：修 bug 不加功能，文档标注实验性）

| 对象 | 理由 |
|---|---|
| `forge clone check`（token 级 Jaccard 重复检测，internal/clone） | checklog 无使用痕迹；与方向 B 的 unused-scan/cheat-scan 职责部分重叠。先冻结一个版本周期观察，无外部使用反馈则移除【已执行移除 2026-09-06 死代码清扫：冻结期无使用反馈（本机 checklog 零痕迹、npm 未发含冻结标注的版本），clone check 与 internal/clone 整体删除】 |
| `forge suggest` | 使用证据缺失；与 forge next（单命令引导）职责重叠——next 是 vNext 设计钦定的入口，suggest 是前代产物。冻结并评估并入 next【已执行移除 2026-09-06：decline/reset 与 forge off/on 完全重复、status 零使用；marker 助手保留（off/on 双写垫片消费）】 |
| skills 治理命令长尾中的 `skills analyze` / `skills mine` | 与 usage/effectiveness 功能邻接、使用证据弱；冻结评估合并【已执行移除 2026-09-06】 |

### 2.4 机制级砍除（删约定不改代码的"死机制"）

| 对象 | 理由与处置 |
|---|---|
| session-continuity 的 session-history.jsonl | 审计原话："自述无任何 forge 命令读写它"——无工具强制的约定必腐。二选一（审计建议）：工具化为 taskcontext 自动落盘（与方向 C 的 handoff 兼容协同），或删除该段【已执行：该段已删（2026-08 前置提交），SKILL.md 以"结构化任务跟踪优先 + HANDOFF.md 兜底"表述，forge 侧集成笔记承载 forge task resume/context 真相源】 |
| cross-tool-context 的 AI_CONTEXT.md 主线 | SKILL.md 自述已被 forge task 架空为"次等导出视图"。重构定位为"forge task 双向锚定"为主（对齐方向 C 的 VS Code export/社区 HANDOFF.md 兼容），AI_CONTEXT.md 降为无 forge 时的降级方案【已执行 2026-09-05：主线改为"结构化任务系统双向锚定"，AI_CONTEXT.md 压缩为降级一节】 |
| on-demand-guards 的 prompt 型 /freeze | 审计判定"机制先天不可靠（长会话/压缩后必漂移）"。处置：/freeze 实现为 forge 真 hook（freeze-guard 已存在——打通激活 UX 即可），skill 退化为激活/解除说明层【已执行：/freeze 段改为主路径=freeze 真 hook（PreToolUse 执法）、STOP 纪律为降级路径；forge freeze 命令见 forge 侧集成笔记】 |

### 2.5 明确不砍（防误伤，提前记录理由）

- **task 命令族 40+ 子命令**：状态机闭环件（mine/reclaim/reopen/cancel/question/answer 都是分派 V2 语义），且是方向 C 的底座。命令数量大 ≠ 冗余——普查确认 cli 架构债已清偿。
- **eval 命令族 15 个**（1.50 刚建）：方向 E 的全部载体。
- **多机同步族**（registry/project sync/node/trust/harness repo）：方向 A 的 git 通道与多机证据归档复用这套传输。
- **skills 互斥组/回归电池命令**（mutex-*/battery/eval-cases）：I10 的上游提案素材——Agent Skills 规范里没有对应物，是差异化。
- **hazard/freeze/act/review 族**：hazard-guard 实际 blocked 22 次；act 是 PDCA 反馈臂。
- **22 个 hook**：无一砍——重定位而非裁撤（F4）。
- **doclint/docs lint + doc-review**：输出可读性门禁，9 次使用 + doc-gate blocked 3 次，且是企业版证据包组成部分。

## 三、执行批次（与调研 P0 排序咬合）

1. **第一批（并行两条线）**：
   - 线 1（P0 功能）：D1 OTel exporter → B 的监控分段与自报一致性门禁（纯判定逻辑，checklog 输入现成）→ A 的 git/PR 收口（架构迁移）。
   - 线 2（聚焦清理）：设计族拆包 + 2.2 瘦身批 + 2.4 死机制处理——这批不改 Go 核心，主要是 skills/ 内容与 plugin pack，风险低、立减路由负担。
2. **第二批**：B 的 held-out gap 门禁（需测试派生设计）+ safe-halt；C 的 issue-tracker 镜像与 always-on 治理包；F6 eval 升级。
3. **第三季度级**：F 商业化（open core 付费墙）按调研方向 F 单独立项。

## 四、证据口径说明

- 使用数据来源：本机 dogfooding（E:\Forge 项目 DataDir，2026-08-23 起的 checklog），单人单项目——对"核心项目工作流"代表性好，对前端/多宿主场景无代表性，故前端族处置主要依据审计+方向而非零触发本身。
- 砍/冻结判定未获得 npm 侧用户数据佐证（无下载量与 issue 反馈渠道数据）；冻结类处置设计为可逆（先冻结后移除），拆包类不删除任何内容。
- 本文档为决策记录；执行时按批次另开任务（task start）走门禁。
