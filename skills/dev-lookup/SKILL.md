---
name: dev-lookup
description: >
  开发期单点快速检索：查 API 签名、查报错含义、查库用法、查版本兼容、查语法、查官方示例。
  Use when: 开发或调试时需要确认某个具体技术点——"这个 API 怎么调""这个错误什么意思""这个库怎么用""这两个版本兼容吗""这个语法对不对"。
  SKIP: 深度调研/多源对比/产出调研报告（用 research-workflow）；提方案前的环境验证（用 evidence-based-proposal）；飞书/云文档操作（用 lark-*）。
metadata:
  pattern: routing + fallback
  domain: dev-productivity
---

# Dev Lookup — 开发期单点快速检索

开发或调试时确认某个具体技术点。**单点、即时、直接返回答案，不写报告不 spawn worker**。

和调研类 skill 的量级区别：

| | dev-lookup（本 skill） | research-workflow |
|---|---|---|
| 场景 | 查 API/报错/用法/版本/语法 | 调研某方向/竞品/趋势 |
| 查询次数 | ≤5 次，拿到就用 | ≥60 次，多 agent |
| 产出 | inline 答案 + URL | run_dir 报告 + 飞书发布 |
| 互证 | 不做 | 四档信度分级 |

## pi 本机检索基线（实测）

pi 无 web_search/web_fetch/browser 内置工具，联网只能 bash curl。本机实测：

| 通道 | 状态 | 用途 |
|---|---|---|
| 官方文档站 curl（MDN/python/node/react/docs.rs/go/k8s） | ✅ 全通 | 查 API/语法/错误码 |
| `gh` CLI（已认证 5000/h） | ✅ | 找代码示例/issues/README |
| 包仓库 API（crates.io / npm registry） | ✅ | 查包元数据/版本/依赖 |
| Stack Exchange API（api.stackexchange.com） | ✅ | 查报错的社区解法 |
| Bing 搜索结果页 | ⚠️ 可抓但链接质量差，需大量二次筛选 | 仅作兜底 |
| 百度 | ❌ 验证码墙（技术英文词直接被拦） | 别用 |
| Stack Overflow 网页版 | ❌ 403 反爬 | 用 SE API 替代 |
| Jina Reader / JS 渲染源 | ❌ 超时 | 别用 |

**核心纪律**：走官方文档站 + gh + 包仓库 API + SE API 这四条主路，它们精准且本机可靠。别碰百度/SO 网页/Jina。

## 按问题类型选检索路径

| 问题类型 | 首选路径 | 命令模板 |
|---|---|---|
| 查 API 签名 | 官方文档站 / docs.rs(rust) | 见下「查 API」 |
| 查报错含义 | 官方错误码文档 + SE API | 见下「查报错」 |
| 查库用法 | 包仓库 API + gh README | 见下「查库」 |
| 查版本兼容 | 包仓库 API dependencies | 见下「查版本」 |
| 查语法 | 官方文档站 | 见下「查语法」 |
| 找真实代码示例 | gh search code | 见下「找示例」 |

### 查 API 签名

```bash
# Rust crate 文档（docs.rs 自动生成，最全）
curl -sL "https://docs.rs/{crate}/{version}/{crate}/" -H "User-Agent: Mozilla/5.0"

# 官方文档站
curl -sL "https://doc.rust-lang.org/std/"                       # Rust std
curl -sL "https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Promise"  # JS
curl -sL "https://docs.python.org/3/library/{module}.html"       # Python
curl -sL "https://nodejs.org/api/{module}.html"                  # Node
curl -sL "https://pkg.go.dev/{pkg}"                              # Go

# 不确定 API 是否存在 → 直接读源码确认签名（evidence-based-proposal 原则）
gh repo view {owner}/{repo} --json url
gh api repos/{owner}/{repo}/contents/src/lib.rs
```

### 查报错含义

```bash
# Rust 错误码（官方解释）
curl -sL "https://doc.rust-lang.org/error_codes/E0432.html"

# 报错的社区解法（SE API，不反爬）
curl -s "https://api.stackexchange.com/2.3/search?order=desc&sort=votes&intitle={错误关键词}&site=stackoverflow" \
  -H "Accept: application/json" | jq '.items[:3] | .[] | {title, link, score}'

# 找别人怎么处理同样报错
gh search code "{错误码或错误关键词}" --limit 5
```

### 查库（用法/是否好用）

```bash
# crates.io
curl -s "https://crates.io/api/v1/crates/{crate}" -H "User-Agent: agent" | jq '.crate | {description, downloads, recent_version}'

# npm
curl -s "https://registry.npmjs.org/{pkg}/latest" | jq '{name, version, dependencies}'

# GitHub README（最真实的用法）
gh repo view {owner}/{repo}
gh api repos/{owner}/{repo}/readme --jq '.content' | base64 -d
```

### 查版本兼容

```bash
# 包的所有版本
curl -s "https://crates.io/api/v1/crates/{crate}" | jq '.versions[] | {num, yanked, created_at}' | head -20
curl -s "https://registry.npmjs.org/{pkg}" | jq '.versions | keys'

# 依赖关系（看是否锁了某个大版本）
curl -s "https://crates.io/api/v1/crates/{crate}/{version}/dependencies" | jq '.[] | {crate_id, req}'
```

### 找真实代码示例

```bash
# 搜代码（gh 已认证，最准）
gh search code "{API或模式}" --language {lang} --limit 10

# 搜相关项目
gh search repos "{关键词}" --language {lang} --sort stars --limit 5
```

## 降级链

```
问题
  ├─ 有官方文档站/包仓库 → curl 直查（最准）
  │   拿到答案 → inline 返回，结束
  ├─ 官方没讲清/要实战经验 → SE API + gh search code
  │   拿到答案 → inline 返回，结束
  ├─ 都没命中 → web-search-bridge（需配置搜索 API key）
  │   定向源查不到的非技术内容走通用搜索桥接
  └─ web-search-bridge 不可用或仍无 → 诚实说"本机未查到可靠答案"
      建议用户：给出可手动验证的关键词，或说明需要联网检索环境
```

**禁止的无效尝试**（本机确定失败，别浪费时间）：
- ❌ `web_search` / `web_fetch` / `browser`（pi 无此内置工具）
- ❌ curl 百度搜技术词（验证码墙）
- ❌ curl Stack Overflow 网页版（403）
- ❌ curl r.jina.ai（超时）
- ❌ curl Google/Bing/DuckDuckGo/SearX（本机超时，走 web-search-bridge 的 API 桥接）

## 输出格式

直接 inline 返回答案，**不写报告文件、不建 run_dir**：

```
【查到】{API/报错/库} —— {一句话答案}

{关键签名 / 解法 / 用法代码片段}

来源：
- [官方文档](URL)
- [SE 高票答案](URL)（如用到）
```

搜不到就说"本机未查到，建议关键词：X"，不编造。

## 上限纪律

- **≤5 次查询**：拿到答案立即停，不无限深挖（那是 research-workflow 的事）
- **拿到就用**：开发期检索是为了写代码，确认了就回去写，不要陷进去做综述
- **不产报告**：结果直接给用户，不落 map.md/report.md
- **引用必带 URL**：每条事实附来源，方便用户复核

## Gotchas（高信号）

- **gh 优先于 curl 抓 github**：gh 已认证（5000/h）且结构化，curl 抓 github 页面拿到的是 HTML 壳
- **JS 渲染源直接放弃**：Twitter/小红书/LinkedIn 在本机不可采（Jina 超时、无 browser），不要用 curl 硬抓 JS 壳，找替代源（官方公告/新闻转载/学术镜像）
- **百度搜技术英文词会被验证码墙**：技术检索别用百度，走官方文档站/gh/SE API
- **Stack Overflow 网页版 403**：用 SE API（api.stackexchange.com）不反爬，不要抓网页
- **正文 <500 字符视为失败**：curl 抓到反爬页/验证页常返 200 但内容空，看长度不看状态码

## 与其他 skill 的分工

- 写代码前要确认环境/方案可行性 → **evidence-based-proposal**（方法论：先验证再提方案）
- 调研一个方向/竞品/技术趋势，要出报告 → **research-workflow**（多 agent 深度调研）
- 查数据/对比/进展/事实核实（需≥2源交叉但不出报告） → **fact-research**（轻量网络调研，5-20 次）
- 开发中卡在某个具体技术点，要快速查清 → **dev-lookup**（本 skill）
