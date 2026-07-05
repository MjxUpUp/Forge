# Sourcing Toolkit — pi 环境采集工具路由

本文件是 `research-workflow` Phase 1 worker 采集原始素材时的工具路由参考。
方法论借鉴 agent-search（路由 + 降级 + 自检），但**全部基于 pi 本机实测可用性**重写——
pi 没有 web_fetch/web_search/browser 等内置工具，联网只能靠 bash curl。

## pi 本机基线（实测，非推测）

**工具现实**：pi 工具集 = bash / read / write / edit / deliver / subagent。**无** web_fetch、web_search、browser 等联网内置工具。

**网络现实**（WSL bash curl 实测）：
- ✅ 直连可达：baidu、github、arxiv、mp.weixin.qq.com 等常规静态/API 站点
- ❌ 不可达：`r.jina.ai`（Jina Reader，超时）、`reddit.com` JSON API（封锁，需代理）
- 本机已装可用：`gh`（已认证 5000/h）、`docker`、`jq`
- 本机未装：`yt-dlp`、`bird`、`mcporter`、`python3`、`feedparser`

**关键断点**：JS 重度渲染源（Twitter 时间线、小红书、LinkedIn、登录态页面）在 pi 本机**基本抓不了**——Jina Reader 挂了、无 browser 工具。worker 遇到这类源要**预先判定不可采**，不要浪费搜索配额硬抓，改走替代源（官方公告/新闻转载/学术镜像）。

## 采集前先自检（bash，幂等）

```bash
# 快速确认本机哪些采集通道活着（pi 适配，纯 bash + curl + command -v）
test_reach () {
  local url="$1" name="$2"
  local code; code=$(curl -s -o /dev/null -w "%{http_code}" -m 6 "$url" 2>/dev/null || echo 000)
  [ "$code" = "200" ] && echo "✅ $name ($code)" || echo "❌ $name ($code)"
}
test_reach "https://www.baidu.com"        "baidu(国内基线)"
test_reach "https://github.com"           "github(gh后端)"
test_reach "https://r.jina.ai/"           "jina(JS渲染)"
test_reach "https://www.reddit.com/r/test.json" "reddit"
command -v gh      >/dev/null && echo "✅ gh"      || echo "❌ gh"
command -v yt-dlp  >/dev/null && echo "✅ yt-dlp"  || echo "❌ yt-dlp(未装)"
command -v python3 >/dev/null && echo "✅ python3" || echo "❌ python3(未装)"

# === web-search-bridge 额度预检（批量调研前必跑，避免跑到一半额度耗尽）===
# 定向源不够时会用 web-search-bridge（付费搜索 API），批量调用前预检额度
# Tavily 能预检月度额度（优先看）；Serper 只能看速率窗口；Exa 无法预检
QUOTA_SCRIPT="$HOME/.claude/skills/web-search-bridge/scripts/web-search-quota.sh"
if [ -f "$QUOTA_SCRIPT" ]; then
  echo "--- web-search-bridge 额度 ---"
  bash "$QUOTA_SCRIPT" check all 2>&1 | grep -E 'TAVILY|SERPER|EXA'
fi
```

把结果记进 `{run_dir}/map.md` 顶部「采集通道状态」，后续 worker 按此选工具，不重复探测。

**额度预检的使用决策**（避免预检本身浪费额度）：
- **批量调研**（撒网≥10、锁定≥6 worker、文件增强）：开工前跑一次预检，写到 map.md。成本低（1 次）价值高（避免中途额度耗尽）
- **单次降级查询**（fact-research 偶尔用一次 web-search-bridge）：**不预检**，直接调（预检成本 > 价值）
- 预检发现 Tavily 月度额度 <20% → worker 优先走 Serper/Exa，或在 map.md 标注「web-search-bridge 额度紧张，优先用定向源」

## 按内容类型选采集方式（pi 路由表）

| 内容类型 | 首选方式 | 本机可用 | 备注 |
|---|---|---|---|
| 已知 URL 的静态页/文档 | `curl -sL URL -H "User-Agent: Mozilla/5.0"` | ✅ | 加 `-L` 跟跳转；正文 >500 字符才算成功 |
| GitHub repo/issue/code | `gh search repos/code`、`gh repo view`、`gh api` | ✅ | **已认证，最可靠**。优先用 gh 而非 curl 抓 github |
| arXiv / 学术静态页 | curl 直连 | ✅ | arxiv 本机可达 |
| 微信公众号文章 | curl 直连 | ✅ | mp.weixin.qq.com 可达，但正文需从 HTML 抽 |
| 纯文本搜索（找链接） | curl 抓搜索引擎结果页 | ⚠️ | baidu/bing 结果页反爬强，质量差；优先用 gh/官方站搜索 |
| **通用网络搜索**（定向源不够时） | **web-search-bridge skill**（Tavily/Serper/Exa） | ✅需key | 付费 API；**批量调研前预检额度**（见上「采集前先自检」） |
| JS 渲染页（Twitter/小红书/LinkedIn） | （Jina 挂、无 browser） | ❌ | **本机不可采**，改走新闻转载/官方公告/学术镜像；Exa 语义搜索可作部分替代 |
| Reddit | curl JSON API | ❌ | 本机封锁；要代理或换 Exa |
| YouTube/B站字幕 | `yt-dlp --write-auto-sub` | ❌未装 | `pip install yt-dlp` 后可用 |
| RSS | `python3 + feedparser` | ❌未装 | 装后可用；无 python3 时 curl 抓 feed XML + jq 粗解析 |

## 降级链（pi 版，不同于 agent-search 的四层）

pi 没有 web_fetch/browser，降级链只有两层 + 一个明确终止点：

```
目标 URL
  ├─ 是 github → 直接 gh（最可靠，绕过 curl）
  ├─ 其他 → Layer 1: curl -sL + UA
  │         成功标志: 正文 >500 字符 且 非反爬/验证页
  │         失败(000/403/空白/JS壳) →
  └─ Layer 2: 换替代源（官方公告 / 新闻转载 / 学术镜像 / gh 上的转载）
              仍失败 → 标注「本机不可采」记入 map.md，不硬抓
```

**禁止的无效尝试**（pi 下确定失败，别浪费时间）：
- ❌ 调用 web_fetch / web_search / browser（pi 无此工具）
- ❌ curl `r.jina.ai`（本机超时）
- ❌ curl `reddit.com` JSON（本机封锁）

## 域名可达性表（实测维护）

worker 抓到新域名时，把结果补进这张表，供后续 worker 复用：

| 域名 | curl 直连 | 备注 |
|---|---|---|
| baidu.com | ✅ | 国内基线 |
| github.com | ✅ | 但优先用 gh CLI |
| arxiv.org | ✅ | 静态学术页 |
| mp.weixin.qq.com | ✅ | 公众号 |
| r.jina.ai | ❌ | JS 渲染主力，本机超时 |
| reddit.com | ❌ | 封锁 |
| x.com / twitter.com | ❌ | 需 JS 渲染 + Cookie |
| xiaohongshu.com | ❌ | 需 MCP/Docker |
| linkedin.com | ❌ | 需 Jina（挂）或登录 |

## 给 worker 的采集纪律

1. **先看 map.md 顶部「采集通道状态」**，按已知可用通道选工具，不重复探测
2. **gh 优先于 curl 抓 github**——gh 已认证、结构化、5000/h
3. **JS 渲染源直接放弃 + 找替代**，不要用 curl 硬抓 JS 壳（拿到的只是空模板）
4. **curl 必带 `-sL` + `User-Agent`**，正文 <500 字符视为失败
5. **每条 finding 的 URL 必须实测可达**，不可达的源不能作为 `[^N^]` 引用
