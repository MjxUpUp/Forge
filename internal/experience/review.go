package experience

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Harness/forge/internal/scoringtypes"
	"github.com/Harness/forge/internal/taskcontext"
)

// ShouldReview determines whether a task score warrants a review.
// Thresholds: <70 mandatory, 70-79 optional, >=80 none.
func ShouldReview(overall float64) (create bool, mandatory bool) {
	if overall < 70 {
		return true, true
	}
	if overall < 80 {
		return true, false
	}
	return false, false
}

// LowDimensions extracts dimensions with score < 70 from a ScoreResult.
func LowDimensions(result *scoringtypes.ScoreResult) []LowDimension {
	var lows []LowDimension
	for _, ds := range result.Dimensions {
		if ds.Score < 70 {
			lows = append(lows, LowDimension{
				Dimension: ds.Dimension,
				Score:     ds.Score,
				Detail:    ds.Detail,
			})
		}
	}
	return lows
}

// reviewDir returns the directory for review files under the given root.
func reviewDir(root string) string {
	return filepath.Join(root, ".forge", "reviews")
}

// reviewPath returns the file path for a specific task's review.
func reviewPath(root, taskRef string) string {
	return filepath.Join(reviewDir(root), taskcontext.SanitizeRef(taskRef)+".json")
}

// CreateReview creates a review request if one does not already exist (idempotent).
func CreateReview(root string, taskRef string, result *scoringtypes.ScoreResult) error {
	dir := reviewDir(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create reviews dir: %w", err)
	}

	path := reviewPath(root, taskRef)
	// Idempotent: skip if already exists.
	if _, err := os.Stat(path); err == nil {
		return nil
	}

	_, mandatory := ShouldReview(result.Overall)
	review := &ReviewRequest{
		TaskRef:       taskRef,
		Score:         result.Overall,
		Grade:         result.Grade,
		LowDimensions: LowDimensions(result),
		Mandatory:     mandatory,
		Status:        ReviewPending,
		CreatedAt:     result.ScoredAt,
	}

	return SaveReview(root, review)
}

// LoadReview reads a review request from disk.
func LoadReview(root, taskRef string) (*ReviewRequest, error) {
	path := reviewPath(root, taskRef)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("review for %q not found", taskRef)
		}
		return nil, fmt.Errorf("read review: %w", err)
	}
	var r ReviewRequest
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("parse review: %w", err)
	}
	return &r, nil
}

// SaveReview writes a review request to disk.
func SaveReview(root string, r *ReviewRequest) error {
	dir := reviewDir(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create reviews dir: %w", err)
	}

	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal review: %w", err)
	}

	path := reviewPath(root, r.TaskRef)
	return os.WriteFile(path, data, 0o644)
}

// ListReviews returns all review requests in the .forge/reviews directory.
func ListReviews(root string) ([]*ReviewRequest, error) {
	dir := reviewDir(root)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read reviews dir: %w", err)
	}

	var reviews []*ReviewRequest
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		var r ReviewRequest
		if err := json.Unmarshal(data, &r); err != nil {
			continue
		}
		reviews = append(reviews, &r)
	}
	return reviews, nil
}

// UpdateReviewStatus changes the status of an existing review and persists it.
func UpdateReviewStatus(root, taskRef string, status ReviewStatus) error {
	r, err := LoadReview(root, taskRef)
	if err != nil {
		return err
	}
	r.Status = status
	return SaveReview(root, r)
}

// ResolveReview marks a review as resolved independently of any proposal.
//
// This is the escape hatch that decouples review resolution from AcceptProposal.
// AcceptProposal (integration.go) is otherwise the ONLY path to ReviewResolved,
// which meant any review with zero proposals to accept — a dimension-template
// gap, a SaveProposal failure, or all proposals rejected — deadlocked the
// task-verify Stop hook on a pending mandatory review. With a direct resolve,
// a stuck review can always be cleared via `forge experience resolve <task-ref>`.
// AcceptProposal still resolves as before when a proposal is accepted.
func ResolveReview(root, taskRef string) error {
	return UpdateReviewStatus(root, taskRef, ReviewResolved)
}
