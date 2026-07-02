package knowledge

import (
	"time"
)

// Entry is a single cross-project experience entry.
type Entry struct {
	ID          string    `json:"id"`
	Category    string    `json:"category"` // gotchas, patterns, apis
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Patterns    []string  `json:"patterns"`  // regex patterns for violation scanning
	Severity    string    `json:"severity"`  // error, warning, info
	Source      string    `json:"source"`    // project that produced this knowledge
	CreatedAt   time.Time `json:"created_at"`
}

// Index is the index.json for ~/.forge/knowledge/.
type Index struct {
	Version string  `json:"version"`
	Entries []Entry `json:"entries"`
}

// ValidCategories are the allowed knowledge categories.
var ValidCategories = map[string]bool{
	"gotchas":  true,
	"patterns": true,
	"apis":     true,
}
