# Forge Engine

AI Coding Quality Gatekeeper 的开发门禁管道引擎。

独立于 [HarnessCenter](..)（Skill 平台）的管道执行引擎，通过 `hc install` 消费 Skill 平台的能力。

## 安装

```bash
cd forge
go build -o ~/.forge/bin/forge ./cmd/forge/
```

## 快速开始

```bash
# 在任意项目中初始化管道
forge init --mode medium

# 查看管道状态
forge status

# 运行单道门禁
forge gate gate-1-prd

# 运行完整管道
forge run
```

## 命令

| 命令 | 功能 |
|------|------|
| `forge init` | 创建 `.forge/` 目录 + pipeline.yml + state.json + hooks |
| `forge gate <id>` | 执行单道门禁（前置检查 → hook → checks → 状态写入） |
| `forge run [--from <id>]` | 顺序执行全部启用门禁 |
| `forge status [--json] [--system]` | 管道全景图 / 系统健康检查 |
| `forge knowledge list/add/check` | 跨项目经验库管理 |

## 门禁 (Gate)

| Gate | 名称 | 说明 |
|------|------|------|
| gate-0-research | 立项调研 | 竞品分析、技术可行性（large 模式） |
| gate-1-prd | 需求定义 | PRD + 验收条件 |
| gate-2-design | 技术方案 | ADR + 接口定义（large 模式） |
| gate-3-plan | 实现计划 | 任务拆解 + 依赖无环 |
| gate-4-implement | 代码实现 | 编译 + 测试 + 断言弱化检查 |
| gate-5-test | 测试验证 | 覆盖率 + E2E |
| gate-6-acceptance | 项目验收 | PRD 覆盖 + 设计一致性 |
| gate-7-archive | 经验归档 | 跨项目经验提取（large 模式） |
| gate-8-release | 发布 | checklist + CHANGELOG + semver |

## 规则求值引擎

支持 8 种规则模式的自动求值：

| 模式 | 示例 |
|------|------|
| 文件不含关键词 | `compile.log 无 ERROR` |
| 文件包含关键词 | `prd.md 包含 Out of Scope` |
| JSON 字段值 | `test-results.json.failed == 0` |
| JSON 计数 | `competitors.json.count >= 3` |
| 文件存在性 | `README.md 已更新` |
| 前序 gate 通过 | `所有前序 status.json.passed == true` |
| 定性检查 | `每个功能有验收条件` → 委派给 skill |

## 与 HarnessCenter 的关系

```
HarnessCenter（Skill 平台）         Pipeline Engine（管道引擎）
hc scan → 扫描 Skill 质量           forge init → 项目初始化
hc publish → 发布 Skill             forge run → 运行管道
hc install → 安装 Skill             forge gate → 运行单道门禁
      ↑                                    ↑
      └──── hc install 安装门禁 Skill ──────┘
```

Pipeline Engine 不做 Skill 审核，HarnessCenter 不做管道执行。通过 `hc install` 衔接。

## 测试

```bash
go test ./...
```

## 技术栈

- Go 1.24
- Cobra (CLI)
- yaml.v3 (配置解析)
