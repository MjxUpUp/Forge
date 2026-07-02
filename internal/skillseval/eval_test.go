package skillseval

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractTriggers(t *testing.T) {
	desc := "做某事。Use when: 用户需要解析文档、导出表格、汇总数据 or 发邮件。SKIP: 只读查看 或 纯翻译。"
	triggers, skips := ExtractTriggers(desc)
	if len(triggers) == 0 {
		t.Fatalf("want triggers, got %v", triggers)
	}
	found := false
	for _, tr := range triggers {
		if strings.Contains(tr, "解析文档") {
			found = true
		}
	}
	if !found {
		t.Fatalf("triggers 缺「解析文档」: %v", triggers)
	}
	if len(skips) == 0 {
		t.Fatalf("want skips, got %v", skips)
	}
}

func TestExtractTriggers_LimitsAndFilters(t *testing.T) {
	// triggers≤5、skips≤3，长度≤3 片段丢弃
	desc := "Use when: a、bb、ccc、dddd、eeee、ffff、gggg。SKIP: s1 或 s2 或 s3 或 s4。"
	triggers, skips := ExtractTriggers(desc)
	if len(triggers) > 5 {
		t.Fatalf("triggers 应≤5，got %d", len(triggers))
	}
	if len(skips) > 3 {
		t.Fatalf("skips 应≤3，got %d", len(skips))
	}
	for _, tr := range triggers {
		if len([]rune(tr)) <= 3 {
			t.Fatalf("trigger 不应含≤3 字符片段: %q", tr)
		}
	}
}

func TestGenerateEvalPrompts_Replacements(t *testing.T) {
	desc := "Use when: 用户说重启服务、用户需要清理缓存、用户要发邮件。SKIP: 普通查询。"
	should, _ := GenerateEvalPrompts("x", desc)
	joined := strings.Join(should, "|")
	if strings.Contains(joined, "用户说") {
		t.Fatalf("「用户说」未替换为空: %v", should)
	}
	if !strings.Contains(joined, "我需要清理缓存") {
		t.Fatalf("「用户需要」未替换为「我需要」: %v", should)
	}
	if !strings.Contains(joined, "我要发邮件") {
		t.Fatalf("「用户要」未替换为「我要」: %v", should)
	}
}

func TestEvalSkill_GeneratesMarkdown(t *testing.T) {
	canonical := t.TempDir()
	sd := filepath.Join(canonical, "my-skill")
	mustWrite(t, os.MkdirAll(sd, 0755))
	mustWrite(t, os.WriteFile(filepath.Join(sd, "SKILL.md"),
		[]byte("---\nname: my-skill\ndescription: 测试 skill。Use when: 用户需要做X、用户要做Y。SKIP: 做Z。\n---\n\nbody\n"), 0644))
	md, err := EvalSkill(canonical, "my-skill")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(md, "# Skill Eval: my-skill") {
		t.Fatalf("缺标题: %s", md)
	}
	if !strings.Contains(md, "Should-Trigger") {
		t.Fatalf("缺 Should-Trigger 段: %s", md)
	}
	if !strings.Contains(md, "我需要做X") {
		t.Fatalf("缺生成的 trigger prompt: %s", md)
	}
	if !strings.Contains(md, "记录结果") {
		t.Fatalf("缺记录结果表: %s", md)
	}
}

// TestEvalSkill_NoEllipsisOnShortDesc：短描述（≤200 rune）不应追加 "..."。
// 原实现无脑 firstNRunes(desc,200)+"..."，把完整短描述误读为被截断。
func TestEvalSkill_NoEllipsisOnShortDesc(t *testing.T) {
	canonical := t.TempDir()
	sd := filepath.Join(canonical, "short")
	mustWrite(t, os.MkdirAll(sd, 0755))
	shortDesc := "短描述。Use when: 用户需要做X。SKIP: 做Y。"
	mustWrite(t, os.WriteFile(filepath.Join(sd, "SKILL.md"),
		[]byte("---\nname: short\ndescription: "+shortDesc+"\n---\n\nbody\n"), 0644))
	md, err := EvalSkill(canonical, "short")
	if err != nil {
		t.Fatal(err)
	}
	descBlock := "## Description（被测对象）\n```\n" + shortDesc + "\n```"
	if !strings.Contains(md, descBlock) {
		t.Fatalf("短描述应无省略号完整展示，期望含 %q，got:\n%s", descBlock, md)
	}
}
