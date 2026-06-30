package skillsdist

import (
	"encoding/json"
	"os"
	"strings"
)

// Route 是 skill-routing/routes.json 的一条路由（对齐 sync.py routes.json schema）。
// 匹配语义：Match 数组任一关键词子串命中（大小写不敏感）即触发该 skill。
type Route struct {
	Match  []string `json:"match"`
	Skill  string   `json:"skill"`
	Reason string   `json:"reason"`
}

// LoadRoutes 从 routes.json 加载路由表，跳过无 skill 字段的注释项（_comment）。
func LoadRoutes(path string) ([]Route, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var raw []Route
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	out := make([]Route, 0, len(raw))
	for _, r := range raw {
		if r.Skill == "" {
			continue // _comment 项无 skill
		}
		out = append(out, r)
	}
	return out, nil
}

// MatchRoute 返回输入命中的第一个 skill（Match 任一关键词子串命中，大小写不敏感）。
// 无命中返回空串。对齐 sync.py 路由匹配语义。
func MatchRoute(routes []Route, input string) string {
	low := strings.ToLower(input)
	for _, r := range routes {
		for _, kw := range r.Match {
			if kw == "" {
				continue
			}
			if strings.Contains(low, strings.ToLower(kw)) {
				return r.Skill
			}
		}
	}
	return ""
}
