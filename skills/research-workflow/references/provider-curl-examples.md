# Provider curl 调用示例

四个 provider 的统一调用封装：query 进、带引用的结果出。body 一律用 `jq -n` 构造，避免 key/查询里的特殊字符破坏 JSON（Windows 下尤其重要）。

## Tavily（首选）

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

**注意**：`search_depth: "advanced"` 是必要的——basic 模式结果少且对中文查询支持差（见 SKILL.md Gotchas）。

## Serper

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

## Brave Search

```bash
curl -s --max-time 15 "https://api.search.brave.com/res/v1/web/search?q=$(jq -rn --arg q "$QUERY" '$q|@uri')&count=5" \
  -H "Accept: application/json" \
  -H "X-Subscription-Token: $BRAVE_SEARCH_API_KEY" \
  | jq '{
    results: [.web.results[]? | {title, url, description: (.description[:200])}]
  }'
```

## Exa（语义搜索，找"类似内容"）

```bash
curl -s --max-time 15 "https://api.exa.ai/search" \
  -H "x-api-key: $EXA_API_KEY" \
  -H "Content-Type: application/json" \
  -d "{\"query\": \"$QUERY\", \"numResults\": 5, \"type\": \"neural\", \"contents\": {\"text\": {\"maxCharacters\": 200}}}" \
  | jq '{
    results: [.results[]? | {title, url, text: (.text[:200])}]
  }'
```
