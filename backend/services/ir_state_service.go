package services

import (
	iradapter "backend/adapters/ir"
	backendir "backend/ir"
	"backend/models"
)

// CaptureState learns or re-learns a raw_samples State. It persists the new
// learning data only after enough valid captures and representative timings
// have both been produced successfully.
func (s IRAdapterService) CaptureState(roomID string, deviceID string, stateID string, request models.IRStateCaptureRequest) (models.IRStateCaptureResponse, error) {
	state, err := s.GetState(roomID, deviceID, stateID)
	if err != nil {
		return models.IRStateCaptureResponse{}, err
	}
	if err := s.validateStoredStateForOperation(roomID, deviceID, state); err != nil {
		return models.IRStateCaptureResponse{}, err
	}
	if err := validateStateCaptureFormat(state); err != nil {
		return models.IRStateCaptureResponse{}, err
	}

	samples, maxAttempts, err := resolveStateCaptureRequest(request)
	if err != nil {
		return models.IRStateCaptureResponse{}, err
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
		return models.IRStateCaptureResponse{}, &ServiceError{
			Kind:    ErrorCaptureInsufficient,
			Message: string(models.IRStateActionErrorNotEnoughCaptures),
			Payload: map[string]interface{}{
				"ok":                 false,
				"error":              string(models.IRStateActionErrorNotEnoughCaptures),
				"required_samples":   samples,
				"valid_captures":     validCaptures,
				"count_distribution": backendir.BuildCountDistribution(captures),
				"captures":           captures,
			},
		}
	}

	timings, err := backendir.InferRepresentativeTimings(captures)
	if err != nil {
		return models.IRStateCaptureResponse{}, &ServiceError{
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
		return models.IRStateCaptureResponse{}, rawValidationServiceError(err)
	}

	state.Captures = captures
	state.TimingsUS = timings
	state.Count = len(timings)
	if err := s.irAdapterRepository.SaveState(roomID, deviceID, state); err != nil {
		return models.IRStateCaptureResponse{}, &ServiceError{
			Kind:    ErrorInternal,
			Message: "failed to save ir state",
			Detail:  err.Error(),
			Err:     err,
		}
	}

	return models.IRStateCaptureResponse{
		OK:              true,
		State:           state,
		RequiredSamples: samples,
		ValidCaptures:   validCaptures,
	}, nil
}

func resolveStateCaptureRequest(request models.IRStateCaptureRequest) (int, int, error) {
	commandRequest := models.IRCommandCaptureRequest{
		Samples:     request.Samples,
		MaxAttempts: request.MaxAttempts,
	}
	return resolveCommandCaptureRequest(commandRequest)
}

func validateStateCaptureFormat(state models.IRState) error {
	if state.Format == models.OperationFormatRawSamples {
		return nil
	}
	return &ServiceError{
		Kind:    ErrorInvalidRequest,
		Message: "ir state format is not supported for capture",
	}
}

// SendState sends a learned raw_samples State through the existing RAW send
// path. Repeat is resolved from the request, State, IR config, then one.
func (s IRAdapterService) SendState(roomID string, deviceID string, stateID string, request models.IRStateSendRequest) (models.IRStateSendResponse, error) {
	state, err := s.GetState(roomID, deviceID, stateID)
	if err != nil {
		return models.IRStateSendResponse{}, err
	}
	if err := s.validateStoredStateForOperation(roomID, deviceID, state); err != nil {
		return models.IRStateSendResponse{}, err
	}
	if err := validateIRExecutionMode(state.Format, request.ExecutionMode, state.Protocol); err != nil {
		return models.IRStateSendResponse{}, err
	}
	if err := validateStateSendFormat(state); err != nil {
		return models.IRStateSendResponse{}, err
	}

	plan, err := s.buildStateSendPlan(roomID, deviceID, state, request.Repeat)
	if err != nil {
		return models.IRStateSendResponse{}, err
	}

	switch plan.Format {
	case models.OperationFormatRawSamples:
		timings, err := backendir.GetTimingsForSendData(state.Captures, state.TimingsUS)
		if err != nil {
			return models.IRStateSendResponse{}, &ServiceError{
				Kind:    ErrorConflict,
				Message: string(models.IRStateActionErrorNotLearned),
				Err:     err,
			}
		}
		if err := backendir.ValidateRawTimings(timings, len(timings)); err != nil {
			return models.IRStateSendResponse{}, rawValidationServiceError(err)
		}
		plan.TimingsUS = timings
	case models.OperationFormatRaw:
		if err := backendir.ValidateRawTimings(state.TimingsUS, state.Count); err != nil {
			return models.IRStateSendResponse{}, rawValidationServiceError(err)
		}
		plan.TimingsUS = state.TimingsUS
	case models.OperationFormatCode:
		plan.ExecutionMode = *request.ExecutionMode
		if plan.ExecutionMode == models.IRExecutionModeBackendRaw {
			timings, err := backendir.EncodeCodeToRaw(plan.Protocol, plan.Code)
			if err != nil {
				return models.IRStateSendResponse{}, codeEncoderServiceError(err)
			}
			if err := backendir.ValidateRawTimings(timings, len(timings)); err != nil {
				return models.IRStateSendResponse{}, rawValidationServiceError(err)
			}
			plan.TimingsUS = timings
		} else if option := s.resolveNativeCodeAvailability(plan.Protocol); !option.Available {
			return models.IRStateSendResponse{}, nativeCodeUnavailableError(plan, option.Reason)
		}
	case models.OperationFormatPresetCommand:
	default:
		return models.IRStateSendResponse{}, unsupportedSendFormatError("ir state format is not supported for send")
	}

	if err := validateIRSendCarrier(plan, request.ExecutionMode); err != nil {
		return models.IRStateSendResponse{}, err
	}

	execution, err := s.executeIRSendPlan(plan)
	if err != nil {
		return models.IRStateSendResponse{}, newControllerSendError(string(models.IRStateActionErrorControllerDaemon), plan, request.ExecutionMode, execution, err)
	}

	return models.IRStateSendResponse{
		OK:               true,
		RoomID:           roomID,
		DeviceID:         deviceID,
		StateID:          stateID,
		Format:           state.Format,
		Protocol:         plan.Protocol,
		ExecutionMode:    executionModeValue(request.ExecutionMode),
		DeliveryState:    execution.DeliveryState,
		Repeat:           plan.Repeat,
		RequestedRepeats: execution.RequestedRepeats,
		CompletedRepeats: execution.CompletedRepeats,
		RetryableWith:    []models.IRExecutionMode{},
	}, nil
}

func validateStateSendFormat(state models.IRState) error {
	if state.Format == models.OperationFormatRawSamples || state.Format == models.OperationFormatRaw {
		return nil
	}
	if state.Format == models.OperationFormatCode {
		return validateIRCodeOperation(state.Protocol, state.Code)
	}
	if state.Format == models.OperationFormatPresetCommand {
		if iradapter.IsAllowedPresetCommand(state.PicoCommand) {
			return nil
		}
		message := "preset command is not allowed"
		return &ServiceError{
			Kind:    ErrorInvalidRequest,
			Message: message,
			Payload: models.IRSendErrorResponse{
				OK:            false,
				Error:         message,
				DeliveryState: models.IRDeliveryStateNotSent,
				RetryableWith: []models.IRExecutionMode{},
			},
		}
	}
	return &ServiceError{
		Kind:    ErrorInvalidRequest,
		Message: "ir state format is not supported for send",
	}
}
