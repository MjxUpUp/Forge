package tasktypes

import (
	"encoding/json"
	"testing"
	"time"
)

// TestArtifactApprovalAndOverridesSerialization 钉住产物链新键的序列化承诺面
//（schema 键只增不删的 golden 棘轮依赖这些字段落盘）：artifact_approvals 嵌套键
// 与 overrides.artifact_chain 必须出现；反序列化回读等价（状态文件兼容性）。
func TestArtifactApprovalAndOverridesSerialization(t *testing.T) {
	now := time.Now()
	s := TaskState{
		ArtifactAdvisoryFired: true,
		ArtifactApprovals: map[string]ArtifactApproval{
			"spec": {By: "human", At: now, Hash: "abc123"},
		},
		Overrides: TaskOverrides{ArtifactChain: "disable"},
	}
	data, err := json.Marshal(&s)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	for _, key := range []string{
		`"artifact_advisory_fired"`,
		`"artifact_approvals"`,
		`"artifact_approvals"."spec"`, // map 键也是承诺面（seed 填满纪律）
		`"overrides"."artifact_chain"`,
	} {
		if !jsonPathPresent(data, key) {
			t.Errorf("序列化面缺键 %s: %s", key, data)
		}
	}
	var back TaskState
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !back.ArtifactAdvisoryFired || back.Overrides.ArtifactChain != "disable" {
		t.Errorf("回读不等价: %+v", back)
	}
	apr, ok := back.ArtifactApprovals["spec"]
	if !ok || apr.By != "human" || apr.Hash != "abc123" {
		t.Errorf("审批回读不等价: %+v", back.ArtifactApprovals)
	}
}

// jsonPathPresent 在序列化字节里做扁平键存在性检查（"a"."b" 表示相邻出现）。
func jsonPathPresent(data []byte, path string) bool {
	parts := splitPath(path)
	idx := 0
	for _, p := range parts {
		at := indexOfFrom(data, p, idx)
		if at < 0 {
			return false
		}
		idx = at + len(p)
	}
	return true
}

func splitPath(p string) []string {
	var out []string
	start := 0
	for i := 0; i < len(p); i++ {
		if p[i] == '.' && (i == 0 || p[i-1] != '\\') {
			out = append(out, p[start:i])
			start = i + 1
		}
	}
	return append(out, p[start:])
}

func indexOfFrom(data []byte, sub string, from int) int {
	s := string(data)
	for i := from; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
