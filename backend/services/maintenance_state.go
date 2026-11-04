package services

import "fmt"

type MaintenanceOperationStatus string

const (
	MaintenancePrepared           MaintenanceOperationStatus = "prepared"
	MaintenanceApplying           MaintenanceOperationStatus = "applying"
	MaintenanceApplied            MaintenanceOperationStatus = "applied"
	MaintenanceVerifying          MaintenanceOperationStatus = "verifying"
	MaintenanceVerified           MaintenanceOperationStatus = "verified"
	MaintenancePartialFailure     MaintenanceOperationStatus = "partial_failure"
	MaintenanceVerificationFailed MaintenanceOperationStatus = "verification_failed"
)

type MaintenanceActionStatus string

const (
	MaintenanceActionPending  MaintenanceActionStatus = "pending"
	MaintenanceActionApplying MaintenanceActionStatus = "applying"
	MaintenanceActionApplied  MaintenanceActionStatus = "applied"
	MaintenanceActionVerified MaintenanceActionStatus = "verified"
	MaintenanceActionFailed   MaintenanceActionStatus = "failed"
)

func ValidateMaintenanceOperationTransition(from, to MaintenanceOperationStatus) error {
	allowed := map[MaintenanceOperationStatus][]MaintenanceOperationStatus{
		MaintenancePrepared:  {MaintenanceApplying},
		MaintenanceApplying:  {MaintenanceApplied, MaintenancePartialFailure},
		MaintenanceApplied:   {MaintenanceVerifying},
		MaintenanceVerifying: {MaintenanceVerified, MaintenanceVerificationFailed},
	}
	for _, next := range allowed[from] {
		if next == to {
			return nil
		}
	}
	return fmt.Errorf("invalid maintenance operation transition %q -> %q", from, to)
}

func ValidateMaintenanceActionTransition(from, to MaintenanceActionStatus) error {
	allowed := map[MaintenanceActionStatus][]MaintenanceActionStatus{
		MaintenanceActionPending:  {MaintenanceActionApplying},
		MaintenanceActionApplying: {MaintenanceActionApplied, MaintenanceActionFailed},
		MaintenanceActionApplied:  {MaintenanceActionVerified, MaintenanceActionFailed},
	}
	for _, next := range allowed[from] {
		if next == to {
			return nil
		}
	}
	return fmt.Errorf("invalid maintenance action transition %q -> %q", from, to)
}
