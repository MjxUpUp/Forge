# 版本化改写（Versioned Rewrite）— 补充调研的正确流程

## 目录

- [为什么要版本化（规则约束为何失效）](#为什么要版本化规则约束为何失效)
- [版本化改写流程（Phase 5 完整执行）](#版本化改写流程phase-5-完整执行)
- [补充内容映射表（{run_dir}/merge_plan.md）](#补充内容映射表run_dirmerge_planmd)
- [发布前自检（{run_dir}/rewrite_check.md）](#发布前自检run_dirrewrite_checkmd)
- [v{N+1} — {TS}](#vn1-ts)
- [回滚机制](#回滚机制)
- [版本目录结构（一次完整补充调研后）](#版本目录结构一次完整补充调研后)
- [与非版本化流程的对比](#与非版本化流程的对比)
- [Gotchas（高信号）](#gotchas高信号)

补充调研内容到已有报告时，**禁止 append 追加新章节**（会产生"9.总结，10.补充调研"的割裂结构）。
本文件规定版本化改写机制：原文档快照 → 基于原文档+补充内容改写 → 发布新版 → 版本归档。

主文件 SKILL.md 的 Phase 5 给主控要点，执行补充融入时**必须读本文件**。

---

## 为什么要版本化（规则约束为何失效）

`failure-cases.md` 案例 1/3 记录的真实失败：Phase 5 规则写了"补充内容拆散融入现有章节"，
但执行时模型反复走 append 捷径，连修 4 轮才纠正。

**根因**：靠"模型自觉遵守文档规则"不可靠。append 比"fetch 原文→重写整篇"省事，
模型有持续走捷径的倾向。

**解法**：把约束从"文档规则"升级为"流程强制机制"——版本化改写流程本身就不允许 append，
每一步都有产物，出错可回溯到上一版本。

---

## 版本化改写流程（Phase 5 完整执行）

### 步骤 1：快照原文档（v_current）

**先 fetch 当前飞书文档完整内容**（禁止凭记忆）：

```bash
cd {run_dir}
export PYTHONIOENCODING=utf-8
lark-cli docs +fetch --api-version v2 --doc {obj_token} --doc-format markdown > current.md
```

校验 fetch 成功：
- `current.md` 非空且 >100 字符（防反爬空页）
- 包含已知章节标题（确认是目标文档）

**存为版本快照**：

```bash
mkdir -p {run_dir}/versions
TS=$(date '+%Y%m%d-%H%M')
cp current.md {run_dir}/versions/v{N}_${TS}_pre-merge.md
# N = 当前版本号，从 1 开始递增
```

版本快照命名：`v{版本号}_{时间戳}_{阶段}.md`
- `_pre-merge`：补充内容融入前（原文档快照，回滚锚点）
- `_post-merge`：融入后改写版（发布版备份）

### 步骤 2：补充内容收集与映射

本步骤基于 Phase 1 增量调研产出的 `{run_dir}/dive_NN.md`（gap 维度深挖）。

**逐条映射补充内容到现有章节**（这是关键，禁止新建章节）：

```markdown
## 补充内容映射表（{run_dir}/merge_plan.md）

| 补充知识点 | 来源 | 融入目标章节 | 融入位置 |
|---|---|---|---|
| 国内市场数据 2025 | dive_03.md | 一、市场概况 | 第一节末尾追加段落 |
| 新产品 X 分析 | dive_03.md | 二、产品对比 | 表格新增一行 |
| 新争议点 | dive_04.md | 四、争议与挑战 | 新增子小节 4.x |
| ... | ... | ... | ... |

无法映射的知识点 → 新建小节，**插在"结论"章节之前**（结论永远最后一章）。
```

**映射纪律**：
1. 读现有文档完整 outline（从 `current.md`）
2. 每个补充知识点找归属章节
3. 能融入的标记融入位置（段落/表格/子小节）
4. 无法融入的才新建小节——插在结论前
5. **结论章节本身要更新**（吸收新发现，重写结论段）

### 步骤 3：基于原文档改写（不 append，重写整篇）

在 `current.md` 基础上，按映射表逐处修改，生成 `report.md`：

```bash
# 改写后的完整文档（含补充内容融入现有章节）
# 写到 {run_dir}/report.md
```

**改写规则**：
- 以 `current.md` 为底稿，不是从零写
- 按映射表逐个融入：目标章节 + 补充内容，保持原有行文风格
- 章节顺序不变（除非补充内容需要重排逻辑，但结论永远最后）
- 格式与原文一致（原文 markdown 就 markdown，XML 富格式就 XML——从 fetch 结果判断）
- 结论段重写，吸收新发现

**禁止的反模式**：
- ❌ 在结论后追加"补充调研"章节
- ❌ 凭记忆改写（必须基于 fetch 到的 current.md）
- ❌ 格式不一致（fetch 是 XML 却用 markdown 写补充）

### 步骤 4：发布前自检（Gate）

发布前必须过自检，否则回到步骤 3：

```markdown
## 发布前自检（{run_dir}/rewrite_check.md）

- [ ] report.md 章节顺序与 current.md 一致（无结论后追加新章）
- [ ] 结论是最后一章，且已吸收补充内容更新
- [ ] 补充内容已拆散融入对应章节（无独立的"补充"章节）
- [ ] 格式与原文一致（markdown/XML 统一）
- [ ] 无原有内容被删除或篡改（对比 current.md）
- [ ] 无凭记忆拼凑的措辞（基于 current.md 实际内容）
```

任一未过 → 回步骤 3 修正，不发布。

### 步骤 5：发布新版（overwrite）

```bash
cd {run_dir}
export PYTHONIOENCODING=utf-8
lark-cli docs +update --api-version v2 --doc {obj_token} \
  --command overwrite --doc-format markdown --content "@report.md"
```

**overwrite 前确认**：
- `report.md` 已过步骤 4 自检
- `obj_token` 与原文档一致（不是新建）
- 有 `v{N}_pre-merge` 快照可回滚（步骤 1 的保险）

### 步骤 6：归档新版快照 + 增量记录

```bash
TS=$(date '+%Y%m%d-%H%M')
cp report.md {run_dir}/versions/v$((N+1))_${TS}_post-merge.md
```

在 `{run_dir}/CHANGELOG.md` 追加版本记录（这是唯一允许 append 的地方——changelog 本就该追加）：

```markdown
## v{N+1} — {TS}

补充内容：{一句话本次补充的主题}
来源：{run_dir}/dive_NN.md
改动：
- 融入章节：一、二、四（详见 merge_plan.md）
- 新增小节：4.x {小节名}（插在结论前）
- 结论已更新
快照：versions/v{N}_pre-merge.md（回滚锚点）、v{N+1}_post-merge.md（本版）
```

---

## 回滚机制

发布后发现改写出错（内容丢失/格式崩/结构错）：

```bash
# 回滚到 pre-merge 快照
cd {run_dir}
lark-cli docs +update --api-version v2 --doc {obj_token} \
  --command overwrite --doc-format markdown \
  --content "@versions/v{N}_{TS}_pre-merge.md"
```

回滚后记录原因到 `CHANGELOG.md`，修正 `report.md` 重新走步骤 4-6。

---

## 版本目录结构（一次完整补充调研后）

```
{run_dir}/
├── versions/
│   ├── v1_20260601-1000_post-merge.md      # 初版报告
│   ├── v2_20260615-1400_pre-merge.md       # 第一次补充前快照
│   ├── v2_20260615-1430_post-merge.md      # 第一次补充后
│   ├── v3_20260619-0900_pre-merge.md       # 第二次补充前快照
│   └── v3_20260619-0930_post-merge.md      # 第二次补充后（当前线上版）
├── current.md                               # 本次 fetch 的线上版
├── merge_plan.md                            # 本次补充映射表
├── rewrite_check.md                         # 本次自检结果
├── report.md                                # 本次改写后版本
├── CHANGELOG.md                             # 版本变更日志
└── dive_NN.md                               # 本次增量调研素材
```

---

## 与非版本化流程的对比

| 维度 | 旧流程（append） | 版本化改写 |
|---|---|---|
| 补充内容位置 | 结论后追加新章（割裂） | 拆散融入现有章节（连贯） |
| 出错恢复 | 无法回滚，需手动修复 | 回滚到 pre-merge 快照 |
| 可追溯性 | 无 | CHANGELOG + 版本快照 |
| 格式一致性 | 易不一致（凭记忆） | 基于 fetch 的 current.md |
| 可靠性 | 靠模型自觉（常失效） | 流程强制（每步有产物） |

---

## Gotchas（高信号）

- **pre-merge 快照是保险**：步骤 1 必须存，发布翻车时是唯一回滚锚点。跳过 = 裸奔
- **fetch 是改写的基础**：禁止凭记忆改写。current.md 是唯一可信底稿，记忆 ≠ 文档实际措辞
- **结论永远最后**：即便补充内容逻辑上属于"总结性"，也重写结论段而非在结论后加章
- **格式从 fetch 判断**：current.md 是 markdown 就 markdown，是 XML 就 XML。不凭记忆猜格式
- **CHANGELOG 是唯一可 append 的**：其他地方 append 都是错。CHANGELOG 本就该按时间追加
- **自检不过不发布**：步骤 4 的 Gate 是硬门控，任一项未过回步骤 3，不要带病发布
- **obj_token 不变**：补充是更新同一文档，不是新建。obj_token 必须与原文档一致
