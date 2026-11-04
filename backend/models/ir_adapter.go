package models

import "time"

type IRRemoteType string

const (
	IRRemoteTypeCommand  IRRemoteType = "command"
	IRRemoteTypeStateful IRRemoteType = "stateful"
)

type OperationFormat string

const (
	OperationFormatRawSamples    OperationFormat = "raw_samples"
	OperationFormatRaw           OperationFormat = "raw"
	OperationFormatCode          OperationFormat = "code"
	OperationFormatPresetCommand OperationFormat = "preset_command"
)

type IRExecutionMode string

const (
	IRExecutionModeBackendRaw IRExecutionMode = "backend_raw"
	IRExecutionModeNativeCode IRExecutionMode = "native_code"
)

type IRDeliveryState string

const (
	IRDeliveryStateNotSent       IRDeliveryState = "not_sent"
	IRDeliveryStatePartiallySent IRDeliveryState = "partially_sent"
	IRDeliveryStateUnknown       IRDeliveryState = "unknown"
	IRDeliveryStateSent          IRDeliveryState = "sent"
)

type IRVerificationStatus string

const (
	IRVerificationStatusConfirmed IRVerificationStatus = "confirmed"
	IRVerificationStatusUnknown   IRVerificationStatus = "unknown"
)

type IRExecutionModeOption struct {
	Available          bool                 `json:"available"`
	VerificationStatus IRVerificationStatus `json:"verification_status"`
	Reason             string               `json:"reason"`
}

type IRExecutionModeOptions struct {
	NativeCode IRExecutionModeOption `json:"native_code"`
	BackendRaw IRExecutionModeOption `json:"backend_raw"`
}

type IRExecutionOptionsResponse struct {
	Format         OperationFormat        `json:"format"`
	Protocol       string                 `json:"protocol"`
	ExecutionModes IRExecutionModeOptions `json:"execution_modes"`
}

type IRCapabilities struct {
	OK            bool     `json:"ok"`
	Commands      []string `json:"commands"`
	CodeProtocols []string `json:"code_protocols"`
	RawCarrierKHz []int    `json:"raw_carrier_khz"`
}

type IRWireErrorCode string

const (
	IRWireErrorCodeUnsupportedCommand        IRWireErrorCode = "unsupported_command"
	IRWireErrorCodeUnsupportedProtocol       IRWireErrorCode = "unsupported_protocol"
	IRWireErrorCodeInvalidCode               IRWireErrorCode = "invalid_code"
	IRWireErrorCodeInvalidRawTiming          IRWireErrorCode = "invalid_raw_timing"
	IRWireErrorCodeInvalidRawSeparator       IRWireErrorCode = "invalid_raw_separator"
	IRWireErrorCodeEmptyRawTimings           IRWireErrorCode = "empty_raw_timings"
	IRWireErrorCodeRawTimingExceedsMaximum   IRWireErrorCode = "raw_timing_exceeds_maximum"
	IRWireErrorCodeRawDurationExceedsMaximum IRWireErrorCode = "raw_duration_exceeds_maximum"
	IRWireErrorCodeTooManyRawTimings         IRWireErrorCode = "too_many_raw_timings"
)

func (code IRWireErrorCode) IsPreSendRejection() bool {
	switch code {
	case IRWireErrorCodeUnsupportedCommand,
		IRWireErrorCodeUnsupportedProtocol,
		IRWireErrorCodeInvalidCode,
		IRWireErrorCodeInvalidRawTiming,
		IRWireErrorCodeInvalidRawSeparator,
		IRWireErrorCodeEmptyRawTimings,
		IRWireErrorCodeRawTimingExceedsMaximum,
		IRWireErrorCodeRawDurationExceedsMaximum,
		IRWireErrorCodeTooManyRawTimings:
		return true
	default:
		return false
	}
}

type IRSendTargetType string

const (
	IRSendTargetTypeCommand IRSendTargetType = "command"
	IRSendTargetTypeState   IRSendTargetType = "state"
)

type IRStateFieldType string

const (
	IRStateFieldTypeEnum    IRStateFieldType = "enum"
	IRStateFieldTypeNumber  IRStateFieldType = "number"
	IRStateFieldTypeBoolean IRStateFieldType = "boolean"
	IRStateFieldTypeString  IRStateFieldType = "string"
)

type IRConfig struct {
	IRRemoteType  IRRemoteType `json:"ir_remote_type"`
	CarrierKHz    int          `json:"carrier_khz"`
	DefaultRepeat int          `json:"default_repeat,omitempty"`
}

type IRCaptureResponse struct {
	OK        bool   `json:"ok"`
	Device    string `json:"device,omitempty"`
	State     string `json:"state,omitempty"`
	Count     int    `json:"count,omitempty"`
	TimingsUS []uint `json:"timings_us,omitempty"`
	Error     string `json:"error,omitempty"`
}

type IRCapture struct {
	Attempt    int       `json:"attempt"`
	OK         bool      `json:"ok"`
	Valid      bool      `json:"valid"`
	Count      int       `json:"count"`
	TimingsUS  []uint    `json:"timings_us,omitempty"`
	Error      string    `json:"error,omitempty"`
	CapturedAt time.Time `json:"captured_at"`
}

type IRCommand struct {
	ID     string          `json:"id"`
	Name   string          `json:"name"`
	Format OperationFormat `json:"format"`

	CarrierKHz int         `json:"carrier_khz,omitempty"`
	Count      int         `json:"count,omitempty"`
	TimingsUS  []uint      `json:"timings_us,omitempty"`
	Captures   []IRCapture `json:"captures,omitempty"`

	PicoCommand string `json:"pico_command,omitempty"`
	Protocol    string `json:"protocol,omitempty"`
	Code        string `json:"code,omitempty"`
	Repeat      int    `json:"repeat,omitempty"`
}

// IRCommandWriteRequest is the HTTP write model for IR Commands. Pointer
// fields preserve whether a field was omitted, which protects raw_samples
// learning data from normal create and update requests.
type IRCommandWriteRequest struct {
	ID     *string          `json:"id"`
	Name   *string          `json:"name"`
	Format *OperationFormat `json:"format"`

	CarrierKHz *int         `json:"carrier_khz"`
	Count      *int         `json:"count"`
	TimingsUS  *[]uint      `json:"timings_us"`
	Captures   *[]IRCapture `json:"captures"`

	PicoCommand *string `json:"pico_command"`
	Protocol    *string `json:"protocol"`
	Code        *string `json:"code"`
	Repeat      *int    `json:"repeat"`
}

// IRCommandCaptureRequest keeps optional fields as pointers so that the
// capture API can distinguish omitted values from explicit zero values.
type IRCommandCaptureRequest struct {
	Samples     *int `json:"samples"`
	MaxAttempts *int `json:"max_attempts"`
}

type IRCommandCaptureResponse struct {
	OK              bool      `json:"ok"`
	Command         IRCommand `json:"command"`
	RequiredSamples int       `json:"required_samples"`
	ValidCaptures   int       `json:"valid_captures"`
}

// IRCommandSendRequest keeps Repeat optional so the service can apply the
// command, config, and default repeat fallback in that order.
type IRCommandSendRequest struct {
	Repeat        *int             `json:"repeat"`
	ExecutionMode *IRExecutionMode `json:"execution_mode"`
}

type IRCommandSendResponse struct {
	OK               bool              `json:"ok"`
	RoomID           string            `json:"room_id"`
	DeviceID         string            `json:"device_id"`
	CommandID        string            `json:"command_id"`
	Format           OperationFormat   `json:"format"`
	Protocol         string            `json:"protocol"`
	ExecutionMode    IRExecutionMode   `json:"execution_mode"`
	DeliveryState    IRDeliveryState   `json:"delivery_state"`
	Repeat           int               `json:"repeat"`
	RequestedRepeats int               `json:"requested_repeats"`
	CompletedRepeats int               `json:"completed_repeats"`
	RetryableWith    []IRExecutionMode `json:"retryable_with"`
}

type IRStateSchema map[string]IRStateFieldSchema

type IRStateFieldSchema struct {
	Type        IRStateFieldType    `json:"type"`
	Values      []string            `json:"values,omitempty"`
	Min         *float64            `json:"min,omitempty"`
	Max         *float64            `json:"max,omitempty"`
	Presence    *IRStatePresence    `json:"presence,omitempty"`
	Label       *string             `json:"label,omitempty"`
	Description *string             `json:"description,omitempty"`
	Unit        *string             `json:"unit,omitempty"`
	Step        *float64            `json:"step,omitempty"`
	AllowEmpty  *bool               `json:"allow_empty,omitempty"`
	Cases       *[]IRStateFieldCase `json:"cases,omitempty"`
}

type IRState struct {
	ID     string                 `json:"id"`
	Name   string                 `json:"name"`
	State  map[string]interface{} `json:"state"`
	Format OperationFormat        `json:"format"`

	CarrierKHz int         `json:"carrier_khz,omitempty"`
	Count      int         `json:"count,omitempty"`
	TimingsUS  []uint      `json:"timings_us,omitempty"`
	Captures   []IRCapture `json:"captures,omitempty"`

	PicoCommand string `json:"pico_command,omitempty"`
	Protocol    string `json:"protocol,omitempty"`
	Code        string `json:"code,omitempty"`
	Repeat      int    `json:"repeat,omitempty"`
}

// IRStateWriteRequest is the HTTP write model for IR States. Pointer fields
// preserve whether a field was omitted, which will let normal writes protect
// raw_samples learning data without conflating an omitted value with zero.
type IRStateWriteRequest struct {
	ID     *string                 `json:"id"`
	Name   *string                 `json:"name"`
	State  *map[string]interface{} `json:"state"`
	Format *OperationFormat        `json:"format"`

	CarrierKHz *int         `json:"carrier_khz"`
	Count      *int         `json:"count"`
	TimingsUS  *[]uint      `json:"timings_us"`
	Captures   *[]IRCapture `json:"captures"`

	PicoCommand *string `json:"pico_command"`
	Protocol    *string `json:"protocol"`
	Code        *string `json:"code"`
	Repeat      *int    `json:"repeat"`
}

// IRStateCaptureRequest keeps optional fields as pointers so the capture API
// can distinguish omitted values from explicit zero values.
type IRStateCaptureRequest struct {
	Samples     *int `json:"samples"`
	MaxAttempts *int `json:"max_attempts"`
}

type IRStateCaptureResponse struct {
	OK              bool    `json:"ok"`
	State           IRState `json:"state"`
	RequiredSamples int     `json:"required_samples"`
	ValidCaptures   int     `json:"valid_captures"`
}

// IRStateCaptureInsufficientResponse is the fixed 422 response contract for
// capture attempts that cannot collect enough valid RAW samples.
type IRStateCaptureInsufficientResponse struct {
	OK                bool           `json:"ok"`
	Error             string         `json:"error"`
	RequiredSamples   int            `json:"required_samples"`
	ValidCaptures     int            `json:"valid_captures"`
	CountDistribution map[string]int `json:"count_distribution"`
	Captures          []IRCapture    `json:"captures"`
}

// IRStateSendRequest keeps Repeat optional so the service can apply the
// request, State, config, and default repeat fallback in that order.
type IRStateSendRequest struct {
	Repeat        *int             `json:"repeat"`
	ExecutionMode *IRExecutionMode `json:"execution_mode"`
}

type IRStateSendResponse struct {
	OK               bool              `json:"ok"`
	RoomID           string            `json:"room_id"`
	DeviceID         string            `json:"device_id"`
	StateID          string            `json:"state_id"`
	Format           OperationFormat   `json:"format"`
	Protocol         string            `json:"protocol"`
	ExecutionMode    IRExecutionMode   `json:"execution_mode"`
	DeliveryState    IRDeliveryState   `json:"delivery_state"`
	Repeat           int               `json:"repeat"`
	RequestedRepeats int               `json:"requested_repeats"`
	CompletedRepeats int               `json:"completed_repeats"`
	RetryableWith    []IRExecutionMode `json:"retryable_with"`
}

// IRSendErrorResponse is the fixed error payload shared by Command and State
// send APIs. Zero values are serialized so both endpoints keep one JSON shape.
type IRSendErrorResponse struct {
	OK               bool                 `json:"ok"`
	Error            string               `json:"error"`
	Detail           string               `json:"detail"`
	Protocol         string               `json:"protocol"`
	ExecutionMode    IRExecutionMode      `json:"execution_mode"`
	DeliveryState    IRDeliveryState      `json:"delivery_state"`
	RequestedRepeats int                  `json:"requested_repeats"`
	CompletedRepeats int                  `json:"completed_repeats"`
	RetryableWith    []IRExecutionMode    `json:"retryable_with"`
	Issues           []IRStateSchemaIssue `json:"issues,omitempty"`
}

// IRStateActionError is a fixed error value shared by the State action
// service and HTTP boundary. HTTP status mapping is kept with the action
// implementation so existing ServiceError handling remains the response path.
type IRStateActionError string

const (
	IRStateActionErrorRemoteTypeNotStateful IRStateActionError = "ir remote type is not stateful"
	IRStateActionErrorSchemaNotFound        IRStateActionError = "ir state schema not found"
	IRStateActionErrorNotLearned            IRStateActionError = "ir state is not learned"
	IRStateActionErrorNotEnoughCaptures     IRStateActionError = "not enough valid captures"
	IRStateActionErrorControllerDaemon      IRStateActionError = "controller daemon error"
)

type IRSendPlan struct {
	RoomID     string           `json:"room_id"`
	DeviceID   string           `json:"device_id"`
	TargetType IRSendTargetType `json:"target_type"`
	TargetID   string           `json:"target_id"`
	Format     OperationFormat  `json:"format"`

	CarrierKHz int    `json:"carrier_khz,omitempty"`
	Repeat     int    `json:"repeat,omitempty"`
	TimingsUS  []uint `json:"timings_us,omitempty"`

	PicoCommand   string          `json:"pico_command,omitempty"`
	Protocol      string          `json:"protocol,omitempty"`
	Code          string          `json:"code,omitempty"`
	ExecutionMode IRExecutionMode `json:"execution_mode,omitempty"`

	UnsupportedReason string `json:"unsupported_reason,omitempty"`
}
