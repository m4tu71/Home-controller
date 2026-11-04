package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"backend/repositories"
	"backend/services"
)

type maintenanceBackupRestorer interface {
	RestoreDryRun(string) (repositories.BackupRestorePreview, error)
	RestoreRehearsal(string) (repositories.BackupRestoreResult, error)
}

type maintenanceTrashFinalDeleter interface {
	DryRun(string) (services.TrashFinalDeletePreview, error)
	Apply(string, string) (repositories.TrashFinalDeleteResult, error)
}

func runTrashFinalDeleteCLI(args []string, service maintenanceTrashFinalDeleter, tty io.ReadWriter, out io.Writer) error {
	if len(args) != 3 || service == nil || out == nil {
		return errors.New("usage: backend maintenance {trash-final-delete-dry-run --entry-id <entry-id>|trash-final-delete --plan-id <plan-id>}")
	}
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	switch args[0] {
	case "trash-final-delete-dry-run":
		if args[1] != "--entry-id" {
			return errors.New("trash-final-delete-dry-run requires --entry-id")
		}
		preview, err := service.DryRun(args[2])
		if err != nil {
			return err
		}
		return encoder.Encode(preview)
	case "trash-final-delete":
		if args[1] != "--plan-id" || tty == nil {
			return errors.New("trash-final-delete requires --plan-id and a controlling TTY")
		}
		if _, err := fmt.Fprint(tty, "Re-enter the Trash entry ID to permanently delete: "); err != nil {
			return err
		}
		scanner := bufio.NewScanner(tty)
		scanner.Buffer(make([]byte, 0, 128), 256)
		if !scanner.Scan() {
			if scanner.Err() != nil {
				return scanner.Err()
			}
			return services.ErrTrashFinalDeleteConfirmation
		}
		result, err := service.Apply(args[2], strings.TrimRight(scanner.Text(), "\r\n"))
		if err != nil {
			return err
		}
		return encoder.Encode(result)
	default:
		return fmt.Errorf("unknown maintenance command %q", args[0])
	}
}

func runMaintenanceCLI(args []string, service maintenanceBackupRestorer, out io.Writer) error {
	if len(args) != 2 {
		return errors.New("usage: backend maintenance {restore-dry-run|restore-rehearsal} <backupId>")
	}
	if service == nil || out == nil {
		return errors.New("backup restore CLI unavailable")
	}
	var result any
	switch args[0] {
	case "restore-dry-run":
		v, err := service.RestoreDryRun(args[1])
		if err != nil {
			return err
		}
		result = v
	case "restore-rehearsal":
		v, err := service.RestoreRehearsal(args[1])
		if err != nil {
			return err
		}
		result = v
	default:
		return fmt.Errorf("unknown maintenance command %q", args[0])
	}
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}
