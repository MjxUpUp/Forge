package change

import (
	"strings"
	"testing"
)

// 托管块是 agent 在这个仓库里看到的第一份协议；它必须是可执行的指令，
// 而不是「读了也不知道下一步做什么」的说明文。
func TestAgentsProtocol_IsActionable(t *testing.T) {
	required := []string{
		"## 开始之前（硬规则）",
		"先跑 `forge next`",
		"## Task 流程（门禁在命令里）",
		"门禁是命令，不是 hook",
		"按空白切分后直接 exec，不经 shell",
		"验收预跑",
		"forge task report --ref",
		"forge review pass --score",
		"--reviewer",
		"forge task start --ref <ref> --change <id>",
		"forge impact",
	}
	for _, want := range required {
		if !strings.Contains(tmplAgents, want) {
			t.Errorf("agents-protocol 模板缺少可执行协议的关键句：%q", want)
		}
	}
}

// 1.85 起宿主层删除：模板里不能再教 agent 依赖 hook 执法。
func TestAgentsProtocol_NoRemovedHostLayerClaims(t *testing.T) {
	forbidden := []string{
		"hazard-guard",
		"hooks: on",
	}
	for _, bad := range forbidden {
		if strings.Contains(tmplAgents, bad) {
			t.Errorf("agents-protocol 模板仍包含已删除的宿主层叙述：%q", bad)
		}
	}
}

// 硬规则必须排在说明文之前：第一屏决定 agent 做什么。
func TestAgentsProtocol_HardRulesComeFirst(t *testing.T) {
	rules := strings.Index(tmplAgents, "## 开始之前（硬规则）")
	flow := strings.Index(tmplAgents, "## change 流程")
	if rules < 0 || flow < 0 {
		t.Fatalf("模板缺少「开始之前」或「change 流程」段：rules=%d flow=%d", rules, flow)
	}
	if rules > flow {
		t.Error("「开始之前（硬规则）」必须排在「change 流程」之前")
	}
}

// 协议不能回退成 change-first：任务是一等入口，change 可后补。
func TestAgentsProtocol_ForbidsChangeFirstWording(t *testing.T) {
	forbidden := []string{
		"必须先有",
		"功能任务必须",
		"先有 ready",
	}
	for _, bad := range forbidden {
		if strings.Contains(tmplAgents, bad) {
			t.Errorf("agents-protocol 模板回退到 change-first 叙述：%q", bad)
		}
	}
}
