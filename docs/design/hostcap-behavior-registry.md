# hostcap 行为注册表收尾：数据/行为分离的完成态（2026-09）

- 依据：`internal/hostcap/hostcap.go` 包文档自述（行为函数因 import 环留在 hookdispatch 侧）；插件哲学调研（`plugin-philosophy.md` 第二节/第四节）；宿主接线考古（research run_dir dive_07.md）。
- 范围：把"接一个新宿主要改 N 处、漏一处即归因断点"的温床清干净——数据（hostcap 注册表）与行为（hookdispatch 函数列）分离的**完整性执法**。不做 DI 容器（裁断见第四节）。
- 定位：`plugin-philosophy.md` seam 三分法中 "swappable seam" 的收尾落地。

---

## 一、现状盘点（2026-09-16 实测）

**已收编为数据（hostcap 注册表，12 宿主）**：ShellSessionEnv、StdinSessionFields、StdinDialect（+StdinReplacesParse）、ContextChannels/DefaultChannel、DroppedStdoutEvents、PromoteAdvisory、PatchToolName、InstallIndicators。字面 `agent == "<host>"` 特判的**真实现状**（落地时棘轮测试实测修正本节初稿的"仅剩空串判断"——那是一次不完整 grep 的错误断言）：生产代码存留 **2 文件 3 处** kimi 字面量（`hookdispatch/hook_emitters.go:337` advisoryPromotionDisabled 的 FORGE_KIMI_ADVISORY 作用域、`hookdispatch/hook_kimi_advisory.go:78` EmitAdvisoryRouted 的 kimi 分支、`:112` AdvisoryEmissionChannel 通道标注），全部是带文档理由的 kimi 专精件而非可泛化差异——处置为**棘轮豁免登记**（文件 × 预期命中数 + 理由，见 `hookdispatch/hook_registry_test.go` 的 noHostLiteralAllowlist），新增位点即红。

**仍以行为函数存在 hookdispatch 侧（两处）**：

| 行为列 | 位置 | 键 | 现状保证 |
|---|---|---|---|
| stdin 方言归一化 | `internal/hookdispatch/hook_normalize.go:25` `stdinNormalizers` | agent 名（windsurf/kimi/reasonix/cline） | 落地后：TestHostcapDialectRegistry 双向钉死（原仅注释） |
| 输出发射 | `internal/hookdispatch/hook_emitters.go:55` `outputEmitters` | 宿主名（6 键：kimi/codex/cursor/copilot/windsurf/cline；缺失走 `EmitAgentOutput` 的 Claude 默认） | 落地后：TestHostcapEmitterRegistry + claudeDefaultEmitters 显式默认名单（原无守卫） |

**温床的准确形状**：不是"还有字面特判"，而是**两个 map 的键集合没有数据真相源执法**——新增一个带方言的宿主时，hostcap 加一行 `StdinDialect: "foo"` 但忘写 normalizer，运行时静默走默认解析（正是 hostcap 包文档描述的那类归因断点，只是换了个位置）。

## 二、目标（适应度函数）

新增一宿主的有效 diff 收敛为：**hostcap 注册表一行（必） + translator 一文件（必） + normalizer/emitter 各一段（按需）**，且"按需"漏配在**第一道守卫测试**就红——不依赖人记得包文档注释。

## 三、方案（四步，全为测试与文档，无生产代码行为变更）

1. **`TestHostcapDialectRegistry`（hookdispatch）**：遍历 `hostcap.Hosts`——`StdinDialect != ""` ⇒ `stdinNormalizers` 必有同名键；反向——`stdinNormalizers` 的每个键必须命中某个 Host 的 StdinDialect（死键即信号）。把 hook_normalize.go 现有注释"键须与 StdinDialect 一致"从注释升为断言。
2. **`TestHostcapEmitterRegistry`（hookdispatch）**：每个 `Host.Name` 必须在输出分派表有显式条目，或该宿主显式走 Claude 兼容默认（与 ContextChannel 的默认语义对齐：未知宿主=Claude 兼容，已知宿主=必须显式声明走默认还是走专属 emitter）。两个 map 的键集合由此获得与 `Checks` roster 同级的执法。
3. **字面量棘轮 `TestNoHostLiteralGates`**：扫 `internal/` 生产代码中 `==/!= "<已知宿主名>"` 形态的字面量比较（排除：`_test.go` 与整行注释；豁免=`noHostLiteralAllowlist` 显式表——文件 × 预期命中数 + 理由；空串判断因不在词表自然不命中，hostcap 包自身无命中故无需显式豁免），新增即红。宿主名清单直接来自 `hostcap.Hosts`——注册表即词表。
4. **接新宿主清单（ONBOARDING）写进 hostcap 包文档**：hostcap 行 → translator → ForgeHookSpec 接线 + 镜像守卫（`TestPluginPack_HooksMirrorSettings` / `TestDshPluginSpecMirrorsSpec` 同族）→ normalizer/emitter（按需，第 1/2 步守卫逼出）→ README 多 agent 表 → compat 快照重钉。清单的每一行都对应一个已存在的机械执法点，清单本身不是执法、是路线图。

## 四、明确不做（裁断留痕）

**不做 Behaviors 接口注入 / per-host DI 容器。** 12 个宿主全部编译期已知、行为函数耦合 HookInput/HookOutput 协议类型且直写 stdout——为此把协议类型拖进叶子包或在 cli 建"注册-解析"两层，是 12 个已知实现下抽象交租不划算（implementation-discipline 懒惰阶梯、YAGNI 同源裁断）。"数据在 hostcap（谁需要什么行为）、行为在 hookdispatch（怎么做）、完整性由守卫测试钉住"即为该约束下的完成态。

**Deferred 三要素（何时才值得 DI 化）**：触发条件——受支持宿主 ≥16，或出现"第三方在内核外新增宿主"的真实需求（届时行为列必须可外部注册，数据/行为分离被迫升格）；复验日期 2026-12-16；节律——周度回顾扫 open 项。命中前，本设计即为终态。

## 五、证据与出处

- `internal/hostcap/hostcap.go:1-23`（包文档：设计缘由、import 环、两阶段收编自述）、`Hosts` 注册表（203-354）。
- `internal/hookdispatch/hook_normalize.go:18-33`（stdinNormalizers 与"键须一致"注释）、`hook.go:365-383`（StdinDialect 分派）。
- `internal/taskpipeline/session.go:308`（唯一存留 `agent ==` 判断，空串语义）。
- 对照研究：`~/.forge/research/cordis-koishi-plugin-philosophy-20260916-1224/report.md` 第八节 8.1 条 4（hostcap 收尾）。
