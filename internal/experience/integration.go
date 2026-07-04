package experience

import (
	"fmt"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/knowledge"
)

// AcceptProposal accepts a proposed experience entry, writing it into the
// global knowledge store. It also updates the proposal and source review status.
func AcceptProposal(proj *forgedata.Project, proposalID string) error {
	return acceptProposal(proj, proposalID, "auto-extracted:")
}

// acceptProposal is the shared accept path. sourcePrefix is prepended to the
// source review ref so the knowledge entry records how it entered the store:
// "auto-extracted:" for manual `forge experience accept`,
// "auto-accepted:high-confidence:" for severe low-score auto-acceptance.
func acceptProposal(proj *forgedata.Project, proposalID, sourcePrefix string) error {
	p, err := LoadProposal(proj, proposalID)
	if err != nil {
		return err
	}
	if p.Status != PropProposed {
		return fmt.Errorf("proposal %s is %s, not proposed", proposalID, p.Status)
	}

	idx, err := knowledge.LoadIndex()
	if err != nil {
		return fmt.Errorf("load knowledge index: %w", err)
	}

	entry := knowledge.Entry{
		ID:          p.ID,
		Category:    p.Category,
		Title:       p.Title,
		Description: p.Description,
		Patterns:    p.Patterns,
		Severity:    p.Severity,
		Source:      sourcePrefix + p.SourceReview,
		CreatedAt:   p.CreatedAt,
	}

	if err := idx.AddEntry(entry); err != nil {
		return fmt.Errorf("add knowledge entry: %w", err)
	}

	if err := UpdateProposalStatus(proj, proposalID, PropAccepted); err != nil {
		return fmt.Errorf("update proposal status: %w", err)
	}

	if p.SourceReview != "" {
		_ = UpdateReviewStatus(proj, p.SourceReview, ReviewResolved)
	}

	return nil
}

// RejectProposal rejects a proposed experience entry, changing its status
// to rejected. No knowledge store modifications are made.
func RejectProposal(proj *forgedata.Project, proposalID string) error {
	p, err := LoadProposal(proj, proposalID)
	if err != nil {
		return err
	}
	if p.Status != PropProposed {
		return fmt.Errorf("proposal %s is %s, not proposed", proposalID, p.Status)
	}

	return UpdateProposalStatus(proj, proposalID, PropRejected)
}
