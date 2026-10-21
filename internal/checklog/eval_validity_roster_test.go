package checklog

import "testing"

// TestEvalValidityRoster 钉住周期总纲 R3/R5 新增的两个检查名：都在 CheckName
// roster 里（compat 面 2 可见），都不在 evidence 白名单——它们是评测工具自身的
// 审计轨迹（preflight 拒绝启动、golden test 层查询计数），与当前任务的验证无关。
func TestEvalValidityRoster(t *testing.T) {
	want := map[CheckName]string{
		CheckEvalPreflight:       "eval-preflight",
		CheckEvalGoldenTestQuery: "eval-golden-test-query",
	}
	roster := map[CheckName]bool{}
	for _, n := range AllCheckNames() {
		roster[CheckName(n)] = true
	}
	for name, literal := range want {
		if string(name) != literal {
			t.Fatalf("检查名字面量漂移: %q != %q", name, literal)
		}
		if !roster[name] {
			t.Fatalf("%s 应在 AllCheckNames roster 里", name)
		}
		if verificationChecks[name] {
			t.Fatalf("%s 不得进 evidence 白名单（评测工具审计轨迹不是任务验证证据）", name)
		}
	}
}
