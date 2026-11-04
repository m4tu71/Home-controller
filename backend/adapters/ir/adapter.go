package ir

import (
	"encoding/json"
	"time"

	"backend/controller_client"
	backendir "backend/ir"
	"backend/models"
)

var allowedPresetCommands = map[string]struct{}{
	"LIGHT_ON_CH3":          {},
	"LIGHT_FULL_CH3":        {},
	"LIGHT_OFF_CH3":         {},
	"LIGHT_NIGHT_CH3":       {},
	"LIGHT_BRIGHT_UP_CH3":   {},
	"LIGHT_BRIGHT_DOWN_CH3": {},
}

var sendControllerCommandResult = controller_client.SendCommandResult

// IsAllowedPresetCommand is the single backend allowlist for IR Command and
// State preset_command values. LIGHT_STATUS_CH3 is intentionally excluded
// because it is a status query, not an IR transmission operation.
func IsAllowedPresetCommand(command string) bool {
	_, ok := allowedPresetCommands[command]
	return ok
}

func CaptureRaw() (string, error) {
	return controller_client.SendCommand("IR_CAPTURE")
}

func Capture(attempt int) models.IRCapture {
	response, err := CaptureRaw()
	if err != nil {
		return models.IRCapture{
			Attempt:    attempt,
			OK:         false,
			Valid:      false,
			Count:      0,
			Error:      err.Error(),
			CapturedAt: time.Now(),
		}
	}

	var capture models.IRCaptureResponse
	if err := json.Unmarshal([]byte(response), &capture); err != nil {
		return models.IRCapture{
			Attempt:    attempt,
			OK:         false,
			Valid:      false,
			Count:      0,
			Error:      "invalid capture response: " + err.Error(),
			CapturedAt: time.Now(),
		}
	}

	valid := capture.OK && backendir.ValidateCapturedTimings(capture.TimingsUS)

	result := models.IRCapture{
		Attempt:    attempt,
		OK:         capture.OK,
		Valid:      valid,
		Count:      len(capture.TimingsUS),
		TimingsUS:  capture.TimingsUS,
		Error:      capture.Error,
		CapturedAt: time.Now(),
	}

	if capture.OK && !valid {
		result.Error = "invalid or too short IR signal"
	}

	return result
}

func SendRaw(timings []uint) (string, error) {
	result, err := SendRawResult(timings)
	return result.ResponseBody, err
}

func SendRawResult(timings []uint) (controller_client.CommandResult, error) {
	return sendControllerCommandResult(backendir.BuildIRSendRawCommand(timings))
}

func SendPresetCommand(command string) (string, error) {
	result, err := SendPresetCommandResult(command)
	return result.ResponseBody, err
}

func SendPresetCommandResult(command string) (controller_client.CommandResult, error) {
	return sendControllerCommandResult(command)
}

func GetCapabilities() (models.IRCapabilities, controller_client.CommandResult, error) {
	result, err := sendControllerCommandResult("IR_CAPABILITIES")
	if err != nil {
		return models.IRCapabilities{}, result, err
	}
	var capabilities models.IRCapabilities
	if parseErr := json.Unmarshal([]byte(result.ResponseBody), &capabilities); parseErr != nil {
		return models.IRCapabilities{}, result, parseErr
	}
	return capabilities, result, nil
}

func SendCodeResult(protocol string, code string) (controller_client.CommandResult, error) {
	return sendControllerCommandResult("IR_SEND_CODE:" + protocol + ":" + code)
}
