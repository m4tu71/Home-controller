package models

// DataHealthIssue describes a read-only finding. Hash is the SHA-256 of the
// observed file bytes, or of the directory's sorted immediate entry names.
type DataHealthIssue struct {
	ID                    string `json:"id"`
	Severity              string `json:"severity"`
	Path                  string `json:"path"`
	CanonicalPath         string `json:"canonical_path"`
	Hash                  string `json:"hash"`
	Message               string `json:"message"`
	HasDestructiveActions bool   `json:"has_destructive_actions"`
}
