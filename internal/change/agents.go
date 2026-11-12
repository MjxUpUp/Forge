package change

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// AGENTS.md 的托管块：forge 只改两个标记之间的内容，标记外是人写的东西。
// 这样仓库里那份给 agent 的说明书可以由代码维护，又不会覆盖团队自己的约定。
const (
	AgentsFile  = "AGENTS.md"
	agentsBegin = "<!-- forge:begin -->"
	agentsEnd   = "<!-- forge:end -->"
)

// AgentsAction 描述 EnsureAgentsBlock 这次做了什么，便于 CLI 如实报告。
type AgentsAction string

const (
	AgentsCreated   AgentsAction = "created"   // 文件不存在，整份写入
	AgentsUpdated   AgentsAction = "updated"   // 已有托管块，替换其中内容
	AgentsAppended  AgentsAction = "appended"  // 文件存在但没有托管块，追加
	AgentsUnchanged AgentsAction = "unchanged" // 托管块内容已是最新
)

// EnsureAgentsBlock 幂等写入 AGENTS.md 的托管块。
//
// 规则：只动 <!-- forge:begin --> 与 <!-- forge:end --> 之间；标记外一字不改。
// 只有一个标记时拒绝动手——猜一边等于静默改错位置，比报错更糟。
func EnsureAgentsBlock(root string) (AgentsAction, error) {
	path := filepath.Join(root, AgentsFile)
	block := agentsBlock()

	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.WriteFile(path, []byte(block), 0o644); err != nil {
			return "", fmt.Errorf("change: 写 %s: %w", AgentsFile, err)
		}
		return AgentsCreated, nil
	}
	if err != nil {
		return "", fmt.Errorf("change: 读 %s: %w", AgentsFile, err)
	}

	text := string(raw)
	begin := strings.Index(text, agentsBegin)
	end := strings.Index(text, agentsEnd)
	switch {
	case begin < 0 && end < 0:
		next := strings.TrimRight(text, "\n")
		if next != "" {
			next += "\n\n"
		}
		next += block
		if err := os.WriteFile(path, []byte(next), 0o644); err != nil {
			return "", fmt.Errorf("change: 写 %s: %w", AgentsFile, err)
		}
		return AgentsAppended, nil
	case begin < 0 || end < 0 || end < begin:
		return "", fmt.Errorf("change: %s 里的 %s / %s 不成对——先手工修好再同步", AgentsFile, agentsBegin, agentsEnd)
	}

	old := text[begin : end+len(agentsEnd)]
	if old == strings.TrimRight(block, "\n") {
		return AgentsUnchanged, nil
	}
	next := text[:begin] + strings.TrimRight(block, "\n") + text[end+len(agentsEnd):]
	if err := os.WriteFile(path, []byte(next), 0o644); err != nil {
		return "", fmt.Errorf("change: 写 %s: %w", AgentsFile, err)
	}
	return AgentsUpdated, nil
}

// agentsBlock 返回带成对标记的托管块（以换行结尾）。
func agentsBlock() string {
	body := strings.TrimSpace(renderTemplate(tmplAgents, "", ""))
	return agentsBegin + "\n" + body + "\n" + agentsEnd + "\n"
}
