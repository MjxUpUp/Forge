# 插件哲学：扩展面的取舍（2026-09）

- 依据：Cordis/dsh 深度调研（`~/.forge/research/cordis-koishi-plugin-philosophy-20260916-1224/report.md`，2026-09-16；7 维度并行深挖，关键断言 ≥2 独立源或一手 registry/API 互证）+ Forge 本地架构考古（同目录 `dive_07.md`）。
- 范围：把调研裁断出的三个判断固化为团队共识——**不学"一切皆插件"**（执法协议需静态可审计）；**采纳** Cordis 的 disposer 契约与 seam 三分法；为每个判断挂上重估判据（deferred 三要素，见 `evolution-discipline-norms.md` ②）。本文是共识与决策留痕，不携带实现；落地方案在关联文档。
- 关联落地：`hostcap-behavior-registry.md`（行为注册表收尾）、`compat-bridge-face.md`（compat 第七面）、`forge-dsh-provider-roadmap.md`(forge-dsh 生态位)。

---

## 一、立场声明

Forge 的扩展面是**执法协议**，不是功能市场。因此：

> **扩展面少而统一、静态可枚举、每一处变更必须进 diff 被审阅。**

这句立场与 dsh 的 "Everything is a Plugin" 并不矛盾，而是同一枚硬币的两面：dsh 的前提是"作者可以是 AI、出错可即刻卸载、宿主进程可以为实验让路"；执法协议的对称前提是"**每个阻断位点都曾被人类审阅过，且缺席会响亮失败**"。两个前提各自自洽，互相不可移植——这正是本文拒绝/采纳清单的全部依据。

## 二、为什么不学"一切皆插件"

三条独立论据（任一单独成立即可否决，实际三条同时成立）：

1. **语义相反**。Cordis 的服务模型是 fail-silent 惰性激活："直到此服务的值变为 truthy 为止，该插件的函数体不会被加载"、服务缺失让插件静默 pending、"ctx 上服务凭空出现"（cordis.moe《服务与依赖》；iceyao 源码解读）。门禁需要的是 **fail-visible**：检查缺失、遥测缺失、依赖缺失都必须响亮（现有先例：遥测缺失时硬门禁降级 advisory 并落 `telemetry-missing` 审计行）。把 DI 语义搬进门禁 = 把错误推迟到最不可见的时刻。
2. **审计性是快照执法的前提**。compat 七面快照的执法力来自"位点集合有限且静态"——`forge compat report` 能把"新增一个 BLOCKED 位点"变成 PR diff 里必须被审阅的一行。若门禁可运行时动态注册，位点集合无限，快照退化为合影，棘轮失去棘齿。dsh 自己也承认这条边界：其 `capability-seams.md` 把服务显式分为 **core spine service（不可换）**、swappable capability seam、bundle composition point 三类——口号是"一切皆插件"，结构是"分层标注哪些不可插"。
3. **谱系证据一边倒**。VSCode 用"静态声明（contributes）先行 + 惰性激活分离"换来了可审计的扩展面；OSGi 的 classloader 地狱、K8s CRD 的 YAML 泥潭、"工具越多 Agent 反而越弱"（dsh 社区对第一版的核心批评）、openEuler 的"先单体，复杂了再拆"——全部指向同一结论：暴露给调用方的扩展面必须少而统一。

### 裁断表："一切皆插件"的子主张逐条裁决

| 子主张 | 裁决 | 理由 / Forge 对应物 |
|---|---|---|
| 一切领域能力做成可卸载组件 | **拒**（内核侧） | 3 道门禁 / 23 hook / 检查规则是执法协议本体，静态可枚举是 compat 棘轮的前提 |
| 每个注册持有逆操作（revertible effects） | **采** | 见第三节 disposer 契约 |
| 服务依赖声明、生命周期由依赖表达 | **部分采** | gate 前置链已是 HARD stop（executor_check_common.go:43-51）；不做运行时 truthy 等待 |
| 显式标注不可换层（core spine） | **采** | 见第四节 seam 三分法 |
| 运行时热挂载新组件 | **拒**（对执法面）/ **采**（对数据面） | schema.yaml 档位、skill triggers 数据声明保持用户可配；hook/gate 不开放运行时注册 |
| 配置层自由组合、无需改源码 | **已采** | escape hatch 三层（env / per-task override / profile）+ schema.yaml，维持 |

## 三、采纳①：disposer 契约（可逆副作用）

**Cordis 的依据与教训都要抄。** 依据：官方实践规则原话"每个注册都应有对应的 disposer……reload 和 teardown 时会按预期撤销"；教训：上游 fiber.ts 曾有三个重入 dispose 缺口，dsh vendor 后自己打补丁堵上（vendor/README 18 条修改台账）——**契约本身不会自动正确，必须配守卫测试**。

**Forge 化定义**：注册即登记逆操作，卸载时逆序回收。范围（五类对象，按现状缺口排序）：

| 对象 | 现状 | 缺口 |
|---|---|---|
| 临时文件/工作区 | 部分有（quarantine、轮转） | 无统一登记面 |
| hook 接线（用户级 settings/plugin manifest 写入） | init 写入有 | **卸载/重接线无对账**（init 默认零项目写入方向的对称面） |
| 降级状态（fail-open 之后的恢复路径） | 落审计行 | 恢复动作无逆操作语义 |
| artifact/评分登记 | append-only（方向正确） | 作废路径已有（漂移审批），需纳入同一词汇 |
| watcher/后台进程 | watchdog、reclaim | 与本文词汇未对齐 |

**形态裁断**：不引入容器。Go 侧最小契约 `type Disposer func() error` + 登记表（谁注册、label、逆序执行）+ 统一消费入口（`forge off` / uninstall 路径）。**验收标准（适应度函数）**：`forge init && forge off` 循环后机器零残留（对照 `forge init` 零项目写入承诺）；契约完整性由守卫测试钉住（每个 Register 必须能追溯到 disposer——漏配即红，同 fiber.ts 教训）。

**Deferred 三要素**：若"五类对象之外的注册形态"出现 ≥2 类，或出现跨进程副作用（远端登记），重估是否升格为框架级 lifecycle——触发条件+复验日期挂在落地 PR 的本文回填节。

## 四、采纳②：seam 三分法

把 dsh `capability-seams.md` 的三分类翻译为 Forge 词汇，并**对现状做一次显式标注**（本表即首个产出；新能力提案必须先回答"属于哪一类"）：

| 分类 | 定义 | Forge 现状标注 |
|---|---|---|
| **core spine**（不可换） | 执法协议本体；变更走 advisory→BLOCKED 升档承诺（mechanism-hardening P1-3） | 3 道门禁、23 hook 名册、cheat-scan 7 模式、doclint D1-D8、21 条 skill 安全规则、CheckName/逃生舱 roster |
| **swappable seam**（可换实现） | 接口稳定、实现可替换/可扩展 | hostcap 能力注册表（数据）、hookdispatch 的 stdinNormalizers / 输出 emitter（行为列）、agentbridge Translator（每宿主一个文件）、persistence（jsonl/sqlite 类） |
| **composition point**（数据声明组合） | 用户/配置在运行时组合，无需改内核 | schema.yaml 产物链档位、skill frontmatter triggers（半开：命名 condition 词汇表在内核）、conventions profile、per-task override、profile 三档 |
| **外部桥**（新增第四格，Forge 特有） | 内核外进程内接线层，双端契约 | plugins/forge-dsh（Cordis 包装层）、plugins/forge 宿主接线包 |

配套规则三条：(1) spine 变更必须出现在 compat 快照 diff（已执行）；(2) seam 变更收敛为"注册表加行 + 守卫测试钉完整性"（见 `hostcap-behavior-registry.md`）；(3) **"plugin"一词的语义辨析写进 README**（待办——主 README 尚无此文本，随 README 下一次结构性修订补入；触发条件：任一外部贡献者混淆两种 plugin 语义）——Forge 的 plugin=宿主接线打包（分发层），dsh/Cordis 的 plugin=运行时可装卸组件（运行时层），同名不同物，forge-dsh 恰好横跨两者（是前者的打包、后者的消费者）。

## 五、明确不采纳清单

1. **运行时插件加载器**——编译期单体是特性：兼容承诺（数据只增不删、文案契约、升档预告）的强度恰恰来自中央化；松散插件生态给不出同强度承诺（考古 Tension 1 的反方论证，成立）。
2. **进程内事件总线**——与"一切证据先落盘、进程随时死"的 durability 直觉相斥。读侧对 CheckName 字符串的解析若痛感上升，正解是"总线=append-only log 的**类型化读面**"（schema 化消费），不是内存 pub/sub。
3. **DI 容器 / 惰性激活**——论据见第二节第 1 条。
4. **"一切皆 gate"**——暴露面少而统一；新检查先回答"能否并进既有检查段"（lazy ladder）。

## 六、重估判据（本文的 deferred 三要素）

- **触发条件①**：受支持宿主 ≥16，或出现无法经 agentbridge 声明式接入的第三方宿主形态（→ 重估 hostcap DI 化，见 `hostcap-behavior-registry.md`）。
- **触发条件②**：90 天窗内 CheckName 字符串解析导致的跨包破坏 ≥2 次（→ 重估类型化读面）。
- **触发条件③**：出现"必须运行时注册执法位点"的真实需求场景（非假设），且 fail-visible 有替代方案（→ 重估动态注册）。
- **复验日期**：2026-12-16（落地后 90 天）。**复验节律**：周度回顾（session-retrospective）扫 open deferred 项时逐条核对；`grep -rn "一切皆" docs/ skills/` 与 `forge compat report` 可机械核对。

## 七、证据与出处

- 调研主报告：`~/.forge/research/cordis-koishi-plugin-philosophy-20260916-1224/report.md`（含 72 条全局编号引用）；维度原始证据同目录 dive_01-07.md，信度分级 verify.md，跨维洞察 insight.md（I2/I4/I5 为本文直接来源）。
- dsh 一手：docs/architecture.md（"no privileged core to patch"、live reload 边界）、docs/capability-seams.md（spine/seam/composition 三分）、docs/cordis-primer（五概念、disposer 规则）、vendor/README.md（18 补丁台账）。
- Cordis/koishi 一手：cordis.moe《服务与依赖》（truthy 等待/回滚语义）、koishi.chat《可逆的插件系统》（"Cordis 的名字来源于拉丁语的心"、可逆性承诺）、arXiv:2608.25512（temporal/spatial composability 形式化）。
- 本地考古：dive_07.md（扩展点盘点表、硬编码清单、双轨制张力）。
