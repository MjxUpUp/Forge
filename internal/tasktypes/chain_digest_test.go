package tasktypes

import (
	"encoding/json"
	"testing"
)

// TestArtifactApproval_ChainDigestJSON 钉住链定义指纹的序列化契约
// （docs/design/task-tree-guard.md §6）：新字段可写可读；存量审批（没有该字段）
// 反序列化后为空串——空串在下游被当作「未绑定链定义」，绝不读成漂移。
func TestArtifactApproval_ChainDigestJSON(t *testing.T) {
	b, err := json.Marshal(ArtifactApproval{By: "human", ChainDigest: "abc123"})
	if err != nil {
		t.Fatal(err)
	}
	var back ArtifactApproval
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back.ChainDigest != "abc123" {
		t.Fatalf("ChainDigest 未往返：%s", b)
	}

	// 空值必须省略（omitempty）——否则存量任务每次写盘都多一个空键。
	empty, err := json.Marshal(ArtifactApproval{By: "human"})
	if err != nil {
		t.Fatal(err)
	}
	if string(empty) == "" || json.Valid(empty) == false {
		t.Fatalf("空审批序列化异常：%s", empty)
	}
	if hasKey(empty, "chain_digest") {
		t.Fatalf("空 ChainDigest 必须省略，got %s", empty)
	}

	var legacy ArtifactApproval
	if err := json.Unmarshal([]byte(`{"by":"human"}`), &legacy); err != nil {
		t.Fatal(err)
	}
	if legacy.ChainDigest != "" {
		t.Fatalf("存量审批必须解出空串，got %q", legacy.ChainDigest)
	}
}

// hasKey 报告 JSON 对象顶层是否含该键（测试小助手，避免引入 map 遍历噪声）。
func hasKey(b []byte, key string) bool {
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return false
	}
	_, ok := m[key]
	return ok
}
