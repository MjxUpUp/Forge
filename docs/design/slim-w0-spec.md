# W0 Spec：hook 瘦身（W0.3 档位 + W0.2 耗时预算门 + W0.1 死检查报告）

## 问题（当前断点）

用户头号痛点：hook 太多太重。现状 8 事件/11 matcher/33 条目——一次 Bash 工具
调用要拉起 4 个 forge 进程（PreToolUse/Bash 组），SessionStart 一次拉 6 个；
每个 spawn 都是 Go 启动 + 部分 bash 子进程。同时缺两样治理工具：
1. 减法没有数据面——哪些检查「触发多、拦截零」无报告可查；
2. 重量没有预算门——hook 组耗时无 CI 基线，慢性膨胀不可见。

## 方案（三件套）

### W0.3 接线档位（lite / standard / full）
- `hooks.Profile` + `ForgeHookSpecForProfile`：lite = 拦截 + 管线 + 状态机
  最小集（白名单 12 hook；advisory 增强/触发通道/会话引导/集成扫描一律不入；
  任务循环必需的 task-verify/tool-track 等刻意保留——偏离原「仅 guard+sentinel」
  草案是为保管线完整，已在类型注释披露）；standard = 完整现行名册（默认档，
  行为不变）；full = 预留超集位。
- 两层生效：接线过滤（GenerateUserSettings + 6 个 translator + windsurf 出口
  过滤按 ActiveProfile 裁剪）+ 运行时门（runHook 对白名单外 hook 走宿主 allow
  通道静默秒过，零 bash 拉起——覆盖插件渠道用户）。
- 持久化 ~/.forge/profile；`forge init --profile <p>` 写入；FORGE_PROFILE env
  显式覆盖。默认 standard 保持现网行为不变；默认切 lite 需 W0.1 数据 + W2
  摩擦证据（已登记触发条件）。
- CLI 门禁不受影响：taskpipeline executor 有自己的 runEmbeddedHook。

### W0.2 耗时预算硬门（分派重构之外的可独立交付半场）
- `TestPreToolUseBash_LatencyBudget`：PreToolUse/Bash 全组（bash-guard 快照 +
  hazard-guard 语义分词 + gate-cmd-form + skill-trigger）放行路径端到端 10 次，
  mean ≤ 2s / 单次 ≤ 5s 预算（本地实测 mean 102ms / max 246ms——预算宽于
  CI 噪音、远紧于宿主 60s）。超标即 CI 红——「慢性变重」从此可见。
- 每-事件单入口 batch 分派（claude additionalContext 合并语义 + 镜像测试批量
  迁移 + compat 重钉）登记为后续任务（触发条件/复验节律已入任务 next）。

### W0.1 死检查报告（`forge eval dead-checks [--window 90d] [--json]`）
- 聚合 checklog 台账（LoadAllAll）+ hazard 事件台账，按检查分三态：
  live（有拦截，决定性证据）/ dead-candidate（观察 ≥30 且拦截为零 → 减法
  候选）/ insufficient-data（观察不足，绝不把无数据包装成「已证死」）。
- 诚实边界在输出中明示：只覆盖写 checklog 的检查面；嵌入式 hook 的 PASS 不
  逐次落账是刻意设计，hook 类触发率为下界。处置须人工归因复核 + census/ADR
  留痕，下线可配置级恢复。

## 已知边界

- lite 的会话内省的是 spawn 次数（接线过滤）与检查开销（运行时门）。插件渠道
  分两类：claude-code plugin payload（pluginpack）与 codebuddy marketplace 载荷
  不可删改，只能靠运行时门（spawn 仍在，检查开销归零）；**copilot 插件清单由
  forge 生成（copilot_hooks.go），属可过滤的反例**——已按档过滤。
- windsurf 名册硬编码，档位过滤在其 builder 出口单独实现（已在代码注明）。
- 死检查报告对未落账 hook 不出具「已证死」结论（设计如此）。

## 验收（可测判据）

- 档位：profile_test 4 函数（三档规模 lite 6 事件/13 条目、standard/full 与
  名册等价、白名单准入、优先级 env>文件>默认）+ runHook 档位门 2 测试（裁剪
  hook 静默秒过 / 在档 hook 照常）+ 镜像测试封闭性（FORGE_PROFILE=standard）。
- 耗时预算：TestPreToolUseBash_LatencyBudget（CI 硬门，实测 mean 102ms）。
- 死检查：parseDeadCheckWindow（非法 fail-closed）+ classifyDeadCheck（三态，
  有拦截即 live）单测 + 命令注册经 docs-consistency 验证。
- 全仓：build/vet/gofmt/staticcheck/deadcode 零故障；compat 快照重钉仅含
  刻意增量（dead-checks 命令、init --profile flag、新文件计数）。
