package change

import (
	"os"
	"path/filepath"
	"strings"
)

// ReadAgentsBlock 返回 AGENTS.md 里托管块的正文（标记之间，已去首尾空白、行尾归一为 LF）。
//
// ok=false 表示文件不存在或没有托管块——调用方要能区分「块陈旧」与「根本没有块」，
// 这两种情况的修复动作不同。
func ReadAgentsBlock(root string) (body string, ok bool) {
	raw, err := os.ReadFile(filepath.Join(root, AgentsFile))
	if err != nil {
		return "", false
	}
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	begin := strings.Index(text, agentsBegin)
	end := strings.Index(text, agentsEnd)
	if begin < 0 || end < 0 || end < begin {
		return "", false
	}
	inner := text[begin+len(agentsBegin) : end]
	return strings.TrimSpace(inner), true
}
