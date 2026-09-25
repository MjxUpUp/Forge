# curl 定向源通道状态（环境实测记录）

> ⚠️ **特定网络环境实测，非通用结论**：以下通道状态表是作者在特定国内网络环境下的实测结果，你的网络环境（地区/运营商/代理）可能完全不同。**开工前先逐条自检连通性，以自检结果为准**，不要照搬表内 ✅/❌ 结论。

## 自检方法

逐条对目标通道发一次小请求，看返回内容长度与结构（不看状态码——反爬页常返 200 但内容空）：

```bash
# 示例：自检 HN API（拿到 JSON 即通）
curl -s --max-time 10 "https://hn.algolia.com/api/v1/search?query=test&tags=story" | head -c 200

# 示例：自检某文档站（正文 <500 字符视为失败）
curl -sL --max-time 10 "https://developer.mozilla.org/en-US/docs/Web/JavaScript" | wc -c
```

把自检结论写进当前工作环境记录（如 run_dir/map.md 顶部），后续按自检后的路由选通道。

## 通用事实调研通道（research-workflow Phase L 用）

| 通道 | 状态 | 适用 |
|---|---|---|
| HackerNews Algolia API | ✅ 通 | 技术新闻/融资/产品发布/社区讨论 |
| GitHub API（repos/issues/search code） | ✅ 通 | 公司动态/项目活跃度/真实代码示例 |
| Stack Exchange API | ✅ 通 | 技术社区高票问答/对比经验 |
| arXiv API | ✅ 通（http→https 重定向） | 学术/技术进展/论文 |
| techcrunch / theverge / 主流科技媒体 | ✅ 可直 curl | 行业新闻/融资/产品（需解析 HTML） |
| Bing 搜索结果页 | ⚠️ 302/质量差 | 兜底，需大量二次筛选 |
| DuckDuckGo / Google / Wikipedia / SearX | ❌ 超时/reset | 该环境下不可用，自检后再定 |
| Jina Reader / JS 渲染源 | ❌ 超时 | 该环境下不可用，自检后再定 |

**核心纪律**：走 HN + GitHub + SE + arXiv + 主流媒体直 curl 这几条定向路。查不到就诚实说"本机未查到"，不编造。

## 技术单点检索通道（dev-lookup 用）

| 通道 | 状态 | 用途 |
|---|---|---|
| 官方文档站 curl（MDN/python/node/react/docs.rs/go/k8s） | ✅ 通常通 | 查 API/语法/错误码 |
| `gh` CLI（认证后 5000/h） | ✅（需装+认证） | 找代码示例/issues/README |
| 包仓库 API（crates.io / npm registry） | ✅ 通常通 | 查包元数据/版本/依赖 |
| Stack Exchange API（api.stackexchange.com） | ✅ 通常通 | 查报错的社区解法 |
| Bing/百度 搜索结果页 | ⚠️ 反爬/验证码墙，质量差 | 仅作兜底 |
| Stack Overflow 网页版 | ❌ 常返 403 | 用 SE API 替代 |
| Jina Reader / JS 渲染源 | ❌ 常超时 | 有 browser 工具的 agent 可直接渲染抓 |

**核心纪律**：走官方文档站 + gh + 包仓库 API + SE API 这四条主路，它们精准可靠。有内置 `web_search` 的 agent 优先用内置搜索补充；curl agent 别碰 SO 网页/Jina/通用搜索引擎结果页。
