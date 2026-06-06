# Forge

AI 写的代码，你放心直接提交吗？

Forge 是一个开发质量门禁引擎。它在你用 AI 编写代码的过程中，自动插入结构化的质量检查——从需求定义到发布，确保每一步的产出物都经过验证。

## 它解决什么问题

用 AI 写代码很快，但质量不确定：
- AI 生成的代码有没有测试？测试是不是真的在验证正确的行为？
- 实现是否覆盖了 PRD 中的所有需求？
- 有没有该做但没做的事（CHANGELOG、README 更新）？

Forge 不替代你审查代码，而是在 AI 编码的**过程中**自动拦截质量问题。

## 怎么工作

```
你提需求 → Claude Code 执行 Forge 管道 → 每道门禁验证产出物 → 全部通过才能提交
```

1. `forge init` 在项目中初始化管道（根据项目规模自动选择门禁数量）
2. Claude Code 读取生成的 Skill，按管道顺序执行每道门禁
3. 每道门禁检查产出物（文件是否存在、内容是否包含关���词、JSON 字段值是否正确）
4. 全部通过 → 代码可以提交；有门禁失败 → AI 自动修复后重新验证

## 快速开始

需要 [Claude Code](https://docs.anthropic.com/en/docs/claude-code) 已安装。

```bash
# 安装
npm install -g @agentfare/forge

# 在项目目录初始化
cd your-project
forge init

# 在 Claude Code 中告诉 AI 执行管道
# Claude Code 会自动读取 Forge 生成的 Skill 并驱动管道
```

## 命令参考

| 命令 | 说明 |
|------|------|
| `forge init [--mode small|medium|large]` | 初始化管道 |
| `forge status [--json]` | 查看管道状态（列出所有 gate ID） |
| `forge gate <gate-id>` | 验证指定门禁的产出物 |
| `forge validate` | 检查 pipeline.yml 配置是否正确 |

## 门禁管道

`forge init` 根据项目规模自动选择门禁：

| Gate ID | 名称 | small | medium | large |
|---------|------|:-----:|:------:|:-----:|
| gate-1-prd | 需求定义 | ✓ | ✓ | ✓ |
| gate-3-plan | 实现计划 | ✓ | ✓ | ✓ |
| gate-4-implement | 代码实现 | | ✓ | ✓ |
| gate-5-test | 测试验证 | | ✓ | ✓ |
| gate-6-acceptance | 项目验收 | | ✓ | ✓ |
| gate-8-release | 发布 | | | ✓ |
| gate-0-research | 立项调研 | | | ✓ |
| gate-2-design | 技术方案 | | | ✓ |
| gate-7-archive | 经验归档 | | | ✓ |

`forge status` 会显示当前项目中所有启用的 gate ID。

## 安装方式

```bash
# npm（推荐）
npm install -g @agentfare/forge

# 或从 GitHub Releases 下载二进制
# https://github.com/MjxUpUp/forge/releases
```

## License

MIT
