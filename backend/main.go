package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"

	"backend/controller_client"
	"backend/handlers"
	"backend/middleware"
	"backend/repositories"
	"backend/services"
	appconfig "webcontroller/config"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

func run(args []string) error {
	path, rest, err := appconfig.ParseArgs(args)
	if err != nil {
		return err
	}
	config, err := appconfig.Load(path)
	if err != nil {
		return err
	}
	if len(rest) > 0 && rest[0] == "maintenance" {
		if len(rest) > 1 && (rest[1] == "trash-final-delete-dry-run" || rest[1] == "trash-final-delete") {
			service, err := configuredTrashFinalDeleteService(config)
			if err != nil {
				return err
			}
			var tty io.ReadWriter
			if rest[1] == "trash-final-delete" {
				terminal, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
				if err != nil {
					return fmt.Errorf("controlling TTY required: %w", err)
				}
				defer terminal.Close()
				tty = terminal
			}
			return runTrashFinalDeleteCLI(rest[1:], service, tty, os.Stdout)
		}
		repository, err := configuredBackupRepository(config)
		if err != nil {
			return fmt.Errorf("maintenance backup configuration invalid: %w", err)
		}
		service := services.NewBackupService(repository, nil)
		return runMaintenanceCLI(rest[1:], service, os.Stdout)
	}
	if len(rest) != 0 {
		return fmt.Errorf("unexpected argument %q", rest[0])
	}
	mux := newHTTPMuxWithConfig(config)
	listener, err := listenConfiguredHTTP(config)
	if err != nil {
		return err
	}
	log.Printf("Go API server started on %s", listener.Addr())
	return http.Serve(listener, middleware.Logging(mux))
}

func listenConfiguredHTTP(config appconfig.Config) (net.Listener, error) {
	return net.Listen("tcp", config.HTTPListen)
}

// configuredTrashFinalDeleteService is the single config-root construction
// path used by the maintenance CLI. Keeping it separate makes the dependency
// wiring directly testable without opening a real controlling TTY.
func configuredTrashFinalDeleteService(config appconfig.Config) (*services.TrashFinalDeleteService, error) {
	state, err := repositories.NewMaintenanceState(repositories.MaintenanceStateConfig{
		Dir: config.MaintenanceRoot, RuntimeDataDir: config.DataRoot,
		RepositoryDir: config.SoftwareRoot, WebRootDir: config.WebRoot,
		BackupDir: config.BackupRoot, TrashDir: config.TrashRoot,
	})
	if err != nil {
		return nil, fmt.Errorf("maintenance state configuration invalid: %w", err)
	}
	trash, err := repositories.NewTrashRepository(repositories.TrashConfig{
		Dir: config.TrashRoot, RuntimeDataDir: config.DataRoot,
		RepositoryDir: config.SoftwareRoot, WebRootDir: config.WebRoot,
		StateDir: config.MaintenanceRoot, BackupDir: config.BackupRoot,
		RetentionDays: config.TrashRetentionDays,
	})
	if err != nil {
		return nil, fmt.Errorf("Trash final-delete configuration invalid: %w", err)
	}
	backups, err := configuredBackupRepository(config)
	if err != nil {
		return nil, fmt.Errorf("maintenance backup configuration invalid: %w", err)
	}
	return services.NewTrashFinalDeleteService(trash, services.NewMaintenancePlanService(state.Plans(), nil), state.Operations(), backups, config.TrashRoot), nil
}

func configuredBackupRepository(config appconfig.Config) (*repositories.BackupRepository, error) {
	return repositories.NewBackupRepository(repositories.BackupConfig{
		Dir: config.BackupRoot, RuntimeDataDir: config.DataRoot,
		RepositoryDir: config.SoftwareRoot, WebRootDir: config.WebRoot,
		StateDir: config.MaintenanceRoot, TrashDir: config.TrashRoot,
	})
}

func newHTTPMux() *http.ServeMux {
	return newHTTPMuxWithDataRoot("")
}

type maintenanceMuxDependencies struct {
	runtimeDataDir      string
	trashDir            string
	state               *repositories.MaintenanceState
	trashRepository     *repositories.TrashRepository
	crudTrashRepository *repositories.TrashRepository
	backupRepository    *repositories.BackupRepository
}

func newHTTPMuxWithDataRoot(roomsDir string) *http.ServeMux {
	runtimeDataDir := "/var/lib/web-controller/data"
	if roomsDir != "" {
		runtimeDataDir = filepath.Dir(roomsDir)
	}
	deps := maintenanceMuxDependencies{runtimeDataDir: runtimeDataDir, trashDir: os.Getenv("WEB_CONTROLLER_TRASH_DIR")}
	var err error
	deps.state, err = repositories.NewMaintenanceStateFromEnvWithRuntimeDataDir(runtimeDataDir)
	if err != nil {
		log.Printf("maintenance history unavailable: %v", err)
	}
	deps.trashRepository, err = repositories.NewTrashRepositoryFromEnvWithRuntimeDataDir(runtimeDataDir)
	if err != nil {
		log.Printf("Trash history unavailable: %v", err)
	}
	deps.crudTrashRepository = deps.trashRepository
	deps.backupRepository, err = repositories.NewBackupRepositoryFromEnvWithRuntimeDataDir(runtimeDataDir)
	if err != nil {
		log.Printf("backup detail unavailable: %v", err)
	}
	return newHTTPMuxWithDependencies(roomsDir, deps)
}

func newHTTPMuxWithConfig(config appconfig.Config) *http.ServeMux {
	if err := controller_client.ConfigureSocketPath(config.SocketPath); err != nil {
		log.Printf("controller daemon socket unavailable: %v", err)
	}
	deps := maintenanceMuxDependencies{
		runtimeDataDir: config.DataRoot,
		trashDir:       config.TrashRoot,
	}
	var err error
	deps.state, err = repositories.NewMaintenanceState(repositories.MaintenanceStateConfig{
		Dir: config.MaintenanceRoot, RuntimeDataDir: config.DataRoot,
		RepositoryDir: config.SoftwareRoot, WebRootDir: config.WebRoot,
		BackupDir: config.BackupRoot, TrashDir: config.TrashRoot,
	})
	if err != nil {
		log.Printf("maintenance history unavailable: %v", err)
	}
	deps.trashRepository, err = repositories.NewTrashRepository(repositories.TrashConfig{
		Dir: config.TrashRoot, RuntimeDataDir: config.DataRoot,
		RepositoryDir: config.SoftwareRoot, WebRootDir: config.WebRoot,
		StateDir: config.MaintenanceRoot, BackupDir: config.BackupRoot,
		RetentionDays: config.TrashRetentionDays,
	})
	if err != nil {
		log.Printf("Trash history unavailable: %v", err)
	}
	deps.crudTrashRepository = deps.trashRepository
	deps.backupRepository, err = configuredBackupRepository(config)
	if err != nil {
		log.Printf("backup detail unavailable: %v", err)
	}
	return newHTTPMuxWithDependencies(filepath.Join(config.DataRoot, "rooms"), deps)
}

func newHTTPMuxWithDependencies(roomsDir string, deps maintenanceMuxDependencies) *http.ServeMux {
	handlers.ConfigureDataRoot(roomsDir)
	mux := http.NewServeMux()

	mux.HandleFunc("/api/time", handlers.TimeHandler)
	mux.HandleFunc("/api/health", handlers.HealthHandler)
	mux.HandleFunc("/api/health/controller", handlers.ControllerHealthHandler)

	mux.HandleFunc("/api/controller/rooms", handlers.RoomsHandler)
	mux.HandleFunc("/api/controller/rooms/", handlers.RoomsHandler)

	var history *services.MaintenanceHistoryService
	var operations services.MaintenanceOperationStore
	state := deps.state
	if state != nil {
		reconciler := services.NewMaintenanceReconciler(state.Operations(), services.MaintenanceFilesystemInspector{
			RuntimeRoot: deps.runtimeDataDir,
			TrashRoot:   deps.trashDir,
		})
		if err := reconciler.Scan(); err != nil {
			log.Printf("maintenance startup reconcile failed: %v", err)
		} else {
			history = services.NewMaintenanceHistoryService(state.Operations())
			operations = state.Operations()
		}
	}
	operationsHandler := handlers.NewMaintenanceOperationsHandler(history)
	mux.Handle("/api/controller/maintenance/operations", operationsHandler)
	mux.Handle("/api/controller/maintenance/operations/", operationsHandler)
	var trashRead *services.TrashReadService
	trashRepository := deps.trashRepository
	if trashRepository != nil {
		trashRead = services.NewTrashReadService(trashRepository)
	}
	var crudDeletes *services.CRUDTrashDeleteService
	crudTrashRepository := deps.crudTrashRepository
	if state != nil && crudTrashRepository != nil {
		crudDeletes = services.NewCRUDTrashDeleteService(state.Operations(), crudTrashRepository,
			deps.runtimeDataDir, deps.trashDir)
	}
	handlers.ConfigureCRUDTrashDeleteService(crudDeletes)
	trashHandler := handlers.NewTrashReadHandler(trashRead)
	if trashRepository != nil && operations != nil {
		trashHandler.WithRestore(services.NewTrashRestoreService(trashRepository, operations, deps.runtimeDataDir, deps.trashDir))
	}
	mux.Handle("/api/controller/maintenance/trash", trashHandler)
	mux.Handle("/api/controller/maintenance/trash/", trashHandler)
	var backupService *services.BackupService
	backupRepository := deps.backupRepository
	if backupRepository != nil {
		if state != nil {
			planService := services.NewMaintenancePlanService(state.Plans(), backupRepository)
			backupService = services.NewBackupService(backupRepository, state.Operations()).WithPlanService(planService)
		} else {
			backupService = services.NewBackupService(backupRepository, nil)
		}
	}
	mux.Handle(handlers.BackupCreateRoute, handlers.NewBackupCreateHandler(backupService))
	mux.Handle("/api/controller/maintenance/backups/", handlers.NewBackupDetailHandler(backupService))
	dataHealthService := services.NewDataHealthService(repositories.NewDataHealthRepository(deps.runtimeDataDir))
	mux.Handle(handlers.DataHealthRoute, handlers.NewDataHealthHandler(dataHealthService))
	var cleanupPlanService *services.CleanupPlanService
	if state != nil && backupRepository != nil {
		cleanupPlanService = services.NewCleanupPlanService(dataHealthService, services.NewMaintenancePlanService(state.Plans(), backupRepository))
	}
	mux.Handle(handlers.CleanupDryRunRoute, handlers.NewCleanupDryRunHandler(cleanupPlanService))
	var migrationDryRun *services.MigrationDryRunService
	if state != nil && backupRepository != nil {
		migrationDryRun = services.NewMigrationDryRunService(deps.runtimeDataDir, services.NewMigrationPlanService(services.NewMaintenancePlanService(state.Plans(), backupRepository)))
	}
	mux.Handle(handlers.MigrationDryRunRoute, handlers.NewMigrationDryRunHandler(migrationDryRun))
	if migrationDryRun != nil {
		preset := services.NewBedroomPresetPlanService(deps.runtimeDataDir, services.NewMigrationPlanService(services.NewMaintenancePlanService(state.Plans(), backupRepository)))
		mux.Handle(handlers.BedroomPresetDryRunRoute, handlers.NewBedroomPresetDryRunHandler(preset))
	} else {
		mux.Handle(handlers.BedroomPresetDryRunRoute, handlers.NewBedroomPresetDryRunHandler(nil))
	}
	if migrationDryRun != nil && operations != nil && backupService != nil {
		apply := services.NewMigrationApplyService(services.NewMigrationPlanService(services.NewMaintenancePlanService(state.Plans(), backupRepository)), state.Operations(), backupService, deps.runtimeDataDir)
		mux.Handle(handlers.MigrationApplyRoute, handlers.NewMigrationApplyHandler(apply))
		verify := services.NewMigrationVerifyService(services.NewMigrationPlanService(services.NewMaintenancePlanService(state.Plans(), backupRepository)), state.Operations(), deps.runtimeDataDir)
		mux.Handle(handlers.MigrationVerifyRoute, handlers.NewMigrationVerifyHandler(verify))
		rollback := services.NewMigrationRollbackPlanService(services.NewMaintenancePlanService(state.Plans(), backupRepository), services.NewMigrationPlanService(services.NewMaintenancePlanService(state.Plans(), backupRepository)), state.Operations(), backupService, deps.runtimeDataDir)
		mux.Handle(handlers.MigrationRollbackDryRunRoute, handlers.NewMigrationRollbackDryRunHandler(rollback))
		if trashRepository != nil {
			rollbackApply := services.NewMigrationRollbackApplyService(rollback, state.Operations(), backupService, trashRepository, deps.runtimeDataDir, deps.trashDir)
			mux.Handle(handlers.MigrationRollbackApplyRoute, handlers.NewMigrationRollbackApplyHandler(rollbackApply))
		} else {
			mux.Handle(handlers.MigrationRollbackApplyRoute, handlers.NewMigrationRollbackApplyHandler(nil))
		}
	} else {
		mux.Handle(handlers.MigrationApplyRoute, handlers.NewMigrationApplyHandler(nil))
		mux.Handle(handlers.MigrationVerifyRoute, handlers.NewMigrationVerifyHandler(nil))
		mux.Handle(handlers.MigrationRollbackDryRunRoute, handlers.NewMigrationRollbackDryRunHandler(nil))
		mux.Handle(handlers.MigrationRollbackApplyRoute, handlers.NewMigrationRollbackApplyHandler(nil))
	}
	if cleanupPlanService != nil && operations != nil && backupService != nil && trashRepository != nil {
		apply := services.NewCleanupApplyService(cleanupPlanService, state.Operations(), backupService, trashRepository, deps.runtimeDataDir, deps.trashDir)
		mux.Handle(handlers.CleanupApplyRoute, handlers.NewCleanupApplyHandler(apply))
	} else {
		mux.Handle(handlers.CleanupApplyRoute, handlers.NewCleanupApplyHandler(nil))
	}
	if state != nil && backupRepository != nil && operations != nil && backupService != nil {
		plans := services.NewMaintenancePlanService(state.Plans(), backupRepository)
		migrationPlans := services.NewMigrationPlanService(plans)
		verify := services.NewMigrationVerifyService(migrationPlans, state.Operations(), deps.runtimeDataDir)
		legacy := services.NewLegacyIRCutoverPlanService(deps.runtimeDataDir, plans, state.Operations(), verify)
		mux.Handle(handlers.LegacyIRCutoverDryRunRoute, handlers.NewLegacyIRCutoverDryRunHandler(legacy))
		if trashRepository != nil {
			apply := services.NewLegacyIRCutoverApplyService(legacy, state.Operations(), backupService, trashRepository, deps.runtimeDataDir, deps.trashDir)
			mux.Handle(handlers.LegacyIRCutoverApplyRoute, handlers.NewLegacyIRCutoverApplyHandler(apply))
		} else {
			mux.Handle(handlers.LegacyIRCutoverApplyRoute, handlers.NewLegacyIRCutoverApplyHandler(nil))
		}
	} else {
		mux.Handle(handlers.LegacyIRCutoverDryRunRoute, handlers.NewLegacyIRCutoverDryRunHandler(nil))
		mux.Handle(handlers.LegacyIRCutoverApplyRoute, handlers.NewLegacyIRCutoverApplyHandler(nil))
	}

	return mux
}
