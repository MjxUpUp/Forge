package evalkit

// wedgedrill_test.go — 楔子演练脚本形状的单元测试（e2e 全链路在
// internal/cli/eval_cli_test.go TestEvalWedgeDrillE2E——预构建二进制跑真实命令；
// 这里钉脚本定义本身的机械不变量，同目录配对源文件）。

import (
	"strings"
	"testing"
)

// TestWedgeScriptShape 钉住首证据脚本的机械不变量：步骤非空、forgeBin 注入到
// 每个 forge 调用、expectFail 恰好一步（红态 verify——只见过通过的证据不是证据），
// 且红态步骤之后必有绿态 verify（先挂后过是演练的叙事骨架）。
func TestWedgeScriptShape(t *testing.T) {
	steps := wedgeScript("/fake/forge")
	if len(steps) == 0 {
		t.Fatal("脚本不应为空")
	}
	expectFailIdx := -1
	for i, st := range steps {
		if len(st.argv) == 0 || st.argv[0] == "" {
			t.Fatalf("step %d (%s) argv 为空", i, st.name)
		}
		if strings.Contains(st.argv[0], "{forge}") {
			t.Fatalf("step %d (%s) 占位符未注入（wedgeScript 直接收真实 bin，不走 {forge} 替换）", i, st.name)
		}
		if st.expectFail {
			if expectFailIdx != -1 {
				t.Fatalf("expectFail 步骤多于一个: %d 与 %d", expectFailIdx, i)
			}
			expectFailIdx = i
		}
	}
	if expectFailIdx == -1 {
		t.Fatal("缺红态步骤（expectFail 恰一个）——没有如实挂的 verify，证据链叙事不成立")
	}
	// 红态 verify 之后（隔一步 fixture 修复）必有非 expectFail 的绿态 verify——
	// 先挂后过才是"修复"的证据。
	red := steps[expectFailIdx]
	if red.argv[len(red.argv)-1] != "verify-acceptance" {
		t.Fatalf("expectFail 步骤应是 verify-acceptance（红态），got %v", red.argv)
	}
	greenIdx := -1
	for i := expectFailIdx + 1; i < len(steps); i++ {
		if steps[i].argv[len(steps[i].argv)-1] == "verify-acceptance" {
			greenIdx = i
			break
		}
	}
	if greenIdx == -1 {
		t.Fatal("红态之后没有绿态 verify-acceptance")
	}
	if steps[greenIdx].expectFail {
		t.Fatal("绿态 verify 不得标记 expectFail")
	}
}

// TestWedgeDrillResultJSONRoundTrip 钉住报告形状可序列化（PersistWedgeReport
// 落盘 JSON——消费方是回归对比，字段漂移 = 历史报告不可比）。
func TestWedgeDrillResultJSONRoundTrip(t *testing.T) {
	in := WedgeDrillResult{Passed: true, Total: "1.2s", Steps: []WedgeStepResult{
		{Name: "git init", Passed: true, Duration: "5ms"},
	}}
	body, err := jsonMarshal(in)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"passed": true`, `"total": "1.2s"`, `"steps"`} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("JSON 缺 %q: %s", want, body)
		}
	}
}
