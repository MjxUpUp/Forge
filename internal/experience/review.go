package experience

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/scoringtypes"
	"github.com/MjxUpUp/Forge/internal/taskcontext"
)

// ShouldReview determines whether a task score warrants a review.
// R2 根因修复：D 级（60-69）和 F 级（<60）任务必须走 mandatory review——不再允许
// "门禁通过但 D 级完成"。PendingMandatory 据此阻塞 task-complete，强制经验闭环介入。
// 70-79（C 级）可选 review（human 确认 borderline）；>=80（A/B 级）不创建。
func ShouldReview(overall float64) (create bool, mandatory bool) {
	if overall < 70 {
		return true, true // D/F 级：mandatory review，阻塞 task-complete 直到 resolved
	}
	if overall < 80 {
		return true, false // C 级（70-79）：可选 review（human 确认 borderline）
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

// reviewPath returns the file path for a specific task's review under the
// project's user-level DataDir (proj.ReviewsDir() = ~/.forge/projects/<key>/reviews/).
func reviewPath(proj *forgedata.Project, taskRef string) string {
	return filepath.Join(proj.ReviewsDir(), taskcontext.SanitizeRef(taskRef)+".json")
}

// CreateReview creates a review request if one does not already exist (idempotent).
func CreateReview(proj *forgedata.Project, taskRef string, result *scoringtypes.ScoreResult) error {
	dir := proj.ReviewsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create reviews dir: %w", err)
	}

	path := reviewPath(proj, taskRef)
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

	return SaveReview(proj, review)
}

// LoadReview reads a review request from disk.
func LoadReview(proj *forgedata.Project, taskRef string) (*ReviewRequest, error) {
	path := reviewPath(proj, taskRef)
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
func SaveReview(proj *forgedata.Project, r *ReviewRequest) error {
	dir := proj.ReviewsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create reviews dir: %w", err)
	}

	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal review: %w", err)
	}

	path := reviewPath(proj, r.TaskRef)
	return os.WriteFile(path, data, 0o644)
}

// ListReviews returns all review requests in the project's user-level reviews dir.
func ListReviews(proj *forgedata.Project) ([]*ReviewRequest, error) {
	dir := proj.ReviewsDir()
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
func UpdateReviewStatus(proj *forgedata.Project, taskRef string, status ReviewStatus) error {
	r, err := LoadReview(proj, taskRef)
	if err != nil {
		return err
	}
	r.Status = status
	return SaveReview(proj, r)
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
func ResolveReview(proj *forgedata.Project, taskRef string) error {
	return UpdateReviewStatus(proj, taskRef, ReviewResolved)
}

// PendingMandatory returns the task's review when it is a MANDATORY review still
// in the pending state — i.e. completion must be blocked until it is resolved.
// Returns (nil, false) when no review exists, the review is optional, or it has
// already been resolved.
//
// This is the enforcement point for low-scoring (<70) tasks. Previously the
// task-verify Stop hook blocked session end on a pending mandatory review; with
// task-verify now advisory, `forge task complete` calls this to block task
// completion instead. Failing at complete is a task-level block (the active
// task ref survives), so the session is not trapped in a stop-retry loop.
func PendingMandatory(proj *forgedata.Project, taskRef string) (*ReviewRequest, bool) {
	// Tolerates a nil proj (non-git project / ProjectFor failure): with no DataDir
	// there is nowhere to read reviews from, so there can be no pending mandatory
	// review. This lets task runTaskComplete call PendingMandatory(proj)
	// unconditionally without a nil-guard wrapper; write-path callers (CreateReview
	// et al.) still must skip on nil since they dereference proj.
	if proj == nil {
		return nil, false
	}
	r, err := LoadReview(proj, taskRef)
	if err != nil || r == nil {
		return nil, false
	}
	if r.Mandatory && r.Status == ReviewPending {
		return r, true
	}
	return nil, false
}
