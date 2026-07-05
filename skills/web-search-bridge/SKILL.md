---
name: web-search-bridge
description: "通用网络搜索桥接：当 dev-lookup/fact-research/research-workflow 的定向源（官方文档/包仓库/社区API）查不到、需要通用搜索引擎时调用。Use when: 定向源查不到某非技术事实、需要全网搜索、要桥接 Tavily/Serper/Exa/Brave Search API 时、其他调研 skill 降级链末位、说\"全网搜一下\"\"Google 一下\"时。SKIP: 技术问题（dev-lookup）、轻量事实交叉（fact-research）、深度调研报告（research-workflow）、本机已可用的定向源（直接用）。"
metadata:
  pattern: routing + fallback
  domain: search
  tier: bridge
---

# Web Search Bridge — 通用网络搜索桥接

本机**无可用通用搜索引擎**（Google/Bing/DuckDuckGo/SearX 均超时或质量差），dev-lookup / fact-research / research-workflow 只能走定向源。本 skill 桥接付费搜索 API（Tavily/Serper/Exa/Brave），提供统一 `web_search(query)` 接口，作为三个调研 skill 的**降级链末位**。

## 本机检索能力分层

```
通用网络搜索（本 skill，需 API key）
  ↑ 降级
深度调研（research-workflow，多 worker + 报告）
  ↑ 升级
轻量事实交叉（fact-research，HN/SE/媒体定向源）
  ↑ 升级
单点技术检索（dev-lookup，文档站/包仓库/SE）
```

**本 skill 不是默认检索入口**——是当定向源不够时的补充桥。优先用 dev-lookup / fact-research，它们已覆盖大多数场景且零成本。

## When to Use

- 其他调研 skill 在定向源查不到，明确需要通用搜索
- 查非技术的具体事实（某公司最新动态、某人观点、某事件时间线）
- 用户明示"全网搜""Google 一下"
- fact-research / research-workflow 的降级链走到了末位

## SKIP（优先用更轻的）

- API/报错/库用法 → **dev-lookup**（官方文档站直查更准）
- 数据/对比/进展，HN/SE/arXiv 能查到 → **fact-research**（零成本）
- 深度调研出报告 → **research-workflow**

## Provider 路由（按可用性和质量）

按以下顺序尝试，第一个可用的 provider 即用。**每个 provider 的环境变量检查通过才用，否则跳到下一个**：

| 优先级 | Provider | 环境变量 | 特点 | 适用 |
|---|---|---|---|---|
| 1 | **Tavily** | `TAVILY_API_KEY` | AI 优化，返回摘要+来源，LLM 友好 | 通用首选 |
| 2 | **Serper** | `SERPER_API_KEY` | Google 结果，质量高，便宜 | 需 Google 级结果 |
| 3 | **Brave Search** | `BRAVE_SEARCH_API_KEY` | 独立索引，隐私优先 | 去重/不同视角 |
| 4 | **Exa** | `EXA_API_KEY` | 语义搜索，找"类似内容"强 | 找相关/深度内容 |

**检查 provider 可用性**（每次调用前）：

```bash
for key_var in TAVILY_API_KEY SERPER_API_KEY BRAVE_SEARCH_API_KEY EXA_API_KEY; do
  val="${!key_var}"
  [ -n "$val" ] && echo "✅ $key_var 可用"
done
```

全空 → 本 skill 不可用，诚实告知用户"未配置搜索 API key，只能用定向源"。

## 额度管理（调用前预检）

各 provider 额度能力差异显著（实测）：

| Provider | 预检能力 | 说明 |
|---|---|---|
| **Tavily** | ✅ 月度额度 | `GET /usage` 返回 plan_usage/plan_limit，可预检月度余额 |
| **Serper** | ⚠️ 仅速率窗口 | 响应头 `x-ratelimit-*`（25req/s），无月度额度预检 |
| **Exa** | ❌ 无预检 | 无 usage API、响应头无额度，靠调用失败（429）被动应对 |

**调用前预检**（避免无谓消耗额度）：

```bash
# 检查所有可用 provider 的额度状态
bash scripts/web-search-quota.sh check all

# 只查某个 provider
bash scripts/web-search-quota.sh check tavily

# 看本地累计调用统计（避免重复查询额度本身消耗额度）
bash scripts/web-search-quota.sh status
```

**预检纪律**：
- **不是每次调用都预检**——预检本身有成本（Serper 预检要 1 次真实调用）
- **批量调用前预检**（research-workflow 的 worker 开工、撒网模式 10+ 次调用）：开工前跑一次 `check all`，优先选额度充裕的 provider，结果写进调研记录。research-workflow 的「采集前自检」环节已自动嵌入此步
- **单次降级查询不预检**（fact-research 偶尔用一次）：直接调，预检成本 > 价值
- Tavily 能预检月度余额，优先用它（额度低于 20% 自动警告）
- Serper 只能防瞬时超限（速率窗口），不防月度耗尽
- Exa 无预检能力，调用失败报 429 时才知超额——谨慎用
- 每次成功调用后 `record <provider>` 记录本地统计，避免重复查询额度 API

## 统一调用接口

所有 provider 封装成相同的输入输出：query 进、带引用的结果出。

### Tavily（首选）

```bash
curl -s --max-time 15 "https://api.tavily.com/search" \
  -H "Content-Type: application/json" \
  -d "$(jq -n --arg key \"$TAVILY_API_KEY\" --arg q \"$QUERY\" '{
    api_key: $key, query: $q, max_results: 5,
    include_answer: true, search_depth: "advanced"
  }')" | jq '{
    answer: .answer,
    results: [.results[]? | {title, url, content: (.content[:200])}]
  }'
```

**注意**：`search_depth: "advanced"` 是必要的——basic 模式结果少且对中文查询支持差（见 Gotchas）。body 用 `jq -n` 构造，避免 key/查询里的特殊字符破坏 JSON。

### Serper

```bash
curl -s --max-time 15 "https://google.serper.dev/search" \
  -H "X-API-KEY: $SERPER_API_KEY" \
  -H "Content-Type: application/json" \
  -d "{\"q\": \"$QUERY\", \"num\": 5}" \
  | jq '{
    organic: [.organic[]? | {title, link, snippet: (.snippet[:200])}],
    news: [.news[]? | {title, link, date}][:3],
    answer: (.knowledgeGraph.description // .answerBox.snippet // null)
  }'
```

### Brave Search

```bash
curl -s --max-time 15 "https://api.search.brave.com/res/v1/web/search?q=$(jq -rn --arg q "$QUERY" '$q|@uri')&count=5" \
  -H "Accept: application/json" \
  -H "X-Subscription-Token: $BRAVE_SEARCH_API_KEY" \
  | jq '{
    results: [.web.results[]? | {title, url, description: (.description[:200])}]
  }'
```

### Exa（语义搜索，找"类似内容"）

```bash
curl -s --max-time 15 "https://api.exa.ai/search" \
  -H "x-api-key: $EXA_API_KEY" \
  -H "Content-Type: application/json" \
  -d "{\"query\": \"$QUERY\", \"numResults\": 5, \"type\": \"neural\", \"contents\": {\"text\": {\"maxCharacters\": 200}}}" \
  | jq '{
    results: [.results[]? | {title, url, text: (.text[:200])}]
  }'
```

## 执行流程

1. **检查可用 provider**（环境变量）—— 全空则告知用户不可用
2. **额度预检**（重要）：`bash scripts/web-search-quota.sh check all`，优先选额度充裕的；Tavily 能查月度余额优先用，低于 20% 警告
3. **按优先级调第一个可用且额度充裕的**（Tavily > Serper > Brave > Exa）
4. **解析统一输出**：answer（如有）+ results（title/url/snippet）
5. **结果交叉**：关键事实仍需 ≥2 个结果来源（同 fact-research 纪律，API 返回的单源不等于交叉验证）
6. **带引用返回**：每条结果附 URL，可复核
7. **记录调用**（可选）：`bash scripts/web-search-quota.sh record <provider>` 累计本地统计

## 输出格式（与其他调研 skill 一致）

```
## 搜索: {query}

**即时答案**（如 provider 返回）：{answer}

### 结果
1. **{title}** — {snippet}
   {url}
2. **{title}** — {snippet}
   {url}
...

### 来源
[^1^]: {url} — {标题}
```

## 上限纪律

- **≤10 次 API 调用**：通用搜索结果到即停，不无限翻页
- **成本意识**：付费 API，别滥用。深度挖掘交给 research-workflow 的 worker
- **交叉验证**：API 返回的多结果是"同源"，关键事实仍需跨 provider 或跨定向源验证
- **降级诚实**：所有 provider 都空/失败，明确说"搜索 API 不可用"，不伪造结果

## Gotchas（高信号）

- **额度能力差异大**：Tavily 能预检月度额度（优先用），Serper 只能看速率窗口（防瞬时超限不防月耗尽），Exa 完全无预检（靠 429 被动应对）。长任务前跑 `check all` 预检
- **预检本身也消耗额度**：Serper 的额度检查需要一次真实调用读响应头，别频繁跑；用 `record` 维护本地统计减少预检次数
- **本 skill 是桥不是主入口**：优先 dev-lookup / fact-research 的定向源，它们零成本且更精准。本 skill 只在定向源不够时补
- **环境变量而非配置文件**：key 通过 `TAVILY_API_KEY` 等环境变量传入，不写死在脚本里（安全 + 多 provider 切换）
- **Windows 路径/key 转义**：curl -d 的 JSON 里 key 可能含特殊字符，用 jq 构造 JSON body 更安全（见各 provider 示例的 `jq -n`）
- **include_answer 优先取**：Tavily/Serper 的 `answer`/`answerBox` 是提炼答案，比翻 results 快
- **付费额度**：各 provider 有免费额度上限，超了会报错。调用失败先检查 quota（HTTP 429）
- **语义搜索 vs 关键词**：Exa 是 neural 搜索，适合"找类似 X 的内容"；找精确事实用 Tavily/Serper 的关键词搜索
- **结果≠事实**：搜索引擎返回的结果仍可能是过时/错误信息，关键事实按 fact-research 的交叉验证纪律处理
- **provider 失败降级**：第一个 provider 超时/报错，自动试下一个，不全空不放弃
- **中文查询用英文重试**（实测高频坑）：Tavily 对中文查询支持差——"OpenAI 最新融资 2025" 返回 0 结果，但 "OpenAI funding 2025" 返回完整答案。**中文查询查不到时，先期译成英文重试**。Serper/Exa 中文支持较好，Tavily 尤其要注意
- **Tavily search_depth 影响**：basic 模式下结果较少，深度查询加 `"search_depth":"advanced"`（成本略高但结果全）
- **错误 key 返回 401 不崩溃**：联调实测错误 key 返回 HTTP 401，provider 路由逻辑应捕获此状态试下一个 provider，不报告为脚本崩溃

## 与其他 skill 的分工（降级链）

```
dev-lookup（技术单点，定向源）
  └─ 查不到非技术内容 → fact-research
      └─ HN/SE/媒体定向源查不到 → web-search-bridge（本 skill）
          └─ 需深度多源报告 → research-workflow（多 worker 编排）
```

- **dev-lookup / fact-research**：本 skill 是它们的降级链末位，定向源查不到时才调用
- **research-workflow**：它的 worker 可在工序 3 调用本 skill 作为采集工具之一

## 配置检查（开工前一次性确认）

```bash
# 看 4 个 provider 哪些可用
echo "搜索 API 可用性："
[ -n "${TAVILY_API_KEY:-}" ] && echo "  ✅ Tavily" || echo "  ❌ Tavily (未配 TAVILY_API_KEY)"
[ -n "${SERPER_API_KEY:-}" ] && echo "  ✅ Serper" || echo "  ❌ Serper (未配 SERPER_API_KEY)"
[ -n "${BRAVE_SEARCH_API_KEY:-}" ] && echo "  ✅ Brave" || echo "  ❌ Brave (未配 BRAVE_SEARCH_API_KEY)"
[ -n "${EXA_API_KEY:-}" ] && echo "  ✅ Exa" || echo "  ❌ Exa (未配 EXA_API_KEY)"
```

任一可用即可使用本 skill。用户配置环境变量后即时生效（无需重启，skill 每次调用读环境变量）。

## 参考

- 额度预检脚本：[scripts/web-search-quota.sh](scripts/web-search-quota.sh)
  - `check [tavily|serper|exa|all]`：预检额度状态（Tavily 月度余额 / Serper 速率窗口 / Exa 无预检）
  - `status`：查看本地累计调用统计
  - `record <provider>`：记录一次调用（供调用脚本 source 使用）
