package taskpipeline

import "testing"

// TestParseExternalOriginURL 锁定外部 issue URL → ExternalOrigin 解析。linear/github 两类
// 是 proof-of-work 闭环衔接 spawn 式编排器（Symphony）的关键入口：编排器把 issue URL 传给
// forge_task_start --from_issue，解析结果锚定 task 到外部 issue。解析漂移会让 task origin
// 静默丢失，编排器无法按 issue 追踪 task。覆盖已知 tracker / 非 URL / 未知 tracker 退化。
func TestParseExternalOriginURL(t *testing.T) {
	cases := []struct {
		name string
		url  string
		want ExternalOrigin
	}{
		{
			name: "linear 完整路径",
			url:  "https://linear.app/forge/issue/ABC-123",
			want: ExternalOrigin{Tracker: "linear", IssueID: "ABC-123", Identifier: "ABC-123", URL: "https://linear.app/forge/issue/ABC-123"},
		},
		{
			name: "github org/repo/issues",
			url:  "https://github.com/MjxUpUp/Forge/issues/42",
			want: ExternalOrigin{Tracker: "github", IssueID: "42", Identifier: "MjxUpUp/Forge#42", URL: "https://github.com/MjxUpUp/Forge/issues/42"},
		},
		{name: "空串返零值", url: "", want: ExternalOrigin{}},
		{name: "非 URL 仅保留原串", url: "plain-string", want: ExternalOrigin{URL: "plain-string"}},
		{name: "未知 tracker 仅保留 URL", url: "https://jira.example.com/browse/ABC-123", want: ExternalOrigin{URL: "https://jira.example.com/browse/ABC-123"}},
		{name: "linear 无 issue 段不崩", url: "https://linear.app/forge", want: ExternalOrigin{Tracker: "linear", URL: "https://linear.app/forge"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ParseExternalOriginURL(c.url)
			if got != c.want {
				t.Errorf("ParseExternalOriginURL(%q) = %+v, want %+v", c.url, got, c.want)
			}
		})
	}
}
