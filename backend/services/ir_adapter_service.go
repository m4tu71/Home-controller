package services

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	iradapter "backend/adapters/ir"
	"backend/controller_client"
	backendir "backend/ir"
	"backend/models"
	"backend/repositories"
)

const (
	irStateDoesNotMatchSchemaErrorMessage        = "ir state does not match schema"
	irStateSchemaConflictsWithStatesErrorMessage = "ir state schema conflicts with existing states"
	irCodeProtocolPanasonic40                    = "PANASONIC_40"
)

type IRAdapterService struct {
	irAdapterRepository repositories.IRAdapterRepository
	deviceRepository    repositories.DeviceRepository
	roomRepository      repositories.RoomRepository
	trashDeletes        *CRUDTrashDeleteService
	capture             func(int) models.IRCapture
	sendRaw             func([]uint) (string, error)
	sendPresetCommand   func(string) (string, error)
	sendRawResult       func([]uint) (controller_client.CommandResult, error)
	sendPresetResult    func(string) (controller_client.CommandResult, error)
	getCapabilities     func() (models.IRCapabilities, controller_client.CommandResult, error)
	sendCodeResult      func(string, string) (controller_client.CommandResult, error)
}

func NewIRAdapterService() IRAdapterService {
	return newIRAdapterService(repositories.NewIRAdapterRepository(), repositories.NewDeviceRepository(), repositories.NewRoomRepository())
}

func NewIRAdapterServiceWithRoomsDir(roomsDir string) IRAdapterService {
	return newIRAdapterService(repositories.NewIRAdapterRepositoryWithRoomsDir(roomsDir), repositories.NewDeviceRepositoryWithRoomsDir(roomsDir), repositories.NewRoomRepositoryWithBaseDir(roomsDir))
}

func newIRAdapterService(irRepository repositories.IRAdapterRepository, deviceRepository repositories.DeviceRepository, roomRepository repositories.RoomRepository) IRAdapterService {
	return IRAdapterService{
		irAdapterRepository: irRepository,
		deviceRepository:    deviceRepository,
		roomRepository:      roomRepository,
		capture:             iradapter.Capture,
		sendRaw:             iradapter.SendRaw,
		sendPresetCommand:   iradapter.SendPresetCommand,
		sendRawResult:       iradapter.SendRawResult,
		sendPresetResult:    iradapter.SendPresetCommandResult,
		getCapabilities:     iradapter.GetCapabilities,
		sendCodeResult:      iradapter.SendCodeResult,
	}
}

func (s IRAdapterService) WithTrashDeletes(deletes *CRUDTrashDeleteService) IRAdapterService {
	s.trashDeletes = deletes
	return s
}

func (s IRAdapterService) GetExecutionOptions(roomID string, deviceID string, protocol string) (models.IRExecutionOptionsResponse, error) {
	protocol = strings.TrimSpace(protocol)
	if protocol != irCodeProtocolPanasonic40 {
		return models.IRExecutionOptionsResponse{}, &ServiceError{
			Kind:    ErrorInvalidRequest,
			Message: "ir code protocol is not supported",
		}
	}
	if err := s.validateIRDevice(roomID, deviceID); err != nil {
		return models.IRExecutionOptionsResponse{}, err
	}

	nativeOption := s.resolveNativeCodeAvailability(protocol)
	return models.IRExecutionOptionsResponse{
		Format:   models.OperationFormatCode,
		Protocol: protocol,
		ExecutionModes: models.IRExecutionModeOptions{
			NativeCode: nativeOption,
			BackendRaw: models.IRExecutionModeOption{
				Available:          true,
				VerificationStatus: models.IRVerificationStatusConfirmed,
				Reason:             "",
			},
		},
	}, nil
}

func (s IRAdapterService) resolveNativeCodeAvailability(protocol string) models.IRExecutionModeOption {
	getter := s.getCapabilities
	if getter == nil {
		getter = iradapter.GetCapabilities
	}
	capabilities, _, err := getter()
	if err != nil || !capabilities.OK {
		return models.IRExecutionModeOption{Available: false, VerificationStatus: models.IRVerificationStatusUnknown, Reason: "Pico capabilityを確認できません"}
	}
	if !containsString(capabilities.Commands, "IR_SEND_CODE") {
		return models.IRExecutionModeOption{Available: false, VerificationStatus: models.IRVerificationStatusConfirmed, Reason: "PicoがIR_SEND_CODEに対応していません"}
	}
	if !containsString(capabilities.CodeProtocols, protocol) {
		return models.IRExecutionModeOption{Available: false, VerificationStatus: models.IRVerificationStatusConfirmed, Reason: "Picoが" + protocol + "に対応していません"}
	}
	return models.IRExecutionModeOption{Available: true, VerificationStatus: models.IRVerificationStatusConfirmed, Reason: ""}
}

func (s IRAdapterService) GetConfig(roomID string, deviceID string) (models.IRConfig, error) {
	if err := s.validateIRDevice(roomID, deviceID); err != nil {
		return models.IRConfig{}, err
	}

	config, err := s.irAdapterRepository.LoadConfig(roomID, deviceID)
	if err != nil {
		if os.IsNotExist(err) {
			return models.IRConfig{}, &ServiceError{
				Kind:    ErrorNotFound,
				Message: "ir config not found",
				Err:     err,
			}
		}
		return models.IRConfig{}, &ServiceError{
			Kind:    ErrorInternal,
			Message: "failed to load ir config",
			Detail:  err.Error(),
			Err:     err,
		}
	}

	return config, nil
}

func (s IRAdapterService) SaveConfig(roomID string, deviceID string, config models.IRConfig) (models.IRConfig, error) {
	if err := s.validateIRDevice(roomID, deviceID); err != nil {
		return models.IRConfig{}, err
	}

	config.IRRemoteType = models.IRRemoteType(strings.TrimSpace(string(config.IRRemoteType)))

	if err := validateIRConfig(config); err != nil {
		return models.IRConfig{}, err
	}
	if _, err := s.irAdapterRepository.LoadConfig(roomID, deviceID); err != nil && !os.IsNotExist(err) {
		return models.IRConfig{}, &ServiceError{Kind: ErrorInternal, Message: "failed to load ir config", Detail: err.Error(), Err: err}
	}

	if err := s.irAdapterRepository.SaveConfig(roomID, deviceID, config); err != nil {
		return models.IRConfig{}, &ServiceError{
			Kind:    ErrorInternal,
			Message: "failed to save ir config",
			Detail:  err.Error(),
			Err:     err,
		}
	}

	return config, nil
}

func (s IRAdapterService) ListCommands(roomID string, deviceID string) ([]models.IRCommand, error) {
	if err := s.validateCommandIRDevice(roomID, deviceID); err != nil {
		return nil, err
	}

	commands, err := s.irAdapterRepository.ListCommands(roomID, deviceID)
	if err != nil {
		if os.IsNotExist(err) {
			return []models.IRCommand{}, nil
		}
		return nil, &ServiceError{
			Kind:    ErrorInternal,
			Message: "failed to read ir commands directory",
			Detail:  err.Error(),
			Err:     err,
		}
	}

	sort.Slice(commands, func(i, j int) bool {
		return commands[i].ID < commands[j].ID
	})

	return commands, nil
}

func (s IRAdapterService) GetCommand(roomID string, deviceID string, commandID string) (models.IRCommand, error) {
	if err := s.validateCommandIRDevice(roomID, deviceID); err != nil {
		return models.IRCommand{}, err
	}

	if !validateIRCommandID(commandID) {
		return models.IRCommand{}, &ServiceError{
			Kind:    ErrorInvalidRequest,
			Message: "invalid ir command id",
		}
	}

	command, err := s.irAdapterRepository.LoadCommand(roomID, deviceID, commandID)
	if err != nil {
		if os.IsNotExist(err) {
			return models.IRCommand{}, &ServiceError{
				Kind:    ErrorNotFound,
				Message: "ir command not found",
				Err:     err,
			}
		}
		return models.IRCommand{}, &ServiceError{
			Kind:    ErrorInternal,
			Message: "failed to load ir command",
			Detail:  err.Error(),
			Err:     err,
		}
	}

	return command, nil
}

func (s IRAdapterService) CreateCommand(roomID string, deviceID string, request models.IRCommandWriteRequest) (models.IRCommand, error) {
	if err := s.validateCommandIRDevice(roomID, deviceID); err != nil {
		return models.IRCommand{}, err
	}

	command := normalizeIRCommand(irCommandFromWriteRequest(request))

	if err := validateIRCommand(command); err != nil {
		return models.IRCommand{}, err
	}
	if command.Format == models.OperationFormatRawSamples {
		if err := validateRawSamplesCreateRequest(request); err != nil {
			return models.IRCommand{}, err
		}
		command.Count = 0
		command.TimingsUS = nil
		command.Captures = nil
	}

	exists, err := s.irAdapterRepository.CommandExists(roomID, deviceID, command.ID)
	if err != nil {
		return models.IRCommand{}, &ServiceError{
			Kind:    ErrorInternal,
			Message: "failed to check ir command",
			Detail:  err.Error(),
			Err:     err,
		}
	}
	if exists {
		return models.IRCommand{}, &ServiceError{
			Kind:    ErrorConflict,
			Message: "ir command already exists",
		}
	}

	if err := s.irAdapterRepository.SaveCommand(roomID, deviceID, command); err != nil {
		return models.IRCommand{}, &ServiceError{
			Kind:    ErrorInternal,
			Message: "failed to save ir command",
			Detail:  err.Error(),
			Err:     err,
		}
	}

	return command, nil
}

func (s IRAdapterService) UpdateCommand(roomID string, deviceID string, commandID string, request models.IRCommandWriteRequest) (models.IRCommand, error) {
	if err := s.validateCommandIRDevice(roomID, deviceID); err != nil {
		return models.IRCommand{}, err
	}

	if !validateIRCommandID(commandID) {
		return models.IRCommand{}, &ServiceError{
			Kind:    ErrorInvalidRequest,
			Message: "invalid ir command id",
		}
	}

	request = normalizeIRCommandWriteRequest(request)
	if request.ID != nil && *request.ID != commandID {
		return models.IRCommand{}, &ServiceError{
			Kind:    ErrorInvalidRequest,
			Message: "ir command id mismatch",
		}
	}

	existingCommand, err := s.irAdapterRepository.LoadCommand(roomID, deviceID, commandID)
	if err != nil {
		if os.IsNotExist(err) {
			return models.IRCommand{}, &ServiceError{
				Kind:    ErrorNotFound,
				Message: "ir command not found",
				Err:     err,
			}
		}
		return models.IRCommand{}, &ServiceError{
			Kind:    ErrorInternal,
			Message: "failed to load ir command",
			Detail:  err.Error(),
			Err:     err,
		}
	}

	command, err := mergeIRCommandWriteRequest(existingCommand, request)
	if err != nil {
		return models.IRCommand{}, err
	}
	command.Protocol, command.Code = normalizeIRCodeFields(command.Protocol, command.Code)
	if err := validateIRCommand(command); err != nil {
		return models.IRCommand{}, err
	}

	if err := s.irAdapterRepository.SaveCommand(roomID, deviceID, command); err != nil {
		return models.IRCommand{}, &ServiceError{
			Kind:    ErrorInternal,
			Message: "failed to save ir command",
			Detail:  err.Error(),
			Err:     err,
		}
	}

	return command, nil
}

func (s IRAdapterService) DeleteCommand(roomID string, deviceID string, commandID string) (CRUDDeleteResult, error) {
	if err := s.validateCommandIRDevice(roomID, deviceID); err != nil {
		return CRUDDeleteResult{}, err
	}

	if !validateIRCommandID(commandID) {
		return CRUDDeleteResult{}, &ServiceError{
			Kind:    ErrorInvalidRequest,
			Message: "invalid ir command id",
		}
	}

	if _, err := s.irAdapterRepository.LoadCommand(roomID, deviceID, commandID); err != nil {
		if os.IsNotExist(err) {
			return CRUDDeleteResult{}, &ServiceError{
				Kind:    ErrorNotFound,
				Message: "ir command not found",
				Err:     err,
			}
		}
		return CRUDDeleteResult{}, &ServiceError{
			Kind:    ErrorInternal,
			Message: "failed to read ir command before delete",
			Detail:  err.Error(),
			Err:     err,
		}
	}
	if s.trashDeletes == nil {
		return CRUDDeleteResult{}, ErrMaintenancePlanNotReady
	}
	relativePath := filepath.ToSlash(filepath.Join("rooms", roomID, "devices", deviceID, "adapters", "ir", "commands", commandID+".json"))
	return s.trashDeletes.DeleteFile("ir_command", commandID, relativePath)
}

func (s IRAdapterService) CaptureCommand(roomID string, deviceID string, commandID string, request models.IRCommandCaptureRequest) (models.IRCommandCaptureResponse, error) {
	command, err := s.GetCommand(roomID, deviceID, commandID)
	if err != nil {
		return models.IRCommandCaptureResponse{}, err
	}

	if err := validateCommandCaptureFormat(command); err != nil {
		return models.IRCommandCaptureResponse{}, err
	}

	samples, maxAttempts, err := resolveCommandCaptureRequest(request)
	if err != nil {
		return models.IRCommandCaptureResponse{}, err
	}

	capture := s.capture
	if capture == nil {
		capture = iradapter.Capture
	}

	captures := []models.IRCapture{}
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		result := applyRawCaptureSafety(capture(attempt))
		captures = append(captures, result)

		if backendir.CountValidCaptures(captures) >= samples {
			break
		}
	}

	validCaptures := backendir.CountValidCaptures(captures)
	if validCaptures < samples {
		return models.IRCommandCaptureResponse{}, &ServiceError{
			Kind:    ErrorCaptureInsufficient,
			Message: "not enough valid captures",
			Payload: map[string]interface{}{
				"ok":                 false,
				"error":              "not enough valid captures",
				"required_samples":   samples,
				"valid_captures":     validCaptures,
				"count_distribution": backendir.BuildCountDistribution(captures),
				"captures":           captures,
			},
		}
	}

	timings, err := backendir.InferRepresentativeTimings(captures)
	if err != nil {
		return models.IRCommandCaptureResponse{}, &ServiceError{
			Kind:    ErrorInvalidRequest,
			Message: "failed to infer representative signal",
			Payload: map[string]interface{}{
				"ok":                 false,
				"error":              "failed to infer representative signal",
				"count_distribution": backendir.BuildCountDistribution(captures),
				"captures":           captures,
			},
			Err: err,
		}
	}
	if err := backendir.ValidateRawTimings(timings, len(timings)); err != nil {
		return models.IRCommandCaptureResponse{}, rawValidationServiceError(err)
	}

	command.Captures = captures
	command.TimingsUS = timings
	command.Count = len(timings)

	if err := s.irAdapterRepository.SaveCommand(roomID, deviceID, command); err != nil {
		return models.IRCommandCaptureResponse{}, &ServiceError{
			Kind:    ErrorInternal,
			Message: "failed to save ir command",
			Detail:  err.Error(),
			Err:     err,
		}
	}

	return models.IRCommandCaptureResponse{
		OK:              true,
		Command:         command,
		RequiredSamples: samples,
		ValidCaptures:   validCaptures,
	}, nil
}

func (s IRAdapterService) GetStateSchema(roomID string, deviceID string) (models.IRStateSchema, error) {
	if err := s.validateStatefulIRDevice(roomID, deviceID); err != nil {
		return nil, err
	}

	schema, err := s.irAdapterRepository.LoadStateSchema(roomID, deviceID)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, &ServiceError{
				Kind:    ErrorNotFound,
				Message: "ir state schema not found",
				Err:     err,
			}
		}
		return nil, &ServiceError{
			Kind:    ErrorInternal,
			Message: "failed to load ir state schema",
			Detail:  err.Error(),
			Err:     err,
		}
	}

	if err := validateIRStateSchema(schema); err != nil {
		return nil, &ServiceError{Kind: ErrorInternal, Message: "failed to load ir state schema", Detail: err.Error(), Err: err}
	}
	return schema, nil
}

func (s IRAdapterService) SaveStateSchema(roomID string, deviceID string, schema models.IRStateSchema) (models.IRStateSchema, error) {
	if err := s.validateStatefulIRDevice(roomID, deviceID); err != nil {
		return nil, err
	}

	schema, err := normalizeIRStateSchema(schema)
	if err != nil {
		return nil, err
	}

	if err := validateIRStateSchema(schema); err != nil {
		return nil, err
	}
	if _, err := s.irAdapterRepository.LoadStateSchema(roomID, deviceID); err != nil && !os.IsNotExist(err) {
		return nil, &ServiceError{Kind: ErrorInternal, Message: "failed to load ir state schema", Detail: err.Error(), Err: err}
	}

	states, err := s.irAdapterRepository.ListStatesStrict(roomID, deviceID)
	if err != nil {
		return nil, &ServiceError{
			Kind:    ErrorInternal,
			Message: "failed to read ir states directory",
			Detail:  err.Error(),
			Err:     err,
		}
	}

	invalidStateIDs := []string{}
	conflictIssues := []models.IRStateSchemaIssue{}
	for _, state := range states {
		issues := irStateSchemaIssues(state.State, schema)
		if len(issues) > 0 {
			invalidStateIDs = append(invalidStateIDs, state.ID)
			for _, issue := range issues {
				issue.StateID = state.ID
				conflictIssues = append(conflictIssues, issue)
			}
		}
	}
	if len(invalidStateIDs) > 0 {
		return nil, &ServiceError{
			Kind:    ErrorConflict,
			Message: irStateSchemaConflictsWithStatesErrorMessage,
			Payload: map[string]interface{}{
				"ok":                false,
				"error":             irStateSchemaConflictsWithStatesErrorMessage,
				"invalid_state_ids": invalidStateIDs,
				"issues":            conflictIssues,
			},
		}
	}

	if err := s.irAdapterRepository.SaveStateSchema(roomID, deviceID, schema); err != nil {
		return nil, &ServiceError{
			Kind:    ErrorInternal,
			Message: "failed to save ir state schema",
			Detail:  err.Error(),
			Err:     err,
		}
	}

	return schema, nil
}

func (s IRAdapterService) ListStates(roomID string, deviceID string) ([]models.IRState, error) {
	if err := s.validateStatefulIRDevice(roomID, deviceID); err != nil {
		return nil, err
	}

	states, err := s.irAdapterRepository.ListStates(roomID, deviceID)
	if err != nil {
		if os.IsNotExist(err) {
			return []models.IRState{}, nil
		}
		return nil, &ServiceError{
			Kind:    ErrorInternal,
			Message: "failed to read ir states directory",
			Detail:  err.Error(),
			Err:     err,
		}
	}

	sort.Slice(states, func(i, j int) bool {
		return states[i].ID < states[j].ID
	})

	return states, nil
}

func (s IRAdapterService) GetState(roomID string, deviceID string, stateID string) (models.IRState, error) {
	if err := s.validateStatefulIRDevice(roomID, deviceID); err != nil {
		return models.IRState{}, err
	}

	if !validateIRStateID(stateID) {
		return models.IRState{}, &ServiceError{
			Kind:    ErrorInvalidRequest,
			Message: "invalid ir state id",
		}
	}

	state, err := s.irAdapterRepository.LoadState(roomID, deviceID, stateID)
	if err != nil {
		if os.IsNotExist(err) {
			return models.IRState{}, &ServiceError{
				Kind:    ErrorNotFound,
				Message: "ir state not found",
				Err:     err,
			}
		}
		return models.IRState{}, &ServiceError{
			Kind:    ErrorInternal,
			Message: "failed to load ir state",
			Detail:  err.Error(),
			Err:     err,
		}
	}

	return state, nil
}

func (s IRAdapterService) CreateState(roomID string, deviceID string, request models.IRStateWriteRequest) (models.IRState, error) {
	if err := s.validateStatefulIRDevice(roomID, deviceID); err != nil {
		return models.IRState{}, err
	}

	state := normalizeIRState(irStateFromWriteRequest(request))

	if err := validateIRState(state); err != nil {
		return models.IRState{}, err
	}
	if state.Format == models.OperationFormatRawSamples {
		if err := validateRawSamplesStateCreateRequest(request); err != nil {
			return models.IRState{}, err
		}
		state.Count = 0
		state.TimingsUS = nil
		state.Captures = nil
	}
	if err := s.validateStateMatchesStoredSchema(roomID, deviceID, state); err != nil {
		return models.IRState{}, err
	}

	exists, err := s.irAdapterRepository.StateExists(roomID, deviceID, state.ID)
	if err != nil {
		return models.IRState{}, &ServiceError{
			Kind:    ErrorInternal,
			Message: "failed to check ir state",
			Detail:  err.Error(),
			Err:     err,
		}
	}
	if exists {
		return models.IRState{}, &ServiceError{
			Kind:    ErrorConflict,
			Message: "ir state already exists",
		}
	}

	if err := s.irAdapterRepository.SaveState(roomID, deviceID, state); err != nil {
		return models.IRState{}, &ServiceError{
			Kind:    ErrorInternal,
			Message: "failed to save ir state",
			Detail:  err.Error(),
			Err:     err,
		}
	}

	return state, nil
}

func (s IRAdapterService) UpdateState(roomID string, deviceID string, stateID string, request models.IRStateWriteRequest) (models.IRState, error) {
	if err := s.validateStatefulIRDevice(roomID, deviceID); err != nil {
		return models.IRState{}, err
	}

	if !validateIRStateID(stateID) {
		return models.IRState{}, &ServiceError{
			Kind:    ErrorInvalidRequest,
			Message: "invalid ir state id",
		}
	}

	request = normalizeIRStateWriteRequest(request)
	if request.ID != nil && *request.ID != stateID {
		return models.IRState{}, &ServiceError{
			Kind:    ErrorInvalidRequest,
			Message: "ir state id mismatch",
		}
	}

	existingState, err := s.irAdapterRepository.LoadState(roomID, deviceID, stateID)
	if err != nil {
		if os.IsNotExist(err) {
			return models.IRState{}, &ServiceError{
				Kind:    ErrorNotFound,
				Message: "ir state not found",
				Err:     err,
			}
		}
		return models.IRState{}, &ServiceError{
			Kind:    ErrorInternal,
			Message: "failed to load ir state",
			Detail:  err.Error(),
			Err:     err,
		}
	}

	if err := validateRegisteredStateWrite(existingState, request); err != nil {
		return models.IRState{}, err
	}
	state, err := mergeIRStateWriteRequest(existingState, request)
	if err != nil {
		return models.IRState{}, err
	}
	state.Protocol, state.Code = normalizeIRCodeFields(state.Protocol, state.Code)
	if err := validateIRState(state); err != nil {
		return models.IRState{}, err
	}
	if err := s.validateStateMatchesStoredSchema(roomID, deviceID, state); err != nil {
		return models.IRState{}, err
	}

	if err := s.irAdapterRepository.SaveState(roomID, deviceID, state); err != nil {
		return models.IRState{}, &ServiceError{
			Kind:    ErrorInternal,
			Message: "failed to save ir state",
			Detail:  err.Error(),
			Err:     err,
		}
	}

	return state, nil
}

func (s IRAdapterService) DeleteState(roomID string, deviceID string, stateID string) (CRUDDeleteResult, error) {
	if err := s.validateStatefulIRDevice(roomID, deviceID); err != nil {
		return CRUDDeleteResult{}, err
	}

	if !validateIRStateID(stateID) {
		return CRUDDeleteResult{}, &ServiceError{
			Kind:    ErrorInvalidRequest,
			Message: "invalid ir state id",
		}
	}

	if _, err := s.irAdapterRepository.LoadState(roomID, deviceID, stateID); err != nil {
		if os.IsNotExist(err) {
			return CRUDDeleteResult{}, &ServiceError{
				Kind:    ErrorNotFound,
				Message: "ir state not found",
				Err:     err,
			}
		}
		return CRUDDeleteResult{}, &ServiceError{
			Kind:    ErrorInternal,
			Message: "failed to read ir state before delete",
			Detail:  err.Error(),
			Err:     err,
		}
	}
	if s.trashDeletes == nil {
		return CRUDDeleteResult{}, ErrMaintenancePlanNotReady
	}
	relativePath := filepath.ToSlash(filepath.Join("rooms", roomID, "devices", deviceID, "adapters", "ir", "states", stateID+".json"))
	return s.trashDeletes.DeleteFile("ir_state", stateID, relativePath)
}

func (s IRAdapterService) PrepareCommandSendPlan(roomID string, deviceID string, commandID string) (models.IRSendPlan, error) {
	command, err := s.GetCommand(roomID, deviceID, commandID)
	if err != nil {
		return models.IRSendPlan{}, err
	}

	return s.buildCommandSendPlan(roomID, deviceID, command, nil)
}

func (s IRAdapterService) SendCommand(roomID string, deviceID string, commandID string, request models.IRCommandSendRequest) (models.IRCommandSendResponse, error) {
	command, err := s.GetCommand(roomID, deviceID, commandID)
	if err != nil {
		return models.IRCommandSendResponse{}, err
	}
	if err := validateIRExecutionMode(command.Format, request.ExecutionMode, command.Protocol); err != nil {
		return models.IRCommandSendResponse{}, err
	}

	plan, err := s.buildCommandSendPlan(roomID, deviceID, command, request.Repeat)
	if err != nil {
		return models.IRCommandSendResponse{}, err
	}

	switch plan.Format {
	case models.OperationFormatRawSamples:
		timings, err := backendir.GetTimingsForSendData(command.Captures, command.TimingsUS)
		if err != nil {
			return models.IRCommandSendResponse{}, &ServiceError{
				Kind:    ErrorConflict,
				Message: "ir command is not learned",
				Err:     err,
			}
		}
		if err := backendir.ValidateRawTimings(timings, len(timings)); err != nil {
			return models.IRCommandSendResponse{}, rawValidationServiceError(err)
		}
		plan.TimingsUS = timings
	case models.OperationFormatRaw:
		if err := backendir.ValidateRawTimings(command.TimingsUS, command.Count); err != nil {
			return models.IRCommandSendResponse{}, rawValidationServiceError(err)
		}
		plan.TimingsUS = command.TimingsUS
	case models.OperationFormatCode:
		plan.ExecutionMode = *request.ExecutionMode
		if plan.ExecutionMode == models.IRExecutionModeBackendRaw {
			timings, err := backendir.EncodeCodeToRaw(plan.Protocol, plan.Code)
			if err != nil {
				return models.IRCommandSendResponse{}, codeEncoderServiceError(err)
			}
			if err := backendir.ValidateRawTimings(timings, len(timings)); err != nil {
				return models.IRCommandSendResponse{}, rawValidationServiceError(err)
			}
			plan.TimingsUS = timings
		} else if option := s.resolveNativeCodeAvailability(plan.Protocol); !option.Available {
			return models.IRCommandSendResponse{}, nativeCodeUnavailableError(plan, option.Reason)
		}
	case models.OperationFormatPresetCommand:
	default:
		return models.IRCommandSendResponse{}, unsupportedSendFormatError("ir command format is not supported for send")
	}

	if err := validateIRSendCarrier(plan, request.ExecutionMode); err != nil {
		return models.IRCommandSendResponse{}, err
	}

	execution, err := s.executeIRSendPlan(plan)
	if err != nil {
		return models.IRCommandSendResponse{}, newControllerSendError("controller daemon error", plan, request.ExecutionMode, execution, err)
	}

	return models.IRCommandSendResponse{
		OK:               true,
		RoomID:           roomID,
		DeviceID:         deviceID,
		CommandID:        commandID,
		Format:           plan.Format,
		Protocol:         plan.Protocol,
		ExecutionMode:    executionModeValue(request.ExecutionMode),
		DeliveryState:    execution.DeliveryState,
		Repeat:           plan.Repeat,
		RequestedRepeats: execution.RequestedRepeats,
		CompletedRepeats: execution.CompletedRepeats,
		RetryableWith:    []models.IRExecutionMode{},
	}, nil
}

type irSendExecutionResult struct {
	DeliveryState    models.IRDeliveryState
	RequestedRepeats int
	CompletedRepeats int
}

// executeIRSendPlan is the common repeat boundary for Command and State sends.
// Delivery certainty is derived only from the typed controller result.
func (s IRAdapterService) executeIRSendPlan(plan models.IRSendPlan) (irSendExecutionResult, error) {
	execution := irSendExecutionResult{
		DeliveryState:    models.IRDeliveryStateSent,
		RequestedRepeats: plan.Repeat,
	}

	var send func() (controller_client.CommandResult, error)
	switch plan.Format {
	case models.OperationFormatRawSamples, models.OperationFormatRaw:
		send = s.rawResultSender(plan.TimingsUS)
	case models.OperationFormatCode:
		if plan.ExecutionMode == models.IRExecutionModeNativeCode {
			send = s.codeResultSender(plan.Protocol, plan.Code)
		} else {
			send = s.rawResultSender(plan.TimingsUS)
		}
	case models.OperationFormatPresetCommand:
		send = s.presetResultSender(plan.PicoCommand)
	default:
		return execution, errors.New("ir send plan format is not executable")
	}

	for attempt := 0; attempt < plan.Repeat; attempt++ {
		result, err := send()
		if err == nil && result.Err != nil {
			err = result.Err
		}
		if err != nil {
			execution.DeliveryState = deliveryStateForControllerFailure(result, execution.CompletedRepeats)
			return execution, err
		}

		execution.CompletedRepeats++
		if attempt < plan.Repeat-1 {
			time.Sleep(100 * time.Millisecond)
		}
	}

	return execution, nil
}

func (s IRAdapterService) codeResultSender(protocol string, code string) func() (controller_client.CommandResult, error) {
	if s.sendCodeResult != nil {
		return func() (controller_client.CommandResult, error) { return s.sendCodeResult(protocol, code) }
	}
	return func() (controller_client.CommandResult, error) { return iradapter.SendCodeResult(protocol, code) }
}

func (s IRAdapterService) rawResultSender(timings []uint) func() (controller_client.CommandResult, error) {
	if s.sendRawResult != nil {
		return func() (controller_client.CommandResult, error) { return s.sendRawResult(timings) }
	}
	if s.sendRaw != nil {
		return func() (controller_client.CommandResult, error) {
			response, err := s.sendRaw(timings)
			return legacyControllerResult(response, err), err
		}
	}
	return func() (controller_client.CommandResult, error) { return iradapter.SendRawResult(timings) }
}

func (s IRAdapterService) presetResultSender(command string) func() (controller_client.CommandResult, error) {
	if s.sendPresetResult != nil {
		return func() (controller_client.CommandResult, error) { return s.sendPresetResult(command) }
	}
	if s.sendPresetCommand != nil {
		return func() (controller_client.CommandResult, error) {
			response, err := s.sendPresetCommand(command)
			return legacyControllerResult(response, err), err
		}
	}
	return func() (controller_client.CommandResult, error) { return iradapter.SendPresetCommandResult(command) }
}

func legacyControllerResult(response string, err error) controller_client.CommandResult {
	return controller_client.CommandResult{ResponseBody: response, Err: err}
}

func deliveryStateForControllerFailure(result controller_client.CommandResult, completedRepeats int) models.IRDeliveryState {
	knownNotSent := result.IsPreSendRejection() ||
		result.FailureStage == controller_client.FailureStageConnect ||
		(result.FailureStage == controller_client.FailureStageWrite && result.BytesWritten == 0 && !result.WriteCompleted)
	if !knownNotSent {
		return models.IRDeliveryStateUnknown
	}
	if completedRepeats > 0 {
		return models.IRDeliveryStatePartiallySent
	}
	return models.IRDeliveryStateNotSent
}

func newControllerSendError(message string, plan models.IRSendPlan, executionMode *models.IRExecutionMode, execution irSendExecutionResult, err error) error {
	return &ServiceError{
		Kind:    ErrorController,
		Message: message,
		Detail:  err.Error(),
		Err:     err,
		Payload: models.IRSendErrorResponse{
			OK:               false,
			Error:            message,
			Detail:           err.Error(),
			Protocol:         plan.Protocol,
			ExecutionMode:    executionModeValue(executionMode),
			DeliveryState:    execution.DeliveryState,
			RequestedRepeats: execution.RequestedRepeats,
			CompletedRepeats: execution.CompletedRepeats,
			RetryableWith:    []models.IRExecutionMode{},
		},
	}
}

func validateIRSendCarrier(plan models.IRSendPlan, executionMode *models.IRExecutionMode) error {
	requiresRawCarrier := plan.Format == models.OperationFormatRawSamples ||
		plan.Format == models.OperationFormatRaw ||
		(plan.Format == models.OperationFormatCode && executionModeValue(executionMode) == models.IRExecutionModeBackendRaw)
	if !requiresRawCarrier || plan.CarrierKHz == defaultCarrierKHz {
		return nil
	}

	message := "ir carrier is not supported for execution"
	return &ServiceError{
		Kind:    ErrorConflict,
		Message: message,
		Payload: models.IRSendErrorResponse{
			OK:               false,
			Error:            message,
			Protocol:         plan.Protocol,
			ExecutionMode:    executionModeValue(executionMode),
			DeliveryState:    models.IRDeliveryStateNotSent,
			RequestedRepeats: plan.Repeat,
			RetryableWith:    []models.IRExecutionMode{},
		},
	}
}

func executionModeValue(executionMode *models.IRExecutionMode) models.IRExecutionMode {
	if executionMode == nil {
		return ""
	}
	return *executionMode
}

func unsupportedSendFormatError(message string) error {
	return &ServiceError{Kind: ErrorInvalidRequest, Message: message}
}

func codeEncoderServiceError(err error) error {
	if errors.Is(err, backendir.ErrUnsupportedCodeProtocol) {
		return &ServiceError{Kind: ErrorConflict, Message: "backend raw encoder is not available", Err: err}
	}
	return &ServiceError{Kind: ErrorInvalidRequest, Message: "invalid ir code", Err: err}
}

func nativeCodeUnavailableError(plan models.IRSendPlan, reason string) error {
	message := "native code execution is not available"
	return &ServiceError{
		Kind:    ErrorConflict,
		Message: message,
		Detail:  reason,
		Payload: models.IRSendErrorResponse{
			OK:               false,
			Error:            message,
			Detail:           reason,
			Protocol:         plan.Protocol,
			ExecutionMode:    models.IRExecutionModeNativeCode,
			DeliveryState:    models.IRDeliveryStateNotSent,
			RequestedRepeats: plan.Repeat,
			RetryableWith:    []models.IRExecutionMode{models.IRExecutionModeBackendRaw},
		},
	}
}

func validateIRExecutionMode(format models.OperationFormat, executionMode *models.IRExecutionMode, protocol string) error {
	if format != models.OperationFormatCode {
		if executionMode == nil {
			return nil
		}
		return newIRSendValidationError("invalid ir execution mode", protocol, *executionMode)
	}

	if executionMode == nil || *executionMode == "" {
		return newIRSendValidationError("ir execution mode is required", protocol, "")
	}
	if *executionMode != models.IRExecutionModeBackendRaw && *executionMode != models.IRExecutionModeNativeCode {
		return newIRSendValidationError("invalid ir execution mode", protocol, *executionMode)
	}
	return nil
}

func newIRSendValidationError(message string, protocol string, executionMode models.IRExecutionMode) error {
	return &ServiceError{
		Kind:    ErrorInvalidRequest,
		Message: message,
		Payload: models.IRSendErrorResponse{
			OK:            false,
			Error:         message,
			Protocol:      protocol,
			ExecutionMode: executionMode,
			DeliveryState: models.IRDeliveryStateNotSent,
			RetryableWith: []models.IRExecutionMode{},
		},
	}
}

func (s IRAdapterService) PrepareStateSendPlan(roomID string, deviceID string, stateID string) (models.IRSendPlan, error) {
	state, err := s.GetState(roomID, deviceID, stateID)
	if err != nil {
		return models.IRSendPlan{}, err
	}

	if err := s.validateStoredStateForOperation(roomID, deviceID, state); err != nil {
		return models.IRSendPlan{}, err
	}
	return s.buildStateSendPlan(roomID, deviceID, state, nil)
}

func (s IRAdapterService) buildCommandSendPlan(roomID string, deviceID string, command models.IRCommand, requestRepeat *int) (models.IRSendPlan, error) {
	if err := validateCommandSendFormat(command); err != nil {
		return models.IRSendPlan{}, err
	}

	carrierKHz, repeat, err := s.resolveCommandSendDefaults(roomID, deviceID, command.CarrierKHz, command.Repeat, requestRepeat)
	if err != nil {
		return models.IRSendPlan{}, err
	}

	plan := models.IRSendPlan{
		RoomID:     roomID,
		DeviceID:   deviceID,
		TargetType: models.IRSendTargetTypeCommand,
		TargetID:   command.ID,
		Format:     command.Format,
		CarrierKHz: carrierKHz,
		Repeat:     repeat,
	}

	switch command.Format {
	case models.OperationFormatRawSamples, models.OperationFormatRaw:
		plan.TimingsUS = command.TimingsUS
	case models.OperationFormatPresetCommand:
		plan.PicoCommand = command.PicoCommand
	case models.OperationFormatCode:
		plan.Protocol = command.Protocol
		plan.Code = command.Code
	}

	return plan, nil
}

func (s IRAdapterService) buildStateSendPlan(roomID string, deviceID string, state models.IRState, requestRepeat *int) (models.IRSendPlan, error) {
	carrierKHz, repeat, err := s.resolveCommandSendDefaults(roomID, deviceID, state.CarrierKHz, state.Repeat, requestRepeat)
	if err != nil {
		return models.IRSendPlan{}, err
	}

	plan := models.IRSendPlan{
		RoomID:     roomID,
		DeviceID:   deviceID,
		TargetType: models.IRSendTargetTypeState,
		TargetID:   state.ID,
		Format:     state.Format,
		CarrierKHz: carrierKHz,
		Repeat:     repeat,
	}

	switch state.Format {
	case models.OperationFormatRawSamples, models.OperationFormatRaw:
		plan.TimingsUS = state.TimingsUS
	case models.OperationFormatPresetCommand:
		plan.PicoCommand = state.PicoCommand
	case models.OperationFormatCode:
		plan.Protocol = state.Protocol
		plan.Code = state.Code
	}

	return plan, nil
}

func (s IRAdapterService) resolveCommandSendDefaults(roomID string, deviceID string, carrierKHz int, commandRepeat int, requestRepeat *int) (int, int, error) {
	config, err := s.irAdapterRepository.LoadConfig(roomID, deviceID)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, 0, &ServiceError{
				Kind:    ErrorNotFound,
				Message: "ir config not found",
				Err:     err,
			}
		}
		return 0, 0, &ServiceError{
			Kind:    ErrorInternal,
			Message: "failed to load ir config",
			Detail:  err.Error(),
			Err:     err,
		}
	}

	if carrierKHz <= 0 {
		carrierKHz = defaultCarrierKHz
		if config.CarrierKHz > 0 {
			carrierKHz = config.CarrierKHz
		}
	}

	repeat, err := resolveCommandRepeat(requestRepeat, commandRepeat, config.DefaultRepeat)
	if err != nil {
		return 0, 0, err
	}

	return carrierKHz, repeat, nil
}

func (s IRAdapterService) validateIRDevice(roomID string, deviceID string) error {
	if !validateRoomID(roomID) {
		return &ServiceError{
			Kind:    ErrorInvalidRequest,
			Message: "invalid room id",
		}
	}

	if !validateDeviceID(deviceID) {
		return &ServiceError{
			Kind:    ErrorInvalidRequest,
			Message: "invalid device id",
		}
	}

	roomExists, err := s.roomRepository.Exists(roomID)
	if err != nil {
		return &ServiceError{
			Kind:    ErrorInternal,
			Message: "failed to check room",
			Detail:  err.Error(),
			Err:     err,
		}
	}
	if !roomExists {
		return &ServiceError{
			Kind:    ErrorNotFound,
			Message: "room not found",
		}
	}

	device, err := s.deviceRepository.Load(roomID, deviceID)
	if err != nil {
		if os.IsNotExist(err) {
			return &ServiceError{
				Kind:    ErrorNotFound,
				Message: "device not found",
				Err:     err,
			}
		}
		return &ServiceError{
			Kind:    ErrorInternal,
			Message: "failed to load device",
			Detail:  err.Error(),
			Err:     err,
		}
	}

	if device.PrimaryAdapter != "ir" {
		return &ServiceError{
			Kind:    ErrorInvalidRequest,
			Message: "device primary adapter is not ir",
		}
	}

	return nil
}

func (s IRAdapterService) validateCommandIRDevice(roomID string, deviceID string) error {
	if err := s.validateIRDevice(roomID, deviceID); err != nil {
		return err
	}

	config, err := s.irAdapterRepository.LoadConfig(roomID, deviceID)
	if err != nil {
		if os.IsNotExist(err) {
			return &ServiceError{
				Kind:    ErrorNotFound,
				Message: "ir config not found",
				Err:     err,
			}
		}
		return &ServiceError{
			Kind:    ErrorInternal,
			Message: "failed to load ir config",
			Detail:  err.Error(),
			Err:     err,
		}
	}

	return validateCommandIRRemoteType(config.IRRemoteType)
}

func (s IRAdapterService) validateStatefulIRDevice(roomID string, deviceID string) error {
	if err := s.validateIRDevice(roomID, deviceID); err != nil {
		return err
	}

	config, err := s.irAdapterRepository.LoadConfig(roomID, deviceID)
	if err != nil {
		if os.IsNotExist(err) {
			return &ServiceError{
				Kind:    ErrorNotFound,
				Message: "ir config not found",
				Err:     err,
			}
		}
		return &ServiceError{
			Kind:    ErrorInternal,
			Message: "failed to load ir config",
			Detail:  err.Error(),
			Err:     err,
		}
	}

	return validateStatefulIRRemoteType(config.IRRemoteType)
}

func (s IRAdapterService) validateStateMatchesStoredSchema(roomID string, deviceID string, state models.IRState) error {
	schema, err := s.GetStateSchema(roomID, deviceID)
	if err != nil {
		return err
	}

	return validateIRStateMatchesSchema(state.State, schema)
}

func normalizeIRCodeFields(protocol string, code string) (string, string) {
	return strings.TrimSpace(protocol), strings.ToUpper(strings.TrimSpace(code))
}

func validateIRCodeOperation(protocol string, code string) error {
	if protocol != irCodeProtocolPanasonic40 {
		return &ServiceError{Kind: ErrorInvalidRequest, Message: "ir code protocol is not supported"}
	}
	if len(code) != 10 {
		return &ServiceError{Kind: ErrorInvalidRequest, Message: "invalid ir code"}
	}
	for _, character := range code {
		if (character < '0' || character > '9') && (character < 'A' || character > 'F') {
			return &ServiceError{Kind: ErrorInvalidRequest, Message: "invalid ir code"}
		}
	}
	return nil
}

func normalizeIRState(state models.IRState) models.IRState {
	state.ID = strings.TrimSpace(state.ID)
	state.Name = strings.TrimSpace(state.Name)
	state.Format = models.OperationFormat(strings.TrimSpace(string(state.Format)))
	state.PicoCommand = strings.TrimSpace(state.PicoCommand)
	state.Protocol, state.Code = normalizeIRCodeFields(state.Protocol, state.Code)
	return state
}

func irStateFromWriteRequest(request models.IRStateWriteRequest) models.IRState {
	state := models.IRState{}
	if request.ID != nil {
		state.ID = *request.ID
	}
	if request.Name != nil {
		state.Name = *request.Name
	}
	if request.State != nil {
		state.State = *request.State
	}
	if request.Format != nil {
		state.Format = *request.Format
	}
	if request.CarrierKHz != nil {
		state.CarrierKHz = *request.CarrierKHz
	}
	if request.Count != nil {
		state.Count = *request.Count
	}
	if request.TimingsUS != nil {
		state.TimingsUS = *request.TimingsUS
	}
	if request.Captures != nil {
		state.Captures = *request.Captures
	}
	if request.PicoCommand != nil {
		state.PicoCommand = *request.PicoCommand
	}
	if request.Protocol != nil {
		state.Protocol = *request.Protocol
	}
	if request.Code != nil {
		state.Code = *request.Code
	}
	if request.Repeat != nil {
		state.Repeat = *request.Repeat
	}
	return state
}

func normalizeIRStateWriteRequest(request models.IRStateWriteRequest) models.IRStateWriteRequest {
	if request.ID != nil {
		value := strings.TrimSpace(*request.ID)
		request.ID = &value
	}
	if request.Name != nil {
		value := strings.TrimSpace(*request.Name)
		request.Name = &value
	}
	if request.Format != nil {
		value := models.OperationFormat(strings.TrimSpace(string(*request.Format)))
		request.Format = &value
	}
	if request.PicoCommand != nil {
		value := strings.TrimSpace(*request.PicoCommand)
		request.PicoCommand = &value
	}
	if request.Protocol != nil {
		value := strings.TrimSpace(*request.Protocol)
		request.Protocol = &value
	}
	if request.Code != nil {
		value := strings.ToUpper(strings.TrimSpace(*request.Code))
		request.Code = &value
	}
	return request
}

func mergeIRStateWriteRequest(existing models.IRState, request models.IRStateWriteRequest) (models.IRState, error) {
	state := existing
	if request.ID != nil {
		state.ID = *request.ID
	}
	if request.Name != nil {
		state.Name = *request.Name
	}
	if request.State != nil {
		state.State = *request.State
	}
	if request.Format != nil {
		state.Format = *request.Format
	}
	if request.CarrierKHz != nil {
		state.CarrierKHz = *request.CarrierKHz
	}
	if request.PicoCommand != nil {
		state.PicoCommand = *request.PicoCommand
	}
	if request.Protocol != nil {
		state.Protocol = *request.Protocol
	}
	if request.Code != nil {
		state.Code = *request.Code
	}
	if request.Repeat != nil {
		state.Repeat = *request.Repeat
	}
	if request.Count != nil {
		state.Count = *request.Count
	}
	if request.TimingsUS != nil {
		state.TimingsUS = *request.TimingsUS
	}
	if request.Captures != nil {
		state.Captures = *request.Captures
	}

	if existing.Format == models.OperationFormatRawSamples && state.Format != models.OperationFormatRawSamples {
		if isLearnedRawSamplesState(existing) {
			return models.IRState{}, readOnlyIRStateLearningDataError()
		}
		if err := validateIRStateFormatTransitionTarget(state); err != nil {
			return models.IRState{}, err
		}
	}

	if state.Format != models.OperationFormatRawSamples {
		return state, nil
	}

	if existing.Format != models.OperationFormatRawSamples {
		if err := validateRawSamplesStateTransitionRequest(request); err != nil {
			return models.IRState{}, err
		}
		state.Count = 0
		state.TimingsUS = nil
		state.Captures = nil
		return state, nil
	}

	if err := validateRawSamplesStateUpdateRequest(request); err != nil {
		return models.IRState{}, err
	}
	state.Count = existing.Count
	state.TimingsUS = existing.TimingsUS
	state.Captures = existing.Captures
	return state, nil
}

func validateRawSamplesStateCreateRequest(request models.IRStateWriteRequest) error {
	if request.Count != nil && *request.Count != 0 {
		return readOnlyIRStateLearningDataError()
	}
	if request.TimingsUS != nil && len(*request.TimingsUS) != 0 {
		return readOnlyIRStateLearningDataError()
	}
	if request.Captures != nil && len(*request.Captures) != 0 {
		return readOnlyIRStateLearningDataError()
	}
	return nil
}

func validateRawSamplesStateUpdateRequest(request models.IRStateWriteRequest) error {
	if request.Count != nil || request.TimingsUS != nil || request.Captures != nil {
		return readOnlyIRStateLearningDataError()
	}
	return nil
}

func validateRawSamplesStateTransitionRequest(request models.IRStateWriteRequest) error {
	if request.Count != nil && *request.Count != 0 {
		return readOnlyIRStateLearningDataError()
	}
	if request.TimingsUS != nil || request.Captures != nil {
		return readOnlyIRStateLearningDataError()
	}
	return nil
}

func isLearnedRawSamplesState(state models.IRState) bool {
	return state.Count != 0 || len(state.TimingsUS) != 0 || len(state.Captures) != 0
}

func readOnlyIRStateLearningDataError() error {
	return &ServiceError{
		Kind:    ErrorInvalidRequest,
		Message: "ir state learning data is read only",
	}
}

func applyRawCaptureSafety(capture models.IRCapture) models.IRCapture {
	if !capture.Valid {
		return capture
	}
	if err := backendir.ValidateRawTimings(capture.TimingsUS, capture.Count); err != nil {
		capture.Valid = false
		capture.Error = rawValidationErrorMessage(err)
	}
	return capture
}

func rawValidationServiceError(err error) error {
	return &ServiceError{
		Kind:    ErrorInvalidRequest,
		Message: rawValidationErrorMessage(err),
		Err:     err,
	}
}

func rawValidationErrorMessage(err error) string {
	switch {
	case errors.Is(err, backendir.ErrRawTimingsTooLong):
		return "raw timings are too long"
	case errors.Is(err, backendir.ErrRawTimingExceedsMaximum):
		return "raw timing exceeds maximum"
	case errors.Is(err, backendir.ErrRawDurationExceedsMaximum):
		return "raw duration exceeds maximum"
	default:
		return "invalid raw timings"
	}
}

func validateIRStateFormatTransitionTarget(state models.IRState) error {
	switch state.Format {
	case models.OperationFormatRaw:
		if err := backendir.ValidateRawTimings(state.TimingsUS, state.Count); err != nil {
			return rawValidationServiceError(err)
		}
	case models.OperationFormatCode:
		return validateIRCodeOperation(state.Protocol, state.Code)
	case models.OperationFormatPresetCommand:
		if !iradapter.IsAllowedPresetCommand(state.PicoCommand) {
			return &ServiceError{Kind: ErrorInvalidRequest, Message: "preset command is not allowed"}
		}
	}

	return nil
}

func normalizeIRStateSchema(schema models.IRStateSchema) (models.IRStateSchema, error) {
	normalized := models.IRStateSchema{}
	for fieldID, fieldSchema := range schema {
		fieldID = strings.TrimSpace(fieldID)
		fieldSchema.Type = models.IRStateFieldType(strings.TrimSpace(string(fieldSchema.Type)))
		if _, exists := normalized[fieldID]; exists {
			return nil, schemaIssueError("duplicate normalized ir state field id", fieldID, nil)
		}
		// Enum values and selector values are exact values, including empty strings.
		normalized[fieldID] = fieldSchema
	}
	return normalized, nil
}

func normalizeIRCommand(command models.IRCommand) models.IRCommand {
	command.ID = strings.TrimSpace(command.ID)
	command.Name = strings.TrimSpace(command.Name)
	command.Format = models.OperationFormat(strings.TrimSpace(string(command.Format)))
	command.PicoCommand = strings.TrimSpace(command.PicoCommand)
	command.Protocol, command.Code = normalizeIRCodeFields(command.Protocol, command.Code)
	return command
}

func normalizeIRCommandWriteRequest(request models.IRCommandWriteRequest) models.IRCommandWriteRequest {
	if request.ID != nil {
		value := strings.TrimSpace(*request.ID)
		request.ID = &value
	}
	if request.Name != nil {
		value := strings.TrimSpace(*request.Name)
		request.Name = &value
	}
	if request.Format != nil {
		value := models.OperationFormat(strings.TrimSpace(string(*request.Format)))
		request.Format = &value
	}
	if request.PicoCommand != nil {
		value := strings.TrimSpace(*request.PicoCommand)
		request.PicoCommand = &value
	}
	if request.Protocol != nil {
		value := strings.TrimSpace(*request.Protocol)
		request.Protocol = &value
	}
	if request.Code != nil {
		value := strings.ToUpper(strings.TrimSpace(*request.Code))
		request.Code = &value
	}
	return request
}

func irCommandFromWriteRequest(request models.IRCommandWriteRequest) models.IRCommand {
	command := models.IRCommand{}
	if request.ID != nil {
		command.ID = *request.ID
	}
	if request.Name != nil {
		command.Name = *request.Name
	}
	if request.Format != nil {
		command.Format = *request.Format
	}
	if request.CarrierKHz != nil {
		command.CarrierKHz = *request.CarrierKHz
	}
	if request.Count != nil {
		command.Count = *request.Count
	}
	if request.TimingsUS != nil {
		command.TimingsUS = *request.TimingsUS
	}
	if request.Captures != nil {
		command.Captures = *request.Captures
	}
	if request.PicoCommand != nil {
		command.PicoCommand = *request.PicoCommand
	}
	if request.Protocol != nil {
		command.Protocol = *request.Protocol
	}
	if request.Code != nil {
		command.Code = *request.Code
	}
	if request.Repeat != nil {
		command.Repeat = *request.Repeat
	}
	return command
}

func mergeIRCommandWriteRequest(existing models.IRCommand, request models.IRCommandWriteRequest) (models.IRCommand, error) {
	request = normalizeIRCommandWriteRequest(request)
	command := existing

	if request.Name != nil {
		command.Name = *request.Name
	}
	if request.Format != nil {
		command.Format = *request.Format
	}
	if request.CarrierKHz != nil {
		command.CarrierKHz = *request.CarrierKHz
	}
	if request.PicoCommand != nil {
		command.PicoCommand = *request.PicoCommand
	}
	if request.Protocol != nil {
		command.Protocol = *request.Protocol
	}
	if request.Code != nil {
		command.Code = *request.Code
	}
	if request.Repeat != nil {
		command.Repeat = *request.Repeat
	}

	if existing.Format == models.OperationFormatRawSamples && isLearnedRawSamplesCommand(existing) && command.Format != models.OperationFormatRawSamples {
		return models.IRCommand{}, readOnlyIRCommandLearningDataError()
	}

	if command.Format == models.OperationFormatRawSamples {
		if err := validateRawSamplesUpdateRequest(request); err != nil {
			return models.IRCommand{}, err
		}
		command.Count = existing.Count
		command.TimingsUS = existing.TimingsUS
		command.Captures = existing.Captures
		return command, nil
	}

	if request.Count != nil {
		command.Count = *request.Count
	}
	if request.TimingsUS != nil {
		command.TimingsUS = *request.TimingsUS
	}
	if request.Captures != nil {
		command.Captures = *request.Captures
	}
	return command, nil
}

func validateRawSamplesCreateRequest(request models.IRCommandWriteRequest) error {
	if request.Count != nil && *request.Count != 0 {
		return readOnlyIRCommandLearningDataError()
	}
	if request.TimingsUS != nil && len(*request.TimingsUS) != 0 {
		return readOnlyIRCommandLearningDataError()
	}
	if request.Captures != nil && len(*request.Captures) != 0 {
		return readOnlyIRCommandLearningDataError()
	}
	return nil
}

func validateRawSamplesUpdateRequest(request models.IRCommandWriteRequest) error {
	if request.Captures != nil || request.TimingsUS != nil || (request.Count != nil && *request.Count != 0) {
		return readOnlyIRCommandLearningDataError()
	}
	return nil
}

func isLearnedRawSamplesCommand(command models.IRCommand) bool {
	return command.Count != 0 || len(command.TimingsUS) != 0 || len(command.Captures) != 0
}

func readOnlyIRCommandLearningDataError() error {
	return &ServiceError{
		Kind:    ErrorInvalidRequest,
		Message: "ir command learning data is read only",
	}
}

func validateCommandIRRemoteType(remoteType models.IRRemoteType) error {
	if remoteType == models.IRRemoteTypeCommand {
		return nil
	}
	return &ServiceError{
		Kind:    ErrorInvalidRequest,
		Message: "ir remote type is not command",
	}
}

func validateStatefulIRRemoteType(remoteType models.IRRemoteType) error {
	if remoteType == models.IRRemoteTypeStateful {
		return nil
	}
	return &ServiceError{
		Kind:    ErrorInvalidRequest,
		Message: string(models.IRStateActionErrorRemoteTypeNotStateful),
	}
}

func validateIRCommand(command models.IRCommand) error {
	if !validateIRCommandID(command.ID) {
		return &ServiceError{
			Kind:    ErrorInvalidRequest,
			Message: "invalid ir command id",
		}
	}

	if command.Name == "" {
		return &ServiceError{
			Kind:    ErrorInvalidRequest,
			Message: "ir command name is required",
		}
	}

	if !validateOperationFormat(command.Format) {
		return &ServiceError{
			Kind:    ErrorInvalidRequest,
			Message: "invalid ir command format",
		}
	}

	if err := validateStoredRepeat(command.Repeat); err != nil {
		return err
	}
	if command.CarrierKHz < 0 {
		return &ServiceError{Kind: ErrorInvalidRequest, Message: "carrier_khz must be greater than 0"}
	}

	if command.Format == models.OperationFormatRaw {
		if err := backendir.ValidateRawTimings(command.TimingsUS, command.Count); err != nil {
			return rawValidationServiceError(err)
		}
	}
	if command.Format == models.OperationFormatCode {
		if err := validateIRCodeOperation(command.Protocol, command.Code); err != nil {
			return err
		}
	}

	if command.Format == models.OperationFormatPresetCommand {
		if command.PicoCommand == "" || !iradapter.IsAllowedPresetCommand(command.PicoCommand) {
			return &ServiceError{
				Kind:    ErrorInvalidRequest,
				Message: "preset command is not allowed",
			}
		}
	}

	return nil
}

func validateIRState(state models.IRState) error {
	if !validateIRStateID(state.ID) {
		return &ServiceError{
			Kind:    ErrorInvalidRequest,
			Message: "invalid ir state id",
		}
	}

	if state.Name == "" {
		return &ServiceError{
			Kind:    ErrorInvalidRequest,
			Message: "ir state name is required",
		}
	}

	if !validateOperationFormat(state.Format) {
		return &ServiceError{
			Kind:    ErrorInvalidRequest,
			Message: "invalid ir state format",
		}
	}

	if err := validateStoredRepeat(state.Repeat); err != nil {
		return err
	}
	if state.CarrierKHz < 0 {
		return &ServiceError{Kind: ErrorInvalidRequest, Message: "carrier_khz must be greater than 0"}
	}

	if state.Format == models.OperationFormatRaw {
		if err := backendir.ValidateRawTimings(state.TimingsUS, state.Count); err != nil {
			return rawValidationServiceError(err)
		}
	}
	if state.Format == models.OperationFormatCode {
		if err := validateIRCodeOperation(state.Protocol, state.Code); err != nil {
			return err
		}
	}
	if state.Format == models.OperationFormatPresetCommand && !iradapter.IsAllowedPresetCommand(state.PicoCommand) {
		return &ServiceError{Kind: ErrorInvalidRequest, Message: "preset command is not allowed"}
	}

	if err := validateIRStatePayload(state.State); err != nil {
		return err
	}

	return nil
}

func validateIRStatePayload(state map[string]interface{}) error {
	if state == nil {
		return &ServiceError{
			Kind:    ErrorInvalidRequest,
			Message: "ir state payload is required",
		}
	}

	for key, value := range state {
		if strings.TrimSpace(key) == "" {
			return &ServiceError{
				Kind:    ErrorInvalidRequest,
				Message: "ir state key is required",
			}
		}

		switch value.(type) {
		case nil, string, bool, float64:
			continue
		default:
			return &ServiceError{
				Kind:    ErrorInvalidRequest,
				Message: "ir state value must be a JSON primitive",
			}
		}
	}

	return nil
}

func containsString(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func validateIRStateID(stateID string) bool {
	return validateDeviceID(stateID)
}

func validateIRCommandID(commandID string) bool {
	return validateDeviceID(commandID)
}

func validateOperationFormat(format models.OperationFormat) bool {
	switch format {
	case models.OperationFormatRawSamples,
		models.OperationFormatRaw,
		models.OperationFormatCode,
		models.OperationFormatPresetCommand:
		return true
	default:
		return false
	}
}

func validateIRStateFieldID(fieldID string) bool {
	return validateDeviceID(fieldID)
}

func validateIRStateFieldType(fieldType models.IRStateFieldType) bool {
	switch fieldType {
	case models.IRStateFieldTypeEnum,
		models.IRStateFieldTypeNumber,
		models.IRStateFieldTypeBoolean,
		models.IRStateFieldTypeString:
		return true
	default:
		return false
	}
}

func validateIRConfig(config models.IRConfig) error {
	if !validateIRRemoteType(config.IRRemoteType) {
		return &ServiceError{
			Kind:    ErrorInvalidRequest,
			Message: "invalid ir remote type",
		}
	}

	if config.CarrierKHz <= 0 {
		return &ServiceError{
			Kind:    ErrorInvalidRequest,
			Message: "carrier_khz must be greater than 0",
		}
	}

	if err := validateStoredRepeat(config.DefaultRepeat); err != nil {
		return err
	}

	return nil
}

func resolveCommandCaptureRequest(request models.IRCommandCaptureRequest) (int, int, error) {
	samples := 3
	if request.Samples != nil {
		samples = *request.Samples
	}
	if samples < 1 || samples > 7 {
		return 0, 0, &ServiceError{
			Kind:    ErrorInvalidRequest,
			Message: "samples must be between 1 and 7",
		}
	}

	maxAttempts := samples * 3
	if request.MaxAttempts != nil {
		maxAttempts = *request.MaxAttempts
	}
	if maxAttempts < samples || maxAttempts > 20 {
		return 0, 0, &ServiceError{
			Kind:    ErrorInvalidRequest,
			Message: "max_attempts must be between samples and 20",
		}
	}

	return samples, maxAttempts, nil
}

func validateCommandCaptureFormat(command models.IRCommand) error {
	if command.Format == models.OperationFormatRawSamples {
		return nil
	}
	return &ServiceError{
		Kind:    ErrorInvalidRequest,
		Message: "ir command format is not supported for capture",
		Detail:  "capture supports raw_samples only",
	}
}

func validateCommandSendFormat(command models.IRCommand) error {
	switch command.Format {
	case models.OperationFormatRawSamples, models.OperationFormatRaw:
		return nil
	case models.OperationFormatCode:
		return validateIRCodeOperation(command.Protocol, command.Code)
	case models.OperationFormatPresetCommand:
		if command.PicoCommand != "" && iradapter.IsAllowedPresetCommand(command.PicoCommand) {
			return nil
		}
		return &ServiceError{
			Kind:    ErrorInvalidRequest,
			Message: "preset command is not allowed",
		}
	default:
		return &ServiceError{
			Kind:    ErrorInvalidRequest,
			Message: "ir command format is not supported for send",
		}
	}
}

func resolveCommandRepeat(requestRepeat *int, commandRepeat int, configRepeat int) (int, error) {
	if requestRepeat != nil {
		if err := validateEffectiveRepeat(*requestRepeat); err != nil {
			return 0, err
		}
		return *requestRepeat, nil
	}

	if commandRepeat != 0 {
		if err := validateEffectiveRepeat(commandRepeat); err != nil {
			return 0, err
		}
		return commandRepeat, nil
	}

	if configRepeat != 0 {
		if err := validateEffectiveRepeat(configRepeat); err != nil {
			return 0, err
		}
		return configRepeat, nil
	}

	return 1, nil
}

func validateStoredRepeat(repeat int) error {
	if repeat == 0 {
		return nil
	}
	return validateEffectiveRepeat(repeat)
}

func validateEffectiveRepeat(repeat int) error {
	if repeat < 1 || repeat > 5 {
		return &ServiceError{
			Kind:    ErrorInvalidRequest,
			Message: "repeat must be between 1 and 5",
		}
	}
	return nil
}

func validateIRRemoteType(remoteType models.IRRemoteType) bool {
	switch remoteType {
	case models.IRRemoteTypeCommand, models.IRRemoteTypeStateful:
		return true
	default:
		return false
	}
}
