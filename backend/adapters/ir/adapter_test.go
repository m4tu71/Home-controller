package ir

import (
	"errors"
	"reflect"
	"testing"

	"backend/controller_client"
	"backend/models"
)

func TestSendRawResultReturnsTypedControllerResult(t *testing.T) {
	original := sendControllerCommandResult
	t.Cleanup(func() { sendControllerCommandResult = original })

	wantErr := errors.New("timeout")
	wantResult := controller_client.CommandResult{
		ResponseBody:   "partial response",
		FailureStage:   controller_client.FailureStageRead,
		BytesWritten:   18,
		WriteCompleted: true,
		Err:            wantErr,
	}
	var gotCommand string
	sendControllerCommandResult = func(command string) (controller_client.CommandResult, error) {
		gotCommand = command
		return wantResult, wantErr
	}

	gotResult, err := SendRawResult([]uint{3500, 1750, 450, 10000})
	if !errors.Is(err, wantErr) {
		t.Fatalf("SendRawResult() error = %v, want %v", err, wantErr)
	}
	if gotCommand != "IR_SEND_RAW:3500,1750,450,10000" {
		t.Fatalf("controller command = %q", gotCommand)
	}
	if !reflect.DeepEqual(gotResult, wantResult) {
		t.Fatalf("SendRawResult() = %#v, want %#v", gotResult, wantResult)
	}
}

func TestSendPresetCommandResultReturnsParsedRemoteRejection(t *testing.T) {
	original := sendControllerCommandResult
	t.Cleanup(func() { sendControllerCommandResult = original })

	wantErr := controller_client.ErrRemoteRejected
	wantResult := controller_client.CommandResult{
		ResponseBody:   `{"ok":false,"error":"unsupported command","error_code":"unsupported_command"}` + "\n",
		FailureStage:   controller_client.FailureStageRemoteReject,
		BytesWritten:   len("LIGHT_ON_CH3\n"),
		WriteCompleted: true,
		RemoteResponse: &controller_client.RemoteResponse{
			OK:        false,
			Error:     "unsupported command",
			ErrorCode: models.IRWireErrorCodeUnsupportedCommand,
		},
		Err: wantErr,
	}
	var gotCommand string
	sendControllerCommandResult = func(command string) (controller_client.CommandResult, error) {
		gotCommand = command
		return wantResult, wantErr
	}

	gotResult, err := SendPresetCommandResult("LIGHT_ON_CH3")
	if !errors.Is(err, wantErr) {
		t.Fatalf("SendPresetCommandResult() error = %v, want %v", err, wantErr)
	}
	if gotCommand != "LIGHT_ON_CH3" {
		t.Fatalf("controller command = %q", gotCommand)
	}
	if !reflect.DeepEqual(gotResult, wantResult) {
		t.Fatalf("SendPresetCommandResult() = %#v, want %#v", gotResult, wantResult)
	}
	if !gotResult.IsPreSendRejection() {
		t.Fatal("typed adapter result must preserve pre-send rejection classification")
	}
}

func TestLegacySendRawReturnsResponseAndError(t *testing.T) {
	original := sendControllerCommandResult
	t.Cleanup(func() { sendControllerCommandResult = original })

	wantErr := errors.New("read failed")
	sendControllerCommandResult = func(string) (controller_client.CommandResult, error) {
		return controller_client.CommandResult{ResponseBody: "partial", Err: wantErr}, wantErr
	}

	response, err := SendRaw([]uint{450})
	if response != "partial" || !errors.Is(err, wantErr) {
		t.Fatalf("SendRaw() = (%q, %v), want (%q, %v)", response, err, "partial", wantErr)
	}
}

func TestGetCapabilitiesAndSendCodeUseTypedControllerBoundary(t *testing.T) {
	original := sendControllerCommandResult
	t.Cleanup(func() { sendControllerCommandResult = original })
	commands := []string{}
	sendControllerCommandResult = func(command string) (controller_client.CommandResult, error) {
		commands = append(commands, command)
		if command == "IR_CAPABILITIES" {
			return controller_client.CommandResult{ResponseBody: `{"ok":true,"commands":["IR_SEND_RAW","IR_SEND_CODE"],"code_protocols":["PANASONIC_40"],"raw_carrier_khz":[38]}`}, nil
		}
		return controller_client.CommandResult{ResponseBody: `{"ok":true}`}, nil
	}

	capabilities, _, err := GetCapabilities()
	if err != nil || !capabilities.OK || !reflect.DeepEqual(capabilities.Commands, []string{"IR_SEND_RAW", "IR_SEND_CODE"}) || !reflect.DeepEqual(capabilities.CodeProtocols, []string{"PANASONIC_40"}) {
		t.Fatalf("capabilities = %#v, %v", capabilities, err)
	}
	if _, err := SendCodeResult("PANASONIC_40", "344A90BC2C"); err != nil {
		t.Fatalf("SendCodeResult: %v", err)
	}
	want := []string{"IR_CAPABILITIES", "IR_SEND_CODE:PANASONIC_40:344A90BC2C"}
	if !reflect.DeepEqual(commands, want) {
		t.Fatalf("commands = %#v, want %#v", commands, want)
	}
}

func TestGetCapabilitiesReturnsJSONParseError(t *testing.T) {
	original := sendControllerCommandResult
	t.Cleanup(func() { sendControllerCommandResult = original })
	wantResult := controller_client.CommandResult{ResponseBody: `{"ok":true,"commands":`}
	sendControllerCommandResult = func(command string) (controller_client.CommandResult, error) {
		if command != "IR_CAPABILITIES" {
			t.Fatalf("command = %q", command)
		}
		return wantResult, nil
	}

	capabilities, result, err := GetCapabilities()
	if err == nil {
		t.Fatal("GetCapabilities() error = nil, want JSON parse error")
	}
	if !reflect.DeepEqual(result, wantResult) || capabilities.OK {
		t.Fatalf("capabilities/result = %#v / %#v", capabilities, result)
	}
}
