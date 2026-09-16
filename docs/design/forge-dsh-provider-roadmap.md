# forge-dsh 生态位路线：从接线层到质检局（2026-09）

- 依据：插件哲学调研 insight I6（"AI 当插件作者，把质量门禁从外挂变成运行时必需品"）+ D5（dsh 生态成熟度与缺口实证）。
- 范围：forge-dsh 的三阶段定位路线（H1 契约加固 → H2 验证器 → H3 质检局）、dsh 生态质量事故模式 → Forge 能力的需求映射、启动判据与风险边界。
- 立场：**不与 dsh 插件生态竞争内容插件，做那个生态缺的验收与证据层。**

---

## 一、定位判断（为什么这个生态位成立）

dsh 生态四个已实证的缺口（全部一手来源，见第五节），恰好压在 Forge 的既有强项上：

| dsh 生态缺口（实证） | Forge 对应能力 |
|---|---|
| AI 现场生成的插件无质量闸门；单进程共享 context/tool registry，一个坏插件拖垮整个 harness（#1884 事故综述：inject 误写致插件永远 pending、同名注册互相覆盖、JSON Schema 不完整被拒） | 验收实跑、确定性证据链、checklog 归因 |
| "会话→工作交付物"（日报/交接文档）类插件为零（#961 作者自述） | task 工件链（intent/checklist/invariant）+ 评分 |
| 无安全审计（SAFETY.md 自述 not production-ready） | cheat-scan 机械检测 + 21 条 skill 安全规则审计（advisory 可复用为插件审计器） |
| developer preview 破坏性变更无预告 | compat 棘轮/文案契约/升档预告——正是 dsh 侧没有的纪律，可输出为方法论 |

关键非对称：dsh 插件事故的**受害者**是 dsh 用户，**在场者**是 forge-dsh 桥——forge 已经活在每个 dsh 会话的拦截点上，是唯一天然具备取证位置的第三方。

## 二、三阶段路线

### H1 契约加固（当下，随 compat 第七面落地）✅ 已落地 2026-09-16

- `contract.json` + compat 第七面 + README 双端契约标注（见 `compat-bridge-face.md`，不重复）。
- `/forge-status` 补 fail-open 可查性：ring buffer 已有，补**verdict 摘要行**（runs/blocked/fail-open/contexts 聚合，覆盖整个 50 条 ring buffer）——对照 cordis-primer 的"每个注册都应有对应的 disposer"，桥侧对应物是"每次 fail-open 都应有对应的可见面"。（checklog 侧 verdict 摘要事件仍开放：插件侧 fail-open 发生时 forge 进程往往不可达，需要下一次成功调用回带——留待 H2 的 bridge verify 一并设计。）
- 出口判据：桥行为变更 100% 被快照 diff 捕获（结构性保证，非流程保证）——已达成（contract.json 变更必然出现在 compat.report 的 bridges 面）。

### H2 验证器（启动判据见第四节；预计 1-2 月粒度）

新增 `forge bridge verify`（暂名，亦可挂 `forge plugin verify --host dsh`）——对**任意 dsh 插件包**跑两级检查：

1. **静态**：inject 声明 vs 实际 ctx 取用一致性、同名注册冲突、decision 形状合法性（对照 contract.json 的枚举）、配置 schema 完整性——直接映射 #1884 的四大事故模式。
2. **动态**：fake-dsh-runtime 行为重放——复用 forge-dsh 已有的 wiring test 架构（`test/doubles/fake-forge.mjs` 的反向：被测物从 forge 换成插件包），对插件灌入标准事件序列，断言其决策/副作用符合契约。

产出统一走 forge 证据链：检查结果落 checklog、可进评分、可作 `/forge-status` 报告输入。**这一步 forge 开始同时消费两个宿主生态的语义**（Claude 形 hook + Cordis 事件），hostcap 的 StdinDialect 模式是其架构先例。

### H3 质检局（探索，H2 被生态采用后）

dsh 插件作者的发布前流水线：`forge verify` 验收实跑 → 证据链 → 评分徽章（README 可挂）。类比位：dsh 插件生态的 test-pypi/CI。前提是 H2 的检查项在真实插件上积累出判准（golden 集思路同 evals/golden）。

## 三、风险与边界

1. **dsh preview 震荡**：contract.json 带 `dshVerified` 版本对表；bridge verify 的动态重放按 dsh 版本 pin，不追 latest。
2. **不做内核侵入**：验证器只读插件包 + 驱动 fake 运行时，不写 dsh 仓库文件、不装依赖（tool-cordis 同款纪律——"Definitions exist only in memory" 的对称面）。
3. **生态政治**：以"生态工具"身份进入 `dsh-plugin` topic，不捆绑 forge 接线作为验证前提（verify 对未装 forge 的插件照样可跑静态级）。
4. **投入上限**：H3 之前所有工作复用既有机制（checklog/evals/评分），不新造平台。

## 四、启动判据（deferred 三要素）

- **H2 启动条件**：H1 落地 + 满足任一——(a) dsh 生态出现 ≥1 起**本可被 bridge verify 静态级拦截**的真实插件事故（议题区/awesome 目录取样）；(b) 有 ≥2 个插件作者主动求验；(c) forge-dsh 自身在 dsh 版本升级中因插件侧契约漂移断过 ≥1 次（自证需求）。
- **复验日期**：2026-12-16（与 plugin-philosophy 重估同批，周度回顾扫 open 项）。
- **到期处置**：未命中 → 显式 dismiss 或续期，H1 状态即为可停留的终态（桥本身价值独立成立）。

## 五、证据与出处

- #961《从零到发布：给 DeepSeek Harness 写你的第一个插件（实战踩坑全记录）》（github.com/deepseek-ai/deepseek-harness/discussions/961，2026-08-14）：pnpm 强依赖、核心仓库拒外部 PR、"会话→工作交付物零插件"。
- #1884《AI 编写的 DSH 插件为什么容易出问题？——风险综述与防御实践》（同仓 discussions/1884，2026-08-15）：inject 误写/同名覆盖/schema 不全/缺降级四大模式及其防御实践（"不 import 任何 @deepseek-ai/，只留 node 内置模块，要什么都从 ctx 上拿"）。
- SAFETY.md（未安全审计）、README（"THERE WILL BE COMPATIBILITY-BREAKING CHANGES"）、tool-cordis README（临时动态包三重限定）。
- 调研主报告：`~/.forge/research/cordis-koishi-plugin-philosophy-20260916-1224/report.md` 第五、八章。
