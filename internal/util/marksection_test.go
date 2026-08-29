package util

import "testing"

// TestReplaceMarkedSection pins the upsert contract shared by claudemd.go and
// windsurf.go: in-place replacement between the markers, user content preserved on
// both sides, append when markers are absent or inverted.
func TestReplaceMarkedSection(t *testing.T) {
	const start = "<!-- FORGE:START -->"
	const end = "<!-- FORGE:END -->"
	section := start + "\n\n# new\n\n" + end + "\n"

	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "replace between markers preserves user content",
			content: "# head\n\n" + start + "\nSTALE\n" + end + "\n\ntail notes\n",
			want:    "# head\n\n" + start + "\n\n# new\n\n" + end + "\n\ntail notes\n",
		},
		{
			name:    "append when no markers",
			content: "# only user content\n",
			want:    "# only user content\n\n" + section,
		},
		{
			name:    "append when only start marker",
			content: "# doc\n" + start + "\n",
			want:    "# doc\n" + start + "\n\n" + section,
		},
		{
			name:    "append when markers inverted",
			content: end + "\nmiddle\n" + start + "\n",
			want:    end + "\nmiddle\n" + start + "\n\n" + section,
		},
		{
			name:    "replace whole file that is only the section",
			content: start + "\nOLD\n" + end,
			want:    start + "\n\n# new\n\n" + end + "\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ReplaceMarkedSection(tt.content, section, start, end); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// TestStripMarkedSection pins the inverse contract of ReplaceMarkedSection: removal between the markers with seam normalization, identity when markers are absent or inverted, and the empty/one-sided edge cases.
//
// TestStripMarkedSection 钉住 ReplaceMarkedSection 的逆契约：标记间移除 +
// 接缝规整、标记缺失或颠倒时原样返回、空/单侧的边界。StripMarkedSection
// 对 conventions 指纹是承重的（先剥离再哈希，forge 升级才不会翻转仓库档案
// 的过期状态）。
func TestStripMarkedSection(t *testing.T) {
	const start = "<!-- FORGE:START -->"
	const end = "<!-- FORGE:END -->"

	tests := []struct {
		name    string
		content string
		want    string
	}{
		{"两侧都有内容", "# head\n\n" + start + "\nSTALE\n" + end + "\n\ntail\n", "# head\n\ntail\n\n"},
		{"只有标记前内容", "# head\n" + start + "\nSTALE\n" + end + "\n", "# head\n"},
		{"只有标记后内容", start + "\nSTALE\n" + end + "\ntail\n", "tail\n\n"},
		{"整文件就是标记段", start + "\nSTALE\n" + end + "\n", ""},
		{"标记缺失原样返回", "# untouched\n", "# untouched\n"},
		{"标记颠倒原样返回", end + "\nSTALE\n" + start + "\n", end + "\nSTALE\n" + start + "\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := StripMarkedSection(tt.content, start, end); got != tt.want {
				t.Fatalf("StripMarkedSection(%q) = %q, want %q", tt.content, got, tt.want)
			}
		})
	}

	// 逆契约：ReplaceMarkedSection 的调用方在 newSection 里自带标记
	//（claudemd 的 forge 段开头就写 forgeSectionStart）——按真实形态做
	// replace→strip 往返，应回到仅剩用户内容、单一空行接缝的形状。
	original := "# head\n\n" + start + "\nold\n" + end + "\n\ntail\n"
	replaced := ReplaceMarkedSection(original, start+"\n# new section\n"+end, start, end)
	stripped := StripMarkedSection(replaced, start, end)
	if stripped != "# head\n\ntail\n\n" {
		t.Fatalf("replace→strip round trip = %q, want the user content (after-side trailing newlines preserved — behavior-identical move from skillgen)", stripped)
	}
}
