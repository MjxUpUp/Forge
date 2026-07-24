package taskpipeline

import (
	"net/url"
	"strings"
)

// ParseExternalOriginURL 从外部 issue tracker URL 解析 ExternalOrigin。支持 linear / github；
// 未识别 tracker 仅保留 URL（不强依赖 tracker 类型）——spawn 式编排器（Symphony 类）传任意
// issue URL，forge 尽力提取标识，解耦 mount 式 task origin 与 spawn 式 issue origin。
// 这是 proof-of-work 闭环衔接 spawn 式编排器的关键：task.ExternalOrigin 让 task 天然关联
// 外部 issue，编排器拉起 run 时不必靠 branch 名推断 issue。
func ParseExternalOriginURL(rawURL string) ExternalOrigin {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return ExternalOrigin{}
	}
	o := ExternalOrigin{URL: rawURL}
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return o // 非 URL：仅保留原串，不推断 tracker
	}
	segs := splitPathSegments(u.Path)
	host := strings.ToLower(u.Host)

	switch {
	case strings.HasSuffix(host, "linear.app"):
		// linear.app/<team>/issue/<IDENTIFIER>（IDENTIFIER 形如 ABC-123）
		o.Tracker = "linear"
		if i := indexOfSeg(segs, "issue"); i >= 0 && i+1 < len(segs) {
			o.Identifier = segs[i+1]
			o.IssueID = o.Identifier
		}
	case host == "github.com":
		// github.com/<org>/<repo>/issues/<num>
		o.Tracker = "github"
		if i := indexOfSeg(segs, "issues"); i >= 0 && i+1 < len(segs) {
			o.IssueID = segs[i+1]
			if i >= 2 { // i-2=org, i-1=repo
				o.Identifier = segs[i-2] + "/" + segs[i-1] + "#" + segs[i+1]
			}
		}
	}
	return o
}

// splitPathSegments 把 URL path 按 "/" 切，去掉空段（首尾 / 双斜杠产生空段）。
func splitPathSegments(p string) []string {
	var out []string
	for _, s := range strings.Split(strings.Trim(p, "/"), "/") {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// indexOfSeg 返回 seg 在 segs 中的下标，不存在返 -1。
func indexOfSeg(segs []string, seg string) int {
	for i, s := range segs {
		if s == seg {
			return i
		}
	}
	return -1
}
