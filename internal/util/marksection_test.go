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
