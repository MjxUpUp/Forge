package cli

// verify_scenarios_test.go — verify_scenarios.go 中场景断言辅助函数的单元守卫。
// E2E 场景本身需要构建出的 forge 二进制、由 `forge verify` 驱动；结构化注册表
// 断言是有平台相关 bug 史的部分（Windows：JSON 转义反斜杠令原始子串匹配恒红），
// 故在此免子进程钉住。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestScenarioRegistryHasPath_BackslashPathPinned 钉死 Windows 转义修复：
// json.Marshal 写出的注册表会把每个反斜杠转义，旧的原始子串匹配
// （在编码字节里找 `"C:\Users\..."`）永远命不中。结构化比较必须解码 JSON 按
// 值匹配路径——本测试构造的正是这种编码形态并断言命中。
func TestScenarioRegistryHasPath_BackslashPathPinned(t *testing.T) {
	// 含反斜杠的路径：Windows 的真实形态（unix 上也是合法的特殊文件名），
	// 两个平台都能钉。
	want := strings.Join([]string{"C:", "Users", "dev", "proj"}, "\\")
	// 按 forge 子进程的方式编码注册表（json.Marshal——被测的转义行为）。
	reg := struct {
		Projects []struct {
			Path string `json:"path"`
			Key  string `json:"key,omitempty"`
		} `json:"projects"`
	}{
		Projects: []struct {
			Path string `json:"path"`
			Key  string `json:"key,omitempty"`
		}{{Path: want, Key: "k1"}},
	}
	data, err := json.Marshal(reg)
	if err != nil {
		t.Fatal(err)
	}
	// 自检：编码形态确实含转义反斜杠（旧子串匹配失效的条件）。
	if !strings.Contains(string(data), `\\`) {
		t.Fatalf("precondition: encoded registry should contain escaped backslashes, got %s", data)
	}
	if !scenarioRegistryHasPath(data, want) {
		t.Errorf("结构化断言应命中反斜杠路径 %s, registry=%s", want, data)
	}
}

// TestScenarioRegistryHasPath_HitAndMiss 覆盖值语义：不同路径不得命中（防子串
// 巧合——前缀/后缀兄弟目录不算登记），且条目列表与遗留字符串列表两种形态都可解码。
func TestScenarioRegistryHasPath_HitAndMiss(t *testing.T) {
	registered := filepath.Join(string(filepath.Separator), "home", "alice", "repo")
	sibling := registered + "-other"

	entryList := []byte(`{"projects":[{"path":` + quoteJSON(registered) + `,"key":"k"}]}`)
	if !scenarioRegistryHasPath(entryList, registered) {
		t.Errorf("entry-list 形态应命中已登记路径")
	}
	if scenarioRegistryHasPath(entryList, sibling) {
		t.Errorf("兄弟目录 %s 不得命中（子串巧合）", sibling)
	}

	legacy := []byte(`{"projects":[` + quoteJSON(registered) + `]}`)
	if !scenarioRegistryHasPath(legacy, registered) {
		t.Errorf("legacy 字符串列表形态应命中已登记路径")
	}

	if scenarioRegistryHasPath([]byte(`{"projects":[]}`), registered) {
		t.Errorf("空注册表不得命中")
	}
	if scenarioRegistryHasPath([]byte(`not json`), registered) {
		t.Errorf("非法 JSON 应按未命中处理（不 panic）")
	}
}

// quoteJSON 把 s 编码为 JSON 字符串（处理反斜杠/引号转义）。
func quoteJSON(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// TestMasterReminderAcceptanceRegistrationPinned 钉住 2026-09-21 Nightly 红的
// 修复接线：master-reminder 场景在 task complete 前必须登记验收并实跑——
// oracle-pipeline L1 给 complete 加了验收登记硬前置（零标准=考卷缺位直接拦），
// 该门合入时场景未同步适配即红。顺序按源内字节序断言：accept 登记 →
// verify-acceptance 实跑 → complete；任一步脱落或乱序（实跑在 complete 后、
// 或登记缺失）都会让场景在 CI 上确定性红。
func TestMasterReminderAcceptanceRegistrationPinned(t *testing.T) {
	b, err := os.ReadFile("verify_scenarios.go")
	if err != nil {
		t.Fatalf("读 verify_scenarios.go: %v", err)
	}
	src := string(b)
	acceptIdx := strings.Index(src, `"task", "accept", "go version :: go version"`)
	verifyIdx := strings.Index(src, `"task", "verify-acceptance"`)
	completeIdx := strings.Index(src, `"task", "complete", "--ref", "EXP-1"`)
	if acceptIdx < 0 || verifyIdx < 0 || completeIdx < 0 {
		t.Fatalf("master-reminder 验收接线脱落：accept=%d verify-acceptance=%d complete=%d——complete 前必须登记验收并实跑（L1 登记门）", acceptIdx, verifyIdx, completeIdx)
	}
	if !(acceptIdx < verifyIdx && verifyIdx < completeIdx) {
		t.Fatalf("master-reminder 验收接线乱序：登记(accept@%d)与实跑(verify-acceptance@%d)必须在 complete(@%d) 之前", acceptIdx, verifyIdx, completeIdx)
	}
	// 排查盲钉住：complete 失败信息必须带命令输出——Nightly 红当晚 CI 日志只剩
	// 「task complete failed: exit status 1」，真实报错被场景丢弃。needle 用解释
	// 字符串形态（\\n 产出源码里的两字符转义序列）——raw string 写字面 \n 会被
	// srclint TestNoLiteralBackslashNInSingleLineRawStrings 钉住（分支 CI 红）。
	if !strings.Contains(src, "task complete failed: %v\\n%s") {
		t.Fatalf("task complete 失败信息应带命令输出（%%v\\n%%s）——丢输出令 CI 日志只剩 exit status 1")
	}
}
