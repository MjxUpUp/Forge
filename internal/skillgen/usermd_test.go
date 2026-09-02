package skillgen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/protocol"
	"github.com/MjxUpUp/Forge/internal/userassets"
)

// setupUserHomes 把用户级 home（CLAUDE_CONFIG_DIR / CODEX_HOME /
// FORGE_DATA_HOME，外加 ~/.agents skill 目标解析所经的 HOME/USERPROFILE）
// 隔离进 temp dir——用户级生成器在测试中绝不碰真实 home。
func setupUserHomes(t *testing.T) (claudeHome, codexHome string) {
	t.Helper()
	claudeHome = t.TempDir()
	codexHome = t.TempDir()
	t.Setenv(`CLAUDE_CONFIG_DIR`, claudeHome)
	t.Setenv(`CODEX_HOME`, codexHome)
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	userHome := t.TempDir()
	t.Setenv(`HOME`, userHome)
	t.Setenv(`USERPROFILE`, userHome) // Windows 的 os.UserHomeDir 源
	return claudeHome, codexHome
}

// TestGenerateUserClaudeMD_ConditionalPreamble 守护用户级迁移的核心：用户级段
// 对所有项目可见，必须携带条件激活前置说明（仅当前项目已 forge init 时适用），
// 同时保留 Claude surface（/forge-quality）与 FORGE 标记。
func TestGenerateUserClaudeMD_ConditionalPreamble(t *testing.T) {
	claudeHome, _ := setupUserHomes(t)

	if err := GenerateUserClaudeMD(); err != nil {
		t.Fatalf(`GenerateUserClaudeMD: %v`, err)
	}
	b, err := os.ReadFile(filepath.Join(claudeHome, `CLAUDE.md`))
	if err != nil {
		t.Fatalf(`user CLAUDE.md not written: %v`, err)
	}
	got := string(b)

	if !strings.Contains(got, `用户级全局注入`) {
		t.Error(`user CLAUDE.md missing the conditional-activation preamble`)
	}
	if !strings.Contains(got, `完全忽略本段`) {
		t.Error(`user CLAUDE.md preamble must tell agents to ignore the section outside forge projects`)
	}
	if !strings.Contains(got, forgeSectionStart) || !strings.Contains(got, forgeSectionEnd) {
		t.Error(`user CLAUDE.md missing FORGE section markers`)
	}
	if !strings.Contains(got, `/forge-quality`) {
		t.Error(`user CLAUDE.md missing Claude slash command /forge-quality`)
	}
	if strings.Contains(got, `通过 forge CLI`) {
		t.Error(`user CLAUDE.md must not carry the AGENTS.md cross-agent surface line`)
	}
}

// TestGenerateUserAgentsMD_ConditionalPreamble 是 codex 侧守卫：用户级
// ~/.codex/AGENTS.md 携带条件前置说明与 agent-agnostic CLI surface，
// 不含 Claude slash command。
func TestGenerateUserAgentsMD_ConditionalPreamble(t *testing.T) {
	_, codexHome := setupUserHomes(t)

	if err := GenerateUserAgentsMD(); err != nil {
		t.Fatalf(`GenerateUserAgentsMD: %v`, err)
	}
	b, err := os.ReadFile(filepath.Join(codexHome, `AGENTS.md`))
	if err != nil {
		t.Fatalf(`user AGENTS.md not written: %v`, err)
	}
	got := string(b)

	if !strings.Contains(got, `用户级全局注入`) {
		t.Error(`user AGENTS.md missing the conditional-activation preamble`)
	}
	if !strings.Contains(got, `通过 forge CLI`) {
		t.Error(`user AGENTS.md missing agent-agnostic CLI surface line`)
	}
	if strings.Contains(got, `/forge-quality`) {
		t.Error(`user AGENTS.md must not carry Claude-only slash command`)
	}
}

// TestProjectLevelSectionStaysUnconditional 钉住契约的另一半：项目级变体
// （团队模式 `forge init --project`）保持无条件文本——条件前置说明不得泄漏进去。
func TestProjectLevelSectionStaysUnconditional(t *testing.T) {
	for _, forClaude := range []bool{true, false} {
		section := buildForgeSection(forClaude)
		if strings.Contains(section, `用户级全局注入`) {
			t.Errorf(`project-level section (forClaude=%v) must NOT carry the user-level preamble`, forClaude)
		}
		if !strings.Contains(section, `本项目使用 Forge 进行质量保障`) {
			t.Errorf(`project-level section (forClaude=%v) must keep the unconditional intro`, forClaude)
		}
	}
}

// TestGenerateUserClaudeMD_IdempotentUpsert 守护用户级文件的 upsert 契约：
// 重复生成保留标记外的用户内容并原地替换 forge 段（恰好一对标记）。
func TestGenerateUserClaudeMD_IdempotentUpsert(t *testing.T) {
	claudeHome, _ := setupUserHomes(t)
	path := filepath.Join(claudeHome, `CLAUDE.md`)

	if err := os.WriteFile(path, []byte("# my global notes\n\nuser notes stay\n"), 0644); err != nil {
		t.Fatalf(`seed user CLAUDE.md: %v`, err)
	}
	if err := GenerateUserClaudeMD(); err != nil {
		t.Fatalf(`GenerateUserClaudeMD first run: %v`, err)
	}
	if err := GenerateUserClaudeMD(); err != nil {
		t.Fatalf(`GenerateUserClaudeMD second run: %v`, err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf(`read user CLAUDE.md: %v`, err)
	}
	got := string(b)
	if !strings.Contains(got, `user notes stay`) {
		t.Error(`user-level regeneration clobbered user content outside FORGE markers`)
	}
	if n := strings.Count(got, forgeSectionStart); n != 1 {
		t.Errorf(`user CLAUDE.md has %d FORGE:START markers after re-run, want 1`, n)
	}
}

// TestGenerateUserClaudeMD_BackupBeforeFirstWrite 钉死备份+追加契约：forge
// 首次写入前必须已备份原用户文件，回滚能恢复 forge 触碰前的字节。
func TestGenerateUserClaudeMD_BackupBeforeFirstWrite(t *testing.T) {
	claudeHome, _ := setupUserHomes(t)
	path := filepath.Join(claudeHome, `CLAUDE.md`)
	if err := os.WriteFile(path, []byte(`pre-forge user content`), 0644); err != nil {
		t.Fatalf(`seed user CLAUDE.md: %v`, err)
	}

	if err := GenerateUserClaudeMD(); err != nil {
		t.Fatalf(`GenerateUserClaudeMD: %v`, err)
	}
	got, _ := os.ReadFile(path)
	if !strings.Contains(string(got), forgeSectionStart) {
		t.Fatal(`user CLAUDE.md missing forge section after generate`)
	}

	restored, err := userassets.RestoreOriginal(path)
	if err != nil {
		t.Fatalf(`RestoreOriginal: %v`, err)
	}
	if !restored {
		t.Fatal(`no backup was taken before forge's first user-level write`)
	}
	rolled, _ := os.ReadFile(path)
	if string(rolled) != `pre-forge user content` {
		t.Errorf(`rollback restored %q, want the pre-forge bytes`, rolled)
	}
}

// TestStripUserInstructions_RoundTrip 守护 uninstall：strip 从两个用户级文件
// 移除 forge 段、保留其余全部内容，且幂等（第二次 strip 为 no-op）。
func TestStripUserInstructions_RoundTrip(t *testing.T) {
	claudeHome, codexHome := setupUserHomes(t)
	claudePath := filepath.Join(claudeHome, `CLAUDE.md`)
	codexPath := filepath.Join(codexHome, `AGENTS.md`)

	if err := os.WriteFile(claudePath, []byte("# global claude notes\n"), 0644); err != nil {
		t.Fatalf(`seed user CLAUDE.md: %v`, err)
	}
	if err := GenerateUserClaudeMD(); err != nil {
		t.Fatalf(`GenerateUserClaudeMD: %v`, err)
	}
	if err := GenerateUserAgentsMD(); err != nil {
		t.Fatalf(`GenerateUserAgentsMD: %v`, err)
	}

	if err := StripUserInstructions(); err != nil {
		t.Fatalf(`StripUserInstructions: %v`, err)
	}

	claudeGot, err := os.ReadFile(claudePath)
	if err != nil {
		t.Fatalf(`read stripped CLAUDE.md: %v`, err)
	}
	if strings.Contains(string(claudeGot), forgeSectionStart) || strings.Contains(string(claudeGot), `Forge 质量协议`) {
		t.Error(`strip left forge section content in user CLAUDE.md`)
	}
	if !strings.Contains(string(claudeGot), `global claude notes`) {
		t.Error(`strip clobbered user content in user CLAUDE.md`)
	}

	codexGot, err := os.ReadFile(codexPath)
	if err != nil {
		t.Fatalf(`read stripped AGENTS.md: %v`, err)
	}
	if strings.Contains(string(codexGot), forgeSectionStart) {
		t.Error(`strip left forge markers in user AGENTS.md`)
	}

	// 幂等：对已 strip 的文件再次 strip 是 no-op。
	if err := StripUserInstructions(); err != nil {
		t.Fatalf(`StripUserInstructions second run: %v`, err)
	}
	claudeAgain, _ := os.ReadFile(claudePath)
	if string(claudeAgain) != string(claudeGot) {
		t.Error(`second strip changed an already-stripped file (not idempotent)`)
	}
}

// TestStripUserInstructions_MissingFiles 守护全新机器路径：用户级文件都不存在时
// strip 作为 no-op 成功（forge 从未写入时 uninstall 不得失败）。
func TestStripUserInstructions_MissingFiles(t *testing.T) {
	setupUserHomes(t)
	if err := StripUserInstructions(); err != nil {
		t.Fatalf(`StripUserInstructions on missing files: %v`, err)
	}
}

// TestGenerateUserQualitySkill 守护用户级 forge-quality skill：落到
// <claudeHome>/skills/forge-quality/SKILL.md，保留协议内容，且使用条件式
// 措辞（无无条件"本项目"断言、无单项目信息章节）。
func TestGenerateUserQualitySkill(t *testing.T) {
	claudeHome, _ := setupUserHomes(t)
	proto := &protocol.Protocol{
		Version: "1",
		Standards: []protocol.Standard{
			{ID: "compile", Name: "编译必须通过", Description: "每次修改后确认编译通过", Severity: "error", Enabled: true},
		},
	}

	if err := GenerateUserQualitySkill(proto); err != nil {
		t.Fatalf(`GenerateUserQualitySkill: %v`, err)
	}
	b, err := os.ReadFile(filepath.Join(claudeHome, `skills`, `forge-quality`, `SKILL.md`))
	if err != nil {
		t.Fatalf(`user-level SKILL.md not written: %v`, err)
	}
	got := string(b)

	if !strings.Contains(got, `name: forge-quality`) {
		t.Error(`user-level SKILL.md missing frontmatter name`)
	}
	if !strings.Contains(got, `编译必须通过`) {
		t.Error(`user-level SKILL.md missing protocol standards content`)
	}
	if !strings.Contains(got, `[forge-session]`) {
		t.Error(`user-level SKILL.md missing the banner-anchored conditional wording（P3：激活判据锚定会话横幅）`)
	}
	if strings.Contains(got, `你是本项目的质量守护者`) {
		t.Error(`user-level SKILL.md must not unconditionally claim "本项目"`)
	}
	if strings.Contains(got, `## 当前项目信息`) {
		t.Error(`user-level SKILL.md must not carry the single-project info section`)
	}
}

// TestGenerateUserQualitySkill_Targets 钉住用户级 skill 的生成目标列表：除
// ~/.claude/skills 外还写跨 agent 共享的 ~/.agents/skills（kimi 等
// agent-neutral 宿主在此读它——2026-08 该路径的孤儿文件证明 kimi 会加载而
// 无生成器刷新）。~/.agents 存在时写入（与 claude 副本字节一致）；不存在时
// 不得创建（同款自毒防护——Forge 绝不自行创建 agent 的配置 home）。
func TestGenerateUserQualitySkill_Targets(t *testing.T) {
	claudeHome, _ := setupUserHomes(t)
	userHome := os.Getenv(`HOME`)

	// ~/.agents 缺失 → 不得创建（自毒防护）。
	if err := GenerateUserQualitySkill(protocol.DefaultProtocol()); err != nil {
		t.Fatalf(`GenerateUserQualitySkill: %v`, err)
	}
	if _, err := os.Stat(filepath.Join(userHome, `.agents`)); !os.IsNotExist(err) {
		t.Fatalf(`GenerateUserQualitySkill created ~/.agents out of nothing — self-poison guard broken`)
	}

	// ~/.agents 存在（已装 agent-neutral 宿主）→ 副本落盘，与 claude 副本字节一致。
	if err := os.MkdirAll(filepath.Join(userHome, `.agents`), 0755); err != nil {
		t.Fatal(err)
	}
	if err := GenerateUserQualitySkill(protocol.DefaultProtocol()); err != nil {
		t.Fatalf(`GenerateUserQualitySkill: %v`, err)
	}
	agentsSkill := filepath.Join(userHome, `.agents`, `skills`, `forge-quality`, `SKILL.md`)
	agentsBytes, err := os.ReadFile(agentsSkill)
	if err != nil {
		t.Fatalf(`~/.agents/skills/forge-quality/SKILL.md not written: %v`, err)
	}
	claudeBytes, err := os.ReadFile(filepath.Join(claudeHome, `skills`, `forge-quality`, `SKILL.md`))
	if err != nil {
		t.Fatalf(`claude copy not written: %v`, err)
	}
	if string(agentsBytes) != string(claudeBytes) {
		t.Error(`~/.agents copy must be byte-identical to the ~/.claude copy (same generator, same conditional wording)`)
	}
	if !strings.Contains(string(agentsBytes), "[forge-session]") {
		t.Error(`~/.agents copy missing the banner-anchored conditional wording`)
	}
}

// ---- detection self-poison guard (user-level-assets fix) ----

// TestGenerateUserClaudeMD_SkipsWhenClaudeNotInstalled 钉死自毒修复：Claude
// config home 的存在性是 DetectAgents 判断"claude 已安装"的信号，
// GenerateUserClaudeMD 不得创建它——没装 Claude Code 的机器必须保持未检出，
// 也不写指令文件。
func TestGenerateUserClaudeMD_SkipsWhenClaudeNotInstalled(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "claude-not-installed")
	t.Setenv(`CLAUDE_CONFIG_DIR`, missing)
	t.Setenv(`CODEX_HOME`, t.TempDir())
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())

	if err := GenerateUserClaudeMD(); err != nil {
		t.Fatalf("GenerateUserClaudeMD should no-op (nil), got: %v", err)
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Error("GenerateUserClaudeMD created the Claude config home — detection self-poison")
	}
}

// TestGenerateUserAgentsMD_SkipsWhenCodexNotInstalled 是同一契约的 codex 侧守卫。
func TestGenerateUserAgentsMD_SkipsWhenCodexNotInstalled(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "codex-not-installed")
	t.Setenv(`CLAUDE_CONFIG_DIR`, t.TempDir())
	t.Setenv(`CODEX_HOME`, missing)
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())

	if err := GenerateUserAgentsMD(); err != nil {
		t.Fatalf("GenerateUserAgentsMD should no-op (nil), got: %v", err)
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Error("GenerateUserAgentsMD created the codex config home — detection self-poison")
	}
}

// TestGenerateUserQualitySkill_SkipsWhenClaudeNotInstalled 是同一契约的
// quality-skill 守卫。
func TestGenerateUserQualitySkill_SkipsWhenClaudeNotInstalled(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "claude-not-installed")
	t.Setenv(`CLAUDE_CONFIG_DIR`, missing)
	t.Setenv(`CODEX_HOME`, t.TempDir())
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	// 生成器还会写 ~/.agents/skills（经 UserHomeDir 解析）——隔离 HOME，
	// 测试绝不碰真实的跨 agent 目录。
	t.Setenv(`HOME`, t.TempDir())
	t.Setenv(`USERPROFILE`, os.Getenv(`HOME`))

	if err := GenerateUserQualitySkill(protocol.DefaultProtocol()); err != nil {
		t.Fatalf("GenerateUserQualitySkill should no-op (nil), got: %v", err)
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Error("GenerateUserQualitySkill created the Claude config home — detection self-poison")
	}
}

// TestProjectSection_TeamModeQualifiers 钉死生成协议段的 v1.22 限定措辞：
// 自保护行必须声明项目级 .forge/* 与 .claude/settings* 只在团队模式/老项目
// 存在，且用户级资产仅经 forge 命令操作、uninstall --restore 可回滚。
func TestProjectSection_TeamModeQualifiers(t *testing.T) {
	section := buildForgeSection(true)
	for _, want := range []string{
		`团队模式/老项目存在`,
		`forge uninstall --restore`,
	} {
		if !strings.Contains(section, want) {
			t.Errorf("project-level forge section missing %q", want)
		}
	}
}
