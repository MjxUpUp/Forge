# SKILL.md 内容结构模板

```markdown
# Skill 标题

一句话说明 + 核心原则。

## When to Use / When NOT to Use

## 核心步骤 / 规则
<主体内容>

## Common Rationalizations（纪律型 skill 必备——堵借口的最高信号）
| 借口 | 现实 |
|---|---|

## Red Flags（纪律型 skill 必备——"我在 rationalize 的想法"自检清单）

## 易错点（Gotchas）
<从实际失败中积累——最高信号>

## 与其他 skill 的分工
SKIP 指向 + 互补指向

## 参考
- [描述](references/file.md)
```

## 目录结构

```
skill-name/
├── SKILL.md                    # 主文件，≤ 500 行
└── references/                 # 按需加载的详细内容
    ├── examples.md             # 示例
    ├── checklist.md            # 检查清单
    ├── gotchas.md              # 详细易错点
    └── ...
```

**自包含 skill**：所有内容内联（无 references）
**带可复用工具**：SKILL.md + 可执行脚本/模板
**带重型参考**：SKILL.md 概述 + references/*.md（API 文档、600+ 行参考）

## 单文件 vs 目录

- 整个内容能装下、无重型参考 → 单 SKILL.md
- 有可复用工具代码或 >500 行参考 → 目录 + references
