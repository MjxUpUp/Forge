# compat 第七面：外部桥契约（2026-09）

- 依据：`compat-commitments.md` 六面承诺 + `plugins/forge-dsh/README.md` 的双端语义 + 插件哲学调研 insight I3（"vendor+台账 与 棘轮+快照 在哲学上撞车"）。
- 范围：给 compat 快照加第七面 `bridges`——把 forge-dsh 桥的**行为契约**（事件映射、决策形状、fail-open 规则）从"散在三处的文档+行为"升为快照执法面；同时把 README 事件映射表标注为双端契约。
- 定位：`plugin-philosophy.md` seam 表中"外部桥"格的执法落地。

---

## 一、问题：桥契约目前不在任何执法面内

forge-dsh 的桥语义现散在三处，执法强度递减：

| 载体 | 内容 | 现有执法 |
|---|---|---|
| `plugins/forge-dsh/README.md` 事件映射表 | 5 行 `DSH 事件 ↔ forge hook 事件 ↔ 决策形状` + fail-open 契约段 | 无（纯文档） |
| `plugins/forge-dsh/lib/spec.json` | 接线名册镜像 ForgeHookSpec | `TestDshPluginSpecMirrorsSpec`（agentbridge/dsh.go:17，**有**） |
| `plugins/forge-dsh/lib/index.js` + `decisions.js` | 映射与决策折叠的**行为实现** | JS 侧 wiring 测试（行为=实现自证，不锚定契约） |

后果：spec.json 被 Go 测试钉死，但**映射表与 fail-open 承诺**只活在 README 里——JS 侧行为改了、README 忘改、Go 侧快照完全无感，桥两端静默漂移。这正是快照执法面缺一格的形状。

## 二、方案

1. **新增 `plugins/forge-dsh/contract.json`（机器可读双端契约）**：

```json
{
  "bridge": "forge-dsh",
  "dshVerified": "0.1.0-rc.7",
  "events": [
    { "dsh": "tools/pre-execute",  "forge": "PreToolUse",       "decision": "deny" },
    { "dsh": "tools/post-execute", "forge": "PostToolUse",      "decision": "block" },
    { "dsh": "agent/pre-step",     "forge": "UserPromptSubmit", "decision": "reject" },
    { "dsh": "agent/session-start","forge": "SessionStart",     "decision": "inject",
      "note": "source:'compact' 兼发 PostCompact 组（DSH rc.7 无专压缩点）" },
    { "dsh": "agent/turn-stopping","forge": "Stop",             "decision": "steer" }
  ],
  "failOpen": [
    "block 只读 stdout JSON 的 decision 字段，绝不读退出码",
    "基础设施故障（forge 缺失/spawn 失败/超时/解析失败）一律 fail-open 且静默",
    "可见面仅 /forge-status（50 条 ring buffer）与 debug:true"
  ]
}
```

   工具名映射（write→Write 等）与 wiredHooks 名册**不进** contract.json：前者属协议方言（已在 hostcap/translator 语义内），后者已有 spec.json+Go 守卫，两层不合并。

2. **JS 侧锚定**：`index.test.js` 现有 wiring 断言（dispatch 顺序、短路语义、决策映射）改为**从 contract.json 驱动**生成用例——行为测试从"实现自证"变为"实现符合契约"；contract.json 变更必须显式改测试（契约变更即 diff 可见）。

3. **README 标注为双端契约**：事件映射表与 Fail-open contract 节头部加一行——"本表镜像 `contract.json`，属双端契约；改动须同步 contract.json + 两侧测试，并出现在 `forge compat report` diff 中"。

4. **Go 侧第七面**：`compat/scan.go` 读 `plugins/forge-dsh/contract.json` → `Snapshot.Bridges []BridgeContract`（`json:"bridges"`）；`BridgeContract{Bridge string, DshVerified string, Events []EventMapping, FailOpen []string}`。Diff 规则（对齐既有面语义）：

   | 变更 | Breaking |
   |---|---|
   | event mapping removed / forge 事件或 decision 形状 changed | **true**（桥任一端单独升级即断） |
   | failOpen 条目 removed / changed | **true**（执法承诺收紧方向须走文案契约同款预告） |
   | event mapping added / failOpen 新增可观测性条目 | false |
   | dshVerified 变更 | false（版本对表，非承诺面） |

5. **快照重钉**：首个含 `bridges` 面的 `compat.snapshot.json` 提交，diff 审阅一次通过（expand 步骤，纯增面，无行为变更——符合 expand-contract 纪律）。

## 三、泛化与边界

- 面名 `bridges` **不限 dsh**：判定标准 = "forge 内核之外的进程内接线层，其行为语义由本仓文档承诺"。未来若出现第二个外部桥（如宿主侧 SDK 嵌入形态），同列同执法。
- **不做**：把 contract.json 塞进 npm 包让 dsh 侧运行时消费（契约的消费者是本仓的执法面与测试，不是 dsh 运行时）；不给 contract.json 加 schema 版本字段（首个版本即 v1，compat 棘轮本身就是版本执法）。
- 适应度函数：此后任何"改了 index.js 行为但没动 contract.json"的 PR 会在 JS 测试红；"改了 contract.json"的 PR 必然出现在 `forge compat report` diff——漂移从静默变为结构性不可能。

## 四、证据与出处

- `internal/compat/compat.go:38-46`（Snapshot 六面字段）、`compat.go:208-309`（各面 Diff/Breaking 语义先例）。
- `plugins/forge-dsh/README.md:14-34`（事件映射表）、`:74-86`（fail-open 契约）、`lib/spec.json`（名册镜像）。
- `internal/agentbridge/dsh.go:17`（TestDshPluginSpecMirrorsSpec 现状）。
- 对照研究：dsh vendor/README.md 的 18 条补丁台账（"契约要有台账"的同构先例）；调研 report.md 第八节 8.1 条 3。
