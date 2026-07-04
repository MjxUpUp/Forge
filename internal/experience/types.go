// Package experience implements the V5 experience auto-extraction system.
// It manages review requests and experience proposals derived from low-scoring tasks.
package experience

import (
	"time"

	"github.com/MjxUpUp/Forge/internal/scoringtypes"
)

// ReviewStatus tracks the lifecycle of a review request.
type ReviewStatus string

const (
	ReviewPending  ReviewStatus = "pending"
	ReviewAnalyzed ReviewStatus = "analyzed"
	ReviewResolved ReviewStatus = "resolved"
)

// PropStatus tracks the lifecycle of an experience proposal.
type PropStatus string

const (
	PropProposed PropStatus = "proposed"
	PropAccepted PropStatus = "accepted"
	PropRejected PropStatus = "rejected"
)

// ReviewRequest is created when a task scores below threshold.
// Stored at ~/.forge/projects/<key>/reviews/{sanitized-ref}.json.
type ReviewRequest struct {
	TaskRef       string         `json:"task_ref"`
	Score         float64        `json:"score"`
	Grade         string         `json:"grade"`
	LowDimensions []LowDimension `json:"low_dimensions"`
	Mandatory     bool           `json:"mandatory"`
	Status        ReviewStatus   `json:"status"`
	CreatedAt     time.Time      `json:"created_at"`
}

// LowDimension captures a single low-scoring dimension.
type LowDimension struct {
	Dimension scoringtypes.Dimension `json:"dimension"`
	Score     int                    `json:"score"`
	Detail    string                 `json:"detail"`
}

// ExperienceProposal is an AI-generated rule proposal.
// Stored at ~/.forge/projects/<key>/experience/proposed/{id}.json.
type ExperienceProposal struct {
	ID           string     `json:"id"`
	SourceReview string     `json:"source_review"`
	Category     string     `json:"category"`
	Title        string     `json:"title"`
	Description  string     `json:"description"`
	Patterns     []string   `json:"patterns"`
	Severity     string     `json:"severity"`
	Status       PropStatus `json:"status"`
	CreatedAt    time.Time  `json:"created_at"`
}
