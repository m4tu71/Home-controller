package services

import (
	"backend/models"
	"backend/repositories"
	"fmt"
	"path/filepath"
	"strings"
)

// Read only the verified isolated copy, never the live runtime or original bundle.
func validateBackupStatefulReadback(root string, files []models.BackupFile) error {
	repo := repositories.NewIRAdapterRepositoryWithRoomsDir(filepath.Join(root, "rooms"))
	for _, entry := range files {
		parts := strings.Split(entry.Path, "/")
		if entry.JSONStatus != "valid" || len(parts) < 7 || parts[0] != "rooms" || parts[2] != "devices" || parts[4] != "adapters" || parts[5] != "ir" {
			continue
		}
		if len(parts) == 7 && parts[6] == "state_schema.json" {
			schema, err := repo.LoadStateSchema(parts[1], parts[3])
			if err != nil {
				return fmt.Errorf("State Schema readback %s: %w", entry.Path, err)
			}
			if err := validateIRStateSchema(schema); err != nil {
				return fmt.Errorf("State Schema readback %s: %w", entry.Path, err)
			}
		}
		if len(parts) == 8 && parts[6] == "states" && strings.HasSuffix(parts[7], ".json") {
			state, err := repo.LoadState(parts[1], parts[3], strings.TrimSuffix(parts[7], ".json"))
			if err != nil {
				return fmt.Errorf("State readback %s: %w", entry.Path, err)
			}
			if !validateOperationFormat(state.Format) {
				return fmt.Errorf("State readback %s: unsupported format %q", entry.Path, state.Format)
			}
			schema, err := repo.LoadStateSchema(parts[1], parts[3])
			if err != nil {
				return fmt.Errorf("State Schema readback for %s: %w", entry.Path, err)
			}
			if err := validateIRStateMatchesSchema(state.State, schema); err != nil {
				return fmt.Errorf("State readback %s: %w", entry.Path, err)
			}
		}
	}
	return nil
}
