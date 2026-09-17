package checklog

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestEntryOutcomeRoundTrip pins the outcome field contract (discipline-first-gates
// P1-A): a failed gate entry classified as discovery/confirmation serializes the
// field, legacy/unclassified entries omit it entirely (omitempty), and the value
// survives a decode round-trip so post-hoc analysis joins don't lose it.
//
// TestEntryOutcomeRoundTrip 钉住 outcome 字段契约（discipline-first-gates P1-A）：
// 被分类为 discovery/confirmation 的失败门禁条目序列化出该字段，历史/未分类条目
// 完全省略它（omitempty），且值在解码往返后不丢——事后分析 join 不掉字。
func TestEntryOutcomeRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		in   Entry
		want string // JSON 里应出现（非空）或不应出现（空串）的 outcome 值
	}{
		{
			name: "discovery serialized",
			in:   Entry{Check: "test-coverage-gate", Passed: false, Checked: true, Outcome: OutcomeDiscovery},
			want: `"outcome":"discovery"`,
		},
		{
			name: "confirmation serialized",
			in:   Entry{Check: "test-coverage-gate", Passed: false, Checked: true, Outcome: OutcomeConfirmation},
			want: `"outcome":"confirmation"`,
		},
		{
			name: "unclassified omitted",
			in:   Entry{Check: "test-coverage-gate", Passed: true, Checked: true},
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(&tc.in)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if tc.want == "" {
				if strings.Contains(string(data), `"outcome"`) {
					t.Errorf("unclassified entry must omit outcome, got %s", data)
				}
			} else if !strings.Contains(string(data), tc.want) {
				t.Errorf("json missing %s, got %s", tc.want, data)
			}
			var back Entry
			if err := json.Unmarshal(data, &back); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if back.Outcome != tc.in.Outcome {
				t.Errorf("round-trip outcome = %q, want %q", back.Outcome, tc.in.Outcome)
			}
		})
	}
}
