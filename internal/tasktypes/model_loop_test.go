package tasktypes

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// TestLoopExhaustionSerialization 钉住回环键的序列化承诺面（schema 键只增不删
// 的 golden 棘轮依赖）：loop_exhausted 嵌套键（reason/detail/at）必须出现。
func TestLoopExhaustionSerialization(t *testing.T) {
	s := TaskState{LoopExhausted: &LoopExhaustion{
		Reason: LoopReasonRecurrence, Detail: "fingerprint 复活", At: time.Now(),
	}}
	data, err := json.Marshal(&s)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"loop_exhausted"`, `"reason"`, `"detail"`, `"at"`} {
		if !strings.Contains(string(data), key) {
			t.Errorf("序列化面缺键 %s: %s", key, data)
		}
	}
	var back TaskState
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if back.LoopExhausted == nil || back.LoopExhausted.Reason != LoopReasonRecurrence {
		t.Fatalf("回读不等价: %+v", back.LoopExhausted)
	}
}
