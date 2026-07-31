package skillsfm

import "testing"

func TestIsValidSkillName(t *testing.T) {
	valid := []string{"code-review-gate", "tdd-cycle", "a", "skill_1", "skill.name"}
	for _, n := range valid {
		if !IsValidSkillName(n) {
			t.Errorf("expected %q to be valid", n)
		}
	}
	invalid := []string{"", ".", "..", "../evil", "foo/bar", `foo\bar`, "/abs", `C:\x`}
	for _, n := range invalid {
		if IsValidSkillName(n) {
			t.Errorf("expected %q to be invalid", n)
		}
	}
}
