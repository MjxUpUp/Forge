package experience

import (
	"fmt"

	"github.com/Harness/forge/internal/knowledge"
)

// AcceptProposal accepts a proposed experience entry, writing it into the
// global knowledge store. It also updates the proposal and source review status.
func AcceptProposal(root string, proposalID string) error {
	p, err := LoadProposal(root, proposalID)
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
		Source:      "auto-extracted:" + p.SourceReview,
		CreatedAt:   p.CreatedAt,
	}

	if err := idx.AddEntry(entry); err != nil {
		return fmt.Errorf("add knowledge entry: %w", err)
	}

	if err := UpdateProposalStatus(root, proposalID, PropAccepted); err != nil {
		return fmt.Errorf("update proposal status: %w", err)
	}

	if p.SourceReview != "" {
		_ = UpdateReviewStatus(root, p.SourceReview, ReviewResolved)
	}

	return nil
}

// RejectProposal rejects a proposed experience entry, changing its status
// to rejected. No knowledge store modifications are made.
func RejectProposal(root string, proposalID string) error {
	p, err := LoadProposal(root, proposalID)
	if err != nil {
		return err
	}
	if p.Status != PropProposed {
		return fmt.Errorf("proposal %s is %s, not proposed", proposalID, p.Status)
	}

	return UpdateProposalStatus(root, proposalID, PropRejected)
}
