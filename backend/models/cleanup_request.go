package models

import (
	"encoding/hex"
	"fmt"
)

type CleanupDryRunRequest struct {
	Issues []CleanupIssueSelection `json:"issues"`
}

func (r CleanupDryRunRequest) Validate() error {
	if len(r.Issues) == 0 {
		return fmt.Errorf("issues must contain at least one selection")
	}
	seen := map[string]bool{}
	for _, item := range r.Issues {
		if item.IssueID == "" || seen[item.IssueID] || len(item.BeforeHash) != 64 {
			return fmt.Errorf("issues contain an invalid or duplicate selection")
		}
		if _, err := hex.DecodeString(item.BeforeHash); err != nil {
			return fmt.Errorf("issues contain an invalid before_hash")
		}
		seen[item.IssueID] = true
	}
	return nil
}
