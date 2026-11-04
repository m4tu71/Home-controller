package ir

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const ProtocolPanasonic40 = "PANASONIC_40"

var (
	ErrUnsupportedCodeProtocol = errors.New("unsupported ir code protocol")
	ErrInvalidIRCode           = errors.New("invalid ir code")
)

// EncodeCodeToRaw converts a supported protocol/code pair into Pico-compatible
// raw mark/space timings. PANASONIC_40 is emitted MSB first.
func EncodeCodeToRaw(protocol string, code string) ([]uint, error) {
	if strings.TrimSpace(protocol) != ProtocolPanasonic40 {
		return nil, ErrUnsupportedCodeProtocol
	}

	normalizedCode := strings.ToUpper(strings.TrimSpace(code))
	value, err := strconv.ParseUint(normalizedCode, 16, 40)
	if err != nil || len(normalizedCode) != 10 {
		return nil, ErrInvalidIRCode
	}

	const (
		bitCount   = 40
		leaderMark = uint(3500)
		leaderGap  = uint(1750)
		bitMark    = uint(450)
		zeroGap    = uint(450)
		oneGap     = uint(1300)
		stopGap    = uint(10000)
	)

	timings := make([]uint, 0, 2+bitCount*2+2)
	timings = append(timings, leaderMark, leaderGap)
	for bit := bitCount - 1; bit >= 0; bit-- {
		timings = append(timings, bitMark)
		if value&(uint64(1)<<bit) != 0 {
			timings = append(timings, oneGap)
		} else {
			timings = append(timings, zeroGap)
		}
	}
	timings = append(timings, bitMark, stopGap)

	if err := ValidateRawTimings(timings, len(timings)); err != nil {
		return nil, fmt.Errorf("validate encoded ir code: %w", err)
	}

	return timings, nil
}
