---
name: maintainability-and-readability
description: "可维护性与可读性强制规范：cyclomatic/cognitive 复杂度 + function size + naming + SOLID 5 原则 + Clean Code + 模块依赖 + 工具链（linter/SAST）。Use when: code review 含复杂度报警、写新代码/重构、refactor 老代码、设 CI 质量门槛、写 ADR 关于代码风格、给团队立规范时。SKIP: 单 API 设计细节（用 backend-development）/ 单组件实现（用 frontend-development）/ 安全/性能（用 secure-coding + resilience-and-observability）。"
metadata:
  pattern: tool-wrapper
  domain: maintainability
  steps: 4
  composes: [code-review-gate, implementation-discipline, systematic-debugging, backend-development, frontend-development]
---

# 可维护性与可读性规范

> **本 skill 不重复**: 单 API 设计 → `backend-development`；单组件实现 → `frontend-development`；CI/CD / 部署 → `release-readiness`。本 skill 解决"按 SOP 写出/重构易读易维护代码"——含 cyclomatic + cognitive 复杂度指标 + SOLID + 工具链。

## 1. 决策树

```
任务是什么？
├─ 写新代码 → §2.1 5 步写作流程（spec → 测试 → 实现 → 自审 → PR）
├─ Code review 报警复杂度高 → §2.2 重构 5 法（extract function / class / module ...）
├─ 设置 CI 质量门槛 → §2.5 工具链 + 阈值表
├─ 写 ADR 关于代码风格/原则 → §2.4 SOLID 选型 + ADR 模板
├─ Naming 决策（包/类/变量/函数/常量） → §2.3 naming 6 规则
└─ 老代码 refactor 优先序 → §2.6 重构优先级矩阵（业务价值 / 重构成本）
```

## 2. 6 路径规范

### 2.1 5 步写代码流程（Quality First）

```
1. Spec      → 接口 + 主要场景（输入/输出/边界）
2. Test      → 先写失败测试（红）→ 红绿重构
3. Code      → 实现 + 看 §3 负向约束 + §2.5 工具链 CI 检查
4. Self-review → §4 自查清单（post-generation）
5. PR        → forge review pass（code-review-gate）

反向不允：先写实现 → 后补测试 → 跳过 spec。spec 文档化在 §4.6 / references/。
```

### 2.2 重构 5 法（Robert C. Martin "Clean Code"）

```
复杂度报警 → 重构选择：

├─ 1. Extract Function（最常用）
│       函数 > 30 行或 cyclomatic > 10 → 拆函数
│       命名动词/动词短语；不要 extractAndValidate() 冗长
│
├─ 2. Extract Class / Module
│       类 > 300 行或职责多 → 拆类（SRP 原则）
│       "当你觉得'这个类做的事情有点多'" → 必拆
│
├─ 3. Inline（反向）
│       函数主体比函数名还简单 → 直接合并
│       "过度抽象比适当抽象更差"
│
├─ 4. Rename
│       名不符实 → 改名（不要 "fix typo" 而忽略命名问题）
│       PR 不只是改 bug 是改命名（同样重要）
│
└─ 5. Move（移动）
       类 A 用 Class B 但 B 在 Class C 文件 → 物理 move 到 C 文件
```

### 2.3 Naming 6 规则

```
1. Intent-revealing
   ❌ int d;                     ✅ int elapsedDays;
   ❌ list<Data> l;               ✅ list<Data> activeUsers;
   ❌ doProcess();                ✅ sendVerificationEmail();

2. Avoid disinformation
   ❌ accountList（实际是 Map）→ accountsById
   ❌ hp（could be hypotenuse / horizontal pixels）→ horizontalPixels
   ❌ DataInfo（Data 是冗余）→ Data

3. Make meaningful distinctions
   ❌ Customer vs CustomerObject → 都错
   ❌ getActiveAccount() vs getActiveAccounts() vs getActiveAccountInfo()（要 prefix 一致）

4. Use pronounceable names
   ❌ genymdhms（生成日期）→ generationTimestamp
   ❌ dnseurtm（don't use real-time）→ delayNanos

5. Use searchable names
   ❌ "5" 写 magic number → MAX_RETRIES = 5
   ❌ 单字母（i, j, k）→ 循环可，跨函数不可

6. Class/object vs method naming（CONVENTION）
   Class 名 = 名词（Noun）：Account, UserParser
   Method 名 = 动词（Verb）：getName(), save(), validate()
   ❌ AccountProcess() → split Account + AccountService.process()
```

### 2.4 SOLID 5 原则（Robert C. Martin）

```
S Single Responsibility   一类一职
                          反例：UserService 同时管 CRUD + auth + email
                          → UserCRUDService, UserAuthService, UserEmailService

O Open/Closed             对扩展开放，对修改封闭
                          用：interface + 多实现 + DI
                          反例：if-else 分支每加一种类型就改源文件

L Liskov Substitution     子类可替换父类，不破坏语义
                          反例：Bird 子类 Penguin 不该继承 fly() 方法
                          → 拆 interface FlyingBird / SwimmingBird

I Interface Segregation   接口最小化，多接口优于大接口
                          反例：Worker interface 含 work() + eat() + sleep()
                          → Workable, Eatable, Sleepable 拆分

D Dependency Inversion    高层不依赖低层，都依赖抽象
                          反例：Service 里 new Database() 直依赖
                          → 抽 interface DatabaseRepository + 注入实现
```

### 2.5 工具链 + 阈值（CI 质量门槛）

| 工具 | 作用 | 阈值建议 |
|---|---|---|
| **linter** (eslint / golangci-lint / ruff / clippy) | 语法 + style | 0 error |
| **formatter** (prettier / gofmt / rustfmt / black) | 格式化 | 自动 |
| **typechecker** (tsc / mypy) | 类型 | 0 error，strict mode |
| **complexity** (lizard / complexity-report / SonarQube) | cyclomatic + cognitive | ≤10 cyclomatic/function, ≤15 cognitive |
| **lines per function** | 函数行数 | ≤30（>50 重构） |
| **lines per file** | 文件行数 | ≤200（>500 拆） |
| **duplication** (jscpd / duplication-report) | 重复率 | ≤3% |
| **coverage** | 测试覆盖率 | ≥80% |
| **mutator** (mutmut / go-mutesting / PIT) | 变异测试 | ≥70% kill |

**CI 门槛**：
```yaml
fail_build_if:
  - cyclomatic_per_function > 15
  - cognitive_per_function > 20
  - file_lines > 500
  - function_lines_p95 > 50
  - test_coverage < 80%
  - duplication_rate > 5%
```

### 2.6 Refactor 优先级矩阵

| 业务价值 × 重构成本 | 高成本 | 低成本 |
|---|---|---|
| **高价值** | 计划性重构（季度 sprint） | **立即重构**（refactor + bug fix） |
| **低价值** | 删除或弃用 | 不动（性价比低） |

**铁律**：触到 bug 时立即修 + 顺手重构（"boy scout rule"——离开时比来时干净）。但**不**做无 bug 触发的纯重构（=过度工程）。

## 3. 负向约束 + 替代方案

| 不要做 ❌ | 应该做 ✅ |
|---|---|
| 函数 > 50 行 | Extract Function（§2.2） |
| cyclomatic > 20 | 拆函数 + early return / strategy pattern |
| 类做三件事 | SRP 原则 → 拆 3 类 |
| 名 `data / info / manager / process`（无意义） | 名词动作清晰（§2.3） |
| 全局可变 state（`var x = 0; func modify() { x++ }`） | 注入 + 不可变 + 局部变量 |
| `god class`（什么都管） | 拆 SRP 单一职责类 |
| 不用 type checker（"运行时再说"） | tsc strict + mypy strict + 0 any |
| 代码注释解释 "what"（代码已说）| 注释解释 "why"（设计决策 / 历史） |
| magic number `500` | const MAX_QUERY_LENGTH = 500 |
| 复制粘贴 4+ 次相似代码 | 抽公共函数 + 参数化 |

## 4. Post-Generation 自查清单

### 4.1 复杂度类
- [ ] cyclomatic / function ≤ 10（中等 ≤ 15）
- [ ] cognitive complexity ≤ 15
- [ ] 函数 ≤ 30 行（> 50 必重构）
- [ ] 文件 ≤ 200 行（> 500 拆）
- [ ] 参数 ≤ 3 个（> 5 拆对象）

### 4.2 可读性类
- [ ] naming 表达意图（无 d/l/x/data/info 模糊）
- [ ] 没有大段注释解释 what（注释只 why）
- [ ] magic number 都用 const
- [ ] 函数做一件事（单一职责）
- [ ] no `any` / `interface{}` / 裸 `Object`（TS/Go/Python 各自严格）

### 4.3 维护性类
- [ ] 每个模块/类有显式职责（SRP）
- [ ] 依赖通过 interface 注入（DIP）
- [ ] 测试覆盖核心逻辑 ≥ 80%
- [ ] 至少 1 个 example test（demo 用法）
- [ ] changelog / commit message 说明动机（why 而非 what）
- [ ] ADR 写在架构层面（不是代码层面 — 那是注释）

### 4.4 流程类
- [ ] CI 全过（linter + formatter + typecheck + test + coverage）
- [ ] `forge review pass` 通过（含 complexity gate）
- [ ] 没有遗留 TODO（技术债）/有 ADR 解释为什么

## 5. Gotchas（实操易错点）

**G1**: "代码写得快更重要" → 半年后自己看不懂。预防：先 spec 后 code，§2.1 5 步流程。

**G2**: function 30 行硬塞 → cyclomatic 飙。预防：尽早 Extract Function（§2.2 #1）。

**G3**: class 拆得太细 → SRP 过度（10 个类做 1 件功能）。预防：按"能讲清职责"合并（"manager 也做了这个——合理吗"）。

**G4**: naming 用拼音 / 中文 / 缩写 → 团队协作难。预防：英文单词 + 全称（首次出现写全 + 后续缩写）。

**G5**: "先写代码后补文档" → 文档永远缺失。预防：先 spec 后 code（§2.1）。

**G6**: 测覆盖率刷到 90% 但都是 mock 测试 → bug 仍漏。预防：mutation testing（§2.5）+ 真实集成测试。

**G7**: 复杂度只看 cyclomatic → switch/case 一堆仍 cyclomatic 低但维护难。预防：cognitive complexity（SonarQube）+ 视觉审查。

**G8**: "重写一遍！比改烂代码快" → 不熟业务重写反而坏。预防：boy scout rule（局部修）+ 选高 ROI 重构。

## 6. 提交前必跑

```bash
# 1. 静态检查
forge auto-build                     # auto-compile + linter 综合

# 2. 复杂度（diff scope）
lizard src/                          # cyclomatic + cognitive + NLOC
# 或
forge skills validate --skill=maintainability-and-readability

# 3. 重复率
jscpd src/

# 4. 测试 + 覆盖率
go test -race -cover ./...            # Go
pnpm test --coverage                  # Node
pytest --cov=src                      # Python

# 5. Code review（含 maintainability 子检查）
forge review pass                    # code-review-gate

# 6. mutation testing（可选，月级）
mutmut run                          # Python
go-mutesting                        # Go
```

不过 → §4 自查清单补足；过 → commit + 通知 reviewers。

## 7. 与其他 skill 的协作

- **代码审查**：`code-review-gate` 含 complexity 子检查 — 与本 skill 集成
- **重构纪律**：`implementation-discipline` — 是更广的实现纪律，含 commit 卫生
- **debug**：重构时 `systematic-debugging` 避免"改坏"
- **类型层**：TypeScript/Java/Go/Rust 类型系统是"编译期"质量保证，typecheck 是 §4 必备
- **测试**：`tdd-cycle` + `test-discipline` — 测试驱动保持复杂度低

## 8. 行级 / 函数级 / 模块级阈值表

| 维度 | 建议阈值 | hard cap（CI 失败） |
|---|---|---|
| Function lines | ≤ 30 行 | > 80 行 fail |
| Function parameters | ≤ 3 参数（>5 必聚合） | > 7 fail |
| Function cyclomatic complexity | ≤ 10（理想 ≤ 5） | > 15 fail |
| Function cognitive complexity | ≤ 15 | > 25 fail |
| File lines | ≤ 200 行 | > 500 fail |
| Class lines | ≤ 300 行 | > 1000 拆 |
| Class methods (public) | ≤ 15（公开 API） | > 30 拆 |
| Class responsibilities | 1（SRP） | 2+ 名字有 "And" 警 |
| Module dependencies | ≤ 8 (afferent) / ≤ 6 (efferent) | 切 cyclomatic |
| Test coverage | ≥ 80% | < 70% fail |
| Test mutation score | ≥ 70% | < 50% fail |
| Duplication | ≤ 3% | > 5% fail |

## 9. 多语言适配

| 语言 | Linter | Formatter | Typecheck | Complexity |
|---|---|---|---|---|
| **Go** | golangci-lint | gofmt | go vet / staticcheck | gocyclo / lizard |
| **TypeScript** | eslint + @typescript-eslint | prettier | tsc | eslint complexity |
| **Python** | ruff + mypy | black / ruff format | mypy --strict | radon / lizard |
| **Rust** | clippy | rustfmt | cargo check | cargo-geiger / lizard |
| **Java** | checkstyle + spotbugs | google-java-format | javac | cyclomatic-check |

## 参考

- 完整 references 进 `references/`（Clean Code 章节 / Refactoring Catalog / Solid 解读 / 复杂度度量指标）
- 调研权威源：[Clean Code (Robert C. Martin)](https://www.amazon.com/Clean-Code-Handbook-Software-Craftsmanship/dp/0132350882) / [SonarQube rules](https://rules.sonarsource.com) / [Cognitive Complexity (SonarSource)](https://www.sonarsource.com/blog/white-papers/cognitive-complexity-white-paper/) / [lizard docs](https://github.com/terryyin/lizard)
- 写法参照 `skill-authoring-standard`
