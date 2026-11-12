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
// 标记不成对、或出现多对标记时报错并不动文件（只替换第一对会让后面那块永久陈旧）；
// 修好后重跑 `forge change agents`。
func EnsureAgentsBlock(root string) (AgentsAction, error) {
	path := filepath.Join(root, AgentsFile)
	block := agentsBlock()

	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) || (err == nil && strings.TrimSpace(string(raw)) == "") {
		// 新建时标题放标记外：后续同步只碰块内，不会把标题改成 H2。
		if err := os.WriteFile(path, []byte("# "+AgentsFile+"\n\n"+block), 0o644); err != nil {
			return "", fmt.Errorf("change: 写 %s: %w", AgentsFile, err)
		}
		return AgentsCreated, nil
	}
	if err != nil {
		return "", fmt.Errorf("change: 读 %s: %w", AgentsFile, err)
	}

	text := string(raw)
	if strings.Count(text, agentsBegin) > 1 || strings.Count(text, agentsEnd) > 1 {
		return "", fmt.Errorf("change: %s 里有多对 %s / %s——先删到只剩一对，再跑 `forge change agents` 重试", AgentsFile, agentsBegin, agentsEnd)
	}
	begin := strings.Index(text, agentsBegin)
	end := strings.Index(text, agentsEnd)
	switch {
	case begin < 0 && end < 0:
		// 追加路径只在末尾插入，原有字节（含尾部换行）一律保留。
		// 块前统一隔一个空行；文件以 CRLF 结尾时补 CRLF，避免制造混合行尾。
		next := text
		switch {
		case next == "":
		case strings.HasSuffix(next, "\r\n\r\n"):
		case strings.HasSuffix(next, "\r\n"):
			next += "\r\n"
		case strings.HasSuffix(next, "\n\n"):
		case strings.HasSuffix(next, "\n"):
			next += "\n"
		default:
			next += "\n\n"
		}
		next += block
		if err := os.WriteFile(path, []byte(next), 0o644); err != nil {
			return "", fmt.Errorf("change: 写 %s: %w", AgentsFile, err)
		}
		return AgentsAppended, nil
	case begin < 0 || end < 0 || end < begin:
		return "", fmt.Errorf("change: %s 里的 %s / %s 不成对——先手工修好标记，再跑 `forge change agents` 重试", AgentsFile, agentsBegin, agentsEnd)
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
