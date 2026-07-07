---
name: review-batch
description: "整项目级批量代码审查编排：拆模块并行 dispatch subagent 审查，输出去重分级的 punch-list。Use when: 审查整个项目/大范围代码时、重构后做全量验收时、说\"审查整个项目\"\"全项目 review\"\"大规模审查\"\"重构完验收\"\"分模块审查\"时。SKIP: 提交前单次 diff 审查（用 code-review-gate）。"
metadata:
  pattern: pipeline + reviewer
  domain: code-review
  composes: agent-delegation, code-review-gate
---

# 批量代码审查编排

解决"要审查整个项目，主 agent 手工拆模块+派 20 个 subagent+合并去重"的编排缺失。本 skill 是**编排层**：负责怎么拆、怎么并行、怎么汇总；单模块审什么调用 code-review-gate 的规则。

## 核心原则

**你不是审查员，你是审查的指挥官。** 你的价值在**拆分策略**（怎么切模块才不重叠不遗漏）和**汇总去重**（20 个 subagent 各报 10 条，合并后可能只有 80 条独立问题）。审查规则本身复用现有 review skill。

## 编排流程

### Step 1: 识别模块边界（拆分策略）

扫描项目结构，按**模块/目录**拆分审查范围，不是按文件数均分：

- **按职责拆**：`kernel/` / `frontend/` / `agents/` / `quality/` 各成一块
- **大文件单独成块**：单文件 >500 行（如 `pty.rs` 125KB）独立一个 subagent，避免被均分稀释
- **辅助系统成块**：`quality/`/`test/`/`scripts/` 这类聚一块
- **互斥保证**：每个文件只属于一个审查块（subagent 间文件集不相交，见 agent-delegation 红线）

拆完产出 `{审查块清单}`，每块标：范围(目录/文件)、预估行数、建议调用哪个 review skill。

### Step 2: 并行 dispatch 审查 subagent

每块一个 subagent，**同一波并行**（见 agent-delegation 并行纪律）。每个 subagent prompt 必须自包含：

```
你审查模块：<目录/文件范围>
审查规则：用 <code-review-gate> 的检查清单
只读不改。产出格式：
  ## 严重性分级发现
  ### Critical（阻塞）
  - [<文件:行>] <问题> — <为什么严重> — <建议>
  ### Important（修完再走）
  - ...
  ### Minor（记下回头改）
  - ...
不报：风格偏好、主观审美。只报：bug/安全/逻辑错误/架构问题/遗漏
```

**模型选择**：单模块审查用标准模型；架构性模块（kernel/core）用最强模型。

### Step 3: 汇总去重分级

所有 subagent 回齐后，主 agent 做**跨模块合并**：

1. **去重**：同一问题被多个 subagent 报（如"错误处理不一致"跨模块出现）→ 合并成一条，标 `[跨模块]`
2. **重新分级**：单模块看是 Important，跨多模块出现可能是 Critical（系统性问题）
3. **排序**：Critical → Important → Minor，每级内按影响面排序

输出 punch-list：

```markdown
## 审查 punch-list — <项目> @ <日期>
（N 个 subagent 审查 M 个模块，去重后 K 条发现）

### 🔴 Critical（阻塞，必须修）
1. [<文件:行>] <问题> — [跨模块/单点] — <影响>
...

### 🟡 Important（修完再走）
...

### ⚪ Minor（回头改）
...

### 系统性问题（跨模块模式）
- <模式>：在 X/Y/Z 模块都出现 → 根因可能是 <...>
```

### Step 4: 交付 → 修复

punch-list 给用户确认后逐项修复（用对应实现 skill）。**不要审查完直接改**——用户可能调整优先级。

## 决策树

```
用户说"审查整个项目"
├─ 项目规模？
│  ├─ <20 文件 → 不必批量，直接 code-review-gate 单次审
│  └─ ≥20 文件 → 进 Step 1 拆模块
├─ 拆模块时
│  ├─ 有清晰目录结构 → 按目录拆
│  └─ 扁平结构 → 按文件大小聚类，大文件单列
├─ subagent 数量
│  ├─ ≤8 个 → 一波并行
│  └─ >8 个 → 分 2 波（避免上下文爆炸 + 限流，见 research-workflow 429 容错）
└─ 汇总时发现某模块发现量异常多（>15 条）
   └─ 该模块可能质量差，标"建议深度重构"，不只列问题
```

## Gotchas（高信号）

### 问题: subagent 审查范围重叠
**现象**: 两个 subagent 都审了 `kernel/`，发现重复且可能矛盾
**解决**: Step 1 拆分时保证文件集互斥。用 `find <dir> -name` 明确每个 subagent 的文件清单，不靠目录名模糊划分

### 问题: 汇总时不去重，punch-list 冗长
**现象**: 20 subagent × 10 条 = 200 条，用户看不过来
**解决**: Step 3 必须去重。同一问题多次报→合并；跨模块模式→抽成"系统性问题"单独列

### 问题: 把审查当实现（边审边改）
**现象**: subagent 发现问题直接修了，破坏"只读审查"
**解决**: subagent prompt 明确"只读不改"。修复在 Step 4 用户确认后统一做，用独立实现 skill

### 问题: 单级降级（Critical 当 Minor）
**现象**: 跨 5 个模块的同类问题，每个 subagent 报 Important，主 agent 照搬 Important
**解决**: 跨模块高频出现的问题应**升级**为 Critical（系统性）。汇总时重新分级，不直接用 subagent 的原始级别

### 问题: 模块互斥漏跨模块调用方（重构验收场景）
**现象**: 重构后验收时，A 模块改了接口/改名符号，B 模块的调用方仍用旧名。但 A、B 分属不同 subagent（文件集互斥），A subagent 看不到 B，B subagent 不知 A 改了什么 → 跨模块断链无人报
**解决**: 若审查针对**重构/改动**（非全量摸底），Step 1 额外提取 changed-symbols 清单（被改名/删除/改签名的 export·函数·类型），注入**每个** subagent 的 prompt："除审你模块的代码质量，还 `grep -rn` 你模块里是否引用了清单中的旧符号（含 gitignored）"。模块互斥保证文件不重复审，但不该让调用方检查也互斥

## 与其他 skill 的分工

- **agent-delegation**：本 skill 是它的应用场景之一（并行 dispatch + 两阶段审查），遵循其并行纪律
- **code-review-gate**：提交前单次 diff 审查。本 skill 是整项目级，单模块审查时**调用** code-review-gate 的规则

## 适用边界

- ✅ 重构后全量验收、大项目周期性审查、接手陌生代码库摸底
- ❌ 单次提交前审查（用 code-review-gate）
- ❌ <20 文件小项目（直接单次审，编排开销不值得）
