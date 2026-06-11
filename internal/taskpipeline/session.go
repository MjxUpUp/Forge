package taskpipeline

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// sessionMaxIdle is the maximum idle time before a session is rotated.
// If the current session was started more than this duration ago, a new
// session is created on the next task start.
const sessionMaxIdle = 2 * time.Hour

// SessionRecord represents one agent development session.
// Sessions group tasks that were created in the same agent interaction.
type SessionRecord struct {
	SessionID string    `json:"session_id"`
	StartedAt time.Time `json:"started_at"`
	AgentType string    `json:"agent_type,omitempty"`
}

// sessionFilePath returns the path to the current session tracker.
func sessionFilePath(root string) string {
	return filepath.Join(root, ".forge", "session.json")
}

// sessionsLogPath returns the path to the historical sessions log.
func sessionsLogPath(root string) string {
	return filepath.Join(root, ".forge", "sessions.jsonl")
}

// EnsureSession returns the current active session, creating one if needed.
// Sessions older than sessionMaxIdle are rotated — the old session is archived
// to sessions.jsonl and a new one is created.
func EnsureSession(root string) (*SessionRecord, error) {
	// Try to load existing session
	existing, err := loadSession(root)
	if err != nil {
		return nil, err
	}

	if existing != nil && time.Since(existing.StartedAt) < sessionMaxIdle {
		return existing, nil
	}

	// Archive the old session before creating a new one
	if existing != nil {
		if err := archiveSession(root, existing); err != nil {
			return nil, err
		}
	}

	// Create new session
	session := &SessionRecord{
		SessionID: newSessionID(),
		StartedAt: time.Now(),
		AgentType: detectAgentType(root),
	}

	if err := saveSession(root, session); err != nil {
		return nil, err
	}

	// Also write to historical log
	if err := appendSessionLog(root, session); err != nil {
		return nil, err
	}

	return session, nil
}

// loadSession reads the current session file. Returns nil if not found.
func loadSession(root string) (*SessionRecord, error) {
	path := sessionFilePath(root)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var s SessionRecord
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, nil
	}
	return &s, nil
}

// saveSession writes the current session file.
func saveSession(root string, s *SessionRecord) error {
	dir := filepath.Dir(sessionFilePath(root))
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(sessionFilePath(root), data, 0644)
}

// archiveSession writes the completed session to the historical log.
func archiveSession(root string, s *SessionRecord) error {
	return appendSessionLog(root, s)
}

// appendSessionLog appends a session record to .forge/sessions.jsonl.
func appendSessionLog(root string, s *SessionRecord) error {
	dir := filepath.Dir(sessionsLogPath(root))
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	f, err := os.OpenFile(sessionsLogPath(root), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	_, err = f.Write(append(data, '\n'))
	return err
}

// LoadSessions reads all historical session records from .forge/sessions.jsonl.
// Also includes the current active session if one exists.
func LoadSessions(root string) ([]SessionRecord, error) {
	var sessions []SessionRecord

	// Read current session first (most recent)
	current, err := loadSession(root)
	if err != nil {
		return nil, err
	}

	// Read historical log
	f, err := os.Open(sessionsLogPath(root))
	if err != nil {
		if os.IsNotExist(err) {
			// Only current session exists
			if current != nil {
				return []SessionRecord{*current}, nil
			}
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var s SessionRecord
		if err := json.Unmarshal(scanner.Bytes(), &s); err != nil {
			continue
		}
		sessions = append(sessions, s)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	// Add current session if not already in log
	if current != nil {
		found := false
		for _, s := range sessions {
			if s.SessionID == current.SessionID {
				found = true
				break
			}
		}
		if !found {
			sessions = append(sessions, *current)
		}
	}

	return sessions, nil
}

// detectAgentType checks for known agent config directories.
func detectAgentType(root string) string {
	checks := map[string]string{
		".claude":                "claude-code",
		".cursor":                "cursor",
		".github/instructions":   "copilot",
		".windsurfrules":         "windsurf",
	}
	for dir, agent := range checks {
		path := filepath.Join(root, dir)
		if info, err := os.Stat(path); err == nil {
			if dir == ".windsurfrules" {
				// .windsurfrules is a file, not a directory
				if !info.IsDir() {
					return agent
				}
			} else if info.IsDir() {
				return agent
			}
		}
	}
	return ""
}

// newSessionID generates a unique session identifier with timestamp and random suffix.
func newSessionID() string {
	var buf [2]byte
	rand.Read(buf[:])
	return fmt.Sprintf("session-%s-%s", time.Now().Format("20060102150405"), hex.EncodeToString(buf[:]))
}
