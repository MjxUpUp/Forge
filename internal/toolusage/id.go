package toolusage

import (
	"crypto/sha1"
	"encoding/hex"
	"time"
)

// computeID derives a stable 12-char hex identifier for a tool call from its
// identity fields (task + session + timestamp + tool name). Two calls sharing
// all four collide only if they are the same logical call; distinct timestamps
// never collide. The ID lets an anti-pattern violation point back at the exact
// ToolCall that triggered it, so `forge trace` can annotate the offending call
// rather than naming only the tool.
func computeID(c ToolCall) string {
	h := sha1.New()
	h.Write([]byte(c.TaskRef + "|" + c.SessionID + "|" + c.Timestamp.Format(time.RFC3339Nano) + "|" + c.ToolName))
	return hex.EncodeToString(h.Sum(nil))[:12]
}

// ensureID returns the call's existing ID, computing one if absent. Toollog
// entries written before IDs existed deserialize to ID="" — this backfills
// them on load so trace can reference violations even for historical tasks.
func ensureID(c ToolCall) string {
	if c.ID != "" {
		return c.ID
	}
	return computeID(c)
}
