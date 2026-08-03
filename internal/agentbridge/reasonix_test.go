package agentbridge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReasonixTranslator_Translate: a reasonix home that exists (reasonix
// installed) gets the user-level forge-quality skill written under
// <home>/skills/forge-quality/SKILL.md — reasonix's native skill mechanism.
// Content carries the shared conditional-activation wording (visible in every
// project, effective only in forge-registered ones) and drops the project-info
// section.
//
// TestReasonixTranslator_Translate：reasonix home 存在（reasonix 已装）时，
// 用户级 forge-quality skill 写到 <home>/skills/forge-quality/SKILL.md——
// reasonix 的原生 skill 机制。内容带共享条件激活措辞（对所有项目可见，仅在
// forge 注册项目中生效）且移除项目信息章节。
func TestReasonixTranslator_Translate(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	if err := os.MkdirAll(home, 0755); err != nil {
		t.Fatalf("mkdir reasonix home: %v", err)
	}

	if err := (&ReasonixTranslator{}).Translate(t.TempDir(), testInput()); err != nil {
		t.Fatalf("Translate: %v", err)
	}

	path := filepath.Join(home, "skills", "forge-quality", "SKILL.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read skill: %v (want SKILL.md under reasonix skills dir)", err)
	}
	content := string(data)
	for _, want := range []string{
		"name: forge-quality",
		"## 质量标准",
		"## Task Bridge Protocol",
		"仅当当前项目已执行过 `forge init`", // conditional activation, not unconditional "本项目"
	} {
		if !strings.Contains(content, want) {
			t.Errorf("skill content missing %q", want)
		}
	}
	if strings.Contains(content, "## 当前项目信息") {
		t.Errorf("user-level skill must drop the project-info section")
	}
}

// TestReasonixTranslator_NoSelfPoison: a missing reasonix home (reasonix not
// installed) is a clean no-op — Forge must not create the agent's config home
// itself, or DetectAgents' project-independent signal would flip and re-wire on
// every init.
//
// TestReasonixTranslator_NoSelfPoison：reasonix home 缺失（未安装）时干净 no-op
// ——Forge 绝不自行创建 agent 的配置 home，否则会让接线信号翻转、每次 init
// 都重新接线。
func TestReasonixTranslator_NoSelfPoison(t *testing.T) {
	home := filepath.Join(t.TempDir(), "no-such-reasonix-home")
	t.Setenv("REASONIX_HOME", home)

	if err := (&ReasonixTranslator{}).Translate(t.TempDir(), testInput()); err != nil {
		t.Fatalf("Translate: %v", err)
	}
	if _, err := os.Stat(home); !os.IsNotExist(err) {
		t.Errorf("reasonix home must not be created when missing (self-poison guard), stat err = %v", err)
	}
}

// TestReasonixTranslator_Idempotent: translating twice produces byte-identical
// skill content (no drift, no duplicate sections).
//
// TestReasonixTranslator_Idempotent：翻译两次产出逐字节一致的 skill 内容
// （无漂移、无重复段）。
func TestReasonixTranslator_Idempotent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	if err := os.MkdirAll(home, 0755); err != nil {
		t.Fatalf("mkdir reasonix home: %v", err)
	}

	tr := &ReasonixTranslator{}
	dir := t.TempDir()
	if err := tr.Translate(dir, testInput()); err != nil {
		t.Fatalf("Translate 1: %v", err)
	}
	first, err := os.ReadFile(filepath.Join(home, "skills", "forge-quality", "SKILL.md"))
	if err != nil {
		t.Fatalf("read skill 1: %v", err)
	}
	if err := tr.Translate(dir, testInput()); err != nil {
		t.Fatalf("Translate 2: %v", err)
	}
	second, err := os.ReadFile(filepath.Join(home, "skills", "forge-quality", "SKILL.md"))
	if err != nil {
		t.Fatalf("read skill 2: %v", err)
	}
	if string(first) != string(second) {
		t.Errorf("idempotency broken: second Translate changed the skill content")
	}
}

// TestReasonixTranslator_Registered guards AllTranslators membership — `forge init
// --agents reasonix` resolves through translatorMap, so an unregistered translator
// silently wires nothing.
//
// TestReasonixTranslator_Registered 守卫 AllTranslators 成员资格——`forge init
// --agents reasonix` 经 translatorMap 解析，未注册的 translator 会静默不接线。
func TestReasonixTranslator_Registered(t *testing.T) {
	for _, tr := range AllTranslators() {
		if tr.AgentType() == AgentReasonix {
			return
		}
	}
	t.Fatal("ReasonixTranslator not registered in AllTranslators")
}

// TestReasonixDetect: a project dir carrying .reasonix/ (the agent ran there at
// least once) is auto-detected; a clean dir is not. User-level ~/.reasonix is
// deliberately NOT a signal (kimi philosophy) — covered implicitly by
// TestDetectAgents_None with the isolated home.
//
// TestReasonixDetect：项目目录带 .reasonix/（agent 至少在此跑过一次）会被
// auto 检测；干净目录不会。用户级 ~/.reasonix 刻意不作为信号（kimi 哲学）——
// 由隔离 home 下的 TestDetectAgents_None 隐式覆盖。
func TestReasonixDetect(t *testing.T) {
	isolateHome(t) // keep the real home out of DetectAgents' user-level scan

	dir := t.TempDir()
	agents := DetectAgents(dir)
	for _, a := range agents {
		if a == AgentReasonix {
			t.Fatalf("clean project dir must not auto-detect reasonix, got %v", agents)
		}
	}

	if err := os.MkdirAll(filepath.Join(dir, ".reasonix"), 0755); err != nil {
		t.Fatalf("mkdir .reasonix: %v", err)
	}
	agents = DetectAgents(dir)
	found := false
	for _, a := range agents {
		if a == AgentReasonix {
			found = true
		}
	}
	if !found {
		t.Fatalf("project dir with .reasonix/ must auto-detect reasonix, got %v", agents)
	}
}

// TestReasonixConfigHome_EnvAndDefault pins the home resolution: REASONIX_HOME
// wins when set; otherwise ~/.reasonix under the user home.
//
// TestReasonixConfigHome_EnvAndDefault 钉住 home 解析：设了 REASONIX_HOME 用它；
// 否则用户 home 下 ~/.reasonix。
func TestReasonixConfigHome_EnvAndDefault(t *testing.T) {
	t.Setenv("REASONIX_HOME", filepath.Join("C:", "fake", "reasonix"))
	got, err := ReasonixConfigHome()
	if err != nil {
		t.Fatalf("ReasonixConfigHome: %v", err)
	}
	if got != filepath.Join("C:", "fake", "reasonix") {
		t.Errorf("REASONIX_HOME override not honored, got %q", got)
	}

	t.Setenv("REASONIX_HOME", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	got, err = ReasonixConfigHome()
	if err != nil {
		t.Fatalf("ReasonixConfigHome (default): %v", err)
	}
	if want := filepath.Join(home, ".reasonix"); got != want {
		t.Errorf("default home = %q, want %q", got, want)
	}
}
