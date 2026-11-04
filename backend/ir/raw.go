package ir

import (
	"errors"
	"sort"
	"strconv"

	"backend/models"
)

const (
	countTolerance   = 4
	MaxRawTimings    = 1024
	MaxRawTimingUS   = uint(100000)
	MaxRawDurationUS = uint64(1000000)
)

var (
	ErrInvalidRawTimings         = errors.New("invalid raw timings")
	ErrRawTimingsTooLong         = errors.New("raw timings are too long")
	ErrRawTimingExceedsMaximum   = errors.New("raw timing exceeds maximum")
	ErrRawDurationExceedsMaximum = errors.New("raw duration exceeds maximum")
)

// ValidateRawTimings enforces the shared Backend/Pico wire safety contract.
// The subtraction form of the duration check avoids addition overflow.
func ValidateRawTimings(timings []uint, count int) error {
	if count <= 0 || len(timings) == 0 || count != len(timings) {
		return ErrInvalidRawTimings
	}
	if len(timings) > MaxRawTimings {
		return ErrRawTimingsTooLong
	}

	const picoUnsignedIntMax = uint64(^uint32(0))
	var total uint64
	for _, timing := range timings {
		value := uint64(timing)
		if timing == 0 || value > picoUnsignedIntMax {
			return ErrInvalidRawTimings
		}
		if timing > MaxRawTimingUS {
			return ErrRawTimingExceedsMaximum
		}
		if value > MaxRawDurationUS-total {
			return ErrRawDurationExceedsMaximum
		}
		total += value
	}

	return nil
}

// GetTimingsForSendData prepares send timings for Device-level IR Commands
// and States without changing their persisted JSON schema.
func GetTimingsForSendData(captures []models.IRCapture, timingsUS []uint) ([]uint, error) {
	if len(captures) > 0 {
		timings, err := InferRepresentativeTimings(captures)
		if err == nil && len(timings) > 0 {
			return timings, nil
		}
	}

	if len(timingsUS) > 0 {
		return timingsUS, nil
	}

	return nil, errors.New("signal has no timings")
}

func InferRepresentativeTimings(captures []models.IRCapture) ([]uint, error) {
	validCaptures := []models.IRCapture{}

	for _, capture := range captures {
		if capture.Valid && len(capture.TimingsUS) > 0 {
			validCaptures = append(validCaptures, capture)
		}
	}

	if len(validCaptures) == 0 {
		return nil, errors.New("no valid captures")
	}

	sort.Slice(validCaptures, func(i, j int) bool {
		return validCaptures[i].Count < validCaptures[j].Count
	})

	bestGroup := []models.IRCapture{}

	for i, base := range validCaptures {
		group := []models.IRCapture{base}

		for j := i + 1; j < len(validCaptures); j++ {
			diff := validCaptures[j].Count - base.Count
			if diff < 0 {
				diff = -diff
			}

			if diff <= countTolerance {
				group = append(group, validCaptures[j])
			}
		}

		if len(group) > len(bestGroup) {
			bestGroup = group
			continue
		}

		if len(group) == len(bestGroup) && averageCount(group) > averageCount(bestGroup) {
			bestGroup = group
		}
	}

	if len(bestGroup) == 0 {
		return nil, errors.New("no matching capture group")
	}

	baseCount := minCaptureCount(bestGroup)
	representative := make([]uint, baseCount)

	for i := 0; i < baseCount; i++ {
		values := make([]uint, len(bestGroup))

		for j, capture := range bestGroup {
			values[j] = capture.TimingsUS[i]
		}

		representative[i] = medianUint(values)
	}

	return representative, nil
}

func BuildIRSendRawCommand(timings []uint) string {
	command := "IR_SEND_RAW:"

	for i, timing := range timings {
		if i > 0 {
			command += ","
		}

		command += strconv.FormatUint(uint64(timing), 10)
	}

	return command
}

func ValidateCapturedTimings(timings []uint) bool {
	if ValidateRawTimings(timings, len(timings)) != nil {
		return false
	}
	if len(timings) < 10 {
		return false
	}

	var total uint64
	for _, timing := range timings {
		total += uint64(timing)
	}

	if total < 10000 {
		return false
	}

	if timings[0] < 200 {
		return false
	}

	return true
}

func medianUint(values []uint) uint {
	sorted := append([]uint(nil), values...)

	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i] < sorted[j]
	})

	n := len(sorted)
	if n == 0 {
		return 0
	}

	if n%2 == 1 {
		return sorted[n/2]
	}

	return (sorted[n/2-1] + sorted[n/2]) / 2
}

func minCaptureCount(captures []models.IRCapture) int {
	if len(captures) == 0 {
		return 0
	}

	min := len(captures[0].TimingsUS)

	for _, capture := range captures {
		if len(capture.TimingsUS) < min {
			min = len(capture.TimingsUS)
		}
	}

	return min
}

func averageCount(captures []models.IRCapture) int {
	if len(captures) == 0 {
		return 0
	}

	total := 0

	for _, capture := range captures {
		total += capture.Count
	}

	return total / len(captures)
}

func CountValidCaptures(captures []models.IRCapture) int {
	count := 0

	for _, capture := range captures {
		if capture.Valid {
			count++
		}
	}

	return count
}

func BuildCountDistribution(captures []models.IRCapture) map[int]int {
	distribution := map[int]int{}

	for _, capture := range captures {
		if !capture.Valid {
			continue
		}

		distribution[capture.Count]++
	}

	return distribution
}
