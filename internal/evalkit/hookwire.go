package evalkit

// hookwire.go —— 评测夹具的执法 opt-in。
//
// 阶段 C 起 `forge init` 只写仓库契约（spec/forge.yaml，hooks: off /
// profile: "mini"）——新仓库默认 audit-only，执法必须显式 opt-in。评测的
// golden/trap/resume 用例探的正是「执法路径」本身（hook 是否识破陷阱），
// 夹具因此需要在 init 后把契约翻到 hooks: on + profile standard，否则所有
// 探测都在契约门静默放行，评测会变成假阴性。
//
// 只改 YAML 顶层的两个键，注释（含模板里以 "# hooks: off ..." 开头的说明行）
// 保持原样——按行前缀改而不是字符串替换，避免命中注释。

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// enableHooksInContract 把夹具仓库的契约改成 hooks: on + profile: "standard"。
// 契约不存在或两个键都找不到时返回错误——评测夹具必须显式声明执法意图，
// 静默降级会把它变成「探测了一个不执法的仓库」的假绿。
func enableHooksInContract(dir string) error {
	path := filepath.Join(dir, "spec", "forge.yaml")
	body, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// 单元测试里的桩 forge 不会写契约（也无执法路径可验）；缺契约时
			// 视为宿主层夹具，无需 opt-in。真实 forge init 一定写契约。
			return nil
		}
		return fmt.Errorf("read contract: %w", err)
	}
	lines := strings.Split(string(body), "\n")
	hooksSet, profileSet := false, false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "hooks:"):
			lines[i] = "hooks: on"
			hooksSet = true
		case strings.HasPrefix(trimmed, "profile:"):
			lines[i] = `profile: "standard"`
			profileSet = true
		}
	}
	if !hooksSet || !profileSet {
		return fmt.Errorf("contract %s missing hooks/profile keys (hooks=%v profile=%v)", path, hooksSet, profileSet)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		return fmt.Errorf("write contract: %w", err)
	}
	return nil
}
