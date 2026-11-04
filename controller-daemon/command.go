package main

import (
	"strings"
)

const (
	irCapabilitiesCommand     = "IR_CAPABILITIES"
	irSendCodePrefix          = "IR_SEND_CODE:"
	irCodeProtocolPanasonic40 = "PANASONIC_40"
	maxIRCodeProtocolLength   = 32
	panasonic40CodeLength     = 10
	maxIRCodeLength           = 10
)

var forwardCommandToPico = sendCommandToPico

func handleCommand(command string) string {
	if command == irCapabilitiesCommand {
		return forwardPicoCommand(command)
	}

	if strings.HasPrefix(command, irSendCodePrefix) {
		if message, errorCode := validateIRSendCodeCommand(command); errorCode != "" {
			return makeCodedErrorResponse(message, errorCode)
		}
		return forwardPicoCommand(command)
	}

	if strings.HasPrefix(command, "IR_SEND_RAW:") {
		return forwardPicoCommand(command)
	}

	switch command {
	case "LED_ON", "LED_OFF", "LED_STATUS", "IR_TEST",
		"LIGHT_ON_CH3", "LIGHT_FULL_CH3", "LIGHT_OFF_CH3", "LIGHT_STATUS_CH3",
		"LIGHT_NIGHT_CH3", "LIGHT_BRIGHT_UP_CH3", "LIGHT_BRIGHT_DOWN_CH3",
		"IR_CAPTURE":

		return forwardPicoCommand(command)

	default:
		if strings.HasPrefix(command, "IR_") {
			return makeCodedErrorResponse("unsupported command", "unsupported_command")
		}
		return makeErrorResponse("unknown command")
	}
}

func validateIRSendCodeCommand(command string) (string, string) {
	payload := strings.TrimPrefix(command, irSendCodePrefix)
	parts := strings.Split(payload, ":")
	if len(parts) != 2 {
		return "invalid code", "invalid_code"
	}

	protocol, code := parts[0], parts[1]
	if protocol == "" || len(protocol) > maxIRCodeProtocolLength || protocol != irCodeProtocolPanasonic40 {
		return "unsupported protocol", "unsupported_protocol"
	}
	if len(code) > maxIRCodeLength || len(code) != panasonic40CodeLength || !isUpperHex(code) {
		return "invalid code", "invalid_code"
	}

	return "", ""
}

func isUpperHex(value string) bool {
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'A' || character > 'F') {
			return false
		}
	}
	return true
}

func forwardPicoCommand(command string) string {
	response, err := forwardCommandToPico(command)
	if err != nil {
		return makeErrorResponse(err.Error())
	}
	return response
}
