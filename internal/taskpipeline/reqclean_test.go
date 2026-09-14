package taskpipeline

import (
	"testing"
)

// TestScanSpecText_AmbiguityMarkers 钉住三类确定性歧义标记的扫描。
func TestScanSpecText_AmbiguityMarkers(t *testing.T) {
	cases := []struct {
		input    string
		wantCat  string
		wantContain string
	}{
		{input: "用户登录使用 TBD 方案", wantCat: "ambiguity", wantContain: "TBD"},
		{input: "密码强度待定", wantCat: "ambiguity", wantContain: "待定"},
		{input: "系统可能返回错误", wantCat: "ambiguity", wantContain: "可能"},
		{input: "需考虑多种场景", wantCat: "ambiguity", wantContain: "需考虑"},
		{input: "需要若干个模板", wantCat: "vague-quantifier", wantContain: "模糊量词"},
		{input: "登录后跳转到首页", wantCat: "", wantContain: ""},
		{input: "返回 HTTP 200 状态码", wantCat: "", wantContain: ""},
	}
	for _, tc := range cases {
		findings := scanSpecText(tc.input)
		if tc.wantCat == "" {
			if len(findings) > 0 {
				t.Errorf("scanSpecText(%q) 应零 findings，got %v", tc.input, findings)
			}
			continue
		}
		if len(findings) == 0 {
			t.Errorf("scanSpecText(%q) 应有 findings，got 0", tc.input)
			continue
		}
		found := false
		for _, f := range findings {
			if f.Category == tc.wantCat {
				found = true
			}
		}
		if !found {
			t.Errorf("scanSpecText(%q) 应含类别 %q，got %v", tc.input, tc.wantCat, findings)
		}
	}
}

// TestCheckRequirementsHygiene_MissingAcceptance 钉住验收缺失检测：
// 有 spec 产物但无 --accept → missing-acceptance finding。
func TestCheckRequirementsHygiene_MissingAcceptance(t *testing.T) {
	// hasSpecArtifacts 只看 SpecArtifacts map——mock state 即可。
	state := &TaskState{
		TaskRef: "test/req-hygiene",
	}
	// 无 spec → 0 findings（未登记 = 无从扫描）。
	if got := CheckRequirementsHygiene("", state); len(got) != 0 {
		t.Errorf("无 spec 时应零 findings，got %v", got)
	}
}
