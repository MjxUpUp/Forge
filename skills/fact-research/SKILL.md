---
name: fact-research
description: "网络事实轻量调研：查数据/对比/进展/最新动态，多源交叉验证后给带引用的答案。Use when: 查市场份额、融资金额、产品对比（A vs B 怎么选）、技术最新进展、某事实核实、某公司动态、某数据来源时——需要≥2源交叉但不到深度报告量级。SKIP: 单点技术问题（API 签名/报错含义/库用法/版本兼容→dev-lookup）、需出深度调研报告+飞书发布（→research-workflow）、开发调试中卡在具体技术点（→dev-lookup）、提技术方案前环境验证（→evidence-based-proposal）。"
metadata:
  pattern: routing + fallback
  domain: research
  tier: lightweight
---

# Fact Research — 网络事实轻量调研

查一个事实 / 数据 / 对比 / 最新进展，需要多源交叉验证，但不值得出深度报告。**定向源检索 + ≥2 源交叉 + inline 带引用答案，不建 run_dir、不 spawn worker、不发飞书**。

## 三层调研量级（路由依据）

| | dev-lookup | **fact-research（本 skill）** | research-workflow |
|---|---|---|---|
| 场景 | API/报错/库用法 | 数据/对比/进展/事实核实 | 方向/竞品/趋势深度调研 |
| 查询次数 | ≤5 | 5-20 | ≥60 |
| 多源交叉 | 不做 | **≥2 源撞同一事实才采信** | ≥6 worker 四档信度 |
| 产出 | inline 一句话 | inline 结构化带引用 | run_dir 报告 + 飞书发布 |
| 耗时 | 即时 | 几分钟 | 多轮多 agent |

**判断该用哪层**：
- 查"这个 API 怎么调" → dev-lookup
- 查"React 和 Vue 2025 市场份额谁高" → **fact-research**
- 查"前端框架市场格局深度分析，要发报告" → research-workflow

## 本机检索能力边界（实测，必须遵守）

pi 无 web_search/web_fetch/browser 内置工具，联网只能 bash curl。**本机无免费可用通用搜索引擎**（Google/Bing/DuckDuckGo 均超时或质量差）。默认走**定向源**；定向源不够时可桥接付费搜索 API（**web-search-bridge** skill，需配 key）：

| 通道 | 状态 | 适用 |
|---|---|---|
| HackerNews Algolia API | ✅ 通 | 技术新闻/融资/产品发布/社区讨论 |
| GitHub API（repos/issues/search code） | ✅ 通 | 公司动态/项目活跃度/真实代码示例 |
| Stack Exchange API | ✅ 通 | 技术社区高票问答/对比经验 |
| arXiv API | ✅ 通（http→https 重定向） | 学术/技术进展/论文 |
| techcrunch / theverge / 主流科技媒体 | ✅ 可直 curl | 行业新闻/融资/产品（需解析 HTML） |
| Bing 搜索结果页 | ⚠️ 302/质量差 | 兜底，需大量二次筛选 |
| DuckDuckGo / Google / Wikipedia / SearX | ❌ 超时/reset | 别用 |
| Jina Reader / JS 渲染源 | ❌ 超时 | 别用 |

**核心纪律**：走 HN + GitHub + SE + arXiv + 主流媒体直 curl 这几条定向路。查不到就诚实说"本机未查到"，不编造。

## 按问题类型选源

| 问题类型 | 首选源 | 命令模板 |
|---|---|---|
| 融资/估值/公司动态 | HN + 科技媒体直 curl | 见下「查公司动态」 |
| 产品/技术对比 | SE API + GitHub + HN | 见下「查对比」 |
| 市场份额/数据 | 科技媒体 + HN + 官方报告 | 见下「查数据」 |
| 技术最新进展 | arXiv + HN + GitHub trending | 见下「查进展」 |
| 事实核实/定义 | 多源撞（HN+SE+媒体） | 见下「核实事实」 |

### 查公司动态/融资

```bash
# HackerNews 搜相关新闻和讨论（融资/发布常上 HN）
curl -s "https://hn.algolia.com/api/v1/search?query={公司}+{事件}&tags=story" \
  -H "Accept: application/json" | jq '.hits[:5] | .[] | {title, url, points, created_at}'

# GitHub 看官方 repo 活跃度（最新 release/commit）
curl -s "https://api.github.com/repos/{owner}/{repo}/releases/latest" | jq '{tag_name, published_at, name}'
gh api repos/{owner}/{repo} --jq '.stargazers_count, .pushed_at'

# 科技媒体直 curl（拿标题和摘要）
curl -sL "https://techcrunch.com/?s={公司}" -H "User-Agent: Mozilla/5.0" | grep -oE '<h[23][^>]*>.*?</h[23]>' | head -5
```

### 查产品/技术对比

```bash
# Stack Exchange 高票对比问答（A vs B 经验帖）
curl -s "https://api.stackexchange.com/2.3/search/advanced?order=desc&sort=votes&q={A}+{B}&site=stackoverflow&pagesize=5" \
  -H "Accept: application/json" | jq '.items[:3] | .[] | {title, score, link}'

# GitHub star/活跃度对比
curl -s "https://api.github.com/repos/{A_owner}/{A_repo}" | jq '{stars: .stargazers_count, pushed: .pushed_at, open_issues}'
curl -s "https://api.github.com/repos/{B_owner}/{B_repo}" | jq '{stars: .stargazers_count, pushed: .pushed_at, open_issues}'

# HN 上的对比讨论
curl -s "https://hn.algolia.com/api/v1/search?query={A}+vs+{B}" | jq '.hits[:3] | .[] | {title, points}'
```

### 查技术最新进展

```bash
# arXiv 最新论文
curl -sL "https://export.arxiv.org/api/query?search_query=all:{关键词}&max_results=5&sortBy=submittedDate&sortOrder=descending" \
  | grep -oE '<title>[^<]+</title>' | head -10

# HN 最近热议
curl -s "https://hn.algolia.com/api/v1/search_by_date?query={关键词}&tags=story&numericFilters=created_at_i>$(($(date +%s)-2592000))" \
  | jq '.hits[:5] | .[] | {title, points, url}'

# GitHub 最近高星新项目
gh search repos "{关键词}" --sort stars --created ">2025-01-01" --limit 5
```

## 多源交叉验证纪律（核心，区别于 dev-lookup）

**单源不采信，≥2 源撞同一事实才写入答案。**

- 每条关键事实必须标注 ≥2 个独立来源
- 来源冲突 → 明示冲突 + 各源说法，不强行统一
- 找不到第二源 → 标"单一来源，未交叉验证"
- 数字/日期/金额类事实尤其要交叉（易错、易过期）

## 输出格式（inline，不建文件）

```
## {问题}

**结论**：{一句话答案}

### 关键事实
- {事实1} [^1^][^2^]
- {事实2} [^3^][^4^]

### 对比/细节（如适用）
| 维度 | A | B |
|---|---|---|
| ... | ... | ... |

### 注意/时效
- 数据截至 {时间}，{后续可能变化}

来源：
[^1^]: {URL} — {作者/机构, 日期}
[^2^]: {URL} — {作者/机构, 日期}
```

搜不到就诚实说"本机定向源未查到可靠答案"，给可手动验证的关键词，**不编造**。若已配置搜索 API（web-search-bridge），可降级到通用搜索再试一次。

## 上限纪律

- **≤20 次查询**：拿到交叉验证的答案即停，不无限深挖（深挖是 research-workflow 的事）
- **≥2 源**：核心事实必须交叉，单源不采信
- **不产报告**：结果 inline 给用户，不落 run_dir、不发飞书
- **引用必带 URL + 日期**：每条事实可复核

## Gotchas（高信号）

- **本机无免费通用搜索引擎**：Google/Bing/DDG/SearX 都不可用或质量差。别浪费时间试 curl 通用搜索，直接走定向源（HN/GitHub/SE/arXiv/媒体直 curl）。定向源不够时走 **web-search-bridge**（付费 API 桥接，需配 key）
- **Wikipedia 本机超时**：别依赖它查定义，改用多源撞（HN+SE+媒体）
- **数字/日期必交叉**：融资额、市场份额、版本号这类最易过期和出错，≥2 源验证
- **时效性标注**：市场数据/融资动态时间敏感，答案必带"截至 YYYY-MM"，提示用户可能已变化
- **GitHub star ≠ 全部**：star 数反映热度不反映质量，对比时配合活跃度（pushed_at/issues）
- **科技媒体 HTML 解析易碎**：curl 直抓媒体页面靠 grep 标题，改版就失效；优先用 API 类源（HN/SE/arXiv）
- **冲突即信号**：两源数字矛盾不要取平均或挑一个，明示冲突让用户判断

## 与其他 skill 的分工

- **dev-lookup**：单点技术检索（API/报错/库），不交叉验证，≤5 次。本 skill 处理"事实/对比/进展"，需交叉，5-20 次
- **research-workflow**：深度调研出报告 + 飞书发布，≥60 次多 worker。本 skill 不出报告
- **evidence-based-proposal**：提方案前的环境验证（本机能力/API 实测），本 skill 查外部事实
- **lark-***：飞书操作。本 skill 结果 inline 给用户，不发飞书

## 路由信号速查

| 用户说 | 去哪 |
|---|---|
| "这个 API 怎么调/报错啥意思/库怎么用" | dev-lookup |
| "X 和 Y 怎么选/X 最新进展/X 市场份额/X 融了多少" | **fact-research（本 skill）** |
| "深度调研 X 方向，要出报告" | research-workflow |
| "这个方案可行吗（本机环境）" | evidence-based-proposal |
