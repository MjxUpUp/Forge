package hooks

// embed_quality_stopblock_test.go —— TaskVerifyHook 嵌入脚本的 Stop 有界阻断
// 与 HITL 等人态的内容契约(escape-hatch-hardening P1)。行为面由
// internal/e2e/taskverify_stopblock_test.go 实跑钉住;这里钉嵌入脚本本体
// 携带契约要素——防手改 embed 字符串时静默丢段(改动须同时过两处)。

import (
	"strings"
	"testing"
)

func TestTaskVerifyHookCarriesBoundedBlockContract(t *testing.T) {
	s := TaskVerifyHook
	for _, want := range []string{
		"FORGE_TASK_VERIFY_STOP",      // 逃生舱
		"taskverify-stop-blocks-",     // 会话限额 marker
		"未提交代码变更",                     // 窄条件文案
		"限额已耗尽",                       // 超限回落一次性说明
		"HITL 等人",                     // hazard 未确认提醒
		"forge hazard confirm --last", // 确认出口
	} {
		if !strings.Contains(s, want) {
			t.Errorf("TaskVerifyHook 嵌入脚本缺契约要素 %q——有界阻断/HITL 段被丢(行为守卫在 internal/e2e/taskverify_stopblock_test.go)", want)
		}
	}
	// 限额值钉死 3:脚本内出现 "[ \"$_n\" -lt 3 ]"。
	if !strings.Contains(s, `-lt 3 ]`) {
		t.Error("会话限额必须钉 3(与 e2e 断言的 3/3 同源)")
	}
	// 审查修正段(复检 NIT 补钉):untracked 并入 / block 清节流戳 / block 审计行。
	for _, want := range []string{
		"git ls-files --others --exclude-standard", // untracked 新代码文件
		"rm -f \"$_STAMP\"",                        // 连续阻断至限额(节流语义诚实化)
		"bounded stop block",                       // 每次 block 的 fail 审计行
	} {
		if !strings.Contains(s, want) {
			t.Errorf("TaskVerifyHook 缺审查修正段 %q", want)
		}
	}
}
