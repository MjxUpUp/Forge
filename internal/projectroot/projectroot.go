// Package projectroot resolves the forge project root (the directory containing
// a .forge/ subdirectory) from the current working directory.
//
// Shared by cli and mcpserver so the "walk up from cwd to find .forge/" logic
// lives in one place — previously duplicated in cli/root.go and
// mcpserver/server.go, which risked diverging (a bug fixed on one side would
// silently miss the other).
package projectroot

import (
	"fmt"
	"os"
	"path/filepath"
)

// Find walks up from the current working directory to the nearest directory
// containing a project .forge/ subdirectory. Returns the project root, or an
// error if cwd is not inside a forge project.
//
// The user home directory's ~/.forge/ is the GLOBAL state store (knowledge,
// hooks, skills — see knowledge/store.go which hardcodes ~/.forge/knowledge),
// NOT a project root. It is excluded so running forge from a non-project
// directory under home (e.g. ~/Downloads) reports "not in a forge project"
// instead of mistaking home for the project root. A real project's .forge/
// always sits closer to cwd than home does, so this exclusion never hides a
// legitimate project.
func Find() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	homeDir, _ := os.UserHomeDir() // 全局状态目录约定在 home/.forge；解析失败则不排除（退化原行为）
	for {
		if isProjectRoot(dir, homeDir) {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("not in a forge project (no .forge/ directory found)")
		}
		dir = parent
	}
}

// isProjectRoot reports whether dir holds a project .forge/ directory: dir must
// contain .forge/ and must NOT be the user home (whose ~/.forge/ is the global
// state store, indistinguishable from a project by name or contents — both
// carry checklog.jsonl/toollog.jsonl; position is the only clean discriminator).
func isProjectRoot(dir, homeDir string) bool {
	if info, err := os.Stat(filepath.Join(dir, ".forge")); err != nil || !info.IsDir() {
		return false
	}
	if homeDir != "" && samePath(dir, homeDir) {
		return false
	}
	return true
}

// samePath reports whether a and b refer to the same filesystem path. Uses
// os.SameFile (device+inode) so it is robust across case-insensitivity, symlinks,
// and separator/style differences (Git Bash "/c/Users" vs Windows "C:\Users").
// Falls back to a cleaned lexical compare if either path cannot be stat'd.
func samePath(a, b string) bool {
	ia, ea := os.Stat(a)
	ib, eb := os.Stat(b)
	if ea == nil && eb == nil {
		return os.SameFile(ia, ib)
	}
	return filepath.Clean(a) == filepath.Clean(b)
}
