---
name: docs-consistency-guard
description: "给手维护的衍生文档（README 命令表/hook 表/配置项表/feature 列表/API 示例/版本徽章/平台支持表）建立守卫测试，钉住它不落后于代码真相源。Use when: 文档里有会随代码变化的列表或计数；发布前审计文档与代码是否一致；遇到'文档说 8 个但代码只注册 7 个'这类漂移；想给某段手维护文档加自动化防漂移。SKIP: 纯叙述无代码真相源的文档、一次性临时文档、代码逻辑设计审查（用 code-review-gate）、API 签名查证（用 dev-lookup）。"
metadata:
  pattern: tool-wrapper
  domain: docs-quality
---

# 衍生文档一致性守卫

衍生文档（README 表、示例、计数、徽章）是手维护的，**必然**落后于代码——区别只是"什么时候被发现"。守卫测试把"靠人记得发布前查"变成"机器每次 `test` 自动 fail"，从根上消灭文档漂移。

**核心原则：衍生文档若有一个程序可提取的代码真相源，就该有一个钉死它的守卫测试。**

## When to Use / When NOT to Use

**Use when:**
- 文档里有列表/表格/计数，且每一项都对应代码里的一个定义（注册项、enum、struct 字段、路由、migration）
- 发布前审计"文档和代码是否还一致"
- 已经第二次撞上"文档滞后"（同类坑踩过 = 该自动化了）
- 想给一段手维护文档加防漂移保险

**SKIP（路由到别的载体）:**
- 文档纯叙述、无对应代码定义 → 没有真相源可比，守卫测试无从建。改用人工校对或 doc-generator。
- 一次性文档（会议纪要、临时说明）→ 沉淀成本 > 漂移成本。
- 代码逻辑/设计的质量审查 → 用 `code-review-gate`。
- 查 API 签名/库用法 → 用 `dev-lookup`。

## 为什么是守卫测试，不是 hook / 命令 / skill

| 载体 | 触发 | 为什么不适合"文档一致性" |
|---|---|---|
| **守卫测试** ✅ | 每次 `test` | 真相源是代码、衍生是文档，确定性比对；本地 + CI 都跑，零人参与 |
| hook | 工具事件 | 文档滞后不是 Write/Bash 事件，无合适触发点；每次 Write 跑全量比对太重 |
| 命令 | 人显式调用 | 靠人记得跑，和"靠人记得查"同一个坑 |
| skill | agent 执行 | 确定性检查靠 agent 遵循 = 会漏；**能机器强制的绝不进 skill** |
| CI 门禁 | push/tag | 守卫测试本身就在 CI 的 test job 里跑；CI 是它的载体，不是替代 |

## 流程（5 步）

### 1. 找真相源

问：这段衍生文档，每一项在代码里的**权威定义**在哪？真相源必须**程序可提取**——
- 一个返回列表的函数（如 `hooks.HookNames()`）
- 一个 enum / const 块
- 一个 struct 的字段（反射 / 编译期）
- 一份配置文件（`package.json` / `Cargo.toml` / migration）
- 一个生成器的输出

❌ 散在注释里、靠口头约定的，**不算真相源**（无法提取就没法比）。先确认能不能程序化拿到。

### 2. 找衍生文档

哪些文件是**手维护**、会过期的？README 表、命令参考、配置说明、feature 列表、API 示例、平台支持表、版本徽章。

⚠️ 先确认它是不是手维护——如果它是 skillgen/代码生成的（和真相源天然同步），**不需要守卫测试**，守卫只针对手写副本。

### 3. 配对

哪条衍生文档该由哪个真相源派生？建立**一对一映射**：
- README hook 表 ↔ `settings.go` 的 `HookNames()`
- README 命令参考 ↔ cobra 注册的命令
- README env vars 表 ↔ config struct 字段
- README feature 列表 ↔ `Cargo.toml [features]`

### 4. 写守卫测试（用项目现有框架）

不引入新依赖。读真相源提取集合 → 读衍生文档 → 断言**每个元素都出现**。模板见下。

### 5. 进 CI + 失败信息面向修复

测试跟着既有 `test` 跑（已在 CI）。**失败信息必须告诉人补什么**，不是"不一致"：

```go
t.Fatalf("README 缺 hook %q（真相源 settings.go 注册了它）", n)
```

## 真相源 ↔ 衍生文档 配对参考表

在新项目照着这张表快速找到该钉的项：

| 真相源（程序可提取） | 衍生文档（手维护） | 典型项目 |
|---|---|---|
| Hook/命令/插件注册函数 | README hook/命令/插件表 | CLI 工具、Forge |
| enum / const 块 | 配置项表、feature flags 表 | 配置类项目 |
| 路由注册 | README / OpenAPI 示例 | Web 服务 |
| config struct 字段 | env vars 表、配置说明 | 任意带配置的服务 |
| `Cargo.toml [features]` / `package.json exports` | README feature / API 列表 | Rust / Node 库 |
| DB migration / schema | 数据字典、ER 图 | 后端 |
| i18n 资源键 | 文档字符串 key 表 | 国际化项目 |
| release 资产清单 | README 平台支持表 | 跨平台分发 |

## 守卫测试模板（按"提取真相源的方式"分）

提取方式因语言而异——这是无法做成通用库的根因（每个项目手写几十行），但比对逻辑通用。

**Go（注册函数返回列表）**
```go
func TestReadme_HookTable_MatchesSource(t *testing.T) {
    names := hooks.HookNames() // 真相源
    readme, err := os.ReadFile("../../../README.md")
    if err != nil { t.Fatal(err) }
    body := string(readme)
    for _, n := range names {
        if !strings.Contains(body, n) {
            t.Fatalf("README 缺 hook %q（真相源 settings.go 注册了它）", n)
        }
    }
}
```

**TS/JS（解析 package.json exports）**
```ts
test('README API 列表覆盖所有 exports', () => {
  const pkg = require('../package.json')
  const readme = fs.readFileSync('README.md', 'utf8')
  for (const name of Object.keys(pkg.exports)) {
    if (!readme.includes(name)) {
      throw new Error(`README 缺 export ${name}（package.json 声明了它）`)
    }
  }
})
```

**Rust（解析 Cargo.toml [features]）**
```rust
#[test]
fn readme_covers_all_features() {
    let manifest = include_str!("../Cargo.toml");
    let features: Vec<&str> = manifest.lines()
        .skip_while(|l| !l.starts_with("[features]"))
        .skip(1)
        .take_while(|l| !l.starts_with('['))
        .filter_map(|l| l.split('=').next()).map(|s| s.trim())
        .filter(|s| !s.is_empty())
        .collect();
    let readme = include_str!("../README.md");
    for f in features {
        assert!(readme.contains(f), "README 缺 feature {}（Cargo.toml 声明了它）", f);
    }
}
```

**Python（扫描模块成员）**
```python
def test_readme_covers_all_commands():
    import myapp.commands as cmds
    names = [n for n in dir(cmds) if not n.startswith('_')]
    readme = Path('README.md').read_text(encoding='utf-8')
    for n in names:
        assert n in readme, f"README 缺 command {n}"
```

## 设计原则（4 条）

1. **真相源必须程序可提取**——函数返回 / struct 字段 / 配置解析。散在注释的不算。
2. **比对精确到元素**——校验每个项的存在，不只数数量。8==8 但删 A 加 B 仍漂移。
3. **测试便宜**——纯文件读 + 字符串比对，不跑网络/编译/DB。贵了团队会 `skip`。
4. **失败信息面向修复**——报"README 缺 X，真相源有 X"，不报"不一致"。

## Gotchas（从实际踩坑提炼——最高信号）

- **真相源选错**：选了"看起来权威但本身会漂移的"（另一个 README、注释）→ 守卫保护的是错的源头。真相源必须是**代码定义本身**（注册函数/enum/struct/migration），不是它的任何文档化版本。
- **只比数量不比内容**：`assert count == 8` 在"删 A 加 B"时仍过。用元素存在性断言（`Contains`）或精确集合相等。
- **真相源本身漏注册**：守卫只保证"文档 ⊆ 真相源"，发现不了"真相源缺了本该有的项"。这是天花板——它防文档漂移，不防代码遗漏。补法：给真相源也加测试（注册数 == 预期常量）。
- **README 有多个副本**：根 README + `npm/README.md` + `docs/`——分发出去的每个副本都要覆盖。Forge 踩过：根 README 对了，`npm/README.md` hook 表滞后（6 vs 8）发布出去。
- **生成式文档不需要守卫**：若衍生文档由 skillgen / 代码生成（与真相源天然同步），别加守卫——它永远不会漂移。守卫只针对**手维护**副本。先确认这点再动手。
- **跨语言提取差异**：Go 函数返回 / Rust `include_str!` 解析 TOML / TS `require`。提取方式因语言而异，这是无法通用化的根因，每个项目手写几十行即可。
- **数量断言的反模式**：`readme.count('|') == 8`——加一行表就红，且不告诉缺哪个。永远用元素存在性。

## 与其他 skill 的分工

- **session-retrospective**：决定"经验进什么载体"。当它判定"进守卫测试"时，转交本 skill 建具体测试。
- **code-review-gate**：审代码质量/查 AI 作弊。本 skill 是建测试防文档漂移，不审逻辑。
- **dev-lookup**：查 API 签名/库用法。本 skill 只比对文档 vs 注册，不查签名。
- **skill-authoring-standard**：写新 skill 的规范。本 skill 是文档质量，不是 skill 质量。

## 参考

- Forge 实例：`internal/hooks/settings.go` 的 `HookNames()` ↔ `npm/README.md` hook 表（v0.22 缺 7 个、v0.26.4 缺 2 个，反复踩 → 守卫测试消灭之）
- 载体选择逻辑（为什么守卫测试 > hook/命令/skill）：见 `session-retrospective` 的载体决策树
