package cli

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// F.2a confirm chain-separation tests (design F, docs/design/harness-fixes-a-g-2026-09.md):
// a confirm riding mid/late in a compound Bash line is the self-service loop the design severs
// (machine-甲 evidence: 53/53 confirms agent-initiated, several chained with the target command).
// The CLI layer refuses chained confirm args and points to the standalone form.
//
// F.2a confirm 链式分离测试（设计 F）：confirm 嵌在复合命令中/尾段是设计要切断的自助闭环
// 形态（甲机 53/53 自助实录）。CLI 层拒绝链式 confirm 参数并指引单独执行。
func TestConfirmChainSeparated(t *testing.T) {
	yes := [][]string{
		{"&& forge hazard confirm --last"},
		{"; git", "push origin main"},
		{"| tail -1"},
		{"|| echo done"},
	}
	for _, args := range yes {
		if !confirmChainSeparated(args) {
			t.Errorf("confirmChainSeparated(%q) = false, want true (connector-prefixed args = chained form)", args)
		}
	}
	no := [][]string{
		{"git push origin --force"},
		{""},
		{"--last"},
		{"cd E:/Forge && forge task gate x"}, // 连接符在参数内部（确认的恰是那条复合命令），非 confirm 被链式续行
		nil,
	}
	for _, args := range no {
		if confirmChainSeparated(args) {
			t.Errorf("confirmChainSeparated(%q) = true, want false", args)
		}
	}
}

// TestHazardConfirm_ChainedRefused pins the CLI refusal: a chained confirm returns a BLOCKED
// error naming the standalone form — the refusal targets the form, not the operation — and
// routes it to the user's own terminal (F.2b: an agent-run standalone confirm is refused too).
//
// TestHazardConfirm_ChainedRefused 钉住 CLI 拒绝：链式 confirm 返回 BLOCKED 错误并给出
// 单独执行形态——拒绝的是形态不是操作——且指给用户在自己的终端执行（F.2b：agent
// 单独执行同样会被拒）。
func TestHazardConfirm_ChainedRefused(t *testing.T) {
	c := &cobra.Command{}
	err := runHazardConfirm(c, []string{"&& git push origin --delete x"})
	if err == nil || !strings.Contains(err.Error(), "BLOCKED") || !strings.Contains(err.Error(), "forge hazard confirm --last") {
		t.Fatalf("chained confirm must be refused with BLOCKED + standalone guidance, got: %v", err)
	}
	if !strings.Contains(err.Error(), "请用户在自己的终端单独执行") {
		t.Fatalf("chained-confirm guidance must route the standalone confirm to the user's terminal, got: %v", err)
	}
}
