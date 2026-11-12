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
func EnsureAgentsBlock(root string) (AgentsAction, error) {
	return ensureManagedBlock(filepath.Join(root, AgentsFile), "# "+AgentsFile+"\n\n",
		managedBlock{begin: agentsBegin, end: agentsEnd, body: strings.TrimSpace(renderTemplate(tmplAgents, "", ""))})
}

// managedBlock 是"标记包住的一段由代码维护的文字"。
type managedBlock struct {
	begin, end, body string
}

// ensureManagedBlock 幂等写入 path 里的托管块。
//
// 文件不存在或只有空白时按 header+块 整份写；文件存在但没有标记时追加到末尾
// （原有字节与行尾一律保留）；有标记时只替换标记之间。
//
// 不成对、或多对标记都拒绝动手——猜一边等于静默改错位置，比报错更糟；
// 只替换第一对会让后面那块永久陈旧。
func ensureManagedBlock(path, header string, mb managedBlock) (AgentsAction, error) {
	name := filepath.Base(path)
	block := mb.begin + "\n" + mb.body + "\n" + mb.end

	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) || (err == nil && strings.TrimSpace(string(raw)) == "") {
		if err := os.WriteFile(path, []byte(header+block+"\n"), 0o644); err != nil {
			return "", fmt.Errorf("change: 写 %s: %w", name, err)
		}
		return AgentsCreated, nil
	}
	if err != nil {
		return "", fmt.Errorf("change: 读 %s: %w", name, err)
	}

	text := string(raw)
	if strings.Count(text, mb.begin) > 1 || strings.Count(text, mb.end) > 1 {
		return "", fmt.Errorf("change: %s 里有多对 %s / %s——先删到只剩一对，再跑 `forge change agents` 重试", name, mb.begin, mb.end)
	}
	begin := strings.Index(text, mb.begin)
	end := strings.Index(text, mb.end)
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
		next += block + "\n"
		if err := os.WriteFile(path, []byte(next), 0o644); err != nil {
			return "", fmt.Errorf("change: 写 %s: %w", name, err)
		}
		return AgentsAppended, nil
	case begin < 0 || end < 0 || end < begin:
		return "", fmt.Errorf("change: %s 里的 %s / %s 不成对——先手工修好标记，再跑 `forge change agents` 重试", name, mb.begin, mb.end)
	}

	old := text[begin : end+len(mb.end)]
	if old == block {
		return AgentsUnchanged, nil
	}
	next := text[:begin] + block + text[end+len(mb.end):]
	if err := os.WriteFile(path, []byte(next), 0o644); err != nil {
		return "", fmt.Errorf("change: 写 %s: %w", name, err)
	}
	return AgentsUpdated, nil
}

// agentsBlock 返回带成对标记的托管块（以换行结尾）。
func agentsBlock() string {
	body := strings.TrimSpace(renderTemplate(tmplAgents, "", ""))
	return agentsBegin + "\n" + body + "\n" + agentsEnd + "\n"
}

// 单源多指针：规范只有 AGENTS.md 里的一份，其它 agent 的入口文件只放一句
// 指向它的话。文件不存在时默认不创建——替一个不用 Cursor 的仓库建
// .cursor/rules/ 就是污染；要启用显式说 `--pointers`。
const (
	ClaudeFile     = "CLAUDE.md"
	CursorRuleFile = ".cursor/rules/forge.mdc"
	pointerBegin   = "<!-- forge:pointer:begin -->"
	pointerEnd     = "<!-- forge:pointer:end -->"
)

const (
	pointerLead       = "本仓库的协作约定在 `AGENTS.md`（forge 托管块）里——规范只有一份，这里只放指针。"
	cursorFrontmatter = "---\ndescription: Forge 协作约定（指针，正文见 AGENTS.md）\nalwaysApply: true\n---\n\n"
)

// PointerReport 描述一个指针文件这次的结果。
//
// Skipped 为真表示文件不存在、也没有被要求创建：仓库没用这个 agent，
// 不该替它建文件。
type PointerReport struct {
	File    string
	Action  AgentsAction
	Skipped bool
}

// EnsureAgentPointers 维护"指向 AGENTS.md"的指针文件。
//
// create 里点名（"claude"/"cursor"）的文件即使不存在也会创建；没点名的只在
// 文件已存在时更新（说明这个仓库已经用了该 agent）。返回顺序固定：
// CLAUDE.md、.cursor/rules/forge.mdc。
func EnsureAgentPointers(root string, create []string) ([]PointerReport, error) {
	want := make(map[string]bool, len(create))
	for _, name := range create {
		want[name] = true
	}
	var out []PointerReport

	claudePath := filepath.Join(root, ClaudeFile)
	claudeBody := pointerLead + "\n\n@AGENTS.md"
	if exists(claudePath) || want["claude"] {
		action, err := ensureManagedBlock(claudePath, "# "+ClaudeFile+"\n\n",
			managedBlock{begin: pointerBegin, end: pointerEnd, body: claudeBody})
		if err != nil {
			return out, err
		}
		out = append(out, PointerReport{File: ClaudeFile, Action: action})
	} else {
		out = append(out, PointerReport{File: ClaudeFile, Skipped: true})
	}

	cursorPath := filepath.Join(root, CursorRuleFile)
	cursorBody := pointerLead + "\n\n先读 `AGENTS.md` 的托管块，再按它执行。"
	cursorDir := filepath.Dir(cursorPath)
	switch {
	case exists(cursorPath) || want["cursor"]:
		if err := os.MkdirAll(cursorDir, 0o755); err != nil {
			return out, fmt.Errorf("change: 创建 %s: %w", filepath.ToSlash(cursorDir), err)
		}
		action, err := ensureManagedBlock(cursorPath, cursorFrontmatter,
			managedBlock{begin: pointerBegin, end: pointerEnd, body: cursorBody})
		if err != nil {
			return out, err
		}
		out = append(out, PointerReport{File: filepath.ToSlash(CursorRuleFile), Action: action})
	case exists(cursorDir):
		// 目录在就说明仓库已经用 Cursor，补一个 forge 专用规则文件是低成本的。
		action, err := ensureManagedBlock(cursorPath, cursorFrontmatter,
			managedBlock{begin: pointerBegin, end: pointerEnd, body: cursorBody})
		if err != nil {
			return out, err
		}
		out = append(out, PointerReport{File: filepath.ToSlash(CursorRuleFile), Action: action})
	default:
		out = append(out, PointerReport{File: filepath.ToSlash(CursorRuleFile), Skipped: true})
	}
	return out, nil
}
