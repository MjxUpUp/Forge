package experience

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// proposalDir returns the directory for experience proposals under the given root.
func proposalDir(root string) string {
	return filepath.Join(root, ".forge", "experience", "proposed")
}

// proposalPath returns the file path for a specific proposal.
func proposalPath(root, id string) string {
	return filepath.Join(proposalDir(root), id+".json")
}

// GenerateID creates a proposal ID in the format "exp-{hex}" using the current
// nanosecond timestamp modulo 0xFFFFFF.
func GenerateID() string {
	hex := time.Now().UnixNano() % 0xFFFFFF
	return fmt.Sprintf("exp-%x", hex)
}

// SaveProposal writes a proposal to .forge/experience/proposed/{id}.json.
// Auto-generates ID and CreatedAt if empty.
func SaveProposal(root string, p *ExperienceProposal) error {
	autoID := p.ID == ""
	if autoID {
		p.ID = GenerateID()
	}
	if p.CreatedAt.IsZero() {
		p.CreatedAt = time.Now()
	}

	dir := proposalDir(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create proposals dir: %w", err)
	}

	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal proposal: %w", err)
	}

	// GenerateID is time-based (UnixNano % 0xFFFFFF); rapid successive saves —
	// e.g. one proposal per low dimension in a GenerateProposalsForReview loop —
	// can land in the same 16ms bucket, collide on the same path, and silently
	// overwrite an earlier proposal (losing it, which can re-deadlock a review).
	// Retry with a fresh ID on collision, but only for auto-generated IDs:
	// explicit IDs are the caller's responsibility and trusted as-is.
	path := proposalPath(root, p.ID)
	if autoID {
		for attempts := 0; attempts < 8; attempts++ {
			if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
				break
			}
			p.ID = GenerateID()
			path = proposalPath(root, p.ID)
		}
	}
	return os.WriteFile(path, data, 0o644)
}

// LoadProposal reads a single proposal by ID.
func LoadProposal(root, id string) (*ExperienceProposal, error) {
	path := proposalPath(root, id)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("proposal %q not found", id)
		}
		return nil, fmt.Errorf("read proposal: %w", err)
	}
	var p ExperienceProposal
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parse proposal: %w", err)
	}
	return &p, nil
}

// ListProposals lists proposals, optionally filtered by status.
// Pass an empty string to return all proposals.
func ListProposals(root string, status PropStatus) ([]*ExperienceProposal, error) {
	dir := proposalDir(root)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read proposals dir: %w", err)
	}

	var proposals []*ExperienceProposal
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		var p ExperienceProposal
		if err := json.Unmarshal(data, &p); err != nil {
			continue
		}
		if status != "" && p.Status != status {
			continue
		}
		proposals = append(proposals, &p)
	}
	return proposals, nil
}

// UpdateProposalStatus loads a proposal, updates its status, and saves it.
func UpdateProposalStatus(root, id string, status PropStatus) error {
	p, err := LoadProposal(root, id)
	if err != nil {
		return err
	}
	p.Status = status
	return SaveProposal(root, p)
}
