# Forge 兼容承诺表（compat commitments）

状态：生效中（自首个含本文档的发版起；证据链：机制史调研 ~/.forge/research/forge-mechanisms-20260905-1752/report.md §六/§八，K8s deprecation policy / PEP 387 / GODEBUG 形态先例）。

本文是 forge 对外的兼容契约——三向 × 三档承诺矩阵 + 门禁文案契约。机械执法点：`forge compat snapshot/report`（六面盘点，任何面变更显式过 golden）；`TestNpmPlatformVersionsAligned`（分发对齐）；导入侧版本偏移警示（`sync-version-skew`）。

## 一、三向 × 三档承诺矩阵

| 方向 \ 档位 | 强承诺（破坏即 bug） | 弱承诺（尽力 + 行为变更公告） | 不承诺（显式排除） |
|---|---|---|---|
| **对下：数据面**（checklog.jsonl / toollog / TaskState / heldout 侧车） | 已落盘数据永远可读：append-only + legacy 字段兜底 + 惰性重推导；**序列化键只增不删不改名**（compat golden 守卫） | 语义推导规则可随版本演进（同一行数据在不同版本读出的 Strength 可变——审计视角的已知属性，见调研 dive_06 T3） | 历史行的显示排版/未文档化内部字段 |
| **对下：命令面**（CLI 命令与 flag） | 命令与 flag **只增不删不改名**（compat golden 守卫；删除/改名须走下节的预告流程） | flag 语义收紧（advisory→blocked）按文案契约带预告版本 | 输出文本排版（porcelain 可变，Git 惯例）；退出码契约：0=过 / 2=BLOCKED / 1=故障 |
| **对侧：多机同步**（forge-sync bundle） | 导入幂等 + 原样保留（AppendEntries 不重签）；bundle manifest 携带 forge_version | 前向偏移（新 bundle 入旧节点）警告不硬拒（K8s 偏移窗口语义；旧节点 re-export 裁剪较新字段的风险已显性化） | 跨版本字段透传（类型化反序列化会丢弃未知键——建议升级本机 forge 后再转发放） |
| **对上：宿主面**（12 宿主接线） | 未知 hook 事件/未知 stdin 字段不崩不脏（宽容读）；接线幂等（重装不毁用户已有 hooks）；单宿主方言错误不污染他宿主归因 | 新宿主能力机会式跟进（按发版节奏） | 跟随宿主 experimental 特性的稳定性；宿主自身回归（如 v2.0.31 hooks 全失效） |
| **分发面**（npm） | wrapper 与平台子包版本逐版本互锁（CI 守卫 TestNpmPlatformVersionsAligned） | release-please 驱动 semver | — |

## 二、门禁文案契约（advisory → blocked 的预告纪律）

依据 PEP 387（"警告文本必须写明预计生效版本+反馈渠道"是 Python 的硬要求）与 K8s 承诺矩阵形态：

1. **新增 BLOCKED 门禁的拒绝文案必须包含其一**：①预告版本（"自 vX.Y 起 advisory 转 blocked"——若该门禁曾以 advisory 形态发布过）；②"首发即 blocked"声明 + 指向本文档（若门禁与含它的版本同首发，无存量暴露则无预告义务）。
2. **时间承诺**：已发布的 advisory 门禁转 blocked，**预告期 ≥2 个 minor 版本**（PEP 387 口径；期间文案持续显示预告版本）。
3. **逃生舱纪律**：每个 BLOCKED 门禁必须有 `FORGE_*` env 或 per-task override 逃生舱，逃生留 checklog 痕 + evidence 封顶 Weak；豁免行携带 reason/owner 元数据（v1 简化：reason 为机制枚举值——`per-task override` / `env`——尚无自由文本 `--reason` flag，owner 在 env 逃生时记 "env" 而非委托任务；升级为设计文档 P1-2 的后续项）。
4. **机械执法**：`forge compat report` 的 blocking-sites 面使新增 BLOCKED 位点在 golden diff 里显式可见——新增须同时更新本文档或 CHANGELOG 行为变更节，否则 report 以 warn 提示。

## 三、生效口径与存量处理

- 本契约自首个含本文档的版本生效。本批新增门禁（held-out / self-report / gate push 等）与契约同版本首发——按第 2 节②无预告义务；1.50.0 及更早的已发布版本不含它们，无存量用户暴露。
- 既有逃生舱（FORGE_TEST_COVERAGE / FORGE_WORK_ACTIVITY / FORGE_ACCEPTANCE_GATE / FORGE_DOC_GATE / FORGE_SKILL_DECISIONS / FORGE_SELF_REPORT / FORGE_HELDOUT / FORGE_GATE_PUSH）：**不设 TTL**（env 传入式 = 不传即失效），但 dashboard 逃生舱库存提供永久化与 unfulfilled 候选信号（复查而非过期——调研反框架律：衰减靠复查不靠硬过期）。

### 存量处理裁决记录

- **2026-09-06 死代码清扫批**：移除 4 个命令（clone check / suggest 族 / skills analyze、mine）未走 ≥2 minor 预告。可辩护依据：① 零使用证据（本机 checklog 全史无痕迹 + npm 从未发布含冻结标注的版本——冻结声明从未出海，无存量用户接触）；② 决策文档 feature-focus §2.3 冻结决议留痕在先；③ CHANGELOG 行为变更节 + 迁移指引齐备。**援引边界**：此裁决不可作为常规命令删除的先例——若无同等零使用证据，命令删除必须走预告流程。）

## 四、刻意不做（记录裁决，防重新发明）

- **事前审批门禁**：DORA 实证外部审批与交付绩效负相关——用事后审计+自动化复查承接。
- **基线文件式棘轮**（冻结存量违规）：forge 规模走 Google"先清零"路线（新门禁默认只对新任务生效）。
- **GODEBUG 式按版本给默认行为的完整机制**：等 BLOCKED 门禁数量增长后再评估（当前 8 个逃生舱、管理成本不成比例）。
