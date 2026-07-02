---
name: verification-driver
description: "驱动外部工具对产物做端到端验证（API 实测、CLI 驱动、docker 集成、HTTP 断言）。Use when: 需要验证 HTTP API/CLI 工具/docker 服务/集成链路是否真正工作（而非单元测试通过）、声称功能完成前要做端到端验证、用户说\"验证下能不能用\"\"测一下实际效果\"\"端到端测\"时。SKIP: 单元测试质量守卫（用 test-discipline）、TDD 循环（用 tdd-cycle）、纯编译错误（用 compile-fix-loop）、调试 bug 根因（用 systematic-debugging）。"
metadata:
  pattern: pipeline + reviewer
  domain: verification
---

# Verification Driver — 产物端到端验证驱动

**驱动外部工具（curl/jq/docker/gh/CLI）验证产物真的工作**，而非依赖单元测试通过就声称完成。

**核心原则：单元测试通过 ≠ 产物能用。涉及 HTTP/CLI/集成链路的变更，必须驱动真实工具做端到端验证。**

**"应该能用"不是验证。运行命令、看到输出、断言状态，才是验证。**

## When to Use

变更涉及以下任一，必须端到端验证（单元测试不够）：

| 变更类型 | 必须的端到端验证 |
|---|---|
| HTTP API（新增/改接口） | curl 实际请求 + jq 断言响应结构/状态码/字段 |
| CLI 工具（新增/改命令） | 实际运行命令 + 断言 stdout/exit code |
| docker/容器服务 | docker run + 健康检查 + 实际访问 |
| 集成链路（多服务） | 启动全链路 + 请求贯穿 + 每跳验证 |
| 数据库交互 | 真实 DB 写入 + 读回验证（非 mock） |
| IPC/进程通信（如 Tauri） | 真实 IPC 调用（非 mock） |
| 前端渲染 | 实际看到（pi 无 browser 则降级：构建产物 + DOM 断言） |

## pi 本机可用的验证工具（实测）

| 工具 | 验证用途 | 命令示例 |
|---|---|---|
| `curl` + `jq` | HTTP API 端到端 | `curl -s URL \| jq '.status'` 断言值 |
| `docker` | 容器服务 | `docker run` + `curl` 健康检查 |
| `gh` | GitHub 集成（webhook/Action/API） | `gh api` 实测 |
| `node`/`python` | 脚本化断言 | 写验证脚本批量断言 |
| `cargo`/`go`/`npm` | 构建产物 + 集成测试 | `cargo test --test '*'`（集成测试目录） |

**不可用**（pi 本机限制，不要尝试）：
- ❌ playwright / puppeteer / headless browser（未装）—— 前端渲染验证降级为"构建 + 静态 DOM 检查"或请用户手动看
- ❌ web_fetch / browser 工具（pi 无）—— 用 curl 替代

## 验证流程（pipeline）

### Step 1: 确定验证目标 + 断言

不是"跑一下看看"，是"**验证 X 行为，断言 Y 条件**"：
```
目标：验证用户注册 API
断言：
  - POST /register 返回 201
  - 响应 body 含 user_id 且非空
  - DB 里真有这条记录（SELECT 验证）
  - 重复注册返回 409
```

### Step 2: 驱动真实工具执行

用 pi 可用工具实际跑，**不 mock**：

```bash
# API 验证：实际 curl + jq 断言
RESP=$(curl -s -w "\n%{http_code}" -X POST http://localhost:8080/register \
  -H "Content-Type: application/json" -d '{"email":"test@x.com"}')
CODE=$(echo "$RESP" | tail -1)
BODY=$(echo "$RESP" | head -n -1)
[ "$CODE" = "201" ] || { echo "FAIL: 期望201 实际$CODE"; exit 1; }
echo "$BODY" | jq -e '.user_id' >/dev/null || { echo "FAIL: 无 user_id"; exit 1; }

# DB 验证：真实查询（非 mock）
docker exec db psql -U app -c "SELECT count(*) FROM users WHERE email='test@x.com'" | grep -q 1
```

### Step 3: 程序化断言（reviewer）

每个验证步骤必须有**可判定的断言**，不是"看一眼像对了"：
- ✅ `[ "$CODE" = "201" ]` / `jq -e '.field'` / `grep -q "expected"`
- ❌ "输出了 JSON 看起来对" / "没报错应该成了"

断言失败 = 产物有 bug，去修（用 systematic-debugging 找根因），**不停在"验证脚本写错了"**。

### Step 4: 边界 case 补齐

主路径过了，补关键边界：错误输入、权限拒绝、重复请求、空值、超时。每个边界一条断言。

### Step 5: 验证证据留存

把验证命令 + 实际输出存档（写入 run_dir 或贴给用户）：
```
✅ 注册 API 验证通过
  POST /register → 201 {"user_id":123}
  DB 确认: users 表 +1 条
  重复注册 → 409
命令: verify_register.sh
```

## Gotchas（高信号）

- **"编译通过/单测通过"≠ 能用**：HTTP 服务单测全绿但实际端口没监听、CORS 没配、认证中间件顺序错——只有端到端 curl 能抓到
- **mock 是最大陷阱**：httptest.NewRequest 固定 RemoteAddr 测不出限流 bug（test-discipline 已证），端到端必须真实连接
- **断言要判定不要肉眼看**：`echo $RESP` 看一眼 ≠ 验证，必须 `[ ... ]` 或 `jq -e` 让脚本判定 pass/fail
- **边界 case 比主路径更值钱**：主路径对了边界错的产物，上线才暴雷。重复/空值/超时/权限必测
- **验证失败回 systematic-debugging**：验证脚本是"证人"不是"嫌疑人"，验证失败说明产物有 bug，去查代码根因，别改验证脚本迁就
- **pi 无 browser 时前端验证降级**：不能 screenshots 对比，则 `npm run build` 成功 + 关键 DOM 节点存在性断言 + 请用户手动确认视觉

## Red Flags — STOP

- 声称"功能完成"但只跑了单元测试（HTTP/CLI/集成变更必须有端到端）
- 用 mock/httptest 代替真实连接验证集成链路
- 验证输出"看起来对了"而无程序化断言（`[ ... ]` / `jq -e`）
- 验证失败时改验证脚本迁就（应改产物代码）
- 只验证主路径，跳过边界 case
- 把"我手动试了一下"当验证（不可重复、不可回归）
- 尝试用 playwright/puppeteer（pi 未装，会失败）

## 与其他 skill 的分工

- **test-discipline**：测试**质量**守卫（防弱断言/假阳性）——本 skill 驱动验证时产出的断言，质量由 test-discipline 保障
- **tdd-cycle**：写代码**前**的 TDD 循环——本 skill 是产物**后**的端到端验证
- **systematic-debugging**：验证发现 bug 时，用它找根因
- **compile-fix-loop**：纯编译错误不走本 skill
- **dev-workflow/references/verification-strategy.md**：验证分层策略（本 skill 是其中"集成/端到端层"的执行方法）
